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
	"reflect"
	"strings"

	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/node-cli/opts"
	"github.com/virtual-kubelet/node-cli/provider"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubeinformers "k8s.io/client-go/informers"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/virtual-kubelet/tensile-kube/pkg/common"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

// ClientConfig defines the configuration of a lower cluster
type ClientConfig struct {
	// allowed qps of the kube client
	KubeClientQPS int
	// allowed burst of the kube client
	KubeClientBurst int
	// config path of the kube client
	ClientKubeConfigPath string
}

// clientCache wraps the lister of client cluster
type clientCache struct {
	podLister    v1.PodLister
	nsLister     v1.NamespaceLister
	cmLister     v1.ConfigMapLister
	secretLister v1.SecretLister
	nodeLister   v1.NodeLister
}

// VirtualK8S is the key struct to implement the tensile kubernetes
type VirtualK8S struct {
	master               kubernetes.Interface
	client               kubernetes.Interface
	metricClient         versioned.Interface
	config               *rest.Config
	nodeName             string
	version              string
	daemonPort           int32
	ignoreLabels         []string
	clientCache          clientCache
	rm                   *manager.ResourceManager
	updatedNode          chan *corev1.Node
	updatedPod           chan *corev1.Pod
	enableServiceAccount bool
	stopCh               <-chan struct{}
	providerNode         *common.ProviderNode
	configured           bool
}

// NewVirtualK8S reads a kubeconfig file and sets up a client to interact
// with lower cluster
func NewVirtualK8S(cfg provider.InitConfig, cc *ClientConfig,
	ignoreLabelsStr string, enableServiceAccount bool, opts *opts.Opts) (*VirtualK8S, error) {
	ignoreLabels := strings.Split(ignoreLabelsStr, ",")
	if len(cc.ClientKubeConfigPath) == 0 {
		panic("client kubeconfig path can not be empty")
	}
	// client config
	var clientConfig *rest.Config
	client, err := util.NewClient(cc.ClientKubeConfigPath, func(config *rest.Config) {
		config.QPS = float32(cc.KubeClientQPS)
		config.Burst = cc.KubeClientBurst
		// Set config for clientConfig
		clientConfig = config
	})
	if err != nil {
		return nil, fmt.Errorf("could not build clientset for cluster: %v", err)
	}

	// master config, maybe a real node or a pod
	master, err := util.NewClient(cfg.ConfigPath, func(config *rest.Config) {
		config.QPS = float32(opts.KubeAPIQPS)
		config.Burst = int(opts.KubeAPIBurst)
	})
	if err != nil {
		return nil, fmt.Errorf("could not build clientset for cluster: %v", err)
	}

	metricClient, err := util.NewMetricClient(cc.ClientKubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("could not build clientset for cluster: %v", err)
	}

	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("could not get target cluster server version: %v", err)
	}

	informer := kubeinformers.NewSharedInformerFactory(client, 0)
	podInformer := informer.Core().V1().Pods()
	nsInformer := informer.Core().V1().Namespaces()
	nodeInformer := informer.Core().V1().Nodes()
	cmInformer := informer.Core().V1().ConfigMaps()
	secretInformer := informer.Core().V1().Secrets()

	ctx := context.TODO()

	virtualK8S := &VirtualK8S{
		master:               master,
		client:               client,
		metricClient:         metricClient,
		nodeName:             cfg.NodeName,
		ignoreLabels:         ignoreLabels,
		version:              serverVersion.GitVersion,
		daemonPort:           cfg.DaemonPort,
		config:               clientConfig,
		enableServiceAccount: enableServiceAccount,
		clientCache: clientCache{
			podLister:    podInformer.Lister(),
			nsLister:     nsInformer.Lister(),
			cmLister:     cmInformer.Lister(),
			secretLister: secretInformer.Lister(),
			nodeLister:   nodeInformer.Lister(),
		},
		rm:           cfg.ResourceManager,
		updatedNode:  make(chan *corev1.Node, 100),
		updatedPod:   make(chan *corev1.Pod, 100000),
		providerNode: &common.ProviderNode{},
		stopCh:       ctx.Done(),
	}

	virtualK8S.buildNodeInformer(nodeInformer)
	virtualK8S.buildPodInformer(podInformer)

	informer.Start(ctx.Done())
	klog.Info("Informer started")
	if !cache.WaitForCacheSync(ctx.Done(), podInformer.Informer().HasSynced,
		nsInformer.Informer().HasSynced, nodeInformer.Informer().HasSynced, cmInformer.Informer().HasSynced,
		secretInformer.Informer().HasSynced) {
		klog.Fatal("WaitForCacheSync failed")
	}
	return virtualK8S, nil
}

// GetClient return the kube client of lower cluster
func (v *VirtualK8S) GetClient() kubernetes.Interface {
	return v.client
}

// GetMaster return the kube client of upper cluster
func (v *VirtualK8S) GetMaster() kubernetes.Interface {
	return v.master
}

// GetNameSpaceLister returns the namespace cache
func (v *VirtualK8S) GetNameSpaceLister() v1.NamespaceLister {
	return v.clientCache.nsLister
}

func (v *VirtualK8S) buildNodeInformer(nodeInformer informerv1.NodeInformer) {
	nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if !v.configured {
					return
				}
				nodeCopy := v.providerNode.DeepCopy()
				addNode := obj.(*corev1.Node).DeepCopy()
				toAdd := common.ConvertResource(addNode.Status.Capacity)
				if err := v.providerNode.AddResource(toAdd); err != nil {
					return
				}
				// resource we did not add when ConfigureNode should sub
				v.providerNode.SubResource(v.getResourceFromPodsByNodeName(addNode.Name))
				copy := v.providerNode.DeepCopy()
				if !reflect.DeepEqual(nodeCopy, copy) {
					v.updatedNode <- copy
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if !v.configured {
					return
				}
				old, ok1 := oldObj.(*corev1.Node)
				new, ok2 := newObj.(*corev1.Node)
				oldCopy := old.DeepCopy()
				newCopy := new.DeepCopy()
				if !ok1 || !ok2 {
					return
				}
				klog.V(5).Infof("Node %v updated", old.Name)
				v.updateVKCapacityFromNode(oldCopy, newCopy)
			},
			DeleteFunc: func(obj interface{}) {
				if !v.configured {
					return
				}
				nodeCopy := v.providerNode.DeepCopy()
				deleteNode := obj.(*corev1.Node).DeepCopy()
				toRemove := common.ConvertResource(deleteNode.Status.Capacity)
				if err := v.providerNode.SubResource(toRemove); err != nil {
					return
				}
				// resource we did not add when ConfigureNode should add
				v.providerNode.AddResource(v.getResourceFromPodsByNodeName(deleteNode.Name))
				copy := v.providerNode.DeepCopy()
				if !reflect.DeepEqual(nodeCopy, copy) {
					v.updatedNode <- copy
				}
			},
		},
	)
}

func (v *VirtualK8S) buildPodInformer(podInformer informerv1.PodInformer) {
	podInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    v.addPod,
			UpdateFunc: v.updatePod,
			DeleteFunc: v.deletePod,
		},
	)
}

func (v *VirtualK8S) addPod(obj interface{}) {
	if !v.configured {
		return
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	podCopy := pod.DeepCopy()
	util.TrimObjectMeta(&podCopy.ObjectMeta)
	if !util.IsVirtualPod(podCopy) {
		if v.providerNode.Node == nil {
			return
		}
		// Pod created only by lower cluster
		// we should change the node resource
		if len(podCopy.Spec.NodeName) != 0 {
			podResource := util.GetRequestFromPod(podCopy)
			podResource.Pods = resource.MustParse("1")
			v.providerNode.SubResource(podResource)
			klog.Infof("Lower cluster add pod %s, resource: %v, node: %v",
				podCopy.Name, podResource, v.providerNode.Status.Capacity)
			if v.providerNode.Node == nil {
				return
			}
			copy := v.providerNode.DeepCopy()
			v.updatedNode <- copy
		}
		return
	}
	v.updatedPod <- podCopy
}

func (v *VirtualK8S) updatePod(oldObj, newObj interface{}) {
	if !v.configured {
		return
	}
	old, ok1 := oldObj.(*corev1.Pod)
	new, ok2 := newObj.(*corev1.Pod)
	oldCopy := old.DeepCopy()
	newCopy := new.DeepCopy()
	if !ok1 || !ok2 {
		return
	}
	if !util.IsVirtualPod(newCopy) {
		// Pod created only by lower cluster
		// we should change the node resource
		if v.providerNode.Node == nil {
			return
		}
		v.updateVKCapacityFromPod(oldCopy, newCopy)
		return
	}
	if !reflect.DeepEqual(oldCopy.Status, newCopy.Status) || newCopy.DeletionTimestamp != nil {
		util.TrimObjectMeta(&newCopy.ObjectMeta)
		v.updatedPod <- newCopy
	}
}

func (v *VirtualK8S) deletePod(obj interface{}) {
	if !v.configured {
		return
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	podCopy := pod.DeepCopy()
	util.TrimObjectMeta(&podCopy.ObjectMeta)
	if !util.IsVirtualPod(podCopy) {
		if v.providerNode.Node == nil {
			return
		}
		if podStopped(pod) {
			return
		}
		// Pod created only by lower cluster
		// we should change the node resource
		if len(podCopy.Spec.NodeName) != 0 {
			podResource := util.GetRequestFromPod(podCopy)
			podResource.Pods = resource.MustParse("1")
			v.providerNode.AddResource(podResource)
			klog.Infof("Lower cluster add pod %s, resource: %v, node: %v",
				podCopy.Name, podResource, v.providerNode.Status.Capacity)
			if v.providerNode.Node == nil {
				return
			}
			copy := v.providerNode.DeepCopy()
			v.updatedNode <- copy
		}
		return
	}
	v.updatedPod <- podCopy
}

func (v *VirtualK8S) updateVKCapacityFromNode(old, new *corev1.Node) {
	oldStatus, newStatus := compareNodeStatusReady(old, new)
	if !oldStatus && !newStatus {
		return
	}
	toRemove := common.ConvertResource(old.Status.Capacity)
	toAdd := common.ConvertResource(new.Status.Capacity)
	nodeCopy := v.providerNode.DeepCopy()
	if old.Spec.Unschedulable && !new.Spec.Unschedulable || newStatus && !oldStatus {
		v.providerNode.AddResource(toAdd)
		v.providerNode.SubResource(v.getResourceFromPodsByNodeName(old.Name))
	}
	if !old.Spec.Unschedulable && new.Spec.Unschedulable || oldStatus && !newStatus {
		v.providerNode.AddResource(v.getResourceFromPodsByNodeName(old.Name))
		v.providerNode.SubResource(toRemove)

	}
	if !reflect.DeepEqual(old.Status.Allocatable, new.Status.Allocatable) ||
		!reflect.DeepEqual(old.Status.Capacity, new.Status.Capacity) {
		klog.Infof("Start to update node resource, old: %v, new %v", old.Status.Capacity,
			new.Status.Capacity)
		v.providerNode.AddResource(toAdd)
		v.providerNode.SubResource(toRemove)
		klog.Infof("Current node resource, resource: %v, allocatable %v", v.providerNode.Status.Capacity,
			v.providerNode.Status.Allocatable)
	}
	if v.providerNode.Node == nil {
		return
	}
	copy := v.providerNode.DeepCopy()
	if !reflect.DeepEqual(nodeCopy, copy) {
		v.updatedNode <- copy
	}
}

func (v *VirtualK8S) updateVKCapacityFromPod(old, new *corev1.Pod) {
	newResource := util.GetRequestFromPod(new)
	oldResource := util.GetRequestFromPod(old)
	// create pod
	if old.Spec.NodeName == "" && new.Spec.NodeName != "" {
		newResource.Pods = resource.MustParse("1")
		v.providerNode.SubResource(newResource)
		klog.Infof("Lower cluster add pod %s, resource: %v, node: %v",
			new.Name, newResource, v.providerNode.Status.Capacity)
	}
	// delete pod
	if old.Status.Phase == corev1.PodRunning && podStopped(new) {
		klog.Infof("Lower cluster delete pod %s, resource: %v", new.Name, newResource)
		newResource.Pods = resource.MustParse("1")
		v.providerNode.AddResource(newResource)
	}
	// update pod
	if new.Status.Phase == corev1.PodRunning && !reflect.DeepEqual(old.Spec.Containers,
		new.Spec.Containers) {
		if oldResource.Equal(newResource) {
			return
		}
		v.providerNode.AddResource(oldResource)
		v.providerNode.SubResource(newResource)

		klog.Infof("Lower cluster update pod %s, oldResource: %v, newResource: %v",
			new.Name, oldResource, newResource)
	}
	if v.providerNode.Node == nil {
		return
	}
	copy := v.providerNode.DeepCopy()
	v.updatedNode <- copy
	return
}
