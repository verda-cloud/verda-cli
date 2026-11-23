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

package inference

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestModelsListOutputs(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	out, _, err := runInference(t, api, h, runOpts{format: "json"}, "models")
	if err != nil {
		t.Fatal(err)
	}
	var models []verda.InferenceModel
	if err := json.Unmarshal([]byte(out), &models); err != nil || len(models) != 4 {
		t.Fatalf("json: %v (%d models)\n%s", err, len(models), out)
	}
	out, _, err = runInference(t, api, h, runOpts{format: "yaml"}, "models", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "endpoint_model: Qwen/Qwen3-Coder") {
		t.Errorf("yaml must keep snake_case keys:\n%s", out)
	}
	out, _, err = runInference(t, api, h, runOpts{}, "models")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "qwen3-coder") || !strings.Contains(out, "256K") || !strings.Contains(out, "ENDPOINT MODEL") {
		t.Errorf("table:\n%s", out)
	}
}

func TestModelGet(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	out, _, err := runInference(t, api, h, runOpts{}, "models", "get", "kimi-k2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "moonshotai/Kimi-K2") {
		t.Errorf("table:\n%s", out)
	}
	_, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "models", "get")
	if errCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("missing code: %v", err)
	}
}

func TestKeysCreateListDelete(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	out, stderr, err := runInference(t, api, h, runOpts{}, "keys", "create", "--label", "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "dcs_plaintext0001" || !strings.Contains(stderr, "only time the key is shown") {
		t.Errorf("table create: stdout %q stderr %q", out, stderr)
	}

	out, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "keys", "create", "--label", "ci")
	if err != nil {
		t.Fatal(err)
	}
	var created createdKeyView
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.Key != "dcs_plaintext0002" || created.ID != "key-2" {
		t.Fatalf("json create: %v %+v", err, created)
	}

	out, _, err = runInference(t, api, h, runOpts{}, "keys")
	if err != nil || !strings.Contains(out, "my-app") || strings.Contains(out, "dcs_plaintext") {
		t.Errorf("list: %v\n%s", err, out)
	}

	_, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "keys", "delete", "key-1")
	if errCode(err) != "CONFIRMATION_REQUIRED" {
		t.Errorf("delete without --yes in agent mode: %v", err)
	}
	if _, _, err := runInference(t, api, h, runOpts{agent: true, format: "json"}, "keys", "delete", "key-1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if api.keyCount() != 1 {
		t.Errorf("keys left = %d", api.keyCount())
	}
	_, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "keys", "create")
	if errCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("create without label: %v", err)
	}
}

func TestSubscriptionsEnableDisable(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	if _, _, err := runInference(t, api, h, runOpts{}, "subscriptions", "enable", "qwen3-coder", "--tier", "flex"); err != nil {
		t.Fatal(err)
	}
	if len(api.subscribed) != 1 || api.subscribed[0] != "qwen3-coder/flex" {
		t.Errorf("subscribed = %v", api.subscribed)
	}
	_, _, err := runInference(t, api, h, runOpts{}, "subscriptions", "enable", "qwen3-coder", "--tier", "gold")
	if errCode(err) != "VALIDATION_ERROR" {
		t.Errorf("bad tier: %v", err)
	}
	out, _, err := runInference(t, api, h, runOpts{}, "subscriptions")
	if err != nil || !strings.Contains(out, "Inference: active") || !strings.Contains(out, "qwen3-coder") {
		t.Errorf("list: %v\n%s", err, out)
	}
	_, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "subscriptions", "disable", "kimi-k2")
	if errCode(err) != "CONFIRMATION_REQUIRED" {
		t.Errorf("disable without --yes: %v", err)
	}
	out, _, err = runInference(t, api, h, runOpts{agent: true, format: "json"}, "subscriptions", "disable", "kimi-k2", "--yes")
	if err != nil || !strings.Contains(out, `"action": "unsubscribe"`) {
		t.Errorf("disable: %v\n%s", err, out)
	}
	out, _, err = runInference(t, api, h, runOpts{format: "json"}, "subscriptions", "status")
	if err != nil || !strings.Contains(out, `"status": "active"`) {
		t.Errorf("status: %v\n%s", err, out)
	}
}

func TestUsageAndLimits(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	out, _, err := runInference(t, api, h, runOpts{format: "json"}, "usage", "--from", "2026-11-01", "--to", "2026-11-02", "--key", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	var u verda.InferenceUsage
	if err := json.Unmarshal([]byte(out), &u); err != nil || u.Range.Start != "2026-11-01T00:00:00Z" || u.Range.End != "2026-11-02T23:59:59Z" {
		t.Errorf("usage range: %v %+v", err, u.Range)
	}
	_, _, err = runInference(t, api, h, runOpts{}, "usage", "--from", "yesterday")
	if errCode(err) != "VALIDATION_ERROR" {
		t.Errorf("bad date: %v", err)
	}
	out, _, err = runInference(t, api, h, runOpts{}, "usage")
	if err != nil || !strings.Contains(out, "TOTAL") {
		t.Errorf("usage table: %v\n%s", err, out)
	}
	out, _, err = runInference(t, api, h, runOpts{}, "limits")
	if err != nil || !strings.Contains(out, "Project quota tier: 1") || !strings.Contains(out, "unlimited") {
		t.Errorf("limits: %v\n%s", err, out)
	}
	out, _, err = runInference(t, api, h, runOpts{format: "json"}, "limits", "--all-tiers")
	var tiers []verda.InferenceTier
	if err != nil || json.Unmarshal([]byte(out), &tiers) != nil || len(tiers) != 2 {
		t.Errorf("all tiers: %v\n%s", err, out)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://inference.verda.com":      "https://inference.verda.com",
		"https://inference.verda.com/":     "https://inference.verda.com",
		"https://inference.verda.com/v1":   "https://inference.verda.com",
		"http://localhost:8080/v1/":        "http://localhost:8080",
		" https://staging.example/prefix ": "https://staging.example/prefix",
	}
	for in, want := range cases {
		got, err := normalizeBaseURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"inference.verda.com", "ftp://x", ""} {
		if _, err := normalizeBaseURL(bad); err == nil {
			t.Errorf("normalizeBaseURL(%q) accepted", bad)
		}
	}
}
