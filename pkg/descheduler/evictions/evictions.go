/*
 * Copyright ©2020. The virtual-kubelet authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package evictions

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mergetypes "k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	eutils "sigs.k8s.io/descheduler/pkg/descheduler/evictions/utils"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

const (
	unschedulableNode = "unschedulable-node"
)

// nodePodEvictedCount keeps count of pods evicted on node
type nodePodEvictedCount map[*v1.Node]int

// PodEvictor is used for evicting pods
type PodEvictor struct {
	client             clientset.Interface
	policyGroupVersion string
	dryRun             bool
	maxPodsToEvict     int
	nodepodCount       nodePodEvictedCount
	freezeDuration     time.Duration
	record             record.EventRecorder
	base               evictions.PodEvictor
	nodeNum            int
	*util.UnschedulableCache
	CheckUnschedulablePods bool
	sync.RWMutex
}

// NewPodEvictor init a new evictor
func NewPodEvictor(
	client clientset.Interface,
	policyGroupVersion string,
	maxPodsToEvict int,
	nodes []*v1.Node, unschedulableCache *util.UnschedulableCache) *PodEvictor {
	var nodePodCount = make(nodePodEvictedCount)
	for _, node := range nodes {
		// Initialize podsEvicted till now with 0.
		nodePodCount[node] = 0
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.V(3).Infof)
	eventBroadcaster.StartRecordingToSink(&clientcorev1.EventSinkImpl{Interface: client.CoreV1().Events(v1.NamespaceAll)})
	r := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "sigs.k8s.io.descheduler"})

	virtualCount := 0
	for node := range nodePodCount {
		if util.IsVirtualNode(node) && !node.Spec.Unschedulable {
			virtualCount++
		}
	}

	return &PodEvictor{
		client:             client,
		policyGroupVersion: policyGroupVersion,
		dryRun:             false,
		maxPodsToEvict:     maxPodsToEvict,
		nodepodCount:       nodePodCount,
		nodeNum:            virtualCount,
		freezeDuration:     5 * time.Minute,
		record:             r,
		UnschedulableCache: unschedulableCache,
	}
}

// NodeEvicted gives a number of pods evicted for node
func (pe *PodEvictor) NodeEvicted(node *v1.Node) int {
	return pe.nodepodCount[node]
}

// TotalEvicted gives a number of pods evicted through all nodes
func (pe *PodEvictor) TotalEvicted() int {
	var total int
	for _, count := range pe.nodepodCount {
		total += count
	}
	return total
}

// EvictPod returns non-nil error only when evicting a pod on a node is not
// possible (due to maxPodsToEvict constraint). Success is true when the pod
// is evicted on the server side.
func (pe *PodEvictor) EvictPod(ctx context.Context, pod *v1.Pod, node *v1.Node) (bool, error) {
	pe.RLock()
	if pe.maxPodsToEvict > 0 && pe.nodepodCount[node]+1 > pe.maxPodsToEvict {
		pe.RUnlock()
		return false, fmt.Errorf("Maximum number %v of evicted pods per %q node reached", pe.maxPodsToEvict, node.Name)
	}
	pe.RUnlock()

	nodeName := pod.Spec.NodeName
	podCopy := pod.DeepCopy()
	podCopy.UID = ""
	podCopy.ResourceVersion = ""
	podCopy.Spec.NodeName = ""
	if podCopy.Labels == nil {
		podCopy.Labels = map[string]string{}
	}
	podCopy.Labels[util.CreatedbyDescheduler] = "true"
	podCopy.Status = v1.PodStatus{}

	ownerID := pod.Name
	ownerCount := len(pod.OwnerReferences)
	if ownerCount != 0 {
		ownerID = string(pod.OwnerReferences[ownerCount-1].UID)
	}
	pe.Add(nodeName, ownerID)
	ti := pe.GetFreezeTime(nodeName, ownerID)
	klog.V(4).Info(ti)
	affinity, _ := util.ReplacePodNodeNameNodeAffinity(pod.Spec.Affinity,
		ownerID, pe.freezeDuration, pe.isNodeFreeze, nodeName)

	podCopy.Spec.Affinity = affinity
	klog.Infof("New pod affinity %+v", podCopy.Spec.Affinity)
	propagationPolicy := metav1.DeletePropagationBackground
	deleteOptions := &metav1.DeleteOptions{
		GracePeriodSeconds: new(int64),
		PropagationPolicy:  &propagationPolicy,
	}
	copy := pod.DeepCopy()
	addUnschedulablenode(copy)
	patch, err := util.CreateMergePatch(pod, copy)
	if err != nil {
		return false, err
	}
	_, err = pe.client.CoreV1().Pods(pod.Namespace).Patch(ctx, copy.Name,
		mergetypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Error evicting pod: %#v in namespace %#v (%#v)", pod.Name, pod.Namespace, err)
		return false, err
	}

	err = pe.client.CoreV1().Pods(podCopy.Namespace).Delete(ctx, podCopy.Name, *deleteOptions)
	if err != nil && !apierrors.IsNotFound(err) {
		// err is used only for logging purposes
		klog.Errorf("Error evicting pod: %#v in namespace %#v (%#v)", pod.Name, pod.Namespace, err)
		return false, err

	}
	addDescheduleCount(podCopy)

	_, err = pe.client.CoreV1().Pods(podCopy.Namespace).Create(ctx, podCopy, metav1.CreateOptions{})
	klog.V(4).Infof("New pod %+v", podCopy)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		// err is used only for logging purposes
		klog.Errorf("Error re-create pod: %#v in namespace %#v (%#v)", pod.Name, pod.Namespace, err)
		return false, nil
	}
	pe.record.Event(pod, v1.EventTypeNormal, "Rescheduled", "pod re-create by sigs.k8s.io/descheduler")
	klog.Infof("Re-create pod: %#v in namespace %#v success", pod.Name, pod.Namespace)
	pe.Lock()
	pe.nodepodCount[node]++
	pe.Unlock()
	if pe.dryRun {
		klog.V(1).Infof("Evicted pod in dry run mode: %#v in namespace %#v", pod.Name, pod.Namespace)
	} else {
		klog.V(1).Infof("Evicted pod: %#v in namespace %#v", pod.Name, pod.Namespace)
	}
	return true, nil
}

// replacePodNodeNameNodeAffinity replaces the RequiredDuringSchedulingIgnoredDuringExecution
// NodeAffinity of the given affinity with a new NodeAffinity that selects the given nodeName.
// Note that this function assumes that no NodeAffinity conflicts with the selected nodeName.
func (pe *PodEvictor) replacePodNodeNameNodeAffinity(affinity *v1.Affinity, nodeName, ownerID string) (*v1.Affinity,
	int) {
	nodeSelReq := v1.NodeSelectorRequirement{
		Key:      "kubernetes.io/hostname",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   []string{nodeName},
	}

	nodeSelector := &v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{nodeSelReq},
			},
		},
	}

	count := 1
	if affinity == nil {
		return &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
			},
		}, count
	}

	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
		}
		return affinity, count
	}

	nodeAffinity := affinity.NodeAffinity
	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return affinity, count
	}

	terms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if terms == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []v1.NodeSelectorTerm{
			{
				MatchFields: []v1.NodeSelectorRequirement{nodeSelReq},
			},
		}
		return affinity, count
	}

	newTerms := make([]v1.NodeSelectorTerm, 0)
	for _, term := range terms {
		if term.MatchExpressions == nil {
			continue
		}
		mes, noScheduleCount := pe.getNodeSelectorRequirement(term, nodeName, ownerID, nodeSelReq)
		count = noScheduleCount
		term.MatchExpressions = mes
		newTerms = append(newTerms, term)
	}

	// Replace node selector with the new one.
	nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = newTerms
	affinity.NodeAffinity = nodeAffinity
	return affinity, count
}

func (pe *PodEvictor) getNodeSelectorRequirement(term v1.NodeSelectorTerm,
	nodeName, ownerID string, nodeSelReq v1.NodeSelectorRequirement) ([]v1.NodeSelectorRequirement, int) {
	mes := make([]v1.NodeSelectorRequirement, 0)
	count := 0
	for _, me := range term.MatchExpressions {
		if me.Key != nodeSelReq.Key || me.Operator != nodeSelReq.Operator {
			mes = append(mes, me)
			continue
		}
		values := make([]string, 0)
		for _, v := range me.Values {
			klog.V(4).Infof("current term value %v", v)
			if v == nodeName {
				continue
			}
			if pe.isNodeFreeze(v, ownerID, pe.freezeDuration) {
				values = append(values, v)
			}
		}
		if nodeName != "" {
			me.Values = append(values, nodeSelReq.Values...)
		}
		count = len(values)
		mes = append(mes, me)
		continue
	}
	return mes, count
}

func (pe *PodEvictor) isNodeFreeze(node, ownerID string,
	freezeDuration time.Duration) bool {
	freezeTime := pe.GetFreezeTime(node, ownerID)
	klog.V(4).Infof("OwnerID %v, node %v, time %v", ownerID, node, freezeTime)
	if freezeTime == nil {
		return false
	}
	if freezeTime.Add(freezeDuration).After(time.Now()) {
		return true
	}
	return false
}

func evictPod(ctx context.Context, client clientset.Interface, pod *v1.Pod, policyGroupVersion string, dryRun bool) error {
	if dryRun {
		return nil
	}

	var gracePeriodSeconds int64 = 0
	propagationPolicy := metav1.DeletePropagationForeground
	deleteOptions := &metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
		PropagationPolicy:  &propagationPolicy,
	}
	// GracePeriodSeconds ?
	eviction := &policy.Eviction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: policyGroupVersion,
			Kind:       eutils.EvictionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: deleteOptions,
	}
	err := client.PolicyV1beta1().Evictions(eviction.Namespace).Evict(ctx, eviction)

	if err == nil {
		return nil
	}
	if apierrors.IsTooManyRequests(err) {
		return fmt.Errorf("error when evicting pod (ignoring) %q: %v", pod.Name, err)
	}
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("pod not found when evicting %q: %v", pod.Name, err)
	}
	return err
}

func addDescheduleCount(pod *v1.Pod) {
	if pod == nil {
		return
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{util.DescheduleCount: strconv.Itoa(1)}
		return
	}
	countStr, ok := pod.Annotations[util.DescheduleCount]
	if !ok {
		pod.Annotations[util.DescheduleCount] = strconv.Itoa(1)
		return
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		count = 0
	}
	pod.Annotations = map[string]string{util.DescheduleCount: strconv.Itoa(count + 1)}
}

func addUnschedulablenode(pod *v1.Pod) {
	if pod == nil {
		return
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if len(pod.Spec.NodeName) > 0 {
		pod.Annotations[unschedulableNode] = pod.Spec.NodeName
	}
	pod.ResourceVersion = "0"
}
