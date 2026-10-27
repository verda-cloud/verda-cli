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
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestStatusCache_GetSetStale(t *testing.T) {
	c := newContainerStatusCache(time.Hour)

	if got := c.get("unknown"); got != "" {
		t.Fatalf("get unknown: got %q, want empty", got)
	}
	if !c.stale("unknown") {
		t.Fatal("stale unknown: expected true")
	}

	c.set("my-api", "running")

	if got := c.get("my-api"); got != "running" {
		t.Fatalf("get my-api: got %q, want \"running\"", got)
	}
	if c.stale("my-api") {
		t.Fatal("stale my-api: expected false with 1h TTL")
	}

	tiny := newContainerStatusCache(time.Nanosecond)
	tiny.set("stale-api", "healthy")
	time.Sleep(time.Millisecond)
	if !tiny.stale("stale-api") {
		t.Fatal("stale stale-api: expected true with 1ns TTL after sleep")
	}
}

func TestStatusCache_AnyStale(t *testing.T) {
	deployments := []verda.ContainerDeployment{
		{Name: "a"},
		{Name: "b"},
	}

	c := newContainerStatusCache(time.Hour)

	if !c.anyStale(deployments) {
		t.Fatal("anyStale: expected true when cache is empty")
	}

	c.set("a", "running")
	if !c.anyStale(deployments) {
		t.Fatal("anyStale: expected true when 'b' is still missing")
	}

	c.set("b", "healthy")
	if c.anyStale(deployments) {
		t.Fatal("anyStale: expected false after all entries set")
	}
}
