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

package testbase

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TaintNodeNotReady is not ready taint
	TaintNodeNotReady = "node.kubernetes.io/not-ready"
	// TaintNodeUnreachable is unreachable taint
	TaintNodeUnreachable = "node.kubernetes.io/unreachable"
)

// PodForTest return a basic pod for test
func PodForTest() *v1.Pod {
	defaultMode := int32(420)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testbase",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							"cpu": resource.MustParse("10"),
						},
						Requests: map[v1.ResourceName]resource.Quantity{
							"cpu": resource.MustParse("10"),
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
							ReadOnly:  true,
							Name:      "default-token-mvzcf",
						},
					},
				},
			},
			NodeName: "test",
			Volumes: []v1.Volume{
				{
					Name: "default-token-mvzcf",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName:  "default-token-mvzcf",
							DefaultMode: &defaultMode,
						},
					},
				},
			},
		},
	}
}

// PodForTestWithSystemTolerations return a basic pod for test with system Tolerations
func PodForTestWithSystemTolerations() *v1.Pod {
	pod := PodForTest()
	var tolerationTime int64 = 10
	pod.Spec.Tolerations = []v1.Toleration{
		{
			Key:               TaintNodeNotReady,
			Operator:          v1.TolerationOpExists,
			Effect:            v1.TaintEffectNoExecute,
			TolerationSeconds: &tolerationTime,
		},
		{
			Key:               TaintNodeUnreachable,
			Operator:          v1.TolerationOpExists,
			Effect:            v1.TaintEffectNoExecute,
			TolerationSeconds: &tolerationTime,
		},
	}
	return pod
}

// PodForTestWithOtherTolerations return a basic pod for test with other Tolerations
func PodForTestWithOtherTolerations() *v1.Pod {
	pod := PodForTest()
	pod.Spec.Tolerations = []v1.Toleration{
		{
			Key:      "testbase",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoExecute,
		},
	}
	return pod
}

// PodForTestWithSecret return a basic pod for test with secret
func PodForTestWithSecret() *v1.Pod {
	pod := PodForTest()
	pod.Spec.ImagePullSecrets = []v1.LocalObjectReference{{Name: "testbase"}}
	pod.Spec.Volumes = []v1.Volume{
		{
			Name: "test1",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "test1",
				},
			},
		},
	}
	return pod
}

// PodForTestWithConfigmap return a basic pod for test with configmap
func PodForTestWithConfigmap() *v1.Pod {
	pod := PodForTest()
	pod.Spec.Volumes = []v1.Volume{
		{
			Name: "testbase",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: "testbase",
					},
				},
			},
		},
	}
	return pod
}

// PodForTestWithPVC return a basic pod for test with PVC
func PodForTestWithPVC() *v1.Pod {
	pod := PodForTest()
	pod.Spec.Volumes = []v1.Volume{
		{
			Name: "testbase",
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: "testbase-pvc",
					ReadOnly:  false,
				},
			},
		},
		{
			Name: "test1",
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: "testbase-pvc1",
					ReadOnly:  false,
				},
			},
		},
	}
	return pod
}

// PodForTestWithNodeSelector return a basic pod for test with node selector
func PodForTestWithNodeSelector() *v1.Pod {
	pod := PodForTest()
	pod.Spec.NodeSelector = map[string]string{"testbase": "testbase"}
	return pod
}

// PodForTestWithNodeSelectorClusterID return a basic pod for test with NodeSelectorClusterID
func PodForTestWithNodeSelectorClusterID() *v1.Pod {
	pod := PodForTest()
	pod.Spec.NodeSelector = map[string]string{"testbase": "testbase", "clusterID": "1"}
	return pod
}

// PodForTestWithNodeSelectorAndAffinityClusterID return a basic pod for test with NodeSelectorClusterID
func PodForTestWithNodeSelectorAndAffinityClusterID() *v1.Pod {
	pod := PodForTest()
	pod.Spec.NodeSelector = map[string]string{"testbase": "testbase", "clusterID": "1"}
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{NodeSelectorTerms: []v1.
				NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{
					{
						Key:      "test0",
						Operator: v1.NodeSelectorOpIn,
						Values: []string{
							"aa",
						},
					},
					{
						Key:      "clusterID",
						Operator: v1.NodeSelectorOpIn,
						Values: []string{
							"aa",
						},
					},
					{
						Key:      "test",
						Operator: v1.NodeSelectorOpIn,
						Values: []string{
							"aa",
						},
					},
					{
						Key:      "test1",
						Operator: v1.NodeSelectorOpIn,
						Values: []string{
							"aa",
						},
					},
				},
			}}},
			PreferredDuringSchedulingIgnoredDuringExecution: nil,
		},
	}
	return pod
}

// NodeForTest return a basic node
func NodeForTest() *v1.Node {
	return &v1.Node{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testbase",
		},
		Status: v1.NodeStatus{
			Capacity: map[v1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("50"),
				"memory": resource.MustParse("50Gi"),
			},
			Allocatable: map[v1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("50"),
				"memory": resource.MustParse("50Gi"),
			},
		},
	}
}

// PodForTestWithAffinity return a basic pod for test with Affinity
func PodForTestWithAffinity() *v1.Pod {
	pod := PodForTest()
	pod.Spec.NodeSelector = map[string]string{"testbase": "testbase"}
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{NodeSelectorTerms: []v1.
				NodeSelectorTerm{{
				MatchExpressions: []v1.NodeSelectorRequirement{
					{
						Key:      "testbase",
						Operator: v1.NodeSelectorOpIn,
						Values: []string{
							"aa",
						},
					},
				},
			}}},
			PreferredDuringSchedulingIgnoredDuringExecution: nil,
		},
	}
	return pod
}
