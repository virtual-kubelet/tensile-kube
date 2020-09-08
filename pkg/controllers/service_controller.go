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
	"fmt"
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

// ServiceController is a controller sync service and endpoints from master cluster to client cluster
type ServiceController struct {
	master                      kubernetes.Interface
	client                      kubernetes.Interface
	eventRecorder               record.EventRecorder
	serviceQueue                workqueue.RateLimitingInterface
	endpointsQueue              workqueue.RateLimitingInterface
	serviceLister               corelisters.ServiceLister
	serviceListerSynced         cache.InformerSynced
	endpointsLister             corelisters.EndpointsLister
	endpointsListerSynced       cache.InformerSynced
	clientServiceLister         corelisters.ServiceLister
	clientServiceListerSynced   cache.InformerSynced
	clientEndpointsLister       corelisters.EndpointsLister
	clientEndpointsListerSynced cache.InformerSynced

	nsLister corelisters.NamespaceLister
}

// NewServiceController returns a new *ServiceController
func NewServiceController(master kubernetes.Interface, client kubernetes.Interface,
	serviceInformer coreinformers.ServiceInformer, endpointsInformer coreinformers.EndpointsInformer,
	clientServiceInformer coreinformers.ServiceInformer, clientEndpointsInformer coreinformers.EndpointsInformer,
	nsLister corelisters.NamespaceLister,
	serviceRateLimiter, endpointsRateLimiter workqueue.RateLimiter) Controller {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: master.CoreV1().Events(v1.NamespaceAll)})
	var eventRecorder record.EventRecorder
	eventRecorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "virtual-kubelet"})
	ctrl := &ServiceController{
		master:         master,
		client:         client,
		eventRecorder:  eventRecorder,
		nsLister:       nsLister,
		serviceQueue:   workqueue.NewNamedRateLimitingQueue(serviceRateLimiter, "vk service controller"),
		endpointsQueue: workqueue.NewNamedRateLimitingQueue(endpointsRateLimiter, "vk endpoints controller"),
	}
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.serviceAdded,
		UpdateFunc: ctrl.serviceUpdated,
		DeleteFunc: ctrl.serviceAdded,
	})
	ctrl.serviceLister = serviceInformer.Lister()
	ctrl.serviceListerSynced = serviceInformer.Informer().HasSynced
	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.endpointsAdded,
		UpdateFunc: ctrl.endpointsUpdated,
		DeleteFunc: ctrl.endpointsAdded,
	})
	ctrl.endpointsLister = endpointsInformer.Lister()
	ctrl.endpointsListerSynced = endpointsInformer.Informer().HasSynced

	ctrl.clientServiceLister = clientServiceInformer.Lister()
	ctrl.clientServiceListerSynced = clientServiceInformer.Informer().HasSynced
	ctrl.clientEndpointsLister = clientEndpointsInformer.Lister()
	ctrl.clientEndpointsListerSynced = clientEndpointsInformer.Informer().HasSynced
	return ctrl
}

// Run starts and listens on channel events
func (ctrl *ServiceController) Run(workers int, stopCh <-chan struct{}) {
	defer ctrl.serviceQueue.ShutDown()
	defer ctrl.endpointsQueue.ShutDown()
	klog.Infof("Starting controller")
	defer klog.Infof("Shutting controller")
	if !cache.WaitForCacheSync(stopCh, ctrl.serviceListerSynced, ctrl.endpointsListerSynced) {
		klog.Errorf("Cannot sync caches from master")
		return
	}
	klog.Infof("Sync caches from master successfully")
	if !cache.WaitForCacheSync(stopCh, ctrl.clientServiceListerSynced, ctrl.clientEndpointsListerSynced) {
		klog.Errorf("Cannot sync caches from client")
		return
	}
	klog.Infof("Sync caches from client successfully")

	go ctrl.runGC(stopCh)
	for i := 0; i < workers; i++ {
		go wait.Until(ctrl.syncService, 0, stopCh)
		go wait.Until(ctrl.syncEndpoints, 0, stopCh)
	}
	<-stopCh
}

// serviceAdded reacts to a VolumeAttachment creation
func (ctrl *ServiceController) serviceAdded(obj interface{}) {
	service := obj.(*v1.Service)
	if ctrl.shouldEnqueue(&service.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Info("enqueue service add", "key ", key)
		ctrl.serviceQueue.Add(key)
	} else {
		klog.V(6).Infof("Ignoring service %q add", service.Name)
	}
}

// serviceUpdated reacts to a VolumeAttachment update
func (ctrl *ServiceController) serviceUpdated(old, new interface{}) {
	newService := new.(*v1.Service)
	if ctrl.shouldEnqueue(&newService.ObjectMeta) && IsObjectGlobal(&newService.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(new)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.serviceQueue.Add(key)
		klog.V(6).Info("Service update", "key ", key)
	} else {
		klog.V(6).Infof("Ignoring service %q change", newService.Name)
	}
}

// endpointsAdded reacts to a Endpoints creation
func (ctrl *ServiceController) endpointsAdded(obj interface{}) {
	endpoints := obj.(*v1.Endpoints)
	if ctrl.shouldEnqueue(&endpoints.ObjectMeta) {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Info("enqueue endpoint add ", "key ", key)
		ctrl.endpointsQueue.Add(key)
	} else {
		klog.V(6).Infof("Ignoring endpoints %q change", endpoints.Name)
	}
}

// endpointsUpdated reacts to a Endpoints update
func (ctrl *ServiceController) endpointsUpdated(old, new interface{}) {

	newEndpoints := new.(*v1.Endpoints)
	if ctrl.shouldEnqueue(&newEndpoints.ObjectMeta) && IsObjectGlobal(&newEndpoints.ObjectMeta) {
		klog.Error(newEndpoints)
		key, err := cache.MetaNamespaceKeyFunc(new)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		ctrl.endpointsQueue.Add(key)
		klog.V(6).Infof("endpoints update, enqueue endpoints: %v", key)
	} else {
		klog.V(6).Infof("Ignoring endpoints %q change", newEndpoints.Name)
	}
}

// syncService deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *ServiceController) syncService() {
	keyObj, quit := ctrl.serviceQueue.Get()
	if quit {
		return
	}
	defer ctrl.serviceQueue.Done(keyObj)
	key := keyObj.(string)
	namespace, serviceName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.serviceQueue.Forget(key)
		return
	}
	klog.V(4).Infof("Started service processing %q", serviceName)

	if err = ensureNamespace(namespace, ctrl.client, ctrl.nsLister); err != nil {
		ctrl.serviceQueue.AddRateLimited(key)
		klog.Errorf("Create role in client cluster failed, error: %v", err)
		return
	}
	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.serviceQueue.AddRateLimited(key)
			return
		}
		ctrl.serviceQueue.Forget(key)
	}()
	var service *v1.Service
	service, err = ctrl.serviceLister.Services(namespace).Get(serviceName)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			err = fmt.Errorf("get service from master cluster failed, error: %v", err)
			return
		}
		service, err = ctrl.master.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
		if err != nil {
			if !apierrs.IsNotFound(err) {
				err = fmt.Errorf("get service from master cluster failed, error: %v", err)
				return
			}
			if err = ctrl.client.CoreV1().Services(namespace).Delete(serviceName,
				&metav1.DeleteOptions{}); err != nil {
				if !apierrs.IsNotFound(err) {
					klog.Errorf("Delete service in client cluster failed, error: %v", err)
					return
				}
				err = nil
			}
			klog.V(3).Infof("Service %q deleted", serviceName)
			return
		}
	}

	if service.DeletionTimestamp != nil {
		if err = ctrl.client.CoreV1().Services(namespace).Delete(serviceName,
			&metav1.DeleteOptions{}); err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Delete service in client cluster failed, error: %v", err)
				return
			}
			err = nil
		}
		klog.V(3).Infof("Service %q deleted", serviceName)
		return
	}

	klog.V(4).Infof("Service %v/%v to be update or create", namespace, serviceName)
	ctrl.syncServiceHandler(service)
}

// syncEndpoints deals with one key off the queue.  It returns false when it's time to quit.
func (ctrl *ServiceController) syncEndpoints() {
	keyObj, quit := ctrl.endpointsQueue.Get()
	if quit {
		return
	}
	defer ctrl.endpointsQueue.Done(keyObj)
	key := keyObj.(string)
	namespace, endpointsName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		ctrl.endpointsQueue.Forget(key)
		return
	}
	klog.V(4).Infof("Started endpoints processing %q/%q", namespace, endpointsName)

	if err = ensureNamespace(namespace, ctrl.client, ctrl.nsLister); err != nil {
		ctrl.endpointsQueue.AddRateLimited(key)
		klog.Errorf("Create role in client cluster failed, error: %v", err)
		return
	}

	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.endpointsQueue.AddRateLimited(key)
			return
		}
		ctrl.endpointsQueue.Forget(key)
	}()

	// get endpoints to process
	endpoints, err := ctrl.endpointsLister.Endpoints(namespace).Get(endpointsName)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			err = fmt.Errorf("get endpoints from master cluster failed, error: %v", err)
			return
		}
		endpoints, err = ctrl.master.CoreV1().Endpoints(namespace).Get(endpointsName, metav1.GetOptions{})
		if err != nil {
			if !apierrs.IsNotFound(err) {
				return
			}
			if err = ctrl.client.CoreV1().Endpoints(namespace).Delete(endpointsName,
				&metav1.DeleteOptions{}); err != nil {
				if !apierrs.IsNotFound(err) {
					klog.Errorf("Delete endpoint in client cluster failed, error: %v", err)
					return
				}
				err = nil
			}
			// endpoints was deleted in the meantime, ignore.
			klog.V(3).Infof("Endpoints %q deleted", endpointsName)
			return
		}
	}

	if endpoints.DeletionTimestamp != nil {
		if err = ctrl.client.CoreV1().Endpoints(namespace).Delete(endpointsName,
			&metav1.DeleteOptions{}); err != nil {
			if !apierrs.IsNotFound(err) {
				klog.Errorf("Delete service in client cluster failed, error: %v", err)
				return
			}
			err = nil
		}
		klog.V(3).Infof("Endpoints %q deleted", endpointsName)
		return
	}

	klog.V(4).Infof("Endpoints %v/%v to be update or create", namespace, endpointsName)
	ctrl.syncEndpointsHandler(endpoints)
}
func (ctrl *ServiceController) syncServiceHandler(service *v1.Service) {
	key, err := cache.MetaNamespaceKeyFunc(service)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.serviceQueue.AddRateLimited(key)
			return
		}
	}()
	var serviceInSub *v1.Service
	serviceInSub, err = ctrl.clientServiceLister.Services(service.Namespace).Get(service.Name)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		serviceInSub = service.DeepCopy()
		if err = filterService(serviceInSub); err != nil {
			return
		}
		serviceInSub, err = ctrl.client.CoreV1().Services(service.Namespace).Create(serviceInSub)
		if err != nil || serviceInSub == nil {
			err = fmt.Errorf("Create service %v in client cluster failed, error: %v", key, err)
			return
		}
		err = nil
		klog.Infof("Create service %v in client cluster success", key)
		return
	}

	serviceCopy := service.DeepCopy()
	if err = filterService(serviceCopy); err != nil {
		return
	}
	serviceCopy.ResourceVersion = serviceInSub.ResourceVersion
	serviceCopy.Spec.ClusterIP = serviceInSub.Spec.ClusterIP
	klog.V(5).Infof("Old service %+v\n, new %+v", serviceInSub, serviceCopy)
	if _, err = ctrl.patchService(serviceInSub, serviceCopy); err != nil {
		return
	}
	klog.V(4).Infof("Handler service: finished processing %q", service.Name)
}
func (ctrl *ServiceController) syncEndpointsHandler(endpoints *v1.Endpoints) {
	key, err := cache.MetaNamespaceKeyFunc(endpoints)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	defer func() {
		if err != nil {
			klog.Error(err)
			ctrl.endpointsQueue.AddRateLimited(key)
			return
		}
	}()

	var endpointsInSub *v1.Endpoints
	endpointsInSub, err = ctrl.clientEndpointsLister.Endpoints(endpoints.Namespace).Get(endpoints.Name)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return
		}
		endpointsInSub = endpoints.DeepCopy()
		filterCommon(&endpointsInSub.ObjectMeta)
		endpointsInSub, err = ctrl.client.CoreV1().Endpoints(endpoints.Namespace).Create(endpointsInSub)
		if err != nil || endpointsInSub == nil {
			err = fmt.Errorf("Create endpoints in client cluster failed, error: %v", err)
			return
		}
		err = nil
		klog.Infof("Create endpoints %v in client cluster success", key)
		return
	}

	endpointsCopy := endpoints.DeepCopy()
	filterCommon(&endpointsCopy.ObjectMeta)
	endpointsCopy.ResourceVersion = endpointsInSub.ResourceVersion
	klog.V(5).Infof("Old endpoints %+v\n, new %+v", endpointsInSub, endpointsCopy)
	if _, err = ctrl.patchEndpoints(endpointsInSub, endpointsCopy); err != nil {
		return
	}
	klog.V(4).Infof("Handler endpoints: finished processing %q", endpointsInSub.Name)
}
func (ctrl *ServiceController) patchService(service, clone *v1.Service) (*v1.Service, error) {
	if reflect.DeepEqual(service.Spec, clone.Spec) &&
		reflect.DeepEqual(service.Status, clone.Status) {
		return service, nil
	}
	if !CheckGlobalLabelEqual(&service.ObjectMeta, &clone.ObjectMeta) {
		return service, nil
	}

	patch, err := util.CreateMergePatch(service, clone)
	if err != nil {
		return service, err
	}
	newService, err := ctrl.client.CoreV1().Services(service.Namespace).Patch(service.Name,
		mergetypes.MergePatchType, patch)
	if err != nil {
		return service, err
	}
	return newService, nil
}
func (ctrl *ServiceController) patchEndpoints(endpoints, clone *v1.Endpoints) (*v1.Endpoints, error) {
	if reflect.DeepEqual(endpoints.Subsets, clone.Subsets) {
		return endpoints, nil
	}
	if !CheckGlobalLabelEqual(&endpoints.ObjectMeta, &clone.ObjectMeta) {
		return endpoints, nil
	}

	patch, err := util.CreateMergePatch(endpoints, clone)
	if err != nil {
		return endpoints, err
	}
	newEndpoints, err := ctrl.client.CoreV1().Endpoints(endpoints.Namespace).Patch(endpoints.Name,
		mergetypes.MergePatchType,
		patch)
	if err != nil {
		return endpoints, err
	}
	return newEndpoints, nil
}

func (ctrl *ServiceController) shouldEnqueue(obj *metav1.ObjectMeta) bool {
	if obj.Namespace == metav1.NamespaceSystem {
		return false
	}
	if obj.Name == "kubernetes" {
		return false
	}
	return true
}

func (ctrl *ServiceController) gc() {
	serviceList, err := ctrl.clientServiceLister.List(labels.Everything())
	if err != nil {
		klog.Error(err)
		return
	}
	for _, service := range serviceList {
		if service == nil {
			continue
		}
		if !ctrl.shouldEnqueue(&service.ObjectMeta) {
			continue
		}
		if !IsObjectGlobal(&service.ObjectMeta) {
			continue
		}
		_, err = ctrl.serviceLister.Services(service.Namespace).Get(service.Name)
		if err != nil && apierrs.IsNotFound(err) {
			err := ctrl.client.CoreV1().Services(service.Namespace).Delete(service.Name, &metav1.DeleteOptions{})
			if err != nil && !apierrs.IsNotFound(err) {
				klog.Error(err)
			}
			continue
		}
	}
}

func (ctrl *ServiceController) runGC(stopCh <-chan struct{}) {
	wait.Until(ctrl.gc, 3*time.Minute, stopCh)
}
