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
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

// changedFunc fakes cobra's Flags().Changed for the named flags.
func changedFunc(names ...string) func(string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) bool { return set[name] }
}

// defaultContainerOpts mirrors the built-in defaults newCmdContainerCreate
// seeds, so "template unset" can be told apart from "template applied".
func defaultContainerOpts() *containerCreateOptions {
	return &containerCreateOptions{
		Port:            defaultExposedPort,
		HealthcheckPath: defaultHealthcheckPath,
		ComputeSize:     1,
		MaxReplicas:     defaultMaxReplicas,
		Concurrency:     defaultConcurrency,
		QueuePreset:     presetBalanced,
		ScaleDownDelay:  defaultScaleDownDelay,
		RequestTTL:      defaultRequestTTL,
	}
}

func fullTemplate() *template.Template {
	two := 2
	return &template.Template{
		Resource: "container",
		Container: &template.ContainerSpec{
			Spot:            true,
			Compute:         "RTX PRO 6000",
			ComputeSize:     2,
			Image:           "ghcr.io/me/llm:v1.2",
			RegistryCreds:   "ghcr",
			Port:            8080,
			HealthcheckPort: 9090,
			HealthcheckPath: "/readyz",
			Env:             map[string]string{"HF_HOME": "/data/.huggingface", "ZED": "last"},
			EnvSecret:       map[string]string{"HF_TOKEN": "hf-token"},
			Entrypoint:      []string{"python"},
			Cmd:             []string{"serve.py"},
			MinReplicas:     &two,
			MaxReplicas:     9,
			Concurrency:     7,
			QueuePreset:     "custom",
			QueueLoad:       42,
			CPUUtil:         60,
			GPUUtil:         85,
			ScaleUpDelay:    "10s",
			ScaleDownDelay:  "2m",
			RequestTTL:      "1m30s",
			SecretMounts:    []string{"hf-token:/etc/hf/token"},
		},
	}
}

// flag > template: a template must never overwrite something typed on the
// command line.
func TestApplyContainerTemplate_FlagWins(t *testing.T) {
	opts := defaultContainerOpts()
	opts.Compute = "H100"
	opts.Port = 3000
	opts.MinReplicas = 3

	err := applyContainerTemplate(fullTemplate(), opts, changedFunc("compute", "port", "min-replicas"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Compute != "H100" || opts.Port != 3000 || opts.MinReplicas != 3 {
		t.Fatalf("flag-owned fields were overwritten: %+v", opts)
	}
	// Un-owned fields still apply.
	if opts.MaxReplicas != 9 || opts.QueueLoad != 42 || !opts.Spot {
		t.Fatalf("template failed to fill un-owned fields: %+v", opts)
	}
}

// template > built-in default.
func TestApplyContainerTemplate_TemplateApplies(t *testing.T) {
	opts := defaultContainerOpts()
	if err := applyContainerTemplate(fullTemplate(), opts, changedFunc()); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"compute", opts.Compute, "RTX PRO 6000"},
		{"compute-size", opts.ComputeSize, 2},
		{"image", opts.Image, "ghcr.io/me/llm:v1.2"},
		{"registry-creds", opts.RegistryCreds, "ghcr"},
		{"port", opts.Port, 8080},
		{"healthcheck-port", opts.HealthcheckPort, 9090},
		{"healthcheck-path", opts.HealthcheckPath, "/readyz"},
		{"spot", opts.Spot, true},
		{"max-replicas", opts.MaxReplicas, 9},
		{"concurrency", opts.Concurrency, 7},
		{"queue-preset", opts.QueuePreset, "custom"},
		{"queue-load", opts.QueueLoad, 42},
		{"cpu-util", opts.CPUUtil, 60},
		{"gpu-util", opts.GPUUtil, 85},
		{"scale-up-delay", opts.ScaleUpDelay, 10 * time.Second},
		{"scale-down-delay", opts.ScaleDownDelay, 2 * time.Minute},
		{"request-ttl", opts.RequestTTL, 90 * time.Second},
		{"env (sorted)", opts.Env, []string{"HF_HOME=/data/.huggingface", "ZED=last"}},
		{"env-secret", opts.EnvSecret, []string{"HF_TOKEN=hf-token"}},
		{"entrypoint", opts.Entrypoint, []string{"python"}},
		{"cmd", opts.Cmd, []string{"serve.py"}},
		{"secret-mounts", opts.SecretMounts, []string{"hf-token:/etc/hf/token"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

// built-in default survives when the template is silent.
func TestApplyContainerTemplate_DefaultsPreserved(t *testing.T) {
	opts := defaultContainerOpts()
	tmpl := &template.Template{Resource: "container", Container: &template.ContainerSpec{Image: "nginx:1.27"}}
	if err := applyContainerTemplate(tmpl, opts, changedFunc()); err != nil {
		t.Fatal(err)
	}
	if opts.Port != defaultExposedPort || opts.MaxReplicas != defaultMaxReplicas ||
		opts.Concurrency != defaultConcurrency || opts.QueuePreset != presetBalanced ||
		opts.ScaleDownDelay != defaultScaleDownDelay || opts.RequestTTL != defaultRequestTTL ||
		opts.ComputeSize != 1 || opts.MinReplicas != 0 {
		t.Fatalf("built-in defaults were disturbed: %+v", opts)
	}
	if opts.Image != "nginx:1.27" {
		t.Fatalf("template field not applied: %+v", opts.Image)
	}
}

// The *int reason: min_replicas 0 is an explicit value, and a flag beats it.
func TestApplyContainerTemplate_MinReplicasZero(t *testing.T) {
	zero := 0

	opts := defaultContainerOpts()
	err := applyContainerTemplate(&template.Template{Resource: "container",
		Container: &template.ContainerSpec{MinReplicas: &zero}}, opts, changedFunc())
	if err != nil {
		t.Fatal(err)
	}
	if opts.MinReplicas != 0 {
		t.Fatalf("min_replicas 0 not applied: %d", opts.MinReplicas)
	}

	opts2 := defaultContainerOpts()
	opts2.MinReplicas = 3 // as if --min-replicas 3
	err = applyContainerTemplate(&template.Template{Resource: "container",
		Container: &template.ContainerSpec{MinReplicas: &zero}}, opts2, changedFunc("min-replicas"))
	if err != nil {
		t.Fatal(err)
	}
	if opts2.MinReplicas != 3 {
		t.Fatalf("flag-owned min-replicas overwritten by template: %d", opts2.MinReplicas)
	}
}

// Malformed durations name the field.
func TestApplyContainerTemplate_BadDurationNamesField(t *testing.T) {
	err := applyContainerTemplate(&template.Template{Resource: "container",
		Container: &template.ContainerSpec{ScaleDownDelay: "soon"}}, defaultContainerOpts(), changedFunc())
	if err == nil || !strings.Contains(err.Error(), "scale_down_delay") {
		t.Fatalf("want error naming scale_down_delay, got %v", err)
	}
}

// Queue preset from the template must not stomp an explicit --queue-load.
func TestApplyContainerTemplate_QueuePresetYieldsToCustomLoad(t *testing.T) {
	opts := defaultContainerOpts()
	opts.QueueLoad = 10
	err := applyContainerTemplate(fullTemplate(), opts, changedFunc("queue-load"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.QueuePreset == "custom" || opts.QueueLoad != 10 {
		t.Fatalf("template queue fields overrode explicit --queue-load: %+v", opts)
	}
}

// needsContainerWizard: a half-filling template still sends the user through
// the wizard for the rest; a full template (plus name, which templates never
// carry) skips it.
func TestNeedsContainerWizard(t *testing.T) {
	opts := defaultContainerOpts()
	if !needsContainerWizard(opts) {
		t.Fatal("empty opts must need the wizard")
	}
	if err := applyContainerTemplate(fullTemplate(), opts, changedFunc()); err != nil {
		t.Fatal(err)
	}
	if !needsContainerWizard(opts) {
		t.Fatal("template without a name must still trigger the wizard")
	}
	opts.Name = "from-template"
	if needsContainerWizard(opts) {
		t.Fatal("fully-filled opts should skip the wizard")
	}
}

// End-to-end in agent mode: --from fills image/compute/scaling from the
// template; the POST body carries them; --compute-size on the CLI wins.
func TestContainerCreate_FromTemplate_Agent(t *testing.T) {
	t.Setenv("VERDA_HOME", t.TempDir())

	two := 2
	tmpl := &template.Template{
		Resource: "container",
		Container: &template.ContainerSpec{
			Compute:        "B200",
			ComputeSize:    1,
			Image:          "ghcr.io/me/llm:v1.2",
			MaxReplicas:    4,
			QueuePreset:    "cost-saver",
			Env:            map[string]string{"HF_HOME": "/data/.huggingface"},
			MinReplicas:    &two,
			ScaleUpDelay:   "10s",
			RequestTTL:     "2m",
			HealthcheckOff: true,
		},
		Description: "from-template e2e",
	}
	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := template.Save(baseDir, "container", "llm-api", tmpl); err != nil {
		t.Fatal(err)
	}

	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)
	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"create", "--from", "llm-api",
		"--name", "cli-from-tpl",
		"--compute-size", "2", // flag beats template's 1
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container create --from failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.containerBody()
	if body == nil {
		t.Fatalf("server did not receive POST /container-deployments\nstderr:\n%s", stderr.String())
	}
	var got struct {
		Compute struct {
			Name string `json:"name"`
			Size int    `json:"size"`
		} `json:"compute"`
		Scaling struct {
			MinReplicaCount int `json:"min_replica_count"`
			MaxReplicaCount int `json:"max_replica_count"`
			Triggers        struct {
				QueueLoad struct {
					Threshold float64 `json:"threshold"`
				} `json:"queue_load"`
			} `json:"scaling_triggers"`
		} `json:"scaling"`
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v\n%s", err, body)
	}
	if got.Compute.Name != "B200" || got.Compute.Size != 2 {
		t.Fatalf("compute: got %+v, want B200 x2 (flag won)", got.Compute)
	}
	if got.Containers[0].Image != "ghcr.io/me/llm:v1.2" {
		t.Fatalf("image: got %q, want template value", got.Containers[0].Image)
	}
	if got.Scaling.MinReplicaCount != 2 || got.Scaling.MaxReplicaCount != 4 {
		t.Fatalf("scaling replicas: got min=%d max=%d, want 2/4", got.Scaling.MinReplicaCount, got.Scaling.MaxReplicaCount)
	}
	if got.Scaling.Triggers.QueueLoad.Threshold != 6 {
		t.Fatalf("queue preset cost-saver: got threshold %v, want 6", got.Scaling.Triggers.QueueLoad.Threshold)
	}
}

// Bare --from in agent mode is an error, not a picker.
func TestContainerCreate_FromTemplate_AgentBareRefErrors(t *testing.T) {
	t.Setenv("VERDA_HOME", t.TempDir())
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)
	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"create", "--from", "--name", "x", "--image", "i:1", "--compute", "B200", "--yes"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--from requires a template name in agent mode") {
		t.Fatalf("want bare --from agent error, got %v", err)
	}
}
