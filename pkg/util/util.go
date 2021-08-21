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

package util

import (
	"github.com/virtual-kubelet/tensile-kube/pkg/common"
	corev1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/kubernetes/pkg/api/v1/resource"
)

// GetRequestFromPod get resources required by pod
func GetRequestFromPod(pod *corev1.Pod) *common.Resource {
	if pod == nil {
		return nil
	}
	reqs, _ := resourcehelper.PodRequestsAndLimits(pod)
	capacity := common.ConvertResource(reqs)
	return capacity
}
