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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/virtual-kubelet/tensile-kube/pkg/common"
)

func TestConfigureNode(t *testing.T) {
	ctx := context.Background()
	res := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("10"),
	}
	res1 := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("20"),
	}
	res2 := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("17"),
	}
	nodeNotReady := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		Status: corev1.NodeStatus{
			Capacity:    res,
			Allocatable: res,
		},
	}
	node1 := nodeNotReady.DeepCopy()
	node1.Status.Conditions = []corev1.NodeCondition{
		{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		},
	}
	node2 := node1.DeepCopy()
	node2.Name = "node2"
	for _, c := range []struct {
		name               string
		nodes              []*corev1.Node
		pod                *corev1.Pod
		desiredVirtualNode *corev1.Node
	}{
		{
			name:  "one node, notReady",
			nodes: []*corev1.Node{nodeNotReady},
			desiredVirtualNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "fake"},
				Status:     corev1.NodeStatus{},
			},
		},
		{
			name:  "one node, ready",
			nodes: []*corev1.Node{node1},
			desiredVirtualNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "fake"},
				Status: corev1.NodeStatus{
					Capacity:    res,
					Allocatable: res,
				},
			},
		},
		{
			name:  "two nodes",
			nodes: []*corev1.Node{node1, node2},
			desiredVirtualNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "fake"},
				Status: corev1.NodeStatus{
					Capacity:    res1,
					Allocatable: res1,
				},
			},
		},
		{
			name:  "nodes with pod running",
			nodes: []*corev1.Node{node1, node2},
			pod:   fakeNodeWithReq(),
			desiredVirtualNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "fake"},
				Status: corev1.NodeStatus{
					Capacity:    res2,
					Allocatable: res2,
				},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			node := corev1.Node{}
			node.Labels = map[string]string{}
			vk, nodeInformer, podInformer := newFakeVirtualK8SWithNodePod()
			for _, node := range c.nodes {
				nodeInformer.Informer().GetStore().Add(node)
			}
			if c.pod != nil {
				podInformer.Informer().GetStore().Add(c.pod)
			}
			vk.ConfigureNode(ctx, &node)
			if node.Status.Allocatable.Cpu().Sign() !=
				c.desiredVirtualNode.Status.Allocatable.Cpu().Sign() {
				t.Errorf("Desired: %v, get: %v",
					c.desiredVirtualNode.Status.Allocatable, node.Status.Allocatable)
			}
		})
	}
}

func newFakeVirtualK8SWithNodePod() (*VirtualK8S, v1.NodeInformer, v1.PodInformer) {
	client := fake.NewSimpleClientset()
	master := fake.NewSimpleClientset()

	clientInformer := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())

	nodeInformer := clientInformer.Core().V1().Nodes()
	podInformer := clientInformer.Core().V1().Pods()
	return &VirtualK8S{
		master: master,
		client: client,
		clientCache: clientCache{
			nodeLister: nodeInformer.Lister(),
			podLister:  podInformer.Lister(),
		},
		providerNode: &common.ProviderNode{
			Node: &corev1.Node{},
		},
	}, nodeInformer, podInformer
}

func fakeNodeWithReq() *corev1.Pod {
	pod := fakePod("ns")
	res := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("3"),
	}
	pod.Spec.NodeName = "node1"
	pod.Spec.Containers = []corev1.Container{
		{
			Resources: corev1.ResourceRequirements{
				Requests: res,
			},
		},
	}
	return pod
}
