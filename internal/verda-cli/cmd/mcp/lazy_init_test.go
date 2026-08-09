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

package mcp

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// TestLazyClientInitConcurrent reproduces the lazy-init race: mcp-go dispatches
// tool calls on a worker pool, so the first parallel batch (e.g. list_vms +
// get_balance) hits the check-then-set on Server.client concurrently. Under
// -race this is a data race; functionally the factory must run exactly once.
func TestLazyClientInitConcurrent(t *testing.T) {
	t.Parallel()

	client, err := verda.NewClient(
		verda.WithBaseURL("http://127.0.0.1:1"),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	var calls atomic.Int32
	s := NewLazyServer(func() (*verda.Client, error) {
		calls.Add(1)
		// Widen the check-then-set window so parallel first calls overlap.
		time.Sleep(10 * time.Millisecond)
		return client, nil
	})

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.verdaClient()
			if err != nil {
				errs <- err
				return
			}
			if c != client {
				errs <- errors.New("verdaClient returned the wrong client instance")
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("lazy client factory called %d times, want exactly 1", n)
	}
}
