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

package pod

import (
	"context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	base "sigs.k8s.io/descheduler/pkg/descheduler/pod"
)

// IsEvictable checks if a pod is evictable or not.
func IsEvictable(pod *v1.Pod, evictLocalStoragePods bool) bool {
	if !base.IsEvictable(pod, evictLocalStoragePods) {
		return false
	}
	if pod.Status.Phase != v1.PodPending {
		return false
	}
	return true
}

// ListEvictablePodsOnNode returns the list of evictable pods on node.
func ListEvictablePodsOnNode(client clientset.Interface, node *v1.Node, evictLocalStoragePods bool) ([]*v1.Pod, error) {
	pods, err := ListPodsOnANode(client, node)
	if err != nil {
		return []*v1.Pod{}, err
	}
	evictablePods := make([]*v1.Pod, 0)
	for _, pod := range pods {
		if !IsEvictable(pod, evictLocalStoragePods) {
			continue
		} else {
			evictablePods = append(evictablePods, pod)
		}
	}
	return evictablePods, nil
}

// ListPodsOnANode lists pod on some node
func ListPodsOnANode(client clientset.Interface, node *v1.Node) ([]*v1.Pod, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + node.Name + ",status.phase=" + string(v1.PodPending))
	if err != nil {
		return []*v1.Pod{}, err
	}

	podList, err := client.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(),
		metav1.ListOptions{FieldSelector: fieldSelector.String()})
	if err != nil {
		return []*v1.Pod{}, err
	}

	pods := make([]*v1.Pod, 0)
	for i := range podList.Items {
		pods = append(pods, &podList.Items[i])
	}
	return pods, nil
}
