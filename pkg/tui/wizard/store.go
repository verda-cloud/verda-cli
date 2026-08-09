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

package wizard

import (
	"maps"
	"sync"
)

// Store is the engine's shared data layer. Views read from it,
// the engine and loaders write to it.
type Store struct {
	collected map[string]any
	data      map[string]any
	mu        sync.RWMutex
	onChange  func(key string, value any) // optional callback when Set is called
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		collected: make(map[string]any),
		data:      make(map[string]any),
	}
}

// Reset clears all collected and arbitrary data.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collected = make(map[string]any)
	s.data = make(map[string]any)
}

// Collected returns a snapshot of the wizard step values.
func (s *Store) Collected() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := make(map[string]any, len(s.collected))
	maps.Copy(snap, s.collected)
	return snap
}

// SetCollected sets a wizard step value.
func (s *Store) SetCollected(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collected[key] = value
}

// ClearCollected removes a wizard step value.
func (s *Store) ClearCollected(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collected, key)
}

// Get reads an arbitrary value from the store.
func (s *Store) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

// Set writes an arbitrary value to the store.
// If the engine has wired up a change callback (via the message bus),
// it broadcasts a StoreChangedMsg to all views.
func (s *Store) Set(key string, value any) {
	s.mu.Lock()
	s.data[key] = value
	cb := s.onChange
	s.mu.Unlock()
	if cb != nil {
		cb(key, value)
	}
}
