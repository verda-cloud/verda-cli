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
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

func TestResolveEndpoint_FromCreds(t *testing.T) {
	t.Parallel()
	creds := &options.S3Credentials{Endpoint: "https://custom.example.com"}
	got := resolveEndpoint(creds, "")
	if got != "https://custom.example.com" {
		t.Errorf("got %q, want custom", got)
	}
}

func TestResolveEndpoint_FlagOverride(t *testing.T) {
	t.Parallel()
	creds := &options.S3Credentials{Endpoint: "https://custom.example.com"}
	got := resolveEndpoint(creds, "https://flag.example.com")
	if got != "https://flag.example.com" {
		t.Errorf("flag should override; got %q", got)
	}
}

func TestResolveEndpoint_Default(t *testing.T) {
	t.Parallel()
	got := resolveEndpoint(&options.S3Credentials{}, "")
	if got != DefaultEndpoint {
		t.Errorf("got %q, want default %q", got, DefaultEndpoint)
	}
}

func TestValidateAuthMode(t *testing.T) {
	t.Parallel()
	if err := validateAuthMode("credentials"); err != nil {
		t.Errorf("credentials mode should be accepted, got %v", err)
	}
	if err := validateAuthMode(""); err != nil {
		t.Errorf("empty mode should default-accept, got %v", err)
	}
	if err := validateAuthMode("api"); err == nil {
		t.Error("api mode should return not-yet-supported error")
	}
}
