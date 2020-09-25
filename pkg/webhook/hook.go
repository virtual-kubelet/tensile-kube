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
	"fmt"
	"io/ioutil"
	"net/http"

	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
	// (https://github.com/kubernetes/kubernetes/issues/57982)
	defaulter  = runtime.ObjectDefaulter(runtimeScheme)
	desiredMap = map[string]corev1.Toleration{
		util.TaintNodeNotReady: {
			Key:      util.TaintNodeNotReady,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoExecute,
		},
		util.TaintNodeUnreachable: {
			Key:      util.TaintNodeUnreachable,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoExecute,
		},
	}
)

// HookServer is an interface defines a server
type HookServer interface {
	// Serve starts a server
	Serve(http.ResponseWriter, *http.Request)
}

// webhookServer is a sever for webhook
type webhookServer struct {
	ignoreSelectorKeys []string
	pvcLister          v1.PersistentVolumeClaimLister
	Server             *http.Server
}

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionregistrationv1beta1.AddToScheme(runtimeScheme)
}

// NewWebhookServer start a new webhook server
func NewWebhookServer(pvcLister v1.PersistentVolumeClaimLister, ignoreKeys []string) HookServer {
	return &webhookServer{
		ignoreSelectorKeys: ignoreKeys,
		pvcLister:          pvcLister,
	}
}

// mutate k8s pod annotations, Affinity, nodeSelector and etc.
func (whsvr *webhookServer) mutate(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	req := ar.Request
	var (
		err error
		pod corev1.Pod
	)
	switch req.Kind.Kind {
	case "Pod":
		klog.V(4).Infof("Raw request %v", string(req.Object.Raw))
		if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
			klog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}
	default:
		return &v1beta1.AdmissionResponse{
			Allowed: false,
		}
	}
	if err != nil {
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}
	if req.Namespace == "kube-system" {
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}
	if pod.Labels != nil {
		if pod.Labels[util.CreatedbyDescheduler] == "true" {
			return &v1beta1.AdmissionResponse{
				Allowed: true,
			}
		}
		if !util.IsVirtualPod(&pod) {
			return &v1beta1.AdmissionResponse{
				Allowed: true,
			}
		}
	}

	clone := pod.DeepCopy()
	whsvr.trySetNodeName(clone, req.Namespace)
	inject(clone, whsvr.ignoreSelectorKeys)
	klog.V(6).Infof("Final obj %+v", clone)
	patch, err := util.CreateJSONPatch(pod, clone)
	klog.Infof("Final patch %+v", string(patch))
	var result metav1.Status
	if err != nil {
		result.Code = 403
		result.Message = err.Error()
	}
	jsonPatch := v1beta1.PatchTypeJSONPatch
	return &v1beta1.AdmissionResponse{
		Allowed:   true,
		Result:    &result,
		Patch:     patch,
		PatchType: &jsonPatch,
	}
}

// Serve method for webhook server
func (whsvr *webhookServer) Serve(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	klog.V(5).Infof("Receive request: %+v", *r)
	if len(body) == 0 {
		klog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		klog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}
	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		klog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		if r.URL.Path == "/mutate" {
			admissionResponse = whsvr.mutate(&ar)
		}
	}
	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		klog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		klog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

func (whsvr *webhookServer) trySetNodeName(pod *corev1.Pod, ns string) {
	if pod.Spec.Volumes == nil {
		return
	}
	nodeName := ""
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		pvc, err := whsvr.pvcLister.PersistentVolumeClaims(ns).Get(volume.PersistentVolumeClaim.ClaimName)
		if err != nil || pvc == nil {
			continue
		}
		if pvc.Annotations == nil {
			continue
		}
		if len(pvc.Annotations[util.SelectedNodeKey]) != 0 {
			nodeName = pvc.Annotations[util.SelectedNodeKey]
		}
	}
	if len(nodeName) != 0 {
		pod.Spec.NodeName = nodeName
		klog.Infof("Set desired node name to %v ", nodeName)
	}
	return
}

func inject(pod *corev1.Pod, ignoreKeys []string) {
	nodeSelector := make(map[string]string)
	var affinity *corev1.Affinity
	if len(pod.Spec.NodeSelector) == 0 &&
		pod.Spec.Affinity == nil &&
		pod.Spec.Tolerations == nil {
		return
	}

	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		affinity = injectAffinity(pod.Spec.Affinity, ignoreKeys)
	}

	if pod.Spec.NodeSelector != nil {
		nodeSelector = injectNodeSelector(pod.Spec.NodeSelector, ignoreKeys)
	}

	cns := util.ClustersNodeSelection{
		NodeSelector: nodeSelector,
		Affinity:     affinity,
		Tolerations:  pod.Spec.Tolerations,
	}
	cnsByte, err := json.Marshal(cns)
	if err != nil {
		return
	}
	if len(cnsByte) == 0 {
		return
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[util.SelectorKey] = string(cnsByte)

	pod.Spec.Tolerations = getPodTolerations(pod)
}

func getPodTolerations(pod *corev1.Pod) []corev1.Toleration {
	var notReady, unSchedulable bool
	tolerations := make([]corev1.Toleration, 0)
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Key == util.TaintNodeNotReady {
			notReady = true
		}
		if toleration.Key == util.TaintNodeUnreachable {
			unSchedulable = true
		}

		if _, found := desiredMap[toleration.Key]; found {
			tolerations = append(tolerations, desiredMap[toleration.Key])
			continue
		}
		tolerations = append(tolerations, toleration)
	}
	if !notReady {
		tolerations = append(tolerations, desiredMap[util.TaintNodeNotReady])
	}
	if !unSchedulable {
		tolerations = append(tolerations, desiredMap[util.TaintNodeUnreachable])
	}
	return tolerations
}

// injectNodeSelector reserve  ignoreLabels in nodeSelector, others would be removed
func injectNodeSelector(nodeSelector map[string]string, ignoreLabels []string) map[string]string {
	nodeSelectorBackup := make(map[string]string)
	finalNodeSelector := make(map[string]string)
	labelMap := make(map[string]string)
	for _, v := range ignoreLabels {
		labelMap[v] = v
	}
	for k, v := range nodeSelector {
		nodeSelectorBackup[k] = v
	}
	for k, v := range nodeSelector {
		// not found in label, delete
		if labelMap[k] != "" {
			continue
		}
		delete(nodeSelector, k)
		finalNodeSelector[k] = v
	}
	return finalNodeSelector
}

func injectAffinity(affinity *corev1.Affinity, ignoreLabels []string) *corev1.Affinity {
	labelMap := make(map[string]string)
	for _, v := range ignoreLabels {
		labelMap[v] = v
	}
	if affinity.NodeAffinity == nil {
		return nil
	}
	if affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return nil
	}
	required := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if required == nil {
		return nil
	}
	requiredCopy := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.DeepCopy()
	var nodeSelectorTerm []corev1.NodeSelectorTerm
	for termIdx, term := range requiredCopy.NodeSelectorTerms {
		var mes, mfs []corev1.NodeSelectorRequirement
		var mesDeleteCount, mfsDeleteCount int
		for meIdx, me := range term.MatchExpressions {
			if labelMap[me.Key] != "" {
				// found key, do not delete
				continue
			}
			mes = append(mes, *me.DeepCopy())

			required.
				NodeSelectorTerms[termIdx].MatchExpressions = append(required.
				NodeSelectorTerms[termIdx].MatchExpressions[:meIdx-mesDeleteCount], required.
				NodeSelectorTerms[termIdx].MatchExpressions[meIdx-mesDeleteCount+1:]...)
			mesDeleteCount++
		}

		for mfIdx, mf := range term.MatchFields {
			if labelMap[mf.Key] != "" {
				// found key, do not delete
				continue
			}

			mfs = append(mfs, *mf.DeepCopy())
			required.
				NodeSelectorTerms[termIdx].MatchFields = append(required.
				NodeSelectorTerms[termIdx].MatchFields[:mfIdx-mesDeleteCount],
				required.NodeSelectorTerms[termIdx].MatchFields[mfIdx-mfsDeleteCount+1:]...)
			mfsDeleteCount++
		}
		if len(mfs) != 0 || len(mes) != 0 {
			nodeSelectorTerm = append(nodeSelectorTerm, corev1.NodeSelectorTerm{MatchFields: mfs, MatchExpressions: mes})
		}
	}

	filterdTerms := make([]corev1.NodeSelectorTerm, 0)
	for _, term := range required.NodeSelectorTerms {
		if len(term.MatchFields) == 0 && len(term.MatchExpressions) == 0 {
			continue
		}
		filterdTerms = append(filterdTerms, term)
	}
	if len(filterdTerms) == 0 {
		required = nil
	} else {
		required.NodeSelectorTerms = filterdTerms
	}
	affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = required
	if len(nodeSelectorTerm) == 0 {
		return nil
	}
	return &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: nodeSelectorTerm},
	}}
}
