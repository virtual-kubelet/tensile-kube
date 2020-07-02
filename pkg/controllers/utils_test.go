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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

func TestCheckGlobalLabelEqual(t *testing.T) {
	type args struct {
		obj   *metav1.ObjectMeta
		clone *metav1.ObjectMeta
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "label nil",
			args: args{
				obj: &metav1.ObjectMeta{
					Labels:      nil,
					Annotations: nil,
				},
				clone: &metav1.ObjectMeta{
					Labels:      nil,
					Annotations: nil,
				},
			},
			want: false,
		},
		{
			name: "one of labels nil",
			args: args{
				obj: &metav1.ObjectMeta{
					Annotations: map[string]string{util.GlobalLabel: "true"},
				},
				clone: &metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			want: false,
		},
		{
			name: "both labels not nil",
			args: args{
				obj: &metav1.ObjectMeta{
					Annotations: map[string]string{util.GlobalLabel: "true"},
				},
				clone: &metav1.ObjectMeta{
					Annotations: map[string]string{util.GlobalLabel: "true"},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckGlobalLabelEqual(tt.args.obj, tt.args.clone); got != tt.want {
				t.Errorf("CheckGlobalLabelEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsObjectGlobal(t *testing.T) {
	type args struct {
		obj *metav1.ObjectMeta
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "label nil",
			args: args{
				obj: &metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			want: false,
		},
		{
			name: "can not find annotation",
			args: args{
				obj: &metav1.ObjectMeta{
					Annotations: map[string]string{"test": "true"},
				},
			},
			want: false,
		},
		{
			name: "both labels not nil",
			args: args{
				obj: &metav1.ObjectMeta{
					Annotations: map[string]string{util.GlobalLabel: "true"},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsObjectGlobal(tt.args.obj); got != tt.want {
				t.Errorf("IsObjectGlobal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetObjectGlobal(t *testing.T) {
	type args struct {
		obj *metav1.ObjectMeta
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "set global label",
			args: args{
				obj: &metav1.ObjectMeta{
					Labels:      nil,
					Annotations: nil,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetObjectGlobal(tt.args.obj)
			if !IsObjectGlobal(tt.args.obj) {
				t.Fatal("Set Object Global failed")
			}
		})
	}
}
