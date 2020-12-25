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

package provider

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/virtual-kubelet/tensile-kube/pkg/testbase"
)

func TestGetSecret(t *testing.T) {
	pod := testbase.PodForTestWithSecret()
	secrets := []string{"test1", "testbase"}
	real := getSecrets(pod)
	if !reflect.DeepEqual(secrets, real) {
		t.Fatalf("desire %v real %v", secrets, real)
	}
}

func TestGetConfimap(t *testing.T) {
	pod := testbase.PodForTestWithConfigmap()
	secrets := []string{"testbase"}
	real := getConfigmaps(pod)
	if !reflect.DeepEqual(secrets, real) {
		t.Fatalf("desire %v real %v", secrets, real)
	}
}

func TestGetPVC(t *testing.T) {
	pod := testbase.PodForTestWithPVC()
	desire := []string{"testbase-pvc", "testbase-pvc1"}
	real := getPVCs(pod)
	if !reflect.DeepEqual(desire, real) {
		t.Fatalf("desire %v real %v", desire, real)
	}
}

func TestCheckNodeStatusReady(t *testing.T) {
	node := testbase.NodeForTest()
	node1 := node.DeepCopy()
	node1.Status.Conditions = []v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
	}
	node2 := node.DeepCopy()
	node2.Status.Conditions = []v1.NodeCondition{
		{
			Type:   v1.NodeDiskPressure,
			Status: v1.ConditionTrue,
		},
	}

	cases := []struct {
		node  *v1.Node
		ready bool
	}{
		{
			node:  node,
			ready: false,
		},
		{
			node:  node1,
			ready: true,
		},
		{
			node:  node2,
			ready: false,
		},
	}

	for i, c := range cases {
		if c.ready != checkNodeStatusReady(c.node) {
			t.Fatalf("case %v unexpected", i)
		}
	}
}

func TestPodStopped(t *testing.T) {
	pod := testbase.PodForTest()
	pod1 := pod.DeepCopy()
	pod1.Status.Phase = v1.PodSucceeded
	pod2 := pod.DeepCopy()
	pod2.Status.Phase = v1.PodFailed
	pod3 := pod.DeepCopy()
	pod3.Status.Phase = v1.PodFailed
	pod3.Spec.RestartPolicy = v1.RestartPolicyNever
	pod4 := pod.DeepCopy()
	pod4.Status.Phase = v1.PodSucceeded
	pod4.Spec.RestartPolicy = v1.RestartPolicyNever
	cases := []struct {
		pod     *v1.Pod
		stopped bool
	}{
		{
			pod:     pod,
			stopped: false,
		},
		{
			pod:     pod1,
			stopped: false,
		},
		{
			pod:     pod2,
			stopped: false,
		},
		{
			pod:     pod3,
			stopped: true,
		},
		{
			pod:     pod4,
			stopped: true,
		},
	}
	for _, c := range cases {
		if c.stopped != podStopped(c.pod) {
			t.Errorf("Desire %v, but not", c.stopped)
		}
	}
}
