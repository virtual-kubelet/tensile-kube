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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/virtual-kubelet/tensile-kube/pkg/testbase"
)

func TestTrimObjectMeta(t *testing.T) {
	meta := v1.ObjectMeta{
		UID:             "test",
		ResourceVersion: "test",
		SelfLink:        "http://test.com",
		OwnerReferences: []v1.OwnerReference{
			{
				APIVersion: "Apps/v1",
				Kind:       "StatefulSet",
				Name:       "test",
			},
		},
	}
	TrimObjectMeta(&meta)
	if meta.UID != "" || meta.SelfLink != "" || meta.ResourceVersion != "" || meta.OwnerReferences != nil {
		t.Fatal("Unexpected")
	}
}

func TestTrimPod(t *testing.T) {

	desired := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:   "testbase",
			Labels: map[string]string{"virtual-pod": "true"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: map[corev1.ResourceName]resource.Quantity{
							"cpu": resource.MustParse("10"),
						},
						Requests: map[corev1.ResourceName]resource.Quantity{
							"cpu": resource.MustParse("10"),
						},
					},
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
			NodeName: "",
			Volumes:  []corev1.Volume{},
		},
		Status: corev1.PodStatus{},
	}

	desired1 := desired.DeepCopy()
	desired2 := desired.DeepCopy()

	desired1.Annotations = map[string]string{"clusterSelector": `{"nodeSelector":{"test": "test"}}`}
	desired1.Spec.NodeSelector = map[string]string{"test": "test"}
	desired2.Annotations = map[string]string{"tripped-labels": `{"test":"test"}`}

	basePod := testbase.PodForTest()
	basePod1 := basePod.DeepCopy()
	basePod2 := basePod.DeepCopy()

	basePod1.Annotations = map[string]string{"clusterSelector": `{"nodeSelector":{"test": "test"}}`}

	basePod2.Labels = map[string]string{"test": "test"}

	cases := []struct {
		pod       *corev1.Pod
		desire    *corev1.Pod
		trimLabel []string
	}{
		{
			pod:       basePod,
			desire:    desired,
			trimLabel: nil,
		},
		{
			pod:       basePod1,
			desire:    desired1,
			trimLabel: nil,
		},
		{
			pod:       basePod2,
			desire:    desired2,
			trimLabel: []string{"test"},
		},
		{
			pod:       testbase.PodForTestWithOtherTolerations(),
			desire:    desired,
			trimLabel: nil,
		},
		{
			pod:       testbase.PodForTestWithNodeSelector(),
			desire:    desired,
			trimLabel: nil,
		},
		{
			pod:       testbase.PodForTestWithAffinity(),
			desire:    desired,
			trimLabel: nil,
		},
	}
	for _, d := range cases {
		new := TrimPod(d.pod, d.trimLabel)
		if new.String() != d.desire.String() {
			t.Fatalf("Desired:\n %v\n, get:\n %v", d.desire, new)
		}
	}
}

func TestRecoverLabels(t *testing.T) {
	annotations := map[string]string{"tripped-labels": `{"test":"test"}`}
	oldLabels := map[string]string{}
	labels := map[string]string{"test": "test"}
	RecoverLabels(annotations, oldLabels)
	if reflect.DeepEqual(oldLabels, labels) {
		t.Fatal("Unexpected")
	}
	t.Logf("%v %v", oldLabels, labels)

}
