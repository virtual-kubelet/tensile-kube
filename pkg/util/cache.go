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
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	gochache "github.com/patrickmn/go-cache"
)

// UnschedulableCache contaiens cache ownerid/node/freezeTime
type UnschedulableCache struct {
	cache map[string]*gochache.Cache
	sync.RWMutex
}

// NewUnschedulableCache init a new Unschedulable
func NewUnschedulableCache() *UnschedulableCache {
	return &UnschedulableCache{cache: map[string]*gochache.Cache{}}
}

// Add add node/ownerID to cache
func (c *UnschedulableCache) Add(node, ownerID string) {
	c.Lock()
	defer c.Unlock()
	now := time.Now()
	freezeCache := c.cache[ownerID]
	if freezeCache == nil {
		freezeCache = gochache.New(3*time.Minute, 6*time.Minute)
	}
	freezeCache.Add(node, &now, 0)
	c.cache[ownerID] = freezeCache
}

// GetFreezeNodes return the freezed nodes
func (c *UnschedulableCache) GetFreezeNodes(ownerID string) []string {
	c.Lock()
	defer c.Unlock()
	freezeCache := c.cache[ownerID]
	if freezeCache == nil {
		return nil
	}
	nodes := make([]string, 0)
	for key := range freezeCache.Items() {
		nodes = append(nodes, key)
	}
	return nodes
}

// GetFreezeTime returns node/ownerID freeze time
func (c *UnschedulableCache) GetFreezeTime(node, ownerID string) *time.Time {
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

// CheckValidFunc defines the check func
type CheckValidFunc func(string, string, time.Duration) bool

// ReplacePodNodeNameNodeAffinity replaces the RequiredDuringSchedulingIgnoredDuringExecution
// NodeAffinity of the given affinity with a new NodeAffinity that selects the given nodeName.
// Note that this function assumes that no NodeAffinity conflicts with the selected nodeName.
func ReplacePodNodeNameNodeAffinity(affinity *v1.Affinity, ownerID string, expireTime time.Duration,
	checkFuc CheckValidFunc, nodeNames ...string) (*v1.Affinity,
	int) {
	nodeSelReq := v1.NodeSelectorRequirement{
		// Key:      "metadata.name",
		Key:      "kubernetes.io/hostname",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   nodeNames,
	}

	nodeSelector := &v1.NodeSelector{
		NodeSelectorTerms: []v1.NodeSelectorTerm{
			{
				MatchExpressions: []v1.NodeSelectorRequirement{nodeSelReq},
			},
		},
	}

	count := 1

	if affinity == nil {
		return &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
			},
		}, count
	}

	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
		}
		return affinity, count
	}

	nodeAffinity := affinity.NodeAffinity

	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return affinity, count
	}

	terms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if terms == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []v1.NodeSelectorTerm{
			{
				MatchFields: []v1.NodeSelectorRequirement{nodeSelReq},
			},
		}
		return affinity, count
	}

	newTerms := make([]v1.NodeSelectorTerm, 0)
	for _, term := range terms {
		if term.MatchExpressions == nil {
			continue
		}
		mes := make([]v1.NodeSelectorRequirement, 0)
		for _, me := range term.MatchExpressions {
			if me.Key == nodeSelReq.Key && me.Operator == nodeSelReq.Operator {
				values := make([]string, 0)
				for _, v := range me.Values {
					klog.V(4).Infof("current term value %v", v)
					if checkContains(nodeNames, v) {
						continue
					}
					if checkFuc != nil {
						if !checkFuc(v, ownerID, expireTime) && len(v) > 0 {
							values = append(values, v)
						}
					} else {
						values = append(values, v)
					}
				}
				me.Values = append(values, nodeSelReq.Values...)
				count = len(values)
				mes = append(mes, me)
				continue
			}
			mes = append(mes, me)
		}
		term.MatchExpressions = mes
		newTerms = append(newTerms, term)
	}

	// Replace node selector with the new one.
	nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = newTerms
	affinity.NodeAffinity = nodeAffinity
	return affinity, count
}

func checkContains(exists []string, value string) bool {
	if len(exists) == 0 {
		return false
	}
	for _, v := range exists {
		if v == value {
			return true
		}
	}
	return false
}
