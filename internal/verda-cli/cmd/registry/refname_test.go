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

package registry

import (
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// vcrCreds returns a canonical VCR-style credentials struct used across the
// table-driven parser tests.
func vcrCreds() *options.RegistryCredentials {
	return &options.RegistryCredentials{
		Endpoint:  "vccr.io",
		ProjectID: "proj",
	}
}

// sampleDigest is a valid sha256 digest accepted by go-containerregistry.
const sampleDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestNormalize(t *testing.T) {
	type want struct {
		host       string
		project    string
		repository string
		tag        string
		digest     string
	}

	cases := []struct {
		name  string
		raw   string
		creds *options.RegistryCredentials
		want  want
	}{
		{
			name:  "short name, defaults to latest tag",
			raw:   "my-app",
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "my-app", tag: "latest"},
		},
		{
			name:  "short name with tag",
			raw:   "my-app:v1",
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "my-app", tag: "v1"},
		},
		{
			name:  "short name with digest",
			raw:   "my-app@" + sampleDigest,
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "my-app", digest: sampleDigest},
		},
		{
			name:  "short multi-segment repo",
			raw:   "team/sub/app:v1",
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "team/sub/app", tag: "v1"},
		},
		{
			name:  "full VCR ref",
			raw:   "vccr.io/proj/my-app:v1",
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "my-app", tag: "v1"},
		},
		{
			name:  "full registry with port",
			raw:   "registry.local:5000/team/app:v1",
			creds: vcrCreds(),
			want:  want{host: "registry.local:5000", project: "", repository: "team/app", tag: "v1"},
		},
		{
			name:  "full IP with port",
			raw:   "127.0.0.1:5000/a/b:c",
			creds: vcrCreds(),
			want:  want{host: "127.0.0.1:5000", project: "", repository: "a/b", tag: "c"},
		},
		{
			name:  "localhost",
			raw:   "localhost/my-app:v1",
			creds: vcrCreds(),
			want:  want{host: "localhost", project: "", repository: "my-app", tag: "v1"},
		},
		{
			name:  "tag defaulting — omitting both tag and digest yields latest",
			raw:   "vccr.io/proj/my-app",
			creds: vcrCreds(),
			want:  want{host: "vccr.io", project: "proj", repository: "my-app", tag: "latest"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.raw, tc.creds)
			if err != nil {
				t.Fatalf("Normalize(%q) unexpected error: %v", tc.raw, err)
			}
			if got.Host != tc.want.host {
				t.Errorf("Host: got %q want %q", got.Host, tc.want.host)
			}
			if got.Project != tc.want.project {
				t.Errorf("Project: got %q want %q", got.Project, tc.want.project)
			}
			if got.Repository != tc.want.repository {
				t.Errorf("Repository: got %q want %q", got.Repository, tc.want.repository)
			}
			if got.Tag != tc.want.tag {
				t.Errorf("Tag: got %q want %q", got.Tag, tc.want.tag)
			}
			if got.Digest != tc.want.digest {
				t.Errorf("Digest: got %q want %q", got.Digest, tc.want.digest)
			}
			// Exactly one of Tag or Digest must be non-empty.
			if (got.Tag == "") == (got.Digest == "") {
				t.Errorf("exactly one of Tag/Digest must be set: tag=%q digest=%q", got.Tag, got.Digest)
			}
		})
	}
}

func TestNormalize_DockerHubFullRef(t *testing.T) {
	// docker.io is rewritten to index.docker.io by ggcr. Assert via endsWith.
	got, err := Normalize("docker.io/library/nginx:1.25", vcrCreds())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got.Host, "docker.io") {
		t.Errorf("Host: got %q, want suffix docker.io", got.Host)
	}
	if got.Project != "library" {
		t.Errorf("Project: got %q want library", got.Project)
	}
	if got.Repository != "nginx" {
		t.Errorf("Repository: got %q want nginx", got.Repository)
	}
	if got.Tag != "1.25" {
		t.Errorf("Tag: got %q want 1.25", got.Tag)
	}
}

func TestNormalize_ErrorCases(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		creds   *options.RegistryCredentials
		wantSub string
	}{
		{
			name:    "short ref with nil creds",
			raw:     "my-app",
			creds:   nil,
			wantSub: "cannot expand short reference",
		},
		{
			name:    "short ref with creds missing endpoint",
			raw:     "my-app",
			creds:   &options.RegistryCredentials{ProjectID: "proj"},
			wantSub: "cannot expand short reference",
		},
		{
			name:    "short ref with creds missing project id",
			raw:     "my-app",
			creds:   &options.RegistryCredentials{Endpoint: "vccr.io"},
			wantSub: "cannot expand short reference",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(tc.raw, tc.creds)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestParse_RejectsShortRef(t *testing.T) {
	_, err := Parse("my-app")
	if err == nil {
		t.Fatalf("expected error for short ref, got nil")
	}
	if !strings.Contains(err.Error(), "requires a host") {
		t.Errorf("error %q missing substring %q", err.Error(), "requires a host")
	}

	// Multi-segment short ref should also be rejected.
	if _, err := Parse("team/app:v1"); err == nil {
		t.Fatalf("expected error for multi-segment short ref, got nil")
	}
}

func TestParse_FullRef(t *testing.T) {
	got, err := Parse("vccr.io/proj/my-app:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Host != "vccr.io" || got.Project != "proj" || got.Repository != "my-app" || got.Tag != "v1" {
		t.Errorf("unexpected parse: %+v", got)
	}
}

func TestRef_String_RoundTrip(t *testing.T) {
	inputs := []string{
		"my-app",
		"my-app:v1",
		"my-app@" + sampleDigest,
		"team/sub/app:v1",
		"vccr.io/proj/my-app:v1",
		"registry.local:5000/team/app:v1",
		"127.0.0.1:5000/a/b:c",
		"localhost/my-app:v1",
		"docker.io/library/nginx:1.25",
	}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			first, err := Normalize(in, vcrCreds())
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			s := first.String()
			second, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q): %v", s, err)
			}
			if first != second {
				t.Errorf("round-trip mismatch:\n first=%+v\nsecond=%+v\n  repr=%q", first, second, s)
			}
		})
	}
}

func TestRef_FullRepository(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		creds *options.RegistryCredentials
		want  string
	}{
		{
			name:  "VCR ref",
			raw:   "my-app:v1",
			creds: vcrCreds(),
			want:  "proj/my-app",
		},
		{
			name:  "VCR multi-segment",
			raw:   "team/sub/app",
			creds: vcrCreds(),
			want:  "proj/team/sub/app",
		},
		{
			name:  "Docker Hub library ref",
			raw:   "docker.io/library/nginx:1.25",
			creds: vcrCreds(),
			want:  "library/nginx",
		},
		{
			name:  "Local registry, no project",
			raw:   "registry.local:5000/team/app:v1",
			creds: vcrCreds(),
			want:  "team/app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := Normalize(tc.raw, tc.creds)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got := ref.FullRepository(); got != tc.want {
				t.Errorf("FullRepository = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRef_IsVCR(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		creds *options.RegistryCredentials
		want  bool
	}{
		{
			name:  "VCR host matches",
			raw:   "vccr.io/proj/my-app:v1",
			creds: vcrCreds(),
			want:  true,
		},
		{
			name:  "short form normalized to VCR",
			raw:   "my-app",
			creds: vcrCreds(),
			want:  true,
		},
		{
			name:  "docker.io not VCR",
			raw:   "docker.io/library/nginx:1.25",
			creds: vcrCreds(),
			want:  false,
		},
		{
			name:  "nil creds",
			raw:   "vccr.io/proj/my-app:v1",
			creds: nil,
			want:  false,
		},
		{
			name:  "creds with empty endpoint",
			raw:   "vccr.io/proj/my-app:v1",
			creds: &options.RegistryCredentials{},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := Normalize(tc.raw, vcrCreds())
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got := ref.IsVCR(tc.creds); got != tc.want {
				t.Errorf("IsVCR = %v, want %v", got, tc.want)
			}
		})
	}
}
