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

package startupscript

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

func newScriptTestClient(t *testing.T, body string) *verda.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /scripts", func(w http.ResponseWriter, _ *http.Request) {
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

func runScriptListCmd(t *testing.T, body, format string) string {
	t.Helper()
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &bytes.Buffer{}}
	f := &cmdutil.TestFactory{
		ClientOverride:       newScriptTestClient(t, body),
		OutputFormatOverride: format,
	}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdStartupScript(f, ioStreams))
	root.SetArgs([]string{"startup-script", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("startup-script list: %v", err)
	}
	return out.String()
}

const scriptBodyNoCreatedAt = `[{"id":"s-1","name":"bootstrap","script":"#!/bin/bash\necho hi"}]`

// Same defect class as ssh-key list: an absent created_at must not be invented.
func TestScriptListOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	got := runScriptListCmd(t, scriptBodyNoCreatedAt, "json")

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("output emits a zero timestamp as data:\n%s", got)
	}
	if strings.Contains(got, "created_at") {
		t.Errorf("created_at present though the API never sent it:\n%s", got)
	}
	if !strings.Contains(got, "bootstrap") {
		t.Errorf("script name missing:\n%s", got)
	}
}

// The table formats CreatedAt directly, so the same gap surfaces as
// "0001-01-01 00:00" — a plausible-looking date, which is worse than "-".
func TestScriptListTableShowsDashForAbsentCreatedAt(t *testing.T) {
	t.Parallel()

	got := runScriptListCmd(t, scriptBodyNoCreatedAt, "table")

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("table emits a zero timestamp:\n%s", got)
	}
	var dataRow string
	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "bootstrap") {
			dataRow = line
			break
		}
	}
	if dataRow == "" {
		t.Fatalf("no data row in table output:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimRight(dataRow, " "), "-") {
		t.Errorf("absent CREATED should render as %q, got row %q", "-", dataRow)
	}
}

func TestScriptListKeepsRealCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"s-1","name":"bootstrap","script":"x","created_at":"2026-08-11T18:51:12.577Z"}]`

	jsonOut := runScriptListCmd(t, body, "json")
	if !strings.Contains(jsonOut, "2026-08-11T18:51:12") {
		t.Errorf("json lost the timestamp:\n%s", jsonOut)
	}

	tableOut := runScriptListCmd(t, body, "table")
	if !strings.Contains(tableOut, "2026-08-11 18:51") {
		t.Errorf("table lost the timestamp:\n%s", tableOut)
	}
}

func TestScriptListEmpty(t *testing.T) {
	t.Parallel()

	if got := runScriptListCmd(t, `[]`, "json"); strings.Contains(got, "0001-01-01") {
		t.Errorf("empty list emitted a timestamp:\n%s", got)
	}
}
