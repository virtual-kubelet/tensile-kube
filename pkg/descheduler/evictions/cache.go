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

package evictions

import (
	"sync"
	"time"

	gochache "github.com/patrickmn/go-cache"
)

// UnschedulableCache contains cache ownerid/node/freezeTime
type UnschedulableCache struct {
	cache map[string]*gochache.Cache
	sync.RWMutex
}

// NewUnschedulableCache init the cache
func NewUnschedulableCache() *UnschedulableCache {
	return &UnschedulableCache{cache: map[string]*gochache.Cache{}}
}

func (c *UnschedulableCache) add(node, ownerID string) {
	c.Lock()
	defer c.Unlock()
	now := time.Now()
	freezeCache := c.cache[ownerID]
	if freezeCache == nil {
		freezeCache = gochache.New(10*time.Minute, 15*time.Minute)
	}
	freezeCache.Set(node, &now, 0)
	c.cache[ownerID] = freezeCache
}

func (c *UnschedulableCache) getFreezeTime(node, ownerID string) *time.Time {
	c.RLock()
	defer c.RUnlock()
	if c.cache[ownerID] == nil {
		return nil
	}
	timePtr, found := c.cache[ownerID].Get(node)
	if !found {
		return nil
	}

	return timePtr.(*time.Time)
}
