package provider

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/node-cli/opts"
	"github.com/virtual-kubelet/node-cli/provider"
	"github.com/virtual-kubelet/tensile-kube/pkg/common"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubeinformers "k8s.io/client-go/informers"
	v12 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
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

// GetNameSpaceList returns the namespace cache
func (v *VirtualK8S) GetNameSpaceList() v1.NamespaceLister {
	return v.clientCache.nsLister
}

func (v *VirtualK8S) buildNodeInformer(nodeInformer v12.NodeInformer) {
	nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				nodeCopy := v.providerNode.DeepCopy()
				toAdd := common.ConvertResource(obj.(*corev1.Node).Status.Capacity)
				if err := v.providerNode.AddResource(toAdd); err != nil {
					return
				}
				if !reflect.DeepEqual(nodeCopy, v.providerNode) {
					v.updatedNode <- v.providerNode.Node
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				old, ok1 := oldObj.(*corev1.Node)
				new, ok2 := newObj.(*corev1.Node)
				if !ok1 || !ok2 {
					return
				}
				klog.V(5).Infof("Node %v updated", old.Name)
				v.updateVKCapacityFromNode(old, new)
			},
			DeleteFunc: func(obj interface{}) {
				nodeCopy := v.providerNode.DeepCopy()
				toRemove := common.ConvertResource(obj.(*corev1.Node).Status.Capacity)
				if err := v.providerNode.SubResource(toRemove); err != nil {
					return
				}
				if !reflect.DeepEqual(nodeCopy, v.providerNode) {
					v.updatedNode <- v.providerNode.Node
				}
			},
		},
	)
}

func (v *VirtualK8S) buildPodInformer(podInformer v12.PodInformer) {
	podInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					return
				}
				util.TrimObjectMeta(&pod.ObjectMeta)
				if !util.IsVirtualPod(pod) {
					if v.providerNode.Node == nil {
						return
					}
					// Pod created only by lower cluster
					// we should change the node resource
					if len(pod.Spec.NodeName) != 0 {
						podResource := util.GetRequestFromPod(pod)
						podResource.Pods = resource.MustParse("1")
						v.providerNode.SubResource(podResource)
						klog.Infof("Lower cluster add pod %s, resource: %v, node: %v",
							pod.Name, podResource, v.providerNode.Status.Capacity)
						if v.providerNode.Node == nil {
							return
						}
						v.updatedNode <- v.providerNode.Node
					}
					return
				}
				v.updatedPod <- pod
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				old, ok1 := oldObj.(*corev1.Pod)
				new, ok2 := newObj.(*corev1.Pod)
				if !ok1 || !ok2 {
					return
				}
				if !util.IsVirtualPod(new) {
					// Pod created only by lower cluster
					// we should change the node resource
					if v.providerNode.Node == nil {
						return
					}
					v.updateVKCapacityFromPod(old, new)
				}
				if !reflect.DeepEqual(old.Status, new.Status) {
					util.TrimObjectMeta(&new.ObjectMeta)
					v.updatedPod <- new
				}
			},
		},
	)
}

func (v *VirtualK8S) updateVKCapacityFromNode(old, new *corev1.Node) {
	oldStatus, newStatus := compareNodeStatusReady(old, new)
	toRemove := common.ConvertResource(new.Status.Capacity)
	toAdd := common.ConvertResource(new.Status.Capacity)
	nodeCopy := v.providerNode.DeepCopy()
	if old.Spec.Unschedulable && !new.Spec.Unschedulable || newStatus && !oldStatus {
		v.providerNode.AddResource(toAdd)
	}
	if !old.Spec.Unschedulable && new.Spec.Unschedulable || oldStatus && !newStatus {
		v.providerNode.SubResource(toRemove)
	}
	if !reflect.DeepEqual(old.Status.Allocatable, new.Status.Allocatable) ||
		!reflect.DeepEqual(old.Status.Capacity, new.Status.Capacity) {
		klog.Infof("Start to update node resource, old: %v, new %v", old.Status.Capacity,
			new.Status.Capacity)
		v.providerNode.SubResource(toRemove)
		v.providerNode.AddResource(toAdd)
		klog.Infof("Current node resource, resource: %v, allocatable %v", v.providerNode.Status.Capacity,
			v.providerNode.Status.Allocatable)
	}
	if v.providerNode.Node == nil {
		return
	}
	if !reflect.DeepEqual(nodeCopy, v.providerNode) {
		v.updatedNode <- v.providerNode.Node
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
	if old.DeletionTimestamp == nil && new.DeletionTimestamp != nil && !podStopped(new) ||
		old.Status.Phase == corev1.PodRunning && podStopped(new) {
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
	v.updatedNode <- v.providerNode.Node
	return
}
