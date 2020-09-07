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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
)

// CustomResources is a key-value map for defining custom resources
type CustomResources map[corev1.ResourceName]resource.Quantity

// DeepCopy copy the custom resource
func (cr CustomResources) DeepCopy() CustomResources {
	crCopy := CustomResources{}
	for name, quota := range cr {
		crCopy[name] = quota
	}
	return crCopy
}

// Equal return if resources is equal
func (cr CustomResources) Equal(other CustomResources) bool {
	if len(cr) != len(other) {
		return false
	}
	for k, v := range cr {
		v1, ok := other[k]
		if !ok {
			return false
		}
		if !v1.Equal(v) {
			return false
		}
	}
	return true
}

// Resource defines the resources of a pod, it provides func `Add`, `Sub`
// to make computation flexible
type Resource struct {
	// CPU requirement
	CPU resource.Quantity
	// Memory requirement
	Memory resource.Quantity
	// Pods requirement
	Pods resource.Quantity
	// EphemeralStorage requirement
	EphemeralStorage resource.Quantity
	// Custom resource requirement
	Custom CustomResources
}

// NewResource returns A resource struct
func NewResource() *Resource {
	return &Resource{
		Custom: CustomResources{},
	}
}

// Equal is for two resources comparision
func (r *Resource) Equal(other *Resource) bool {
	return r.CPU.Equal(other.CPU) && r.Memory.Equal(other.Memory) && r.Pods.Equal(other.Pods) && r.
		EphemeralStorage.Equal(other.EphemeralStorage) && r.Custom.Equal(other.Custom)
}

// Add adds resource to the current one
func (r *Resource) Add(nc *Resource) {
	r.CPU.Add(nc.CPU)
	r.Memory.Add(nc.Memory)
	r.Pods.Add(nc.Pods)
	r.EphemeralStorage.Add(nc.EphemeralStorage)
	if len(nc.Custom) == 0 {
		return
	}
	for name, quota := range nc.Custom {
		if r.Custom == nil {
			r.Custom = CustomResources{}
		}
		old := r.Custom[name]
		old.Add(quota)
		r.Custom[name] = old
	}
}

// Sub subs resource from the current one
func (r *Resource) Sub(nc *Resource) {
	r.CPU.Sub(nc.CPU)
	r.Memory.Sub(nc.Memory)
	r.Pods.Sub(nc.Pods)
	r.EphemeralStorage.Sub(nc.EphemeralStorage)
	if len(nc.Custom) == 0 {
		return
	}
	for name, quota := range nc.Custom {
		if r.Custom == nil {
			r.Custom = CustomResources{}
		}
		old := r.Custom[name]
		old.Sub(quota)
		r.Custom[name] = old
	}
}

// SetCapacityToNode set the resource the virtual-kubelet node
func (r *Resource) SetCapacityToNode(node *corev1.Node) {
	var CPU, mem, Pods, empStorage resource.Quantity
	if r.CPU.Sign() >= 0 {
		CPU = r.CPU
	}
	if r.Memory.Sign() >= 0 {
		mem = r.Memory
	}
	if r.Pods.Sign() >= 0 {
		Pods = r.Pods
	}
	if r.EphemeralStorage.Sign() >= 0 {
		empStorage = r.EphemeralStorage
	}
	node.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:              CPU,
		corev1.ResourceMemory:           mem,
		corev1.ResourcePods:             Pods,
		corev1.ResourceEphemeralStorage: empStorage,
	}
	for name, quota := range r.Custom {
		node.Status.Capacity[name] = quota
	}

	node.Status.Allocatable = node.Status.Capacity.DeepCopy()
	klog.Infof("%v", node.Status.Capacity)
}

// ConvertResource converts ResourceList to Resource
func ConvertResource(resources corev1.ResourceList) *Resource {
	var cpu, mem, pods, empStorage resource.Quantity
	customResource := CustomResources{}
	for resourceName, quota := range resources {
		switch resourceName {
		case corev1.ResourceCPU:
			cpu = quota
		case corev1.ResourceMemory:
			mem = quota
		case corev1.ResourcePods:
			pods = quota
		case corev1.ResourceEphemeralStorage:
			empStorage = quota
		default:
			customResource[resourceName] = quota
		}
	}
	return &Resource{
		CPU:              cpu,
		Memory:           mem,
		Pods:             pods,
		EphemeralStorage: empStorage,
		Custom:           customResource,
	}
}
