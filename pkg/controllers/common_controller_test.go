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
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
)

type commonTestBase struct {
	c              *CommonController
	masterInformer informers.SharedInformerFactory
	clientInformer informers.SharedInformerFactory
	master         *fake.Clientset
	client         *fake.Clientset
}

func TestCommonController_RunUpdateConfigMap(t *testing.T) {
	configMap := newConfigMap()
	configMap.Data = map[string]string{"test": "test1"}
	configMapCopy := configMap.DeepCopy()
	configMapCopy1 := configMap.DeepCopy()
	configMapCopy1.Annotations = map[string]string{}
	cases := []struct {
		name          string
		configMap     *v1.ConfigMap
		shouldChanged bool
	}{
		{
			name:          "should update",
			configMap:     configMapCopy,
			shouldChanged: true,
		},
		{
			name:          "should not update",
			configMap:     configMapCopy1,
			shouldChanged: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newCommonController()
			stopCh := make(chan struct{})
			go test(b.c, 1, stopCh)
			b.clientInformer.Start(stopCh)
			b.masterInformer.Start(stopCh)
			if _, err := b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Update(
				context.TODO(), c.configMap, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err := wait.Poll(10*time.Millisecond, 10*time.Second, func() (bool, error) {
				cm, err := b.c.clientConfigMapLister.ConfigMaps(c.configMap.Namespace).Get(configMap.Name)
				if err != nil {
					return false, nil
				}
				old := newConfigMap()
				t.Logf("New data: %v, old data: %v", cm.Data, old.Data)
				if reflect.DeepEqual(cm.Data, old.Data) != c.shouldChanged {
					t.Logf("New data: %v, old data: %v", cm.Data, old.Data)
					t.Log("configMap update satisfied")
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				t.Error("configMap update failed")
			}
		})
	}
}

func TestCommonController_RunUpdateSecret(t *testing.T) {

	secret := newSecret()
	secret.StringData = map[string]string{"test": "test1"}
	secretCopy := secret.DeepCopy()
	secretCopy1 := secret.DeepCopy()
	secretCopy1.Annotations = map[string]string{}
	cases := []struct {
		name          string
		secret        *v1.Secret
		shouldChanged bool
	}{
		{
			name:          "should update",
			secret:        secretCopy,
			shouldChanged: true,
		},
		{
			name:          "should not update",
			secret:        secretCopy1,
			shouldChanged: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newCommonController()
			stopCh := make(chan struct{})
			go test(b.c, 1, stopCh)
			b.clientInformer.Start(stopCh)
			b.masterInformer.Start(stopCh)
			if _, err := b.master.CoreV1().Secrets(c.secret.Namespace).Update(context.TODO(),
				c.secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err := wait.Poll(10*time.Millisecond, 10*time.Second, func() (bool, error) {
				newSvc, err := b.c.clientSecretLister.Secrets(c.secret.Namespace).Get(c.secret.Name)
				if err != nil {
					return false, nil
				}
				oldSvc := newSecret()
				t.Logf("New data: %v, old data: %v", newSvc.StringData, oldSvc.StringData)
				if reflect.DeepEqual(newSvc.StringData, oldSvc.StringData) != c.shouldChanged {
					t.Log("secret update satisfied")
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				t.Error("secret update failed")
			}
		})
	}
}

func TestCommonController_RunDeleteConfigMap(t *testing.T) {
	ctx := context.TODO()
	cm := newConfigMap()
	cm.Data = map[string]string{"test": "test1"}
	cm1 := cm.DeepCopy()
	cm1.Annotations = map[string]string{}
	var err error
	cases := []struct {
		name      string
		configMap *v1.ConfigMap
		deleted   bool
	}{
		{
			name:      "should not delete",
			configMap: cm1,
			deleted:   false,
		},
		{
			name:      "should delete",
			configMap: cm,
			deleted:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newCommonController()
			stopCh := make(chan struct{})
			go test(b.c, 1, stopCh)
			b.clientInformer.Start(stopCh)
			b.masterInformer.Start(stopCh)
			delete := false
			err = b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Delete(ctx,
				c.configMap.Name, metav1.DeleteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.c.clientConfigMapLister.ConfigMaps(c.configMap.Namespace).Get(c.configMap.Name)
			if err != nil {
				if !errors.IsNotFound(err) {
					t.Fatal(err)
				}
				delete = true
			}
			if delete == c.deleted {
				t.Log("configmap delete satisfied")
			}
			_, err = b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Create(ctx, c.configMap, metav1.CreateOptions{})
		})
	}
}

func TestCommonController_RunDeleteSecret(t *testing.T) {
	ctx := context.TODO()
	secret := newSecret()
	secret.StringData = map[string]string{"test": "test1"}
	secret1 := secret.DeepCopy()
	secret1.Annotations = map[string]string{}
	cases := []struct {
		name    string
		secret  *v1.Secret
		deleted bool
	}{
		{
			name:    "should not delete",
			secret:  secret1,
			deleted: false,
		},
		{
			name:    "should delete",
			secret:  secret,
			deleted: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newCommonController()
			stopCh := make(chan struct{})
			go test(b.c, 1, stopCh)
			b.clientInformer.Start(stopCh)
			b.masterInformer.Start(stopCh)
			delete := false
			err := b.master.CoreV1().Secrets(c.secret.Namespace).Delete(ctx, c.secret.Name,
				metav1.DeleteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.c.clientSecretLister.Secrets(c.secret.Namespace).Get(c.secret.Name)
			if err != nil {
				if !errors.IsNotFound(err) {
					t.Fatal(err)
				}
				delete = true
			}
			if delete == c.deleted {
				t.Log("secret delete satisfied")
			}
			_, err = b.master.CoreV1().Secrets(c.secret.Namespace).Create(ctx,
				c.secret, metav1.CreateOptions{})
		})
	}
}

func newCommonController() *commonTestBase {
	client := fake.NewSimpleClientset(newConfigMap(), newSecret())
	master := fake.NewSimpleClientset(newConfigMap(), newSecret())

	clientInformer := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())
	masterInformer := informers.NewSharedInformerFactory(master, controller.NoResyncPeriodFunc())

	configMapRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	secretRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	controller := NewCommonController(client, masterInformer, clientInformer, configMapRateLimiter, secretRateLimiter)
	c := controller.(*CommonController)
	return &commonTestBase{
		c:              c,
		masterInformer: masterInformer,
		clientInformer: clientInformer,
		master:         master,
		client:         client,
	}
}

func test(c Controller, t int, stopCh chan struct{}) {
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			close(stopCh)
		case <-stopCh:
			return
		}
	}()
	c.Run(t, stopCh)
}

func newConfigMap() *v1.ConfigMap {
	return &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "default",
			Annotations: map[string]string{"global": "true"},
		},
		Data: map[string]string{"test": "test"},
	}
}

func newSecret() *v1.Secret {
	return &v1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "default",
			Annotations: map[string]string{"global": "true"},
		},
	}
}
