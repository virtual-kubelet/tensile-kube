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

package webhook

import (
	"encoding/json"
	"reflect"
	"testing"

	test "github.com/virtual-kubelet/tensile-kube/pkg/testbase"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	v1 "k8s.io/api/core/v1"
)

func TestGetPodTolerations(t *testing.T) {
	pod1 := test.PodForTestWithSystemTolerations()
	pod2 := test.PodForTestWithOtherTolerations()
	cases := []struct {
		pod               *v1.Pod
		desireTolerations []v1.Toleration
	}{
		{
			pod: pod1,
			desireTolerations: []v1.Toleration{
				{
					Key:      util.TaintNodeNotReady,
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoExecute,
				},
				{
					Key:      util.TaintNodeUnreachable,
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoExecute,
				},
			},
		},
		{
			pod: pod2,
			desireTolerations: []v1.Toleration{
				{
					Key:      "testbase",
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoExecute,
				},
				{
					Key:      util.TaintNodeNotReady,
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoExecute,
				},
				{
					Key:      util.TaintNodeUnreachable,
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoExecute,
				},
			},
		},
	}
	for _, c := range cases {
		tolerations := getPodTolerations(c.pod)
		if !reflect.DeepEqual(tolerations, c.desireTolerations) {
			t.Fatalf("Desire %v, Get %v", tolerations, c.desireTolerations)
		}
	}
}

func TestInject(t *testing.T) {
	cases := []struct {
		name      string
		pod       *v1.Pod
		desireCNS util.ClustersNodeSelection
		keys      []string
	}{
		{
			name: "Pod ForTest With Node Selector",
			pod:  test.PodForTestWithNodeSelector(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase"},
			},
		},
		{
			name: "Pod For Test With Node Selector with clusterID",
			pod:  test.PodForTestWithNodeSelectorClusterID(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase"},
			},
			keys: []string{util.ClusterID},
		},
		{
			name: "Pod For Test With Node Selector and Affinity with clusterID",
			pod:  test.PodForTestWithNodeSelectorAndAffinityClusterID(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase"},
				Affinity: &v1.Affinity{
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
				},
			},
			keys: []string{util.ClusterID},
		},
		{
			name: "Pod For Test With Node Selector and Affinity without match labels",
			pod:  test.PodForTestWithNodeSelectorAndAffinityClusterID(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase", "clusterID": "1"},
				Affinity: &v1.Affinity{
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
				},
			},
		},
		{
			name: "Pod For Test With Node Selector and Affinity with multi match labels",
			pod:  test.PodForTestWithNodeSelectorAndAffinityClusterID(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase"},
				Affinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{NodeSelectorTerms: []v1.
							NodeSelectorTerm{{
							MatchExpressions: []v1.NodeSelectorRequirement{
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
				},
			},
			keys: []string{"clusterID", "test0"},
		},
		{
			name: "Pod For Test With Affinity",
			pod:  test.PodForTestWithAffinity(),
			desireCNS: util.ClustersNodeSelection{
				NodeSelector: map[string]string{"testbase": "testbase"},
				Affinity: &v1.Affinity{
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
				},
			},
		},
	}
	for _, c := range cases {
		t.Logf("Running %v", c.name)
		inject(c.pod, c.keys)
		str := c.pod.Annotations[util.SelectorKey]
		cns := util.ClustersNodeSelection{}
		err := json.Unmarshal([]byte(str), &cns)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("ann: %v", str)
		if !reflect.DeepEqual(cns, c.desireCNS) {
			t.Fatalf("Desire: %v, Get: %v", c.desireCNS, cns)
		}
		t.Logf("Desire: %v, Get: %v", c.desireCNS, cns)
	}
}
