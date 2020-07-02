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
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/virtual-kubelet/tensile-kube/pkg/testbase"
)

func TestIsVirtualNode(t *testing.T) {
	node := testbase.NodeForTest()
	node1 := node.DeepCopy()
	node1.Labels = map[string]string{"type": "virtual-kubelet"}
	cases := []struct {
		name string
		node *v1.Node
		vn   bool
	}{
		{
			"not vk node",
			node,
			false,
		},
		{
			"vk node",
			node1,
			true,
		},
	}
	for _, c := range cases {
		if c.vn != IsVirtualNode(c.node) {
			t.Fatalf("case %v failed", c.name)
		}
	}
}

func TestIsVirtualPod(t *testing.T) {
	pod := testbase.PodForTest()
	pod1 := pod.DeepCopy()
	pod1.Labels = map[string]string{VirtualPodLabel: "true"}
	cases := []struct {
		name string
		pod  *v1.Pod
		vn   bool
	}{
		{
			"not vk pod",
			pod,
			false,
		},
		{
			"vk node",
			pod1,
			true,
		},
	}
	for _, c := range cases {
		if c.vn != IsVirtualPod(c.pod) {
			t.Fatalf("case %v failed", c.name)
		}
	}
}

func TestGetClusterID(t *testing.T) {
	node := testbase.NodeForTest()
	node1 := node.DeepCopy()
	node1.Labels = map[string]string{"clusterID": "vk"}
	cases := []struct {
		name string
		node *v1.Node
		id   string
	}{
		{
			"cluster Id empty",
			node,
			"",
		},
		{
			"cluster Id not empty",
			node1,
			"vk",
		},
	}
	for _, c := range cases {
		if c.id != GetClusterID(c.node) {
			t.Fatalf("case %v failed", c.name)
		}
	}
}

func TestNewClient(t *testing.T) {
	path := "/tmp/test.config"
	f, err := os.Create(path)
	if err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	//defer os.Remove(path)
	cases := []struct {
		name        string
		path        string
		opt         Opts
		configExist bool
		err         bool
	}{
		{
			"empty path",
			"",
			nil,
			false,
			true,
		},
		{
			"not empty",
			path,
			nil,
			false,
			true,
		},
		{
			"not empty, cfg exist",
			path,
			nil,
			true,
			true,
		},
	}
	for _, c := range cases {
		_, retErr := NewClient(c.path, c.opt)

		if c.configExist {
			cfg := `apiVersion: v1
clusters:
- cluster:
    server: http://localhost:80
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
    name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin`
			len, err := f.Write([]byte(cfg))
			if err != nil {
				t.Fatal(err)
			}
			if len == 0 {
				t.Fatal("write failed")
			}
			f.Close()
		}
		if retErr != nil && !c.err {
			t.Fatalf("case: %v, err :%v", c.name, retErr)
		}
		if retErr == nil && c.err {
			t.Fatalf(c.name)
		}
	}
}

func TestCreateMergePatch(t *testing.T) {
	basePod := testbase.PodForTest()
	basePodWithSelector := testbase.PodForTestWithNodeSelector()
	desiredPatch := `{"spec":{"nodeSelector":{"testbase":"testbase"}}}`
	patch, err := CreateMergePatch(basePod, basePodWithSelector)
	if err != nil {
		t.Fatal(err)
	}
	if string(patch) != desiredPatch {
		t.Fatalf("desired path: \n%v\n, get: \n%v\n", desiredPatch, string(patch))
	}
}

func TestCreateJSONPatch(t *testing.T) {
	basePod := testbase.PodForTest()
	basePodWithSelector := testbase.PodForTestWithNodeSelector()
	desiredPatch := `[{"op":"add","path":"/spec/nodeSelector","value":{"testbase":"testbase"}}]`
	patch, err := CreateJSONPatch(basePod, basePodWithSelector)
	if err != nil {
		t.Fatal(err)
	}
	if string(patch) != desiredPatch {
		t.Fatalf("desired path: \n%v\n, get: \n%v\n", desiredPatch, string(patch))
	}
}
