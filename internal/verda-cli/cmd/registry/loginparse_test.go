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

import "testing"

func TestParseDockerLoginCommand(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		user    string
		secret  string
		host    string
		project string
		wantErr bool
	}{
		{
			name:    "staging-style paste from UI",
			in:      "docker login -u vcr-abc+cli -p s3cret registry.staging.verda.com/abc",
			user:    "vcr-abc+cli",
			secret:  "s3cret",
			host:    "registry.staging.verda.com",
			project: "abc",
		},
		{
			name:    "prod-style without path suffix",
			in:      "docker login -u vcr-uuid1+dev -p tok vccr.io",
			user:    "vcr-uuid1+dev",
			secret:  "tok",
			host:    "vccr.io",
			project: "uuid1",
		},
		{
			name:    "long flags, reordered",
			in:      "docker login --password pw --username vcr-u+n vccr.io",
			user:    "vcr-u+n",
			secret:  "pw",
			host:    "vccr.io",
			project: "u",
		},
		{
			name:    "trailing newline + extra spaces",
			in:      "  docker login -u vcr-u+n  -p  pw  vccr.io/u \n",
			user:    "vcr-u+n",
			secret:  "pw",
			host:    "vccr.io",
			project: "u",
		},
		{
			name:    "project mismatch between username and host path",
			in:      "docker login -u vcr-foo+n -p pw vccr.io/bar",
			wantErr: true,
		},
		{
			name:    "username not in vcr- format",
			in:      "docker login -u bobuser -p pw vccr.io",
			wantErr: true,
		},
		{
			name:    "missing secret",
			in:      "docker login -u vcr-a+b vccr.io",
			wantErr: true,
		},
		{
			name:    "not a docker login at all",
			in:      "echo hello",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDockerLogin(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got.Username != tc.user || got.Secret != tc.secret || got.Host != tc.host || got.ProjectID != tc.project {
				t.Errorf("got %+v", got)
			}
		})
	}
}
