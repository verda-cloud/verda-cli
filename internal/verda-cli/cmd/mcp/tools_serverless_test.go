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
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
)

// toolErrorDetails parses the envelope and asserts its code; returns details.
func toolErrorDetails(t *testing.T, res *mcp.CallToolResult, code string) map[string]any {
	t.Helper()
	assertToolErrorCode(t, res, code)
	var env struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	return env.Error.Details
}

// assertFieldError: VALIDATION_ERROR naming field in argument form.
func assertFieldError(t *testing.T, res *mcp.CallToolResult, field string) {
	t.Helper()
	d := toolErrorDetails(t, res, "VALIDATION_ERROR")
	if d["field"] != field {
		t.Fatalf("details.field = %v, want %q (reason: %v)", d["field"], field, d["reason"])
	}
}

// dig walks a decoded JSON document by string keys and int indexes.
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("dig %v: %T is not an object", path, v)
			}
			v = m[k]
		case int:
			arr, ok := v.([]any)
			if !ok || k >= len(arr) {
				t.Fatalf("dig %v: %T is not an array with index %d", path, v, k)
			}
			v = arr[k]
		}
	}
	return v
}

func TestServerlessArgErrorFieldNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err   error
		field string
	}{
		{errors.New("--max-replicas must be >= max(1, --min-replicas)"), "max_replicas"},
		{errors.New("--scale-up-delay must be between 0s and 1h"), "scale_up_delay_seconds"},
		{errors.New("--request-ttl must be between 0s and 24h"), "request_ttl_seconds"},
		{errors.New("--deadline must be between 10m and 168h"), "deadline_seconds"},
		{errors.New("--queue-preset=custom requires --queue-load"), "queue_preset"},
		{&scaling.FieldError{Field: "min-replicas", Message: "must be between 0 and 1000"}, "min_replicas"},
		{errors.New(`deployment name "Bad_Name" must be lowercase`), "name"},
		{errors.New(`container image "nginx:latest" must use a specific tag`), "image"},
		{errors.New(`invalid env name "lower": use uppercase`), "env"},
		{errors.New(`invalid secret mount "a:b": mount path must be absolute`), "secret_mounts"},
		{errors.New("something the SDK said"), "request"},
	}
	for _, tc := range cases {
		res := toolErrorResult(serverlessArgError(tc.err))
		assertFieldError(t, res, tc.field)
	}
}

func TestOptionalIntPtrPresence(t *testing.T) {
	t.Parallel()
	if v, err := optionalIntPtr(map[string]any{}, "x"); err != nil || v != nil {
		t.Fatalf("absent: got %v, %v; want nil, nil", v, err)
	}
	v, err := optionalIntPtr(map[string]any{"x": float64(0)}, "x")
	if err != nil || v == nil || *v != 0 {
		t.Fatalf("explicit zero must be present: got %v, %v", v, err)
	}
	if _, err := optionalIntPtr(map[string]any{"x": "3"}, "x"); err == nil {
		t.Fatal("string must be rejected, not coerced")
	}
	f, err := optionalFloatPtr(map[string]any{"x": 2.5}, "x")
	if err != nil || f == nil || *f != 2.5 {
		t.Fatalf("float: got %v, %v", f, err)
	}
	if _, err := optionalFloatPtr(map[string]any{"x": true}, "x"); err == nil {
		t.Fatal("bool must be rejected")
	}
}
