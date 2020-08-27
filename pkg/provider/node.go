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

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/virtual-kubelet/tensile-kube/pkg/common"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

// ConfigureNode enables a provider to configure the node object that
// will be used for Kubernetes.
func (v *VirtualK8S) ConfigureNode(ctx context.Context, node *corev1.Node) {
	nodes, err := v.clientCache.nodeLister.List(labels.Everything())
	if err != nil {
		return
	}

	nodeResource := common.NewResource()

	for _, n := range nodes {
		if n.Spec.Unschedulable {
			continue
		}
		if !checkNodeStatusReady(n) {
			klog.Infof("Node %v not ready", node.Name)
			continue
		}
		nc := common.ConvertResource(n.Status.Capacity)
		nodeResource.Add(nc)
	}
	podResource := v.getResourceFromPods()
	nodeResource.Sub(podResource)
	nodeResource.SetCapacityToNode(node)
	node.Status.NodeInfo.KubeletVersion = v.version
	node.Status.NodeInfo.OperatingSystem = "linux"
	node.Status.NodeInfo.Architecture = "amd64"
	node.ObjectMeta.Labels[corev1.LabelArchStable] = "amd64"
	node.ObjectMeta.Labels[corev1.LabelOSStable] = "linux"
	node.ObjectMeta.Labels[util.LabelOSBeta] = "linux"
	node.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: os.Getenv("VKUBELET_POD_IP")}}
	node.Status.Conditions = nodeConditions()
	node.Status.DaemonEndpoints = v.nodeDaemonEndpoints()
	v.providerNode.Node = node
	v.configured = true
	return
}

// Ping tries to connect to client cluster
// implement node.NodeProvider
func (v *VirtualK8S) Ping(ctx context.Context) error {
	// If node or master ping fail, we should it as a failed ping
	_, err := v.master.Discovery().ServerVersion()
	if err != nil {
		klog.Error("Failed ping")
		return fmt.Errorf("could not list master apiserver statuses: %v", err)
	}
	_, err = v.client.Discovery().ServerVersion()
	if err != nil {
		klog.Error("Failed ping")
		return fmt.Errorf("could not list client apiserver statuses: %v", err)
	}
	return nil
}

// NotifyNodeStatus is used to asynchronously monitor the node.
// The passed in callback should be called any time there is a change to the
// node's status.
// This will generally trigger a call to the Kubernetes API server to update
// the status.
//
// NotifyNodeStatus should not block callers.
func (v *VirtualK8S) NotifyNodeStatus(ctx context.Context, f func(*corev1.Node)) {
	klog.Info("Called NotifyNodeStatus")
	go func() {
		for {
			select {
			case node := <-v.updatedNode:
				klog.Infof("Enqueue updated node %v", node.Name)
				f(node)
			case <-v.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// nodeDaemonEndpoints returns NodeDaemonEndpoints for the node status
// within Kubernetes.
func (v *VirtualK8S) nodeDaemonEndpoints() corev1.NodeDaemonEndpoints {
	return corev1.NodeDaemonEndpoints{
		KubeletEndpoint: corev1.DaemonEndpoint{
			Port: v.daemonPort,
		},
	}
}

// getResourceFromPods summary the resource already used by pods.
func (v *VirtualK8S) getResourceFromPods() *common.Resource {
	podResource := common.NewResource()
	pods, err := v.clientCache.podLister.List(labels.Everything())
	if err != nil {
		return podResource
	}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodPending && pod.Spec.NodeName != "" ||
			pod.Status.Phase == corev1.PodRunning {
			nodeName := pod.Spec.NodeName
			node, err := v.clientCache.nodeLister.Get(nodeName)
			if err != nil {
				klog.Infof("get node %v failed err: %v", nodeName, err)
				continue
			}
			if node.Spec.Unschedulable || !checkNodeStatusReady(node) {
				continue
			}
			res := util.GetRequestFromPod(pod)
			res.Pods = resource.MustParse("1")
			podResource.Add(res)
		}
	}
	return podResource
}

// getResourceFromPodsByNodeName summary the resource already used by pods according to nodeName
func (v *VirtualK8S) getResourceFromPodsByNodeName(nodeName string) *common.Resource {
	podResource := common.NewResource()
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName)
	pods, err := v.client.CoreV1().Pods(corev1.NamespaceAll).List(metav1.ListOptions{
		FieldSelector: fieldSelector.String(),
	})
	if err != nil {
		return podResource
	}
	for _, pod := range pods.Items {
		if util.IsVirtualPod(&pod) {
			continue
		}
		if pod.Status.Phase == corev1.PodPending ||
			pod.Status.Phase == corev1.PodRunning {
			res := util.GetRequestFromPod(&pod)
			res.Pods = resource.MustParse("1")
			podResource.Add(res)
		}
	}
	return podResource
}

// nodeConditions creates a slice of node conditions representing a
// kubelet in perfect health. These four conditions are the ones which virtual-kubelet
// sets as Unknown when a Ping fails.
func nodeConditions() []corev1.NodeCondition {
	return []corev1.NodeCondition{
		{
			Type:               "Ready",
			Status:             corev1.ConditionTrue,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletReady",
			Message:            "kubelet is posting ready status",
		},
		{
			Type:               "MemoryPressure",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               "DiskPressure",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               "PIDPressure",
			Status:             corev1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientPID",
			Message:            "kubelet has sufficient PID available",
		},
	}
}
