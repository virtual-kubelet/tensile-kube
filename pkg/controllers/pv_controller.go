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
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	mergetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

// PVController is a controller sync pvc and pv from client cluster to master cluster
// PV should be created in client, then sync to master cluster.
// pvc add/delete from master to client
// pvc/pv update、pv add/delete from client to master, this is currently only for `WaitForFirstConsumer`
type PVController struct {
	master                kubernetes.Interface
	client                kubernetes.Interface
	eventRecorder         record.EventRecorder
	pvcClientQueue        workqueue.RateLimitingInterface
	pvClientQueue         workqueue.RateLimitingInterface
	pvcMasterQueue        workqueue.RateLimitingInterface
	pvMasterQueue         workqueue.RateLimitingInterface
	masterPVCLister       corelisters.PersistentVolumeClaimLister
	masterPVCListerSynced cache.InformerSynced
	masterPVLister        corelisters.PersistentVolumeLister
	masterPVListerSynced  cache.InformerSynced

	clientPVCLister       corelisters.PersistentVolumeClaimLister
	clientPVCListerSynced cache.InformerSynced
	clientPVLister        corelisters.PersistentVolumeLister
	clientPVListerSynced  cache.InformerSynced

	hostIP string
}

// NewPVController returns a new *PVController
func NewPVController(master kubernetes.Interface, client kubernetes.Interface,
	pvcInformer coreinformers.PersistentVolumeClaimInformer, pvInformer coreinformers.PersistentVolumeInformer,
	clientPVCInformer coreinformers.PersistentVolumeClaimInformer, clientPVInformer coreinformers.PersistentVolumeInformer,
	pvcRateLimiter, pvRateLimiter workqueue.RateLimiter, hostIP string) Controller {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: master.CoreV1().Events(v1.NamespaceAll)})
	var eventRecorder record.EventRecorder
	eventRecorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "virtual-kubelet"})
	ctrl := &PVController{
		master:         master,
		client:         client,
		eventRecorder:  eventRecorder,
		pvcClientQueue: workqueue.NewNamedRateLimitingQueue(pvcRateLimiter, "vk pvc controller"),
		pvClientQueue:  workqueue.NewNamedRateLimitingQueue(pvRateLimiter, "vk pv controller"),
		pvcMasterQueue: workqueue.NewNamedRateLimitingQueue(pvcRateLimiter, "vk pvc controller"),
		pvMasterQueue:  workqueue.NewNamedRateLimitingQueue(pvRateLimiter, "vk pv controller"),
		hostIP:         hostIP,
	}
	pvcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.pvcInMasterUpdated,
		DeleteFunc: ctrl.pvcInMasterDeleted,
	})
	ctrl.masterPVCLister = pvcInformer.Lister()
	ctrl.masterPVCListerSynced = pvcInformer.Informer().HasSynced
	pvInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: ctrl.pvInMasterDeleted,
	})
	ctrl.masterPVLister = pvInformer.Lister()
	ctrl.masterPVListerSynced = pvInformer.Informer().HasSynced

	clientPVCInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// AddFunc:    ctrl.pvcAdded,
		UpdateFunc: ctrl.pvcInClientUpdated,
		// DeleteFunc: ctrl.pvcAdded,
	})
	ctrl.clientPVCLister = clientPVCInformer.Lister()
	ctrl.clientPVCListerSynced = clientPVCInformer.Informer().HasSynced
	clientPVInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.pvInClientAdded,
		UpdateFunc: ctrl.pvInClientUpdated,
		//DeleteFunc: ctrl.pvAdded,
	})
	ctrl.clientPVLister = clientPVInformer.Lister()
	ctrl.clientPVListerSynced = clientPVInformer.Informer().HasSynced
	return ctrl
}

// Run starts and listens on channel events
func (ctrl *PVController) Run(workers int, stopCh <-chan struct{}) {
	defer ctrl.pvcClientQueue.ShutDown()
	defer ctrl.pvClientQueue.ShutDown()
	defer ctrl.pvcMasterQueue.ShutDown()
	defer ctrl.pvMasterQueue.ShutDown()
	klog.Infof("Starting controller")
	defer klog.Infof("Shutting controller")
	if !cache.WaitForCacheSync(stopCh, ctrl.masterPVListerSynced, ctrl.masterPVCListerSynced) {
		klog.Errorf("Cannot sync caches from master")
		return
	}
	klog.Infof("Sync caches from master successfully")
	if !cache.WaitForCacheSync(stopCh, ctrl.clientPVListerSynced, ctrl.clientPVCListerSynced) {
		klog.Errorf("Cannot sync caches from client")
		return
	}
	klog.Infof("Sync caches from client successfully")
	//go ctrl.runGC(stopCh)
	ctrl.gc()
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.syncPVCStatusFromClient, 0, stopCh)
		go wait.Until(ctrl.syncPVStatusFromClient, 0, stopCh)
		go wait.Until(ctrl.syncPVCFromMaster, 0, stopCh)
		go wait.Until(ctrl.syncPVFromMaster, 0, stopCh)
	}
	<-stopCh
}

// pvcAdded reacts to a PVC creation
func (ctrl *PVController) pvcAdded(obj interface{}) {
	pvc := obj.(*v1.PersistentVolumeClaim)
	if ctrl.shouldEnqueue(&pvc.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Info("Enqueue pvc add", "key ", key)
		ctrl.pvcClientQueue.Add(key)
	} else {
		klog.V(6).Infof("Ignoring pvc %q add", pvc.Name)
	}
}

//pvcInClientUpdated reacts to a PVC update in client cluster
func (ctrl *PVController) pvcInClientUpdated(old, new interface{}) {
	newPVC := new.(*v1.PersistentVolumeClaim)
	if !IsObjectGlobal(&newPVC.ObjectMeta) {
		return
	}

	if ctrl.shouldEnqueue(&newPVC.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(new)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.pvcClientQueue.Add(key)
		klog.V(6).Info("PVC update", "key ", key)
	} else {
		klog.V(6).Infof("Ignoring pvc %q change", newPVC.Name)
	}
}

// pvcInMasterUpdated reacts to a PVC update
func (ctrl *PVController) pvcInMasterUpdated(old, new interface{}) {
	newPVC := new.(*v1.PersistentVolumeClaim)
	if ctrl.shouldEnqueue(&newPVC.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(new)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.pvcMasterQueue.Add(key)
		klog.V(6).Info("PVC update in master", "key ", key)
	} else {
		klog.V(6).Infof("Ignoring pvc %q change", newPVC.Name)
	}
}

// pvcMasterDeleted reacts to a PVC delete
func (ctrl *PVController) pvcInMasterDeleted(obj interface{}) {
	pvc := obj.(*v1.PersistentVolumeClaim)
	if ctrl.shouldEnqueue(&pvc.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(pvc)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.pvcMasterQueue.Add(key)
		klog.V(6).Info("PVC delete", "key ", key)
	} else {
		klog.V(6).Infof("Ignoring pvc %q change", pvc.Name)
	}
}

// pvInClientAdded reacts to a PV creation
func (ctrl *PVController) pvInClientAdded(obj interface{}) {
	pv := obj.(*v1.PersistentVolume)
	if err := ctrl.trySetAnnotation(pv); err != nil {
		return
	}

	if !IsObjectGlobal(&pv.ObjectMeta) {
		return
	}

	if ctrl.shouldEnqueue(&pv.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Info("Enqueue pv add ", "key ", key)
		ctrl.pvClientQueue.Add(key)
	} else {
		klog.V(6).Infof("Ignoring pv %q change", pv.Name)
	}
}

// pvInClientUpdated reacts to a PV update
func (ctrl *PVController) pvInClientUpdated(old, new interface{}) {
	newPV := new.(*v1.PersistentVolume)

	if err := ctrl.trySetAnnotation(newPV); err != nil {
		return
	}

	if !IsObjectGlobal(&newPV.ObjectMeta) {
		return
	}

	if ctrl.shouldEnqueue(&newPV.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(new)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.pvClientQueue.Add(key)
		klog.V(6).Infof("PV update, enqueue pv: %v", key)
	} else {
		klog.V(6).Infof("Ignoring pv %q change", newPV.Name)
	}
}

// pvMasterDelete reacts to a PV update
func (ctrl *PVController) pvInMasterDeleted(obj interface{}) {
	pv := obj.(*v1.PersistentVolume)
	if ctrl.shouldEnqueue(&pv.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(pv)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.pvMasterQueue.Add(key)
		klog.V(6).Infof("PV delete, enqueue pv: %v", key)
	} else {
		klog.V(6).Infof("Ignoring pv %q change", pv.Name)
	}
}

// syncPVCInClient deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *PVController) syncPVCStatusFromClient() {
	keyObj, quit := ctrl.pvcClientQueue.Get()
	if quit {
		return
	}
	defer ctrl.pvcClientQueue.Done(keyObj)
	key := keyObj.(string)
	namespace, pvcName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.pvcClientQueue.Forget(key)
		return
	}
	klog.V(4).Infof("Started pvc processing %q", pvcName)

	defer func() {
		if err != nil {
			ctrl.pvcClientQueue.AddRateLimited(key)
			return
		}
		ctrl.pvcClientQueue.Forget(key)
	}()
	var pvc *v1.PersistentVolumeClaim
	pvc, err = ctrl.clientPVCLister.PersistentVolumeClaims(namespace).Get(pvcName)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			klog.Errorf("Get pvc from client cluster failed, error: %v", err)
			return
		}
		err = nil
		klog.V(3).Infof("PVC %q deleted", pvcName)
		return
	}

	klog.V(4).Infof("PVC %v/%v to be update or create", namespace, pvcName)
	ctrl.syncPVCStatusHandler(pvc)
}

// syncPVCInMaster deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *PVController) syncPVCFromMaster() {
	keyObj, quit := ctrl.pvcMasterQueue.Get()
	if quit {
		return
	}
	defer ctrl.pvcMasterQueue.Done(keyObj)
	key := keyObj.(string)
	namespace, pvcName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.pvcMasterQueue.Forget(key)
		return
	}
	klog.V(4).Infof("Started pvc processing %q", pvcName)

	defer func() {
		if err != nil {
			ctrl.pvcMasterQueue.AddRateLimited(key)
			return
		}
		ctrl.pvcMasterQueue.Forget(key)
	}()
	var pvc *v1.PersistentVolumeClaim
	deletePVCInClient := false
	pvc, err = ctrl.masterPVCLister.PersistentVolumeClaims(namespace).Get(pvcName)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		_, err = ctrl.clientPVCLister.PersistentVolumeClaims(namespace).Get(pvcName)
		if err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Get pvc from master cluster failed, error: %v", err)
				return
			}
			err = nil
			klog.V(3).Infof("PVC %q deleted", pvcName)
			return
		}
		deletePVCInClient = true

	}

	if deletePVCInClient || pvc.DeletionTimestamp != nil {
		if err = ctrl.client.CoreV1().PersistentVolumeClaims(namespace).Delete(pvcName,
			&metav1.DeleteOptions{}); err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Delete pvc from client cluster failed, error: %v", err)
				return
			}
			err = nil
		}
		klog.V(3).Infof("PVC %q deleted", pvcName)
		return
	}

	// capacity updated
	var old *v1.PersistentVolumeClaim
	old, err = ctrl.clientPVCLister.PersistentVolumeClaims(namespace).Get(pvcName)
	if err != nil {
		klog.Errorf("Get pvc from client cluster failed, error: %v", err)
		return
	}

	_, err = ctrl.patchPVC(old, pvc, ctrl.client, false)
	if err != nil {
		klog.Errorf("Get pvc from client cluster failed, error: %v", err)
		return
	}
}

// syncPVInClient deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *PVController) syncPVStatusFromClient() {
	key, quit := ctrl.pvClientQueue.Get()
	if quit {
		return
	}
	defer ctrl.pvClientQueue.Done(key)
	pvName := key.(string)
	klog.V(4).Infof("Started pv processing %q", pvName)
	// get pv to process
	pv, err := ctrl.clientPVLister.Get(pvName)
	defer func() {
		if err != nil {
			ctrl.pvClientQueue.AddRateLimited(key)
			return
		}
		ctrl.pvClientQueue.Forget(key)
	}()
	pvNeedDelete := false
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		err = nil
		pvNeedDelete = true
	}

	if pvNeedDelete || pv.DeletionTimestamp != nil {
		if err = ctrl.master.CoreV1().PersistentVolumes().Delete(pvName,
			&metav1.DeleteOptions{}); err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Delete pvc from master cluster failed, error: %v", err)
				return
			}
			err = nil
		}
		klog.V(3).Infof("PV %q deleted", pvName)
		return
	}

	klog.V(4).Infof("PV %v to be update or create", pvName)
	ctrl.syncPVStatusHandler(pv)
}

// syncPVInMaster deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *PVController) syncPVFromMaster() {
	key, quit := ctrl.pvMasterQueue.Get()
	if quit {
		return
	}
	defer ctrl.pvMasterQueue.Done(key)
	pvName := key.(string)
	klog.V(4).Infof("Started pv processing %q", pvName)
	// get pv to process
	pv, err := ctrl.masterPVLister.Get(pvName)
	defer func() {
		if err != nil {
			ctrl.pvMasterQueue.AddRateLimited(key)
			return
		}
		ctrl.pvMasterQueue.Forget(key)
	}()
	pvNeedDelete := false
	if err != nil {
		if !apierrs.IsNotFound(err) {
			klog.Errorf("Error getting pv %q: %v", pvName, err)
			return
		}
		// Double check
		_, err = ctrl.master.CoreV1().PersistentVolumes().Get(pvName, metav1.GetOptions{})
		if err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Error getting pv %q: %v", pvName, err)
				return
			}
			pvNeedDelete = true
		}

	}

	if pvNeedDelete || pv.DeletionTimestamp != nil {
		if err = ctrl.client.CoreV1().PersistentVolumes().Delete(pvName,
			&metav1.DeleteOptions{}); err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Delete pvc from client cluster failed, error: %v", err)
				return
			}
			err = nil
		}
		klog.V(3).Infof("PV %q deleted", pvName)
		return
	}
}
func (ctrl *PVController) syncPVCStatusHandler(pvc *v1.PersistentVolumeClaim) {
	key, err := cache.MetaNamespaceKeyFunc(pvc)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.pvcClientQueue.AddRateLimited(key)
			return
		}
	}()
	var pvcInMaster *v1.PersistentVolumeClaim
	pvcInMaster, err = ctrl.masterPVCLister.PersistentVolumeClaims(pvc.Namespace).Get(pvc.Name)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		err = nil
		klog.Warningf("pvc %v has been deleted from master client", pvc.Name)
		return
	}

	pvcCopy := pvc.DeepCopy()
	if err = filterPVC(pvcCopy, ctrl.hostIP); err != nil {
		return
	}
	pvcCopy.ResourceVersion = pvcInMaster.ResourceVersion
	klog.V(5).Infof("Old pvc %+v\n, new %+v", pvcInMaster, pvcCopy)
	if _, err = ctrl.patchPVC(pvcInMaster, pvcCopy, ctrl.master, true); err != nil {
		return
	}
	ctrl.eventRecorder.Event(pvcInMaster, v1.EventTypeNormal, "Synced", "status of pvc synced successfully")
	ctrl.pvcClientQueue.Forget(key)
	klog.V(4).Infof("Handler pvc: finished processing %q", pvc.Name)
}
func (ctrl *PVController) syncPVStatusHandler(pv *v1.PersistentVolume) {
	key, err := cache.MetaNamespaceKeyFunc(pv)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.pvClientQueue.AddRateLimited(key)
			return
		}
	}()
	pvCopy := pv.DeepCopy()
	var pvInMaster *v1.PersistentVolume
	pvInMaster, err = ctrl.masterPVLister.Get(pv.Name)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		pvInMaster = pv.DeepCopy()
		filterPV(pvInMaster, ctrl.hostIP)
		if pvCopy.Spec.ClaimRef != nil || pvInMaster.Spec.ClaimRef == nil {
			claim := pvCopy.Spec.ClaimRef
			var newPVC *v1.PersistentVolumeClaim
			newPVC, err = ctrl.masterPVCLister.PersistentVolumeClaims(claim.Namespace).Get(claim.Name)
			if err != nil {
				return
			}
			pvInMaster.Spec.ClaimRef.UID = newPVC.UID
			pvInMaster.Spec.ClaimRef.ResourceVersion = newPVC.ResourceVersion
		}
		pvInMaster, err = ctrl.master.CoreV1().PersistentVolumes().Create(pvInMaster)
		if err != nil || pvInMaster == nil {
			klog.Errorf("Create pv in master cluster failed, error: %v", err)
			return
		}
		ctrl.eventRecorder.Event(pvInMaster, v1.EventTypeNormal, "Synced",
			"pv created when syncing from client cluster successfully")
		klog.Infof("Create pv %v in master cluster success", key)
		return
	}

	filterPV(pvInMaster, ctrl.hostIP)

	if pvCopy.Spec.ClaimRef != nil || pvInMaster.Spec.ClaimRef == nil {
		claim := pvCopy.Spec.ClaimRef
		var newPVC *v1.PersistentVolumeClaim
		newPVC, err = ctrl.masterPVCLister.PersistentVolumeClaims(claim.Namespace).Get(claim.Name)
		if err != nil {
			return
		}
		pvCopy.Spec.ClaimRef.UID = newPVC.UID
		pvCopy.Spec.ClaimRef.ResourceVersion = newPVC.ResourceVersion
	}

	klog.V(5).Infof("Old pv %+v\n, new %+v", pvInMaster, pvCopy)
	if _, err = ctrl.patchPV(pvInMaster, pvCopy, ctrl.master); err != nil {
		return
	}
	ctrl.eventRecorder.Event(pvInMaster, v1.EventTypeNormal, "Synced",
		"pv status synced from client cluster successfully")
	ctrl.pvClientQueue.Forget(key)
	klog.V(4).Infof("Handler pv: finished processing %q", pvInMaster.Name)
}

func (ctrl *PVController) trySetAnnotation(newPV *v1.PersistentVolume) error {
	// add annotation to pv, if pv has bound and pvc is global.
	if newPV.Status.Phase == v1.VolumeBound {
		pvCopy := newPV.DeepCopy()
		if pvCopy.Annotations == nil {
			pvCopy.Annotations = make(map[string]string)
		}
		pvcRef := pvCopy.Spec.ClaimRef
		//try get pvc annotation
		pvc, err := ctrl.clientPVCLister.PersistentVolumeClaims(pvcRef.Namespace).Get(pvcRef.Name)
		if err != nil {
			return err
		}
		if IsObjectGlobal(&pvc.ObjectMeta) {
			SetObjectGlobal(&pvCopy.ObjectMeta)
			newPV, err = ctrl.patchPV(newPV, pvCopy, ctrl.client)
			if err != nil {
				klog.Errorf("Patch pv in client cluster failed, error: %v", err)
				return err
			}
			ctrl.eventRecorder.Event(newPV, v1.EventTypeNormal, "Updated", "global annotation set successfully")
			return nil
		}
		klog.V(5).Infof("Skip set pv annotation for not a global")
	}
	return nil
}

func (ctrl *PVController) patchPVC(pvc, clone *v1.PersistentVolumeClaim,
	client kubernetes.Interface, updateToMaster bool) (*v1.PersistentVolumeClaim, error) {
	if reflect.DeepEqual(pvc.Spec, clone.Spec) &&
		reflect.DeepEqual(pvc.Status, clone.Status) {
		return pvc, nil
	}
	if !CheckGlobalLabelEqual(&pvc.ObjectMeta, &clone.ObjectMeta) {
		if !updateToMaster {
			return pvc, nil
		}
	}

	patch, err := util.CreateMergePatch(pvc, clone)
	if err != nil {
		return pvc, err
	}
	newPVC, err := client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Patch(pvc.Name,
		mergetypes.MergePatchType, patch)
	if err != nil {
		return pvc, err
	}
	return newPVC, nil
}

func (ctrl *PVController) patchPV(pv, clone *v1.PersistentVolume,
	client kubernetes.Interface) (*v1.PersistentVolume, error) {
	if reflect.DeepEqual(pv.Annotations, clone.Annotations) &&
		reflect.DeepEqual(pv.Spec, clone.Spec) &&
		reflect.DeepEqual(pv.Status, clone.Status) {
		return pv, nil
	}

	// seems we should not check, pv create in client cluster, would not bring this at first
	//if !CheckGlobalLabelEqual(&pv.ObjectMeta, &clone.ObjectMeta) {
	//	return pv, nil
	//}

	// dot not change affinity of pv
	clone.Spec.NodeAffinity = pv.Spec.NodeAffinity
	clone.UID = pv.UID
	clone.ResourceVersion = pv.ResourceVersion
	patch, err := util.CreateMergePatch(pv, clone)
	if err != nil {
		return pv, err
	}
	newPV, err := client.CoreV1().PersistentVolumes().Patch(pv.Name,
		mergetypes.MergePatchType,
		patch)
	if err != nil {
		return pv, err
	}
	return newPV, nil
}

func (ctrl *PVController) shouldEnqueue(obj *metav1.ObjectMeta) bool {
	if obj.Namespace == metav1.NamespaceSystem {
		return false
	}
	if obj.Name == "kubernetes" {
		return false
	}

	return true
}

func (ctrl *PVController) shouldEnUpperQueue(old, new *v1.PersistentVolumeClaim) bool {
	if !reflect.DeepEqual(old.Spec.Resources, new.Spec.Resources) {
		return true
	}
	if new.DeletionTimestamp != nil {
		return true
	}
	return false
}

func (ctrl *PVController) gc() {
	pvcs, err := ctrl.clientPVCLister.List(labels.Everything())
	if err != nil {
		klog.Error(err)
		return
	}
	for _, pvc := range pvcs {
		if pvc == nil {
			continue
		}
		if !IsObjectGlobal(&pvc.ObjectMeta) {
			continue
		}
		_, err = ctrl.masterPVCLister.PersistentVolumeClaims(pvc.Namespace).Get(pvc.Name)
		if err != nil && apierrs.IsNotFound(err) {
			err := ctrl.client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(pvc.Name, &metav1.DeleteOptions{})
			if err != nil && !apierrs.IsNotFound(err) {
				klog.Error(err)
			}
			continue
		}
	}
}

func (ctrl *PVController) runGC(stopCh <-chan struct{}) {
	wait.Until(ctrl.gc, 3*time.Minute, stopCh)
}
