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
	b := newCommonController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	configMap := newConfigMap()
	configMap.Data = map[string]string{"test": "test1"}
	configMapCopy := configMap.DeepCopy()
	configMapCopy1 := configMap.DeepCopy()
	configMapCopy1.Annotations = map[string]string{}
	var (
		err error
	)

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
		t.Logf("Running case :%v", c.name)
		success := false
		b.client.CoreV1().ConfigMaps(c.configMap.Namespace).Update(newConfigMap())
		if _, err = b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Update(c.configMap); err != nil {
			t.Fatal(err)
		}

		wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
			configMapCopy, err = b.c.clientConfigMapLister.ConfigMaps(c.configMap.Namespace).Get(configMap.Name)
			if err != nil {
				return false, err
			}
			if reflect.DeepEqual(configMapCopy, newConfigMap()) == c.shouldChanged {
				t.Logf("configMap update satisfied")
				success = true
				return true, nil
			}
			return false, nil
		})
		if !success {
			t.Fatal("configMap update failed")
		}
	}

	close(stopCh)
}

func TestCommonController_RunUpdateSecret(t *testing.T) {
	b := newCommonController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	secret := newSecret()
	secret.StringData = map[string]string{"test": "test1"}
	secretCopy := secret.DeepCopy()
	secretCopy1 := secret.DeepCopy()
	secretCopy1.Annotations = map[string]string{}
	var err error
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
		t.Logf("Running case :%v", c.name)
		success := false
		b.client.CoreV1().Secrets(c.secret.Namespace).Update(newSecret())
		if _, err = b.master.CoreV1().Secrets(c.secret.Namespace).Update(c.secret); err != nil {
			t.Fatal(err)
		}

		wait.Poll(50*time.Millisecond, 1*time.Second, func() (bool, error) {
			secretCopy, err = b.c.clientSecretLister.Secrets(c.secret.Namespace).Get(c.secret.Name)
			if err != nil {
				return false, err
			}
			if reflect.DeepEqual(secretCopy, newSecret()) == c.shouldChanged {
				t.Logf("secret update satisfied")
				success = true
				return true, nil
			}
			return false, nil
		})
		if !success {
			t.Fatal("secret update failed")
		}
	}

	close(stopCh)
}

func TestCommonController_RunDeleteConfigMap(t *testing.T) {
	b := newCommonController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
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
		t.Logf("Running case :%v", c.name)
		delete := false
		err = b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Delete(c.configMap.Name,
			&metav1.DeleteOptions{})
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
			t.Logf("configmap delete satisfied")
		}
		_, err = b.master.CoreV1().ConfigMaps(c.configMap.Namespace).Create(c.configMap)
	}
	close(stopCh)

}

func TestCommonController_RunDeleteSecret(t *testing.T) {
	b := newCommonController()

	stopCh := make(chan struct{})

	b.clientInformer.Start(stopCh)
	b.masterInformer.Start(stopCh)
	go test(b.c, 1, stopCh)
	secret := newSecret()
	secret.StringData = map[string]string{"test": "test1"}
	secret1 := secret.DeepCopy()
	secret1.Annotations = map[string]string{}
	var err error
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
		t.Logf("Running case :%v", c.name)
		delete := false
		err = b.master.CoreV1().Secrets(c.secret.Namespace).Delete(c.secret.Name,
			&metav1.DeleteOptions{})
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
			t.Logf("secret delete satisfied")
		}
		_, err = b.master.CoreV1().Secrets(c.secret.Namespace).Create(c.secret)
	}
	close(stopCh)

}

func newCommonController() *commonTestBase {
	client := fake.NewSimpleClientset(newConfigMap(), newSecret())
	master := fake.NewSimpleClientset(newConfigMap(), newSecret())

	clientInformer := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())
	masterInformer := informers.NewSharedInformerFactory(master, controller.NoResyncPeriodFunc())

	cmInformer := masterInformer.Core().V1().ConfigMaps()
	secretInformer := masterInformer.Core().V1().Secrets()
	clientConfigMapInformer := clientInformer.Core().V1().ConfigMaps()
	clientSecretInformer := clientInformer.Core().V1().Secrets()

	configMapRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	secretRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	controller := NewCommonController(client, cmInformer, secretInformer, clientConfigMapInformer,
		clientSecretInformer, configMapRateLimiter, secretRateLimiter)
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
