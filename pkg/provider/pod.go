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
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	mergetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog"

	"github.com/virtual-kubelet/tensile-kube/pkg/controllers"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

var _ node.PodLifecycleHandler = &VirtualK8S{}
var _ node.PodNotifier = &VirtualK8S{}
var _ node.NodeProvider = &VirtualK8S{}

// CreatePod takes a Kubernetes Pod and deploys it within the provider.
func (v *VirtualK8S) CreatePod(ctx context.Context, pod *corev1.Pod) error {
	if pod.Namespace == "kube-system" {
		return nil
	}
	basicPod := util.TrimPod(pod, v.ignoreLabels)
	klog.V(3).Infof("Creating pod %v/%+v", pod.Namespace, pod.Name)
	if _, err := v.clientCache.nsLister.Get(pod.Namespace); err != nil && errors.IsNotFound(
		err) {
		klog.Infof("Namespace %s does not exist for pod %s, creating it", pod.Namespace, pod.Name)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: pod.Namespace,
			},
		}
		if _, createErr := v.client.CoreV1().Namespaces().Create(ns); createErr != nil && errors.IsAlreadyExists(
			createErr) {
			klog.Infof("Namespace %s create failed error: %v", pod.Namespace, createErr)
			return err
		}
	}
	secretNames := getSecrets(pod)
	configMaps := getConfigmaps(pod)
	pvcs := getPVCs(pod)
	go wait.PollImmediate(500*time.Millisecond, 10*time.Minute, func() (bool, error) {

		klog.V(4).Info("Trying to creating base dependent")
		if err := v.createConfigMaps(ctx, configMaps, pod.Namespace); err != nil {
			klog.Error(err)
			return false, nil
		}
		klog.Infof("Create configmaps %v of %v/%v success", configMaps, pod.Namespace, pod.Name)
		if err := v.createPVCs(ctx, pvcs, pod.Namespace); err != nil {
			klog.Error(err)
			return false, nil
		}
		klog.Infof("Create pvc %v of %v/%v success", pvcs, pod.Namespace, pod.Name)
		return true, nil
	})
	var err error
	wait.PollImmediate(100*time.Millisecond, 1*time.Second, func() (bool, error) {

		klog.V(4).Info("Trying to creating secret and service account")
		// TODO: Instead of creating secrets in the target cluster, could read the secrets in the master
		// and use an init container to write them into the pod?
		if err = v.createSecrets(ctx, secretNames, pod.Namespace); err != nil {
			klog.Error(err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("create secrets failed: %v", err)
	}
	klog.V(6).Infof("Creating pod %+v", pod)
	_, err = v.client.CoreV1().Pods(pod.Namespace).Create(basicPod)
	if err != nil {
		return fmt.Errorf("could not create pod: %v", err)
	}
	klog.V(3).Infof("Create pod %v/%+v success", pod.Namespace, pod.Name)
	return nil
}

// UpdatePod takes a Kubernetes Pod and updates it within the provider.
func (v *VirtualK8S) UpdatePod(ctx context.Context, pod *corev1.Pod) error {
	if pod.Namespace == "kube-system" {
		return nil
	}
	klog.V(3).Infof("Updating pod %v/%+v", pod.Namespace, pod.Name)
	currentPod, err := v.GetPod(ctx, pod.Namespace, pod.Name)
	if err != nil {
		return fmt.Errorf("could not get current pod")
	}
	if !util.IsVirtualPod(pod) {
		klog.Info("Pod is not created by vk, ignore")
		return nil
	}

	podCopy := currentPod.DeepCopy()
	util.GetUpdatedPod(podCopy, pod, v.ignoreLabels)
	if reflect.DeepEqual(currentPod.Spec, podCopy.Spec) &&
		reflect.DeepEqual(currentPod.Annotations, podCopy.Annotations) &&
		reflect.DeepEqual(currentPod.Labels, podCopy.Labels) {
		return nil
	}
	_, err = v.client.CoreV1().Pods(pod.Namespace).Update(podCopy)
	if err != nil {
		return fmt.Errorf("could not update pod: %v", err)
	}
	klog.V(3).Infof("Update pod %v/%+v success ", pod.Namespace, pod.Name)
	return nil
}

// DeletePod takes a Kubernetes Pod and deletes it from the provider.
func (v *VirtualK8S) DeletePod(ctx context.Context, pod *corev1.Pod) error {
	if pod.Namespace == "kube-system" {
		return nil
	}
	klog.V(3).Infof("Deleting pod %v/%+v", pod.Namespace, pod.Name)

	if !util.IsVirtualPod(pod) {
		klog.Info("Pod is not create by vk, ignore")
		return nil
	}

	opts := &metav1.DeleteOptions{
		GracePeriodSeconds: new(int64), // 0
	}
	if pod.DeletionGracePeriodSeconds != nil {
		opts.GracePeriodSeconds = pod.DeletionGracePeriodSeconds
	}

	err := v.client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, opts)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Tried to delete pod %s/%s, but it did not exist in the cluster", pod.Namespace, pod.Name)
			return nil
		}
		return fmt.Errorf("could not delete pod: %v", err)
	}
	klog.V(3).Infof("Delete pod %v/%+v success", pod.Namespace, pod.Name)
	return nil
}

// GetPod retrieves a pod by name from the provider (can be cached).
// The Pod returned is expected to be immutable, and may be accessed
// concurrently outside of the calling goroutine. Therefore it is recommended
// to return a version after DeepCopy.
func (v *VirtualK8S) GetPod(ctx context.Context, namespace string, name string) (*corev1.Pod, error) {
	pod, err := v.clientCache.podLister.Pods(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, errdefs.AsNotFound(err)
		}
		return nil, fmt.Errorf("could not get pod %s/%s: %v", namespace, name, err)
	}
	podCopy := pod.DeepCopy()
	util.RecoverLabels(podCopy.Labels, podCopy.Annotations)
	return podCopy, nil
}

// GetPodStatus retrieves the status of a pod by name from the provider.
// The PodStatus returned is expected to be immutable, and may be accessed
// concurrently outside of the calling goroutine. Therefore it is recommended
// to return a version after DeepCopy.
func (v *VirtualK8S) GetPodStatus(ctx context.Context, namespace string, name string) (*corev1.PodStatus, error) {
	pod, err := v.clientCache.podLister.Pods(namespace).Get(name)
	if err != nil {
		return nil, fmt.Errorf("could not get pod %s/%s: %v", namespace, name, err)
	}
	return pod.Status.DeepCopy(), nil
}

// GetPods retrieves a list of all pods running on the provider (can be cached).
// The Pods returned are expected to be immutable, and may be accessed
// concurrently outside of the calling goroutine. Therefore it is recommended
// to return a version after DeepCopy.
func (v *VirtualK8S) GetPods(_ context.Context) ([]*corev1.Pod, error) {
	set := labels.Set{util.VirtualPodLabel: "true"}
	pods, err := v.clientCache.podLister.List(labels.SelectorFromSet(set))
	if err != nil {
		return nil, fmt.Errorf("could not list pods: %v", err)
	}

	podRefs := []*corev1.Pod{}
	for _, p := range pods {
		if !util.IsVirtualPod(p) {
			continue
		}
		podCopy := p.DeepCopy()
		util.RecoverLabels(podCopy.Labels, podCopy.Annotations)
		podRefs = append(podRefs, podCopy)
	}

	return podRefs, nil
}

// GetContainerLogs retrieves the logs of a container by name from the provider.
func (v *VirtualK8S) GetContainerLogs(ctx context.Context, namespace string, podName string, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	tailLine := int64(opts.Tail)
	limitBytes := int64(opts.LimitBytes)
	sinceSeconds := opts.SinceSeconds
	options := &corev1.PodLogOptions{
		Container:  containerName,
		Timestamps: opts.Timestamps,
		Follow:     opts.Follow,
	}
	if tailLine != 0 {
		options.TailLines = &tailLine
	}
	if limitBytes != 0 {
		options.LimitBytes = &limitBytes
	}
	if !opts.SinceTime.IsZero() {
		*options.SinceTime = metav1.Time{Time: opts.SinceTime}
	}
	if sinceSeconds != 0 {
		*options.SinceSeconds = int64(sinceSeconds)
	}
	if opts.Previous {
		options.Previous = opts.Previous
	}
	if opts.Follow {
		options.Follow = opts.Follow
	}
	logs := v.client.CoreV1().Pods(namespace).GetLogs(podName, options)
	stream, err := logs.Stream()
	if err != nil {
		return nil, fmt.Errorf("could not get stream from logs request: %v", err)
	}
	return stream, nil
}

// RunInContainer executes a command in a container in the pod, copying data
// between in/out/err and the container's stdin/stdout/stderr.
func (v *VirtualK8S) RunInContainer(ctx context.Context, namespace string, podName string, containerName string, cmd []string, attach api.AttachIO) error {
	defer func() {
		if attach.Stdout() != nil {
			attach.Stdout().Close()
		}
		if attach.Stderr() != nil {
			attach.Stderr().Close()
		}
	}()
	req := v.client.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		Timeout(0).
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     attach.Stdin() != nil,
			Stdout:    attach.Stdout() != nil,
			Stderr:    attach.Stderr() != nil,
			TTY:       attach.TTY(),
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(v.config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("could not make remote command: %v", err)
	}

	ts := &termSize{attach: attach}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             attach.Stdin(),
		Stdout:            attach.Stdout(),
		Stderr:            attach.Stderr(),
		Tty:               attach.TTY(),
		TerminalSizeQueue: ts,
	})
	if err != nil {
		return err
	}

	return nil
}

// NotifyPods instructs the notifier to call the passed in function when
// the pod status changes. It should be called when a pod's status changes.
//
// The provided pointer to a Pod is guaranteed to be used in a read-only
// fashion. The provided pod's PodStatus should be up to date when
// this function is called.
//
// NotifyPods will not block callers.
func (v *VirtualK8S) NotifyPods(ctx context.Context, cb func(*corev1.Pod)) {
	klog.Info("Called NotifyPods")
	go func() {
		// to make sure pods have been add to known pods
		time.Sleep(10 * time.Second)
		for {
			select {
			case pod := <-v.updatedPod:
				klog.V(4).Infof("Enqueue updated pod %v", pod.Name)
				// need trim pod, e.g. UID
				util.RecoverLabels(pod.Labels, pod.Annotations)
				cb(pod)
			case <-v.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// createSecrets takes a Kubernetes Pod and deploys it within the provider.
func (v *VirtualK8S) createSecrets(ctx context.Context, secrets []string, ns string) error {
	for _, secretName := range secrets {
		old, err := v.clientCache.secretLister.Secrets(ns).Get(secretName)
		if err == nil {
			continue
		}
		if errors.IsNotFound(err) {
			secret, err := v.rm.GetSecret(secretName, ns)

			if err != nil {
				return err
			}
			util.TrimObjectMeta(&secret.ObjectMeta)
			// skip service account secret
			if secret.Type == corev1.SecretTypeServiceAccountToken {
				continue
			}
			controllers.SetObjectGlobal(&secret.ObjectMeta)
			_, err = v.client.CoreV1().Secrets(ns).Create(secret)
			if err != nil {
				if errors.IsAlreadyExists(err) {
					util.UpdateSecret(old, secret)
					if _, err = v.client.CoreV1().Secrets(ns).Update(old); err != nil {
						klog.Error(err)
					}
					continue
				}
				klog.Errorf("Failed to create secret %v err: %v", secretName, err)
				return fmt.Errorf("could not create secret %s in external cluster: %v", secretName, err)
			}
		}
		return err
	}
	return nil
}

// createConfigMaps a Kubernetes Pod and deploys it within the provider.
func (v *VirtualK8S) createConfigMaps(ctx context.Context, configmaps []string, ns string) error {
	for _, cm := range configmaps {
		old, err := v.clientCache.cmLister.ConfigMaps(ns).Get(cm)
		if err == nil {
			continue
		}
		if errors.IsNotFound(err) {
			configMap, err := v.rm.GetConfigMap(cm, ns)
			if err != nil {
				return fmt.Errorf("find comfigmap %v error %v", cm, err)
			}
			util.TrimObjectMeta(&configMap.ObjectMeta)
			controllers.SetObjectGlobal(&configMap.ObjectMeta)

			_, err = v.client.CoreV1().ConfigMaps(ns).Create(configMap)
			if err != nil {
				if errors.IsAlreadyExists(err) {
					util.UpdateConfigMap(old, configMap)
					if _, err = v.client.CoreV1().ConfigMaps(ns).Update(old); err != nil {
						klog.Error(err)
					}
					continue
				}
				klog.Errorf("Failed to create configmap %v err: %v", cm, err)
				return err
			}
			klog.Infof("Create %v in %v success", cm, ns)
			continue
		}
		return fmt.Errorf("could not check configmap %s in external cluster: %v", cm, err)
	}
	return nil
}

// deleteConfigMaps a Kubernetes Pod and deploys it within the provider.
func (v *VirtualK8S) deleteConfigMaps(ctx context.Context, configmaps []string, ns string) error {
	for _, cm := range configmaps {
		err := v.client.CoreV1().ConfigMaps(ns).Delete(cm, &metav1.DeleteOptions{})
		if err == nil {
			continue
		}
		if errors.IsNotFound(err) {
			continue
		}
		klog.Errorf("could not check configmap %s in external cluster: %v", cm, err)
		return err
	}
	return nil
}

// createPVCs a Kubernetes Pod and deploys it within the provider.
func (v *VirtualK8S) createPVCs(ctx context.Context, pvcs []string, ns string) error {
	for _, cm := range pvcs {
		_, err := v.client.CoreV1().PersistentVolumeClaims(ns).Get(cm, metav1.GetOptions{})
		if err == nil {
			continue
		}
		if errors.IsNotFound(err) {
			pvc, err := v.master.CoreV1().PersistentVolumeClaims(ns).Get(cm, metav1.GetOptions{})
			if err != nil {
				continue
			}
			util.TrimObjectMeta(&pvc.ObjectMeta)
			controllers.SetObjectGlobal(&pvc.ObjectMeta)
			_, err = v.client.CoreV1().PersistentVolumeClaims(ns).Create(pvc)
			if err != nil {
				if errors.IsAlreadyExists(err) {
					continue
				}
				klog.Errorf("Failed to create pvc %v err: %v", cm, err)
				return err
			}
			continue
		}
		return fmt.Errorf("could not check pvc %s in external cluster: %v", cm, err)
	}
	return nil
}

func (v *VirtualK8S) patchConfigMap(cm, clone *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	if reflect.DeepEqual(cm.Data, clone.Data) {
		return cm, nil
	}
	if !controllers.CheckGlobalLabelEqual(&cm.ObjectMeta, &clone.ObjectMeta) {
		return cm, nil
	}

	patch, err := util.CreateMergePatch(cm, clone)
	if err != nil {
		return cm, err
	}
	newCM, err := v.client.CoreV1().ConfigMaps(cm.Namespace).Patch(cm.Name,
		mergetypes.MergePatchType,
		patch)
	if err != nil {
		return cm, err
	}
	return newCM, nil
}

type termSize struct {
	attach api.AttachIO
}

func (t *termSize) Next() *remotecommand.TerminalSize {
	resize := <-t.attach.Resize()
	return &remotecommand.TerminalSize{
		Height: resize.Height,
		Width:  resize.Width,
	}
}
