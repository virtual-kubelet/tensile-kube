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
	"testing"

	"github.com/virtual-kubelet/tensile-kube/pkg/testbase"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResource(t *testing.T) {
	node := ProviderNode{
		Node: testbase.NodeForTest(),
	}
	node1 := ProviderNode{
		Node: node.Node.DeepCopy(),
	}
	cap := &Resource{
		CPU:    resource.MustParse("1"),
		Memory: resource.MustParse("1Gi"),
		Custom: CustomResources{"ip": resource.MustParse("30")},
	}
	node.AddResource(cap)
	if !node.Status.Capacity["cpu"].Equal(resource.MustParse("51")) ||
		!node.Status.Capacity["memory"].Equal(resource.MustParse("51Gi")) {
		t.Fatalf("nodeAddCapacity unexpected %v", node.Status.Capacity)
	}

	node1.SubResource(cap)
	if !node1.Status.Capacity["cpu"].Equal(resource.MustParse("49")) ||
		!node1.Status.Capacity["memory"].Equal(resource.MustParse("49Gi")) {
		t.Fatalf("nodeRemoveCapacity unexpected %v", node1.Status.Capacity)
	}
}
