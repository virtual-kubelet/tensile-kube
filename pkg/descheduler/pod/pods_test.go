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

package pod

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/descheduler/pkg/utils"
	"sigs.k8s.io/descheduler/test"
)

func TestIsEvictable(t *testing.T) {
	n1 := test.BuildTestNode("node1", 1000, 2000, 13, nil)
	type testCase struct {
		pod                   *v1.Pod
		runBefore             func(*v1.Pod)
		evictLocalStoragePods bool
		result                bool
	}

	testCases := []testCase{
		{
			pod: test.BuildTestPod("p1", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.ObjectMeta.OwnerReferences = test.GetNormalPodOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p2", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = map[string]string{"descheduler.alpha.kubernetes.io/evict": "true"}
				pod.ObjectMeta.OwnerReferences = test.GetNormalPodOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p3", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.ObjectMeta.OwnerReferences = test.GetReplicaSetOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p4", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = map[string]string{"descheduler.alpha.kubernetes.io/evict": "true"}
				pod.ObjectMeta.OwnerReferences = test.GetReplicaSetOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p5", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.ObjectMeta.OwnerReferences = test.GetNormalPodOwnerRefList()
				pod.Spec.Volumes = []v1.Volume{
					{
						Name: "sample",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{Path: "somePath"},
							EmptyDir: &v1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(int64(10), resource.BinarySI)},
						},
					},
				}
			},
			evictLocalStoragePods: false,
			result:                false,
		}, {
			pod: test.BuildTestPod("p6", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.ObjectMeta.OwnerReferences = test.GetNormalPodOwnerRefList()
				pod.Spec.Volumes = []v1.Volume{
					{
						Name: "sample",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{Path: "somePath"},
							EmptyDir: &v1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(int64(10), resource.BinarySI)},
						},
					},
				}
			},
			evictLocalStoragePods: true,
			result:                true,
		}, {
			pod: test.BuildTestPod("p7", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = map[string]string{"descheduler.alpha.kubernetes.io/evict": "true"}
				pod.ObjectMeta.OwnerReferences = test.GetNormalPodOwnerRefList()
				pod.Spec.Volumes = []v1.Volume{
					{
						Name: "sample",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{Path: "somePath"},
							EmptyDir: &v1.EmptyDirVolumeSource{
								SizeLimit: resource.NewQuantity(int64(10), resource.BinarySI)},
						},
					},
				}
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p8", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.ObjectMeta.OwnerReferences = test.GetDaemonSetOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                false,
		}, {
			pod: test.BuildTestPod("p9", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = map[string]string{"descheduler.alpha.kubernetes.io/evict": "true"}
				pod.ObjectMeta.OwnerReferences = test.GetDaemonSetOwnerRefList()
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p10", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = test.GetMirrorPodAnnotation()
			},
			evictLocalStoragePods: false,
			result:                false,
		}, {
			pod: test.BuildTestPod("p11", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				pod.Annotations = test.GetMirrorPodAnnotation()
				pod.Annotations["descheduler.alpha.kubernetes.io/evict"] = "true"
			},
			evictLocalStoragePods: false,
			result:                true,
		}, {
			pod: test.BuildTestPod("p12", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				priority := utils.SystemCriticalPriority
				pod.Spec.Priority = &priority
			},
			evictLocalStoragePods: false,
			result:                false,
		}, {
			pod: test.BuildTestPod("p13", 400, 0, n1.Name, nil),
			runBefore: func(pod *v1.Pod) {
				priority := utils.SystemCriticalPriority
				pod.Spec.Priority = &priority
				pod.Annotations = map[string]string{
					"descheduler.alpha.kubernetes.io/evict": "true",
				}
			},
			evictLocalStoragePods: false,
			result:                true,
		},
	}

	for _, test := range testCases {
		test.pod.Status.Phase = v1.PodPending
		test.runBefore(test.pod)
		result := IsEvictable(test.pod, test.evictLocalStoragePods)
		if result != test.result {
			t.Errorf("IsEvictable should return for pod %s %t, but it returns %t", test.pod.Name, test.result, result)
		}

	}
}
