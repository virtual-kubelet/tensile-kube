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
	"testing"

	"github.com/virtual-kubelet/tensile-kube/pkg/testbase"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRequestFromPod(t *testing.T) {
	pod := testbase.PodForTest()
	pod1 := pod.DeepCopy()
	pod2 := pod.DeepCopy()
	pod2.Spec.Containers[0].Resources.Limits = nil
	pod3 := pod.DeepCopy()
	pod3.Spec.Containers[0].Resources.Limits = nil
	pod3.Spec.Containers[0].Resources.Requests = nil
	pod4 := pod.DeepCopy()
	pod4.Spec.Containers[0].Resources.Limits["ip"] = resource.MustParse("1")
	pod4.Spec.Containers[0].Resources.Requests["ip"] = resource.MustParse("1")

	desired := &common.Resource{CPU: resource.MustParse("10"), Memory: resource.MustParse("0"),
		Pods: resource.MustParse("0"), EphemeralStorage: resource.MustParse("0"), Custom: common.CustomResources{}}
	desiredNil := &common.Resource{Custom: common.CustomResources{}}

	desiredCustom := &common.Resource{CPU: resource.MustParse("10"), Memory: resource.MustParse("0"),
		Pods: resource.MustParse("0"), EphemeralStorage: resource.MustParse("0"),
		Custom: common.CustomResources{"ip": resource.MustParse("1")}}

	cases := []struct {
		pod    *v1.Pod
		desire *common.Resource
	}{
		{
			pod:    pod1,
			desire: desired,
		},
		{
			pod:    pod2,
			desire: desired,
		},
		{
			pod:    pod3,
			desire: desiredNil,
		},
		{
			pod:    pod4,
			desire: desiredCustom,
		},
	}
	for _, c := range cases {
		capacity := GetRequestFromPod(c.pod)
		if !c.desire.Equal(capacity) {
			t.Fatalf("desired: \n%v\n, get: \n%v\n", c.desire, capacity)
		}
		t.Logf("desired: \n%v\n, get: \n%v\n", c.desire, capacity)
	}
}
