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
	"time"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func (v *VirtualK8S) GetStatsSummary(ctx context.Context) (*stats.Summary, error) {
	var summary stats.Summary
	selector := labels.SelectorFromSet(map[string]string{
		util.VirtualPodLabel: "true"},
	)
	metrics, err := v.metricClient.MetricsV1beta1().PodMetricses(corev1.NamespaceAll).List(v1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	var cpuAll, memoryAll uint64
	var time time.Time
	for _, metric := range metrics.Items {
		podStats := convert2PodStats(&metric)
		summary.Pods = append(summary.Pods, *podStats)
		cpuAll += *podStats.CPU.UsageNanoCores
		memoryAll += *podStats.Memory.WorkingSetBytes
		if time.IsZero() {
			time = podStats.StartTime.Time
		}
	}
	summary.Node = stats.NodeStats{
		NodeName:  v.providerNode.Name,
		StartTime: v1.Time{Time: time},
		CPU: &stats.CPUStats{
			Time:           v1.Time{Time: time},
			UsageNanoCores: &cpuAll,
		},
		Memory: &stats.MemoryStats{
			Time:            v1.Time{Time: time},
			WorkingSetBytes: &memoryAll,
		},
	}
	return &summary, nil
}

func convert2PodStats(metric *v1beta1.PodMetrics) *stats.PodStats {
	stat := &stats.PodStats{}
	if metric == nil {
		return nil
	}
	stat.PodRef.Namespace = metric.Namespace
	stat.PodRef.Name = metric.Name
	stat.StartTime = metric.Timestamp

	containerStats := stats.ContainerStats{}
	var cpuAll, memoryAll uint64
	for _, c := range metric.Containers {
		containerStats.StartTime = metric.Timestamp
		containerStats.Name = c.Name
		nanoCore := uint64(c.Usage.Cpu().ScaledValue(resource.Nano))
		memory := uint64(c.Usage.Memory().Value())
		containerStats.CPU = &stats.CPUStats{
			Time:           metric.Timestamp,
			UsageNanoCores: &nanoCore,
		}
		containerStats.Memory = &stats.MemoryStats{
			Time:            metric.Timestamp,
			WorkingSetBytes: &memory,
		}
		cpuAll += nanoCore
		memoryAll += memory
		stat.Containers = append(stat.Containers, containerStats)
	}
	stat.CPU = &stats.CPUStats{
		Time:           metric.Timestamp,
		UsageNanoCores: &cpuAll,
	}
	stat.Memory = &stats.MemoryStats{
		Time:            metric.Timestamp,
		WorkingSetBytes: &memoryAll,
	}
	return stat
}
