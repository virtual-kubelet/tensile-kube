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

package multischeduler

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog"
	schedulerappconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	clusterconfig "github.com/virtual-kubelet/tensile-kube/pkg/scheduler/config"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

// Name is scheduler plugin name, will use in configuration file
const Name = "multi-scheduler"

// Configuration defines the lower cluster configuration
type Configuration struct {
	// clusterName
	Name string `json:"name"`
	// Master URL
	KubeMaster string `json:"kube_master,omitempty"`
	// KubeConfig of the cluster
	KubeConfig string `json:"kube_config,omitempty"`
}

// ClusterConfigurations defines the configurations of all the lower clusters
type ClusterConfigurations struct {
	// ClusterConfiguration is a key-value map to store configuration
	ClusterConfiguration map[string]Configuration `json:"cluster_configuration"`
}

// MultiSchedulingPlugin is plugin implemented scheduling framework
type MultiSchedulingPlugin struct {
	frameworkHandler framework.FrameworkHandle
	schedulers       map[string]*Scheduler
}

// Name returns the plugin name
func (m MultiSchedulingPlugin) Name() string {
	return Name
}

var _ framework.FilterPlugin = &MultiSchedulingPlugin{}

// Filter check if a pod can run on node
func (m MultiSchedulingPlugin) Filter(pc context.Context, state *framework.CycleState,
	pod *v1.Pod, nodeInfo *schedulernodeinfo.NodeInfo) *framework.Status {
	if len(pod.Spec.NodeName) == 0 {
		return framework.NewStatus(framework.Success, "")
	}
	snapshot := m.frameworkHandler.SnapshotSharedLister().NodeInfos()
	if snapshot == nil {
		return framework.NewStatus(framework.Success, "")
	}

	if nodeInfo == nil || nodeInfo.Node() == nil {
		return framework.NewStatus(framework.Unschedulable, "node not exit")
	}
	if !util.IsVirtualNode(nodeInfo.Node()) {
		klog.V(5).Infof("node %v is not virtual node", nodeInfo.Node().Name)
		return framework.NewStatus(framework.Success, "")
	}

	schedulerName := util.GetClusterID(nodeInfo.Node())
	if len(schedulerName) == 0 {
		klog.V(5).Infof("Can not found scheduler %v", schedulerName)
		return framework.NewStatus(framework.Success, "")
	}

	scheduler := m.schedulers[schedulerName]

	if scheduler == nil {
		klog.V(5).Infof("Can not found scheduler %v", schedulerName)
		return framework.NewStatus(framework.Success, "")
	}

	podCopy := pod.DeepCopy()

	cns := util.ConvertAnnotations(podCopy.Annotations)
	// remove selector
	if cns != nil {
		podCopy.Spec.NodeSelector = cns.NodeSelector
		podCopy.Spec.Affinity = cns.Affinity
		podCopy.Spec.Tolerations = cns.Tolerations
	} else {
		podCopy.Spec.NodeSelector = nil
		podCopy.Spec.Affinity = nil
		podCopy.Spec.Tolerations = nil
	}

	result, err := scheduler.Algorithm.Schedule(pc, scheduler.Profiles["default-scheduler"], state, podCopy)
	klog.V(5).Infof("%v Nodes, Node %s can be scheduled to run pod", result.FeasibleNodes, result.SuggestedHost)
	if err != nil {
		klog.Infof("Pod selector: %+v, affinity: %+v", pod.Spec.NodeSelector, pod.Spec.Affinity)
		return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("Can not found nodes: %s", err))
	}
	if result.FeasibleNodes == 0 || result.SuggestedHost == "" {
		return framework.NewStatus(framework.Unschedulable, "Can not found nodes")
	}
	return framework.NewStatus(framework.Success, "")
}

// New initializes a new plugin and returns it.
func New(configuration *runtime.Unknown, f framework.FrameworkHandle) (framework.Plugin, error) {
	var configs ClusterConfigurations
	if err := json.Unmarshal(configuration.Raw, &configs); err != nil {
		klog.Errorf("Failed to decode %+v: %v", configuration.Raw, err)
		return nil, fmt.Errorf("failed to decode configuration: %v", err)
	}

	ctx := context.TODO()
	schedulers := make(map[string]*Scheduler)
	for name, config := range configs.ClusterConfiguration {
		klog.V(4).Infof("cluster %s's config: master(%s), kube-config(%s)", name, config.KubeMaster, config.KubeConfig)
		// Init client and Informer
		c, err := clientcmd.BuildConfigFromFlags(config.KubeMaster, config.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to init rest.Config: %v", err)
		}
		c.QPS = 10
		c.Burst = 20

		componentConfig, err := clusterconfig.NewDefaultComponentConfig()
		if err != nil {
			klog.Fatal(err)
		}

		componentConfig.ClientConnection = componentbaseconfig.ClientConnectionConfiguration{
			Kubeconfig: config.KubeConfig,
		}
		componentConfig.DisablePreemption = true

		cfg := schedulerappconfig.Config{
			ComponentConfig: *componentConfig,
		}
		newConfig, err := clusterconfig.Config(cfg, config.KubeMaster)
		if err != nil {
			klog.Fatal(err)
		}

		scheduler, err := NewScheduler(ctx, *newConfig, ctx.Done())
		if err != nil {
			klog.Fatal(err)
		}
		go scheduler.Run(ctx)
		schedulers[name] = scheduler
	}

	return MultiSchedulingPlugin{
		frameworkHandler: f,
		schedulers:       schedulers,
	}, nil
}
