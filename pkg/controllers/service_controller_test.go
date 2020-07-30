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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
)

type svcTestBase struct {
	c              *ServiceController
	masterInformer informers.SharedInformerFactory
	clientInformer informers.SharedInformerFactory
	master         *fake.Clientset
	client         *fake.Clientset
}

func TestServiceController_RunAddService(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	time.Sleep(100 * time.Millisecond)
	service := newService()
	service.Spec = v1.ServiceSpec{
		Ports: []v1.ServicePort{
			{
				Name:       "test",
				Protocol:   v1.ProtocolTCP,
				Port:       80,
				TargetPort: intstr.IntOrString{},
			},
		},
		Selector: map[string]string{
			"test": "test",
		},
		ClusterIP: "None",
	}

	serviceCopy := service.DeepCopy()
	serviceCopy.Spec.ClusterIP = "127.17.0.2"
	serviceCopy.Spec.Selector["test"] = "test1"

	cases := []struct {
		name     string
		service  *v1.Service
		ipChange bool
	}{
		{
			name:     "should add ip none",
			service:  service,
			ipChange: false,
		},
		{
			name:     "should add ip empty",
			service:  serviceCopy,
			ipChange: true,
		},
	}
	for _, c := range cases {
		b.master.CoreV1().Services(c.service.Namespace).Delete(c.service.Name,
			metav1.NewDeleteOptions(0))
		b.client.CoreV1().Services(c.service.Namespace).Delete(c.service.Name,
			metav1.NewDeleteOptions(0))
		a := func() {
			t.Logf("Running case :%v", c.name)
			success := false
			if _, err := b.master.CoreV1().Services(c.service.Namespace).Create(c.service); err != nil {
				t.Fatal(err)
			}
			cc, err := b.master.CoreV1().Services(c.service.Namespace).Get(c.service.Name,
				metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			b.masterInformer.Core().V1().Services().Informer().GetStore().Add(cc)
			wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
				serviceCopy, err := b.c.client.CoreV1().Services(c.service.Namespace).Get(c.service.Name,
					metav1.GetOptions{})
				if err != nil {
					t.Error(err)
					return false, nil
				}
				if serviceCopy == nil {
					t.Error("serviceCopy nil")
					return false, nil
				}
				t.Logf("old ip: %v new ip: %v", c.service.Spec.ClusterIP, serviceCopy.Spec.ClusterIP)
				if (serviceCopy.Spec.ClusterIP != c.service.Spec.ClusterIP) == c.ipChange {
					success = true
					return true, nil
				}
				return false, nil
			})
			if !success {
				t.Fatal("service add failed")
			}
		}
		a()
	}

	close(stopCh)
}

func TestServiceController_RunAddEndPoints(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	time.Sleep(100 * time.Millisecond)
	endpoints := newEndPoints()
	endpoints.Subsets = []v1.EndpointSubset{
		{
			Addresses: []v1.EndpointAddress{
				{
					IP: "192.168.1.10",
				},
			},
		},
	}
	endpointsCopy := endpoints.DeepCopy()
	endpointsCopy1 := endpoints.DeepCopy()
	endpointsCopy1.Annotations = map[string]string{}
	var err error
	cases := []struct {
		name        string
		endpoints   *v1.Endpoints
		shouldAdded bool
	}{
		{
			name:        "should add ",
			endpoints:   endpointsCopy,
			shouldAdded: true,
		},
		{
			name:        "should not not",
			endpoints:   endpointsCopy1,
			shouldAdded: false,
		},
	}

	for _, c := range cases {
		b.master.CoreV1().Endpoints(c.endpoints.Namespace).Delete(c.endpoints.Name,
			metav1.NewDeleteOptions(0))
		b.client.CoreV1().Endpoints(c.endpoints.Namespace).Delete(c.endpoints.Name,
			metav1.NewDeleteOptions(0))
		a := func() {
			t.Logf("Running case :%v", c.name)
			success := false
			if _, err := b.master.CoreV1().Endpoints(c.endpoints.Namespace).Create(c.endpoints); err != nil {
				t.Fatal(err)
			}

			wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
				endpointsCopy, err = b.c.clientEndpointsLister.Endpoints(c.endpoints.Namespace).Get(c.endpoints.Name)
				if err != nil {
					return false, err
				}
				if reflect.DeepEqual(endpointsCopy, c.endpoints) == c.shouldAdded {
					t.Logf("endpoints update satisfied")
					success = true
					return true, nil
				}
				return false, nil
			})
			if !success {
				t.Logf("endpoints update failed")
			}
		}
		a()
	}

	close(stopCh)
}

func TestServiceController_RunUpdateService(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	service := newService()
	service.Spec = v1.ServiceSpec{
		Ports: []v1.ServicePort{
			{
				Name:       "test",
				Protocol:   v1.ProtocolTCP,
				Port:       80,
				TargetPort: intstr.IntOrString{},
			},
		},
		Selector: map[string]string{
			"test": "test",
		},
		ClusterIP: "172.10.0.10",
		Type:      v1.ServiceTypeClusterIP,
	}
	serviceCopy := service.DeepCopy()
	serviceCopy.Spec.Selector["test"] = "test1"
	serviceCopy1 := serviceCopy.DeepCopy()
	serviceCopy1.Annotations = map[string]string{}
	var (
		err error
	)

	cases := []struct {
		name          string
		service       *v1.Service
		shouldChanged bool
	}{
		{
			name:          "should update",
			service:       serviceCopy,
			shouldChanged: true,
		},
		{
			name:          "should not update",
			service:       serviceCopy1,
			shouldChanged: false,
		},
		{
			name:          "should not update, existing not global",
			service:       serviceCopy1,
			shouldChanged: false,
		},
	}

	for _, c := range cases {
		t.Logf("Running case: %v", c.name)
		success := false
		if _, err = b.master.CoreV1().Services(c.service.Namespace).Update(c.service); err != nil {
			t.Fatal(err)
		}

		wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
			serviceCopy, err = b.c.clientServiceLister.Services(c.service.Namespace).Get(service.Name)
			if err != nil {
				return false, err
			}
			if reflect.DeepEqual(serviceCopy, newService()) == c.shouldChanged {
				t.Logf("service update satisfied")
				success = true
				return true, nil
			}
			return false, nil
		})
		if !success {
			t.Fatal("service update failed")
		}
	}

	close(stopCh)
}

func TestServiceController_RunUpdateEndPoints(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	endpoints := newEndPoints()
	endpoints.Subsets = []v1.EndpointSubset{
		{
			Addresses: []v1.EndpointAddress{
				{
					IP: "192.168.1.10",
				},
			},
		},
	}
	endpointsCopy := endpoints.DeepCopy()
	endpointsCopy1 := endpoints.DeepCopy()
	endpointsCopy1.Annotations = map[string]string{}
	var err error
	cases := []struct {
		name          string
		endpoints     *v1.Endpoints
		shouldChanged bool
	}{
		{
			name:          "should update",
			endpoints:     endpointsCopy,
			shouldChanged: true,
		},
		{
			name:          "should not update",
			endpoints:     endpointsCopy1,
			shouldChanged: false,
		},
	}

	for _, c := range cases {
		t.Logf("Running case :%v", c.name)
		success := false
		b.client.CoreV1().Endpoints(c.endpoints.Namespace).Update(newEndPoints())
		if _, err = b.master.CoreV1().Endpoints(c.endpoints.Namespace).Update(c.endpoints); err != nil {
			t.Fatal(err)
		}

		wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
			endpointsCopy, err = b.c.clientEndpointsLister.Endpoints(c.endpoints.Namespace).Get(c.endpoints.Name)
			if err != nil {
				return false, err
			}
			if !reflect.DeepEqual(endpointsCopy, newEndPoints()) == c.shouldChanged {
				t.Logf("endpoints update satisfied")
				success = true
				return true, nil
			}
			return false, nil
		})
		if !success {
			t.Fatal("endpoints update failed")
		}
	}

	close(stopCh)
}

func TestServiceController_RunDeleteService(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	service := newService()
	service1 := service.DeepCopy()
	service1.Annotations = map[string]string{}
	var err error
	cases := []struct {
		name    string
		service *v1.Service
		deleted bool
	}{
		{
			name:    "should not delete",
			service: service1,
			deleted: false,
		},
		{
			name:    "should delete",
			service: service,
			deleted: true,
		},
	}

	for _, c := range cases {
		t.Logf("Running case :%v", c.name)
		delete := false
		err = b.master.CoreV1().Services(c.service.Namespace).Delete(c.service.Name,
			&metav1.DeleteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.c.clientServiceLister.Services(c.service.Namespace).Get(c.service.Name)
		if err != nil {
			if !errors.IsNotFound(err) {
				t.Fatal(err)
			}
			delete = true
		}
		if delete == c.deleted {
			t.Logf("configmap delete satisfied")
		}
		_, err = b.master.CoreV1().Services(c.service.Namespace).Create(c.service)
	}
	close(stopCh)

}

func TestServiceController_RunDeleteEndpoints(t *testing.T) {
	b := newServiceController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	endpoints := newEndPoints()
	endpoints1 := endpoints.DeepCopy()
	endpoints1.Annotations = map[string]string{}
	var err error
	cases := []struct {
		name      string
		endpoints *v1.Endpoints
		deleted   bool
	}{
		{
			name:      "should not delete",
			endpoints: endpoints1,
			deleted:   false,
		},
		{
			name:      "should delete",
			endpoints: endpoints,
			deleted:   true,
		},
	}

	for _, c := range cases {
		t.Logf("Running case :%v", c.name)
		delete := false
		err = b.master.CoreV1().Endpoints(c.endpoints.Namespace).Delete(c.endpoints.Name,
			&metav1.DeleteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.c.clientEndpointsLister.Endpoints(c.endpoints.Namespace).Get(c.endpoints.Name)
		if err != nil {
			if !errors.IsNotFound(err) {
				t.Fatal(err)
			}
			delete = true
		}
		if delete == c.deleted {
			t.Logf("endpoints delete satisfied")
		}
		_, err = b.master.CoreV1().Endpoints(c.endpoints.Namespace).Create(c.endpoints)
	}
	close(stopCh)

}

func newServiceController() *svcTestBase {
	client := fake.NewSimpleClientset(newService(), newEndPoints())
	master := fake.NewSimpleClientset(newService(), newEndPoints())

	clientInformer := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())
	masterInformer := informers.NewSharedInformerFactory(master, controller.NoResyncPeriodFunc())

	serviceInformer := masterInformer.Core().V1().Services()
	endPointsInformer := masterInformer.Core().V1().Endpoints()

	nsLister := masterInformer.Core().V1().Namespaces().Lister()

	clientServiceInformer := clientInformer.Core().V1().Services()
	clientEndPointsInformer := clientInformer.Core().V1().Endpoints()

	serviceRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	endPointsRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)

	controller := NewServiceController(master, client, serviceInformer, endPointsInformer, clientServiceInformer,
		clientEndPointsInformer, nsLister, serviceRateLimiter, endPointsRateLimiter)
	c := controller.(*ServiceController)
	return &svcTestBase{
		c:              c,
		masterInformer: masterInformer,
		clientInformer: clientInformer,
		master:         master,
		client:         client,
	}
}

func newService() *v1.Service {
	return &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "default",
			Annotations: map[string]string{"global": "true"},
		},
	}
}

func newEndPoints() *v1.Endpoints {
	return &v1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "default",
			Annotations: map[string]string{"global": "true"},
		},
	}
}
