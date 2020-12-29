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

package controllers

import (
	"encoding/json"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

func ensureNamespace(ns string, client kubernetes.Interface, nsLister corelisters.NamespaceLister) error {
	_, err := nsLister.Get(ns)
	if err == nil {
		return nil
	}
	if !apierrs.IsNotFound(err) {
		return err
	}
	if _, err = client.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
		return nil
	}

	return err
}

func filterPVC(pvcInSub *v1.PersistentVolumeClaim, hostIP string) error {
	labelSelector := pvcInSub.Spec.Selector.DeepCopy()
	pvcInSub.Spec.Selector = nil
	util.TrimObjectMeta(&pvcInSub.ObjectMeta)
	SetObjectGlobal(&pvcInSub.ObjectMeta)
	if labelSelector != nil {
		labelStr, err := json.Marshal(labelSelector)
		if err != nil {
			return err
		}
		pvcInSub.Annotations["labelSelector"] = string(labelStr)
	}
	if len(pvcInSub.Annotations[util.SelectedNodeKey]) != 0 {
		pvcInSub.Annotations[util.SelectedNodeKey] = hostIP
	}
	return nil
}

func filterPV(pvInSub *v1.PersistentVolume, hostIP string) {
	util.TrimObjectMeta(&pvInSub.ObjectMeta)
	if pvInSub.Annotations == nil {
		pvInSub.Annotations = make(map[string]string)
	}
	if pvInSub.Spec.NodeAffinity == nil {
		return
	}
	if pvInSub.Spec.NodeAffinity.Required == nil {
		return
	}
	terms := pvInSub.Spec.NodeAffinity.Required.NodeSelectorTerms
	for k, v := range pvInSub.Spec.NodeAffinity.Required.NodeSelectorTerms {
		mf := v.MatchFields
		me := v.MatchExpressions
		for k, val := range v.MatchFields {
			if val.Key == util.HostNameKey || val.Key == util.BetaHostNameKey {
				val.Values = []string{hostIP}
			}
			mf[k] = val
		}
		for k, val := range v.MatchExpressions {
			if val.Key == util.HostNameKey || val.Key == util.BetaHostNameKey {
				val.Values = []string{hostIP}
			}
			me[k] = val
		}
		terms[k].MatchFields = mf
		terms[k].MatchExpressions = me
	}
	pvInSub.Spec.NodeAffinity.Required.NodeSelectorTerms = terms
	return
}

func filterCommon(meta *metav1.ObjectMeta) error {
	util.TrimObjectMeta(meta)
	SetObjectGlobal(meta)
	return nil
}

func filterService(serviceInSub *v1.Service) error {
	labelSelector := serviceInSub.Spec.Selector
	serviceInSub.Spec.Selector = nil
	if serviceInSub.Spec.ClusterIP != "None" {
		serviceInSub.Spec.ClusterIP = ""
	}
	util.TrimObjectMeta(&serviceInSub.ObjectMeta)
	SetObjectGlobal(&serviceInSub.ObjectMeta)
	if labelSelector == nil {
		return nil
	}
	labelStr, err := json.Marshal(labelSelector)
	if err != nil {
		return err
	}
	serviceInSub.Annotations["labelSelector"] = string(labelStr)
	return nil
}

// CheckGlobalLabelEqual checks if two objects both has the global label
func CheckGlobalLabelEqual(obj, clone *metav1.ObjectMeta) bool {
	oldGlobal := IsObjectGlobal(obj)
	if !oldGlobal {
		return false
	}
	newGlobal := IsObjectGlobal(clone)
	if !newGlobal {
		return false
	}
	return true
}

// IsObjectGlobal return if an object is global
func IsObjectGlobal(obj *metav1.ObjectMeta) bool {
	if obj.Annotations == nil {
		return false
	}

	if obj.Annotations[util.GlobalLabel] == "true" {
		return true
	}

	return false
}

// SetObjectGlobal add global annotation to an object
func SetObjectGlobal(obj *metav1.ObjectMeta) {
	if obj.Annotations == nil {
		obj.Annotations = map[string]string{}
	}
	obj.Annotations[util.GlobalLabel] = "true"
}
