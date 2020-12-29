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

package descheduler

import (
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions/utils"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"

	"github.com/virtual-kubelet/tensile-kube/cmd/descheduler/app/options"
	"github.com/virtual-kubelet/tensile-kube/pkg/descheduler/evictions"
	"github.com/virtual-kubelet/tensile-kube/pkg/descheduler/strategies"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

// Run start a descheduler server
func Run(rs *options.DeschedulerServer) error {
	ctx := context.Background()
	rsclient, err := util.NewClient(rs.KubeconfigFile, func(c *rest.Config) {
		c.QPS = 100
		c.Burst = 200
	})
	if err != nil {
		return err
	}
	rs.Client = rsclient

	deschedulerPolicy, err := descheduler.LoadPolicyConfig(rs.PolicyConfigFile)
	if err != nil {
		return err
	}
	if deschedulerPolicy == nil {
		return fmt.Errorf("deschedulerPolicy is nil")
	}

	evictionPolicyGroupVersion, err := utils.SupportEviction(rs.Client)
	if err != nil || len(evictionPolicyGroupVersion) == 0 {
		return err
	}

	stopChannel := make(chan struct{})
	return RunDeschedulerStrategies(ctx, rs, deschedulerPolicy, evictionPolicyGroupVersion, stopChannel)
}

type strategyFunction func(ctx context.Context, client clientset.Interface, strategy api.DeschedulerStrategy, nodes []*v1.Node, evictLocalStoragePods bool, podEvictor *evictions.PodEvictor)

// RunDeschedulerStrategies runs the strategies
func RunDeschedulerStrategies(ctx context.Context, rs *options.DeschedulerServer, deschedulerPolicy *api.DeschedulerPolicy, evictionPolicyGroupVersion string, stopChannel chan struct{}) error {
	sharedInformerFactory := informers.NewSharedInformerFactory(rs.Client, 0)
	nodeInformer := sharedInformerFactory.Core().V1().Nodes()

	sharedInformerFactory.Start(stopChannel)
	sharedInformerFactory.WaitForCacheSync(stopChannel)

	strategyFuncs := map[string]strategyFunction{
		"PodLifeTime": strategies.PodLifeTime,
	}

	unschedulableCache := util.NewUnschedulableCache()
	count := 0
	wait.Until(func() {
		count++
		nodes, err := nodeutil.ReadyNodes(rs.Client, nodeInformer, rs.NodeSelector, stopChannel)
		if err != nil {
			klog.V(1).Infof("Unable to get ready nodes: %v", err)
			close(stopChannel)
			return
		}

		if len(nodes) <= 1 {
			klog.V(1).Infof("The cluster size is 0 or 1 meaning eviction causes service disruption or degradation. So aborting..")
			close(stopChannel)
			return
		}
		podEvictor := evictions.NewPodEvictor(
			rs.Client,
			evictionPolicyGroupVersion,
			rs.MaxNoOfPodsToEvictPerNode,
			nodes, unschedulableCache,
		)
		if count%10 == 0 {
			count = count % 10
			podEvictor.CheckUnschedulablePods = true
		}
		for name, f := range strategyFuncs {
			if strategy := deschedulerPolicy.Strategies[api.StrategyName(name)]; strategy.Enabled {
				f(ctx, rs.Client, strategy, nodes, rs.EvictLocalStoragePods, podEvictor)
			}
		}

		// If there was no interval specified, send a signal to the stopChannel to end the wait.Until loop after 1 iteration
		if rs.DeschedulingInterval.Seconds() == 0 {
			close(stopChannel)
		}
	}, rs.DeschedulingInterval, stopChannel)

	return nil
}
