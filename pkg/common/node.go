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

package common

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

// ProviderNode defines the virtual kubelet node of tensile-kube
type ProviderNode struct {
	sync.Mutex
	*corev1.Node
}

// AddResource add resource to the node
func (n *ProviderNode) AddResource(resource *Resource) error {
	if n.Node == nil {
		return fmt.Errorf("ProviderNode node has not init")
	}
	n.Lock()
	defer n.Unlock()
	vkResource := ConvertResource(n.Status.Capacity)

	vkResource.Add(resource)
	vkResource.SetCapacityToNode(n.Node)
	return nil
}

// SubResource sub resource from the node
func (n *ProviderNode) SubResource(resource *Resource) error {
	if n.Node == nil {
		return fmt.Errorf("ProviderNode node has not init")
	}
	n.Lock()
	defer n.Unlock()
	vkResource := ConvertResource(n.Status.Capacity)

	vkResource.Sub(resource)
	vkResource.SetCapacityToNode(n.Node)
	return nil
}
