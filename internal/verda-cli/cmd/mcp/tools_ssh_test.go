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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// newSSHKeyServer wires an MCP Server to a stub replaying an exact
// /ssh-keys body. The SDK's testutil.MockServer cannot stand in: its
// handleGetSSHKeys hardcodes CreatedAt: time.Now(), which is precisely the
// field whose absence is under test.
func newSSHKeyServer(t *testing.T, body string) *Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /ssh-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return NewServer(client)
}

// The MCP surface is where an autonomous reaper actually reads created_at, so
// a zero timestamp here is the one that deletes live keys. Verbatim staging
// body: no created_at key, fingerprint null.
func TestListSSHKeysOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"4d13391d-bdef-49ec-84de-53a2f6174905","name":"meng",` +
		`"key":"ssh-ed25519 AAAA","fingerprint":null}]`
	s := newSSHKeyServer(t, body)

	res, err := s.handleListSSHKeys(context.Background(), callReq("list_ssh_keys", nil))
	if err != nil {
		t.Fatalf("handleListSSHKeys: %v", err)
	}
	got := resultText(t, res)

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("MCP result emits a zero timestamp as data:\n%s", got)
	}
	if strings.Contains(got, "created_at") {
		t.Errorf("created_at present though the API never sent it:\n%s", got)
	}
	if !strings.Contains(got, "4d13391d-bdef-49ec-84de-53a2f6174905") {
		t.Errorf("key id missing:\n%s", got)
	}
}

func TestListSSHKeysKeepsRealCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"k-1","name":"real","key":"ssh-ed25519 AAA","fingerprint":"SHA256:abc",` +
		`"created_at":"2026-08-11T18:51:12.577Z"}]`
	s := newSSHKeyServer(t, body)

	res, err := s.handleListSSHKeys(context.Background(), callReq("list_ssh_keys", nil))
	if err != nil {
		t.Fatalf("handleListSSHKeys: %v", err)
	}
	got := resultText(t, res)

	if !strings.Contains(got, "2026-08-11T18:51:12") {
		t.Errorf("real created_at was dropped:\n%s", got)
	}
	if !strings.Contains(got, "SHA256:abc") {
		t.Errorf("fingerprint was dropped:\n%s", got)
	}
}

// The search filter must keep operating on the SDK values before conversion.
func TestListSSHKeysSearchStillFilters(t *testing.T) {
	t.Parallel()

	body := `[{"id":"k-1","name":"alice","key":"A"},{"id":"k-2","name":"bob","key":"B"}]`
	s := newSSHKeyServer(t, body)

	res, err := s.handleListSSHKeys(context.Background(),
		callReq("list_ssh_keys", map[string]any{"search": "bob"}))
	if err != nil {
		t.Fatalf("handleListSSHKeys: %v", err)
	}
	got := resultText(t, res)

	if !strings.Contains(got, "bob") {
		t.Errorf("search dropped the matching key:\n%s", got)
	}
	if strings.Contains(got, "alice") {
		t.Errorf("search kept a non-matching key:\n%s", got)
	}
	if strings.Contains(got, "0001-01-01") {
		t.Errorf("filtered result still emits a zero timestamp:\n%s", got)
	}
}

// add_ssh_key returns the created key to the agent, so it is the second place a
// zero timestamp can reach a reaper. AddSSHKey POSTs, gets a plain-text id back,
// then re-reads the key — both stubs are needed.
func TestAddSSHKeyOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	const keyID = "4d13391d-bdef-49ec-84de-53a2f6174905"

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("POST /ssh-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(keyID))
	})
	// Re-read after create: same shape as the list endpoint, no created_at.
	mux.HandleFunc("GET /ssh-keys/"+keyID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"` + keyID + `","name":"meng",` +
			`"key":"ssh-ed25519 AAAA","fingerprint":null}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	res, err := NewServer(client).handleAddSSHKey(context.Background(), callReq("add_ssh_key", map[string]any{
		"name":       "meng",
		"public_key": "ssh-ed25519 AAAA",
	}))
	if err != nil {
		t.Fatalf("handleAddSSHKey: %v", err)
	}
	got := resultText(t, res)

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("add_ssh_key emits a zero timestamp as data:\n%s", got)
	}
	if strings.Contains(got, "created_at") {
		t.Errorf("created_at present though the API never sent it:\n%s", got)
	}
	if !strings.Contains(got, keyID) {
		t.Errorf("created key id missing from the result:\n%s", got)
	}
}
