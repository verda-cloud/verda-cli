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

package s3

import (
	"context"
	"io"
	"testing"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildConfigureFlowHappyPath(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile: "default",
	}

	flow := buildConfigureFlow(opts)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.TextResult("staging"),                               // profile
			wizard.TextResult("AKIA1234567890EXAMPLE"),                 // access key
			wizard.TextResult("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE"), // secret key
			wizard.TextResult("https://objects.lab.verda.storage"),     // endpoint
			wizard.TextResult("us-east-1"),                             // region
		),
	)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "staging" {
		t.Errorf("Profile = %q, want %q", opts.Profile, "staging")
	}
	if opts.AccessKey != "AKIA1234567890EXAMPLE" {
		t.Errorf("AccessKey = %q, want %q", opts.AccessKey, "AKIA1234567890EXAMPLE")
	}
	if opts.SecretKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE" {
		t.Errorf("SecretKey = %q, want %q", opts.SecretKey, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE")
	}
	if opts.Endpoint != "https://objects.lab.verda.storage" {
		t.Errorf("Endpoint = %q, want %q", opts.Endpoint, "https://objects.lab.verda.storage")
	}
	if opts.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", opts.Region, "us-east-1")
	}
}

func TestBuildConfigureFlowWithPresetFlags(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile:   "prod",
		AccessKey: "AKIA-PRESET",
		Endpoint:  "https://preset.endpoint",
	}

	// Only secret key and region need prompting.
	flow := buildConfigureFlow(opts)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.TextResult("the-secret"), // secret key
			wizard.TextResult("eu-west-1"),  // region
		),
	)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "prod" {
		t.Errorf("Profile = %q, want %q (preset)", opts.Profile, "prod")
	}
	if opts.AccessKey != "AKIA-PRESET" {
		t.Errorf("AccessKey = %q, want %q (preset)", opts.AccessKey, "AKIA-PRESET")
	}
	if opts.SecretKey != "the-secret" {
		t.Errorf("SecretKey = %q, want %q", opts.SecretKey, "the-secret")
	}
	if opts.Endpoint != "https://preset.endpoint" {
		t.Errorf("Endpoint = %q, want %q (preset)", opts.Endpoint, "https://preset.endpoint")
	}
	if opts.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", opts.Region, "eu-west-1")
	}
}
