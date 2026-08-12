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

package sshkey

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// stagingBodyNoCreatedAt is the verbatim /v1/ssh-keys response captured from
// staging on 2026-08-12: no created_at key at all, fingerprint null. The SDK's
// testutil.MockServer cannot stand in here — it hardcodes CreatedAt: time.Now().
const stagingBodyNoCreatedAt = `[{"id":"4d13391d-bdef-49ec-84de-53a2f6174905",` +
	`"name":"meng",` +
	`"key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMtJUhkgcr0KR5OYrdQAoY/um6pNQ4RwlUK07tE4kUgq meng@datacrunch.io",` +
	`"fingerprint":null}]`

func newSSHKeyTestClient(t *testing.T, body string) *verda.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /ssh-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return client
}

func runListCmd(t *testing.T, body, format string) string {
	t.Helper()
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &bytes.Buffer{}}
	f := &cmdutil.TestFactory{
		ClientOverride:       newSSHKeyTestClient(t, body),
		OutputFormatOverride: format,
	}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdSSHKey(f, ioStreams))
	root.SetArgs([]string{"ssh-key", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("ssh-key list: %v", err)
	}
	return out.String()
}

// A key the API sent no created_at for must not gain one. Emitting Go's zero
// time makes an age-based reaper ("older than 2h ⇒ delete") treat every key as
// ancient and delete keys belonging to running jobs.
func TestListOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	got := runListCmd(t, stagingBodyNoCreatedAt, "json")

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("output emits a zero timestamp as data:\n%s", got)
	}
	if strings.Contains(got, "created_at") {
		t.Errorf("created_at present though the API never sent it:\n%s", got)
	}
	if !strings.Contains(got, "4d13391d-bdef-49ec-84de-53a2f6174905") {
		t.Errorf("key id missing from output:\n%s", got)
	}
}

// A real timestamp must survive untouched — the fix omits absent values, it
// does not drop the field wholesale.
func TestListKeepsRealCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"k-1","name":"real","key":"ssh-ed25519 AAA","fingerprint":"SHA256:abc",` +
		`"created_at":"2026-08-11T18:51:12.577Z"}]`
	got := runListCmd(t, body, "json")

	if !strings.Contains(got, "2026-08-11T18:51:12") {
		t.Errorf("real created_at was dropped:\n%s", got)
	}
	if !strings.Contains(got, "SHA256:abc") {
		t.Errorf("fingerprint was dropped:\n%s", got)
	}
}

// One row without a timestamp must not suppress another row's.
func TestListMixedCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"k-1","name":"nostamp","key":"ssh-ed25519 AAA","fingerprint":null},` +
		`{"id":"k-2","name":"stamped","key":"ssh-ed25519 BBB","fingerprint":"SHA256:xyz",` +
		`"created_at":"2026-08-11T18:51:12.577Z"}]`
	got := runListCmd(t, body, "json")

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("zero timestamp leaked for the row without one:\n%s", got)
	}
	if !strings.Contains(got, "2026-08-11T18:51:12") {
		t.Errorf("the stamped row lost its created_at:\n%s", got)
	}
	if strings.Count(got, "created_at") != 1 {
		t.Errorf("created_at count = %d, want exactly 1:\n%s", strings.Count(got, "created_at"), got)
	}
}

func TestListEmpty(t *testing.T) {
	t.Parallel()

	got := runListCmd(t, `[]`, "json")
	if strings.Contains(got, "0001-01-01") {
		t.Errorf("empty list emitted a timestamp:\n%s", got)
	}
}

// Table output must not print a blank fingerprint column. Asserted on the data
// row only — the header's "----" separator would satisfy a naive dash check.
func TestListTableShowsDashForAbsentFingerprint(t *testing.T) {
	t.Parallel()

	got := runListCmd(t, stagingBodyNoCreatedAt, "table")

	var dataRow string
	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "4d13391d-bdef-49ec-84de-53a2f6174905") {
			dataRow = line
			break
		}
	}
	if dataRow == "" {
		t.Fatalf("no data row for the key in table output:\n%s", got)
	}
	if strings.TrimRight(dataRow, " ") != strings.TrimRight(dataRow, " -") {
		return // ends in "-": absent fingerprint rendered explicitly
	}
	t.Errorf("absent fingerprint rendered blank; want %q at end of row %q", "-", dataRow)
}
