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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func validJobOpts() *batchjobCreateOptions {
	return &batchjobCreateOptions{
		Name:        "nightly-embed",
		Image:       "ghcr.io/org/embedder:v1",
		Compute:     "RTX4500Ada",
		ComputeSize: 1,
		Port:        80,
		MaxReplicas: 3,
		Deadline:    30 * time.Minute,
		RequestTTL:  300 * time.Second,
	}
}

func TestBatchjobRequest_HappyPath(t *testing.T) {
	opts := validJobOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Name != "nightly-embed" {
		t.Errorf("name: got %q", req.Name)
	}
	if req.Compute == nil || req.Compute.Name != "RTX4500Ada" {
		t.Errorf("compute: got %+v", req.Compute)
	}
	if req.Scaling == nil || req.Scaling.DeadlineSeconds != int((30*time.Minute).Seconds()) {
		t.Errorf("deadline: got %+v, want %d", req.Scaling, int((30 * time.Minute).Seconds()))
	}
	if req.Scaling.MaxReplicaCount != 3 {
		t.Errorf("max replicas: got %d", req.Scaling.MaxReplicaCount)
	}
}

func TestBatchjobRequest_RejectsLatest(t *testing.T) {
	opts := validJobOpts()
	opts.Image = "nginx:latest"
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestBatchjobRequest_RequiresDeadline(t *testing.T) {
	opts := validJobOpts()
	opts.Deadline = 0
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestBatchjobMissingFlags(t *testing.T) {
	opts := &batchjobCreateOptions{}
	missing := missingBatchjobCreateFlags(opts)
	want := []string{"--name", "--image", "--compute", "--deadline"}
	if len(missing) != len(want) {
		t.Fatalf("missing: got %v, want %v", missing, want)
	}
	for i, w := range want {
		if missing[i] != w {
			t.Errorf("missing[%d]: got %q, want %q", i, missing[i], w)
		}
	}
}

// TestBatchjobCreate_AgentMode_NoCredsReturnsMissingFlags ensures agent mode validates flags before auth.
func TestBatchjobCreate_AgentMode_NoCredsReturnsMissingFlags(t *testing.T) {
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	ioStreams := cmdutil.IOStreams{In: nil, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runBatchjobCreate(cmd, f, ioStreams, &batchjobCreateOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("expected MISSING_REQUIRED_FLAGS, got code=%q msg=%q", agentErr.Code, agentErr.Message)
	}
	if errors.Is(err, cmdutil.ErrNoClient) {
		t.Fatalf("auth error leaked through — agent-mode flag check must run before VerdaClient: %v", err)
	}
}

func TestBatchjobRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*batchjobCreateOptions)
		wantErr string
	}{
		{"empty name", func(o *batchjobCreateOptions) { o.Name = "" }, "required"},
		{"invalid name", func(o *batchjobCreateOptions) { o.Name = "My_Endpoint" }, "lowercase"},
		{"latest tag", func(o *batchjobCreateOptions) { o.Image = "nginx:latest" }, "latest"},
		{"zero compute size", func(o *batchjobCreateOptions) { o.ComputeSize = 0 }, "compute-size"},
		{"zero max replicas", func(o *batchjobCreateOptions) { o.MaxReplicas = 0 }, "max-replicas"},
		{"bad port", func(o *batchjobCreateOptions) { o.Port = 99999 }, "port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := validJobOpts()
			tc.mutate(opts)
			_, err := opts.request()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestBatchjobRequest_Entrypoint(t *testing.T) {
	opts := validJobOpts()
	opts.Entrypoint = []string{"/bin/sh", "-c"}
	opts.Cmd = []string{"train"}
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	ep := req.Containers[0].EntrypointOverrides
	if ep == nil || !ep.Enabled {
		t.Fatal("expected entrypoint overrides enabled")
	}
	if len(ep.Entrypoint) != 2 || ep.Entrypoint[0] != "/bin/sh" {
		t.Errorf("entrypoint: got %v", ep.Entrypoint)
	}
	if len(ep.Cmd) != 1 || ep.Cmd[0] != "train" {
		t.Errorf("cmd: got %v", ep.Cmd)
	}
}

func TestBatchjobRequest_RegistryCreds(t *testing.T) {
	opts := validJobOpts()
	opts.RegistryCreds = "my-ghcr"
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	rs := req.ContainerRegistrySettings
	if rs == nil || !rs.IsPrivate {
		t.Fatalf("expected IsPrivate=true, got %+v", rs)
	}
	if rs.Credentials == nil || rs.Credentials.Name != "my-ghcr" {
		t.Errorf("creds: got %+v", rs.Credentials)
	}
}

func TestBatchjobRequest_EnvMix(t *testing.T) {
	opts := validJobOpts()
	opts.Env = []string{"HF_HOME=/data/.hf"}
	opts.EnvSecret = []string{"TOKEN=my-secret"}
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	env := req.Containers[0].Env
	if len(env) != 2 {
		t.Fatalf("env count: got %d, want 2", len(env))
	}
	if env[0].Type != envTypePlain {
		t.Errorf("env[0] type: got %q, want %q", env[0].Type, envTypePlain)
	}
	if env[1].Type != envTypeSecret {
		t.Errorf("env[1] type: got %q, want %q", env[1].Type, envTypeSecret)
	}
}

func TestBatchjobRequest_SecretMounts(t *testing.T) {
	opts := validJobOpts()
	opts.SecretMounts = []string{"api-key:/etc/secret/key"}
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	mounts := req.Containers[0].VolumeMounts
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts (scratch + secret), got %d", len(mounts))
	}
	if mounts[0].Type != mountTypeScratch {
		t.Errorf("mount[0]: expected scratch, got %q", mounts[0].Type)
	}
	if mounts[1].Type != mountTypeSecret || mounts[1].SecretName != "api-key" {
		t.Errorf("mount[1]: got %+v", mounts[1])
	}
}
