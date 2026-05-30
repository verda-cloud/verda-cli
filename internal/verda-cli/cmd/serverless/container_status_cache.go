// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package serverless

import (
	"context"
	"sync"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const (
	containerStatusCacheTTL         = 30 * time.Second
	containerStatusFetchConcurrency = 5
	containerStatusUnknown          = "-"
	containerStatusLoading          = "..." // placeholder until LiveList status RPC completes

	// statusRPCTimeout bounds the best-effort describe status call so a slow
	// status endpoint can't consume the parent describe deadline.
	statusRPCTimeout = 5 * time.Second
)

// containerStatusCache: per-name status + TTL because list RPC omits status.
type containerStatusCache struct {
	mu      sync.Mutex
	entries map[string]containerStatusEntry
	ttl     time.Duration
}

type containerStatusEntry struct {
	status    string
	fetchedAt time.Time
}

func newContainerStatusCache(ttl time.Duration) *containerStatusCache {
	return &containerStatusCache{
		entries: make(map[string]containerStatusEntry),
		ttl:     ttl,
	}
}

func (c *containerStatusCache) get(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[name]; ok {
		return e.status
	}
	return ""
}

func (c *containerStatusCache) set(name, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[name] = containerStatusEntry{status: status, fetchedAt: time.Now()}
}

func (c *containerStatusCache) stale(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[name]
	if !ok {
		return true
	}
	return time.Since(e.fetchedAt) > c.ttl
}

func (c *containerStatusCache) anyStale(deployments []verda.ContainerDeployment) bool {
	for i := range deployments {
		if c.stale(deployments[i].Name) {
			return true
		}
	}
	return false
}

// refresh fills missing/stale entries concurrently; errors become "-".
func (c *containerStatusCache) refresh(ctx context.Context, client *verda.Client, deployments []verda.ContainerDeployment) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, containerStatusFetchConcurrency)
	for i := range deployments {
		name := deployments[i].Name
		if !c.stale(name) {
			continue
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s, err := client.ContainerDeployments.GetDeploymentStatus(ctx, name)
			if err != nil || s == nil {
				c.set(name, containerStatusUnknown)
				return
			}
			c.set(name, s.Status)
		}(name)
	}
	wg.Wait()
}
