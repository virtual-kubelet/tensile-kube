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

package strategies

import (
	"context"
	"sync"

	v1 "k8s.io/api/core/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/descheduler/pkg/api"

	"github.com/virtual-kubelet/tensile-kube/pkg/descheduler/evictions"
	podutil "github.com/virtual-kubelet/tensile-kube/pkg/descheduler/pod"
)

// PodLifeTime evicts pods on nodes that were created more than strategy.Params.MaxPodLifeTimeSeconds seconds ago.
func PodLifeTime(ctx context.Context, client clientset.Interface, strategy api.DeschedulerStrategy, nodes []*v1.Node, evictLocalStoragePods bool, podEvictor *evictions.PodEvictor) {
	if strategy.Params.MaxPodLifeTimeSeconds == nil {
		klog.V(1).Infof("MaxPodLifeTimeSeconds not set")
		return
	}
	var wg sync.WaitGroup
	for _, node := range nodes {
		go func(node *v1.Node) {
			wg.Add(1)
			defer wg.Done()
			klog.V(1).Infof("Processing node: %#v", node.Name)
			pods := listOldPodsOnNode(client, node, *strategy.Params.MaxPodLifeTimeSeconds, evictLocalStoragePods)

			f := func(idx int) {
				success, err := podEvictor.EvictPod(ctx, pods[idx], node)
				if success {
					klog.V(1).Infof("Evicted pod: %#v because it was created more than %v seconds ago", pods[idx].Name, *strategy.Params.MaxPodLifeTimeSeconds)
				}

				if err != nil {
					klog.Errorf("Error evicting pod: (%#v)", err)
					return
				}
			}

			workqueue.ParallelizeUntil(context.TODO(), 64, len(pods), f)
		}(node)
	}
	wg.Wait()
	if podEvictor.CheckUnschedulablePods {
		klog.V(1).Info("Processing unschedulabe pods")
		pods := listOldPodsOnNode(client, &v1.Node{}, (*strategy.Params.MaxPodLifeTimeSeconds)*3,
			evictLocalStoragePods)
		f := func(idx int) {
			success, err := podEvictor.EvictPod(ctx, pods[idx], &v1.Node{})
			if success {
				klog.V(1).Infof("Evicted pod: %#v because it was created more than %v seconds ago", pods[idx].Name, *strategy.Params.MaxPodLifeTimeSeconds)
			}

			if err != nil {
				klog.Errorf("Error evicting pod: (%#v)", err)
				return
			}
		}

		workqueue.ParallelizeUntil(context.TODO(), 64, len(pods), f)
	}
}

func listOldPodsOnNode(client clientset.Interface, node *v1.Node, maxAge uint, evictLocalStoragePods bool) []*v1.Pod {
	pods, err := podutil.ListEvictablePodsOnNode(client, node, evictLocalStoragePods)
	if err != nil {
		return nil
	}

	var oldPods []*v1.Pod
	for _, pod := range pods {
		podAgeSeconds := uint(v1meta.Now().Sub(pod.GetCreationTimestamp().Local()).Seconds())
		if podAgeSeconds > maxAge {
			oldPods = append(oldPods, pod)
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == v1.PodScheduled && condition.Reason == "Unschedulable" {
				oldPods = append(oldPods, pod)
				break
			}
		}
	}

	return oldPods
}
