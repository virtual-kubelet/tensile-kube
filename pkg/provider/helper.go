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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

// getSecrets filters the volumes of a pod to get only the secret volumes,
// excluding the serviceaccount token secret which is automatically added by kuberentes.
func getSecrets(pod *corev1.Pod) []string {
	secretNames := []string{}
	for _, v := range pod.Spec.Volumes {
		switch {
		case v.Secret != nil:
			if strings.HasPrefix(v.Name, "default-token") {
				continue
			}
			klog.Infof("pod %s depends on secret %s", pod.Name, v.Secret.SecretName)
			secretNames = append(secretNames, v.Secret.SecretName)

		case v.CephFS != nil:
			klog.Infof("pod %s depends on secret %s", pod.Name, v.CephFS.SecretRef.Name)
			secretNames = append(secretNames, v.Secret.SecretName)
		case v.Cinder != nil:
			klog.Infof("pod %s depends on secret %s", pod.Name, v.Cinder.SecretRef.Name)
			secretNames = append(secretNames, v.Secret.SecretName)
		case v.RBD != nil:
			klog.Infof("pod %s depends on secret %s", pod.Name, v.RBD.SecretRef.Name)
			secretNames = append(secretNames, v.Secret.SecretName)
		}
	}
	secretFromEnv := map[string]string{}
	for _, v := range pod.Spec.Containers {
		for _, val := range v.EnvFrom {
			if val.SecretRef != nil {
				name := val.SecretRef.Name
				secretFromEnv[name] = name
			}
		}
	}
	for _, v := range pod.Spec.InitContainers {
		for _, val := range v.EnvFrom {
			if val.SecretRef != nil {
				name := val.SecretRef.Name
				secretFromEnv[name] = name
			}
		}
	}
	for _, v := range secretFromEnv {
		secretNames = append(secretNames, v)
	}
	if pod.Spec.ImagePullSecrets != nil {
		for _, s := range pod.Spec.ImagePullSecrets {
			secretNames = append(secretNames, s.Name)
		}
	}
	klog.Infof("pod %s depends on secrets %s", pod.Name, secretNames)
	return secretNames
}

// getConfigmaps filters the volumes of a pod to get only the configmap volumes,
func getConfigmaps(pod *corev1.Pod) []string {
	cmNames := []string{}
	for _, v := range pod.Spec.Volumes {
		if v.ConfigMap == nil {
			continue
		}
		cmNames = append(cmNames, v.ConfigMap.Name)
	}
	cmFromeEnv := map[string]string{}
	for _, v := range pod.Spec.InitContainers {
		for _, val := range v.EnvFrom {
			if val.ConfigMapRef != nil {
				name := val.ConfigMapRef.Name
				cmFromeEnv[name] = name
			}
		}
	}
	for _, v := range pod.Spec.Containers {
		for _, val := range v.EnvFrom {
			if val.ConfigMapRef != nil {
				name := val.ConfigMapRef.Name
				cmFromeEnv[name] = name
			}
		}
	}
	for _, v := range cmFromeEnv {
		cmNames = append(cmNames, v)
	}
	klog.Infof("pod %s depends on configMap %s", pod.Name, cmNames)
	return cmNames
}

// getPVCs filters the volumes of a pod to get only the pvc,
func getPVCs(pod *corev1.Pod) []string {
	cmNames := []string{}
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		cmNames = append(cmNames, v.PersistentVolumeClaim.ClaimName)
	}
	klog.Infof("pod %s depends on pvc %v", pod.Name, cmNames)
	return cmNames
}

func checkNodeStatusReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeReady {
			continue
		}
		if condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func compareNodeStatusReady(old, new *corev1.Node) (bool, bool) {
	return checkNodeStatusReady(old), checkNodeStatusReady(new)
}

func podStopped(pod *corev1.Pod) bool {
	return (pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) && pod.Spec.
		RestartPolicy == corev1.RestartPolicyNever
}
