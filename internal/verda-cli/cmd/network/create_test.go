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

package network

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestCreateFlowCollectsAllSteps(t *testing.T) {
	t.Parallel()
	opts := &createOptions{Mode: "custom"}
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
		wizard.TextResult("prod"),
		wizard.SelectResult(1),
		wizard.ConfirmResult(true),
	))
	if err := engine.Run(context.Background(), buildNetworkCreateFlow(opts)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if opts.Name != "prod" || opts.Mode != "auto" || !opts.Default {
		t.Errorf("opts = %+v", opts)
	}
}

func TestCreateFlowSkipsStepsSetByFlags(t *testing.T) {
	t.Parallel()
	opts := &createOptions{Mode: "auto", modeSet: true, defaultSet: true}
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
		wizard.TextResult("only-name"),
	))
	if err := engine.Run(context.Background(), buildNetworkCreateFlow(opts)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if opts.Name != "only-name" || opts.Mode != "auto" || opts.Default {
		t.Errorf("opts = %+v", opts)
	}
}

func TestCreateFlowRejectsLongName(t *testing.T) {
	t.Parallel()
	if err := validateName(strings.Repeat("x", 65)); err == nil {
		t.Error("65 characters must be rejected")
	}
	if err := validateName("  "); err == nil {
		t.Error("blank must be rejected")
	}
}

func TestCreateFlagsRequestBody(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, agentFactory(client), "create", "--name", "prod")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	posts := api.requestsMatching("POST")
	if len(posts) != 1 {
		t.Fatalf("want 1 POST, got %v", posts)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(posts[0].Body), &body)
	if body["name"] != "prod" || body["mode"] != "custom" {
		t.Errorf("body = %s", posts[0].Body)
	}
	if _, ok := body["is_default"]; ok {
		t.Errorf("is_default must be absent without --default: %s", posts[0].Body)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil || view["name"] != "prod" {
		t.Errorf("stdout = %s (err %v)", out, err)
	}
}

func TestCreateDefaultAndAutoMode(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, tableFactory(client, nil), "create", "--name", "edge", "--mode", "auto", "--default")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	body := api.requestsMatching("POST")[0].Body
	if !strings.Contains(body, `"is_default":true`) || !strings.Contains(body, `"mode":"auto"`) {
		t.Errorf("body = %s", body)
	}
	if !strings.Contains(out, "Created private network edge") || !strings.Contains(out, "default") {
		t.Errorf("out = %q", out)
	}
}

func TestCreateMissingNameIsStructured(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, newFakeAPI())
	factories := map[string]cmdutil.Factory{"agent": agentFactory(client), "off a terminal": tableFactory(client, nil)}
	for name, f := range factories {
		_, _, err := runNetwork(t, f, "create")
		if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
			t.Errorf("%s: code = %q (err %v), want MISSING_REQUIRED_FLAGS", name, code, err)
		}
	}
}

func TestCreateRejectsBadMode(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "create", "--name", "x", "--mode", "hybrid")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", code)
	}
	if len(api.requestsMatching("POST")) != 0 {
		t.Error("no request may be sent for an invalid mode")
	}
}
