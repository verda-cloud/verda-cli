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
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

// The SDK validates create requests before sending them. A wizard step that
// accepts a value the SDK rejects fails only after the whole wizard, so every
// step must accept exactly the SDK's range.

type limitInput struct {
	input string
	ok    bool
}

func TestContainerWizardStepsMatchSDKLimits(t *testing.T) {
	utilInputs := []limitInput{{"", true}, {"off", true}, {"4", false}, {"5", true}, {"100", true}, {"101", false}}
	delayInputs := []limitInput{{"", true}, {"0s", true}, {"1h", true}, {"1h1s", false}, {"-1s", false}}
	steps := []struct {
		name   string
		step   func(*containerCreateOptions) wizard.Step
		inputs []limitInput
	}{
		{"compute-size", func(o *containerCreateOptions) wizard.Step { return stepComputeSize(&o.ComputeSize) },
			[]limitInput{{"0", false}, {"1", true}, {"128", true}, {"129", false}}},
		{"min-replicas", func(o *containerCreateOptions) wizard.Step { return stepContainerMinReplicas(&o.MinReplicas) },
			[]limitInput{{"-1", false}, {"0", true}, {"3", true}, {"1001", false}}},
		{"max-replicas", func(o *containerCreateOptions) wizard.Step { return stepMaxReplicas(&o.MaxReplicas) },
			[]limitInput{{"0", false}, {"1", true}, {"1000", true}, {"1001", false}}},
		{"concurrency", func(o *containerCreateOptions) wizard.Step { return stepContainerConcurrency(&o.Concurrency) },
			[]limitInput{{"0", false}, {"1", true}, {"5000", true}, {"5001", false}}},
		{"cpu-util", func(o *containerCreateOptions) wizard.Step { return stepContainerCPUUtil(&o.CPUUtil) }, utilInputs},
		{"gpu-util", func(o *containerCreateOptions) wizard.Step { return stepContainerGPUUtil(&o.GPUUtil) }, utilInputs},
		{"scale-up-delay", func(o *containerCreateOptions) wizard.Step { return stepContainerScaleUpDelay(&o.ScaleUpDelay) }, delayInputs},
		{"scale-down-delay", func(o *containerCreateOptions) wizard.Step { return stepContainerScaleDownDelay(&o.ScaleDownDelay) }, delayInputs},
		{"request-ttl", func(o *containerCreateOptions) wizard.Step { return stepContainerRequestTTL(&o.RequestTTL) },
			[]limitInput{{"", true}, {"0s", true}, {"24h", true}, {"24h1s", false}}},
	}
	for _, s := range steps {
		for _, in := range s.inputs {
			t.Run(s.name+"/"+in.input, func(t *testing.T) {
				opts := validOpts()
				step := s.step(opts)
				if err := step.Validate(in.input); (err == nil) != in.ok {
					t.Fatalf("wizard Validate(%q) = %v, want ok=%v", in.input, err, in.ok)
				}
				if !in.ok {
					return
				}
				step.Setter(in.input)
				req, err := opts.request()
				if err != nil {
					t.Fatalf("wizard accepts %q but request() rejects it: %v", in.input, err)
				}
				if err := req.Validate(); err != nil {
					t.Fatalf("wizard accepts %q but SDK rejects it: %v", in.input, err)
				}
			})
		}
	}
}

func TestBatchjobWizardStepsMatchSDKLimits(t *testing.T) {
	steps := []struct {
		name   string
		step   func(*batchjobCreateOptions) wizard.Step
		inputs []limitInput
	}{
		{"compute-size", func(o *batchjobCreateOptions) wizard.Step { return stepComputeSize(&o.ComputeSize) },
			[]limitInput{{"0", false}, {"1", true}, {"128", true}, {"129", false}}},
		{"max-replicas", func(o *batchjobCreateOptions) wizard.Step { return stepMaxReplicas(&o.MaxReplicas) },
			[]limitInput{{"0", false}, {"1", true}, {"1000", true}, {"1001", false}}},
		{"request-ttl", func(o *batchjobCreateOptions) wizard.Step { return stepBatchjobRequestTTL(&o.RequestTTL) },
			[]limitInput{{"", true}, {"0s", true}, {"168h", true}, {"168h1s", false}}},
	}
	for _, s := range steps {
		for _, in := range s.inputs {
			t.Run(s.name+"/"+in.input, func(t *testing.T) {
				opts := validJobOpts()
				step := s.step(opts)
				if err := step.Validate(in.input); (err == nil) != in.ok {
					t.Fatalf("wizard Validate(%q) = %v, want ok=%v", in.input, err, in.ok)
				}
				if !in.ok {
					return
				}
				step.Setter(in.input)
				req, err := opts.request()
				if err != nil {
					t.Fatalf("wizard accepts %q but request() rejects it: %v", in.input, err)
				}
				if err := req.Validate(); err != nil {
					t.Fatalf("wizard accepts %q but SDK rejects it: %v", in.input, err)
				}
			})
		}
	}
}

// Flag-only and agent runs reach the same limits through request(). The CLI
// checks them first so the error names the flag and its range.
func TestContainerFlagsMatchSDKLimits(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*containerCreateOptions)
		errFlag string // empty: request() and the SDK must accept
	}{
		{"compute-size max", func(o *containerCreateOptions) { o.ComputeSize = 128 }, ""},
		{"compute-size over", func(o *containerCreateOptions) { o.ComputeSize = 129 }, "--compute-size"},
		{"replicas max", func(o *containerCreateOptions) { o.MinReplicas, o.MaxReplicas = 1000, 1000 }, ""},
		{"min-replicas over", func(o *containerCreateOptions) { o.MinReplicas, o.MaxReplicas = 1001, 1001 }, "--min-replicas"},
		{"max-replicas over", func(o *containerCreateOptions) { o.MaxReplicas = 1001 }, "--max-replicas"},
		{"concurrency max", func(o *containerCreateOptions) { o.Concurrency = 5000 }, ""},
		{"concurrency over", func(o *containerCreateOptions) { o.Concurrency = 5001 }, "--concurrency"},
		{"cpu-util off", func(o *containerCreateOptions) { o.CPUUtil = 0 }, ""},
		{"cpu-util min", func(o *containerCreateOptions) { o.CPUUtil = 5 }, ""},
		{"cpu-util under", func(o *containerCreateOptions) { o.CPUUtil = 4 }, "--cpu-util"},
		{"gpu-util max", func(o *containerCreateOptions) { o.GPUUtil = 100 }, ""},
		{"gpu-util under", func(o *containerCreateOptions) { o.GPUUtil = 1 }, "--gpu-util"},
		{"scale-up-delay max", func(o *containerCreateOptions) { o.ScaleUpDelay = time.Hour }, ""},
		{"scale-up-delay over", func(o *containerCreateOptions) { o.ScaleUpDelay = time.Hour + time.Second }, "--scale-up-delay"},
		{"scale-down-delay over", func(o *containerCreateOptions) { o.ScaleDownDelay = time.Hour + time.Second }, "--scale-down-delay"},
		{"scale-down-delay negative", func(o *containerCreateOptions) { o.ScaleDownDelay = -time.Second }, "--scale-down-delay"},
		{"request-ttl max", func(o *containerCreateOptions) { o.RequestTTL = 24 * time.Hour }, ""},
		{"request-ttl over", func(o *containerCreateOptions) { o.RequestTTL = 24*time.Hour + time.Second }, "--request-ttl"},
		{"healthcheck-port over", func(o *containerCreateOptions) { o.HealthcheckPort = 65536 }, "--healthcheck-port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := validOpts()
			c.mutate(opts)
			req, err := opts.request()
			if c.errFlag != "" {
				if err == nil || !strings.Contains(err.Error(), c.errFlag) {
					t.Fatalf("want an error naming %s, got %v", c.errFlag, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if err := req.Validate(); err != nil {
				t.Fatalf("CLI accepts but SDK rejects: %v", err)
			}
		})
	}
}

func TestBatchjobFlagsMatchSDKLimits(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*batchjobCreateOptions)
		errFlag string
	}{
		{"compute-size max", func(o *batchjobCreateOptions) { o.ComputeSize = 128 }, ""},
		{"compute-size over", func(o *batchjobCreateOptions) { o.ComputeSize = 129 }, "--compute-size"},
		{"max-replicas max", func(o *batchjobCreateOptions) { o.MaxReplicas = 1000 }, ""},
		{"max-replicas over", func(o *batchjobCreateOptions) { o.MaxReplicas = 1001 }, "--max-replicas"},
		{"request-ttl max", func(o *batchjobCreateOptions) { o.RequestTTL = 168 * time.Hour }, ""},
		{"request-ttl over", func(o *batchjobCreateOptions) { o.RequestTTL = 168*time.Hour + time.Second }, "--request-ttl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := validJobOpts()
			c.mutate(opts)
			req, err := opts.request()
			if c.errFlag != "" {
				if err == nil || !strings.Contains(err.Error(), c.errFlag) {
					t.Fatalf("want an error naming %s, got %v", c.errFlag, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if err := req.Validate(); err != nil {
				t.Fatalf("CLI accepts but SDK rejects: %v", err)
			}
		})
	}
}
