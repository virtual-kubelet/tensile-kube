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
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	jsonpatch "github.com/evanphx/json-patch"
	jsonpatch1 "github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	// GlobalLabel make object global
	GlobalLabel = "global"
	// SelectorKey is the key of ClusterSelector
	SelectorKey = "clusterSelector"
	// SelectedNodeKey is the node selected by a scheduler
	SelectedNodeKey = "volume.kubernetes.io/selected-node"
	// HostNameKey is the label of HostNameKey
	HostNameKey = "kubernetes.io/hostname"
	// BetaHostNameKey is the label of HostNameKey
	BetaHostNameKey = "beta.kubernetes.io/hostname"
	// LabelOSBeta is the label of os
	LabelOSBeta = "beta.kubernetes.io/os"
	// VirtualPodLabel is the label of virtual pod
	VirtualPodLabel = "virtual-pod"
	// VirtualKubeletLabel is the label of virtual kubelet
	VirtualKubeletLabel = "virtual-kubelet"
	// TrippedLabels is the label of tripped labels
	TrippedLabels = "tripped-labels"
	// ClusterID marks the id of a cluster
	ClusterID = "clusterID"
	// NodeType is define the node type key
	NodeType = "type"
	// BatchPodLabel is the label of batch pod
	BatchPodLabel = "group.batch.scheduler.tencent.com"
	// TaintNodeNotReady will be added when node is not ready
	// and feature-gate for TaintBasedEvictions flag is enabled,
	// and removed when node becomes ready.
	TaintNodeNotReady = "node.kubernetes.io/not-ready"

	// TaintNodeUnreachable will be added when node becomes unreachable
	// (corresponding to NodeReady status ConditionUnknown)
	// and feature-gate for TaintBasedEvictions flag is enabled,
	// and removed when node becomes reachable (NodeReady status ConditionTrue).
	TaintNodeUnreachable = "node.kubernetes.io/unreachable"
	// CreatedbyDescheduler is used to mark if a pod is re-created by descheduler
	CreatedbyDescheduler = "create-by-descheduler"
	// DescheduleCount is used for recording deschedule count
	DescheduleCount = "sigs.k8s.io/deschedule-count"
)

// ClustersNodeSelection is a struct including some scheduling parameters
type ClustersNodeSelection struct {
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
}

// CreateMergePatch return patch generated from original and new interfaces
func CreateMergePatch(original, new interface{}) ([]byte, error) {
	pvByte, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	cloneByte, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.CreateMergePatch(pvByte, cloneByte)
	if err != nil {
		return nil, err
	}
	return patch, nil
}

// CreateJSONPatch return patch generated from original and new interfaces
func CreateJSONPatch(original, new interface{}) ([]byte, error) {
	pvByte, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	cloneByte, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}
	patchs, err := jsonpatch1.CreatePatch(pvByte, cloneByte)
	if err != nil {
		return nil, err
	}
	patchBytes, err := json.Marshal(patchs)
	if err != nil {
		return nil, err
	}
	return patchBytes, nil
}

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
var onlyOneSignalHandler = make(chan struct{})
var shutdownHandler chan os.Signal

// SetupSignalHandler registered for SIGTERM and SIGINT. A stop channel is returned
// which is closed on one of these signals. If a second signal is caught, the program
// is terminated with exit code 1.
func SetupSignalHandler() <-chan struct{} {
	close(onlyOneSignalHandler) // panics when called twice
	shutdownHandler = make(chan os.Signal, 2)
	stop := make(chan struct{})
	signal.Notify(shutdownHandler, shutdownSignals...)
	go func() {
		<-shutdownHandler
		close(stop)
		<-shutdownHandler
		os.Exit(1) // second signal. Exit directly.
	}()
	return stop
}

// Opts define the ops parameter functions
type Opts func(*rest.Config)

// NewClient returns a new client for k8s
func NewClient(configPath string, opts ...Opts) (kubernetes.Interface, error) {
	// master config, maybe a real node or a pod
	var (
		config *rest.Config
		err    error
	)
	config, err = clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("could not read config file for cluster: %v", err)
		}
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(config)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("could not create client for master cluster: %v", err)
	}
	return client, nil
}

// NewMetricClient returns a new client for k8s
func NewMetricClient(configPath string, opts ...Opts) (versioned.Interface, error) {
	// master config, maybe a real node or a pod
	var (
		config *rest.Config
		err    error
	)
	config, err = clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("could not read config file for cluster: %v", err)
		}
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(config)
	}

	metricClient, err := versioned.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("could not create client for master cluster: %v", err)
	}
	return metricClient, nil
}

// IsVirtualNode defines if a node is virtual node
func IsVirtualNode(node *corev1.Node) bool {
	if node == nil {
		return false
	}
	valStr, exist := node.ObjectMeta.Labels[NodeType]
	if !exist {
		return false
	}
	return valStr == VirtualKubeletLabel
}

// IsVirtualPod defines if a pod is virtual pod
func IsVirtualPod(pod *corev1.Pod) bool {
	if pod.Labels != nil && pod.Labels[VirtualPodLabel] == "true" {
		return true
	}
	return false
}

// GetClusterID return the cluster in node label
func GetClusterID(node *corev1.Node) string {
	if node == nil {
		return ""
	}
	clusterName, exist := node.ObjectMeta.Labels[ClusterID]
	if !exist {
		return ""
	}
	return clusterName
}

// UpdateConfigMap updates the configMap data
func UpdateConfigMap(old, new *corev1.ConfigMap) {
	old.Labels = new.Labels
	old.Data = new.Data
	old.BinaryData = new.BinaryData
}

// UpdateSecret updates the secret data
func UpdateSecret(old, new *corev1.Secret) {
	old.Labels = new.Labels
	old.Data = new.Data
	old.StringData = new.StringData
	old.Type = new.Type
}
