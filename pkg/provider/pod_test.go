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
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/controller"
)

func TestCreatePod(t *testing.T) {
	vk, _, podInformer := newFakeVirtualK8S()
	ctx := context.Background()
	for _, c := range []struct {
		name     string
		pod      *corev1.Pod
		error    bool
		existPod *corev1.Pod
	}{
		{
			name:  "kube-system pod, do not create",
			pod:   fakePod("kube-system"),
			error: false,
		},
		{
			name:  "ns not exist",
			pod:   fakePod("test"),
			error: false,
		},
		{
			name:     "pod exists",
			pod:      fakePod("test"),
			existPod: fakePod("test"),
			error:    true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.existPod != nil {
				podInformer.Informer().GetStore().Add(c.pod)
			}
			err := vk.CreatePod(ctx, c.pod)
			if (err != nil) != c.error {
				t.Errorf("Desire: %v, get: %v", c.error, err != nil)
			}
		})
	}
}

func TestUpdatePod(t *testing.T) {
	vk, _, podInformer := newFakeVirtualK8S()
	ctx := context.Background()
	notVirtualPod := fakePod("test")
	updatedNotVirtualPod := notVirtualPod.DeepCopy()
	updatedNotVirtualPod.Annotations = map[string]string{"virtual-pod": "true"}

	virtualPod := notVirtualPod.DeepCopy()
	virtualPod.Labels = map[string]string{"virtual-pod": "true"}
	updatedVirtualPod := notVirtualPod.DeepCopy()
	updatedVirtualPod.Annotations = map[string]string{"virtual-pod": "true"}
	for _, c := range []struct {
		name     string
		pod      *corev1.Pod
		error    bool
		existPod *corev1.Pod
	}{
		{
			name:  "kube-system pod, do not update",
			pod:   fakePod("kube-system"),
			error: false,
		},
		{
			name:  "pod not exist",
			pod:   fakePod("test"),
			error: true,
		},
		{
			name:     "not virtual pod",
			pod:      updatedNotVirtualPod,
			existPod: notVirtualPod,
			error:    false,
		},
		{
			name:     "pod exists",
			pod:      updatedVirtualPod,
			existPod: virtualPod,
			error:    false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.existPod != nil {
				podInformer.Informer().GetStore().Add(c.pod)
			}
			err := vk.UpdatePod(ctx, c.pod)
			if (err != nil) != c.error {
				t.Errorf("Desire: %v, get: %v", c.error, err != nil)
			}
			if c.existPod == nil {
				return
			}
			if !util.IsVirtualPod(c.existPod) {
				return
			}
			pod, err := vk.GetPod(ctx, c.existPod.Namespace, c.existPod.Name)
			if err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(pod, c.pod) {
				t.Error("update pod failed")
			}

		})
	}
}

func TestDeletePod(t *testing.T) {
	vk, _, podInformer := newFakeVirtualK8S()
	ctx := context.Background()
	for _, c := range []struct {
		name     string
		pod      *corev1.Pod
		error    bool
		existPod *corev1.Pod
	}{
		{
			name:  "kube-system pod, do not delete",
			pod:   fakePod("kube-system"),
			error: false,
		},
		{
			name:  "not virtual pod, do not delete",
			pod:   fakePod("kube-system"),
			error: false,
		},
		{
			name:  "pod not exist",
			pod:   fakePod("test"),
			error: false,
		},
		{
			name:     "pod exists",
			pod:      fakePod("test"),
			existPod: fakePod("test"),
			error:    false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.existPod != nil {
				podInformer.Informer().GetStore().Add(c.pod)
			}
			err := vk.DeletePod(ctx, c.pod)
			if (err != nil) != c.error {
				t.Errorf("Desire: %v, get: %v", c.error, err != nil)
			}
		})
	}
}

func TestGetPod(t *testing.T) {
	vk, _, podInformer := newFakeVirtualK8S()
	ctx := context.Background()
	for _, c := range []struct {
		name     string
		pod      *corev1.Pod
		error    bool
		existPod *corev1.Pod
	}{
		{
			name:  "pod not exist",
			pod:   fakePod("test"),
			error: true,
		},
		{
			name:     "pod exists",
			pod:      fakePod("test"),
			existPod: fakePod("test"),
			error:    false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.existPod != nil {
				podInformer.Informer().GetStore().Add(c.pod)
			}
			_, err := vk.GetPod(ctx, c.pod.Namespace, c.pod.Name)
			if (err != nil) != c.error {
				t.Errorf("Desire: %v, get: %v", c.error, err != nil)
			}
		})
	}
}

func newFakeVirtualK8S() (*VirtualK8S, v1.NamespaceInformer, v1.PodInformer) {
	client := fake.NewSimpleClientset()
	master := fake.NewSimpleClientset()

	clientInformer := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())

	nsInformer := clientInformer.Core().V1().Namespaces()
	podInformer := clientInformer.Core().V1().Pods()
	return &VirtualK8S{
		master: master,
		client: client,
		clientCache: clientCache{
			nsLister:  nsInformer.Lister(),
			podLister: podInformer.Lister(),
		},
	}, nsInformer, podInformer
}

func fakePod(ns string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec:   corev1.PodSpec{},
		Status: corev1.PodStatus{},
	}
	if ns != "" {
		pod.Namespace = ns
	}
	return pod
}
