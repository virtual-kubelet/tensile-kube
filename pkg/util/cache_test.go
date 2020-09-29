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

package util

import (
	"testing"
	"time"

	"github.com/patrickmn/go-cache"
)

func TestCache(t *testing.T) {
	uc := UnschedulableCache{cache: map[string]*cache.Cache{}}
	uc.Add("test", "1")
	time.Sleep(5 * time.Second)
	uc.Add("test", "2")
	ft := uc.GetFreezeTime("test", "1")
	if ft == nil {
		t.Fatal("Unexpected results")
	}
	t.Log(ft)
	ft1 := uc.GetFreezeTime("test", "2")
	if ft1 == nil {
		t.Fatal("Unexpected results")
	}
	t.Log(ft1)
}
