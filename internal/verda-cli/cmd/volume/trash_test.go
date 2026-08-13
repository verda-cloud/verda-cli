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

package volume

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const trashBody = `[{"id":"vol-1","name":"box-a-os","size":50,"type":"NVMe_Shared",` +
	`"location":"FIN-00","contract":"PAY_AS_YOU_GO","is_os_volume":true,` +
	`"monthly_price":10,"currency":"usd","deleted_at":"2026-08-11T18:51:12Z"},` +
	`{"id":"vol-2","name":"undated","size":20,"type":"NVMe_Shared",` +
	`"location":"FIN-00","contract":"PAY_AS_YOU_GO","is_os_volume":false}]`

func runTrashCmd(t *testing.T, body, format string, agent bool) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /volumes/trash", func(w http.ResponseWriter, _ *http.Request) {
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

	var out bytes.Buffer
	f := &cmdutil.TestFactory{
		ClientOverride:       client,
		OutputFormatOverride: format,
		AgentModeOverride:    agent,
	}
	cmd := NewCmdTrash(f, cmdutil.IOStreams{Out: &out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs(nil)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("volume trash: %v", err)
	}
	return out.String()
}

// `trash -o json` used to print an ANSI table: the command never consulted the
// output format at all.
func TestTrashHonorsJSONOutput(t *testing.T) {
	t.Parallel()

	got := runTrashCmd(t, trashBody, "json", true)

	if strings.ContainsRune(got, '\033') {
		t.Errorf("JSON output carries ANSI escapes:\n%q", got)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, got)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	if rows[0]["deleted_at"] != "2026-08-11T18:51:12Z" {
		t.Errorf("deleted_at = %v, want it preserved", rows[0]["deleted_at"])
	}
	if _, ok := rows[1]["deleted_at"]; ok {
		t.Errorf("undated volume carries deleted_at: %v", rows[1])
	}
	if _, ok := rows[1]["created_at"]; ok {
		t.Errorf("undated volume carries created_at: %v", rows[1])
	}
	if rows[0]["name"] != "box-a-os" || rows[0]["size"] != float64(50) {
		t.Errorf("identity fields lost: %v", rows[0])
	}
	// The disclaimer is human-facing; it must never enter the machine contract.
	if strings.Contains(got, cmdutil.PriceDisclaimer) {
		t.Errorf("disclaimer leaked into structured output:\n%s", got)
	}
}

// DeletedAt drives the 96-hour recovery countdown, so a fabricated date here
// would misreport how long a volume can still be restored.
func TestTrashTableMarksAbsentDeletedAt(t *testing.T) {
	t.Parallel()

	got := runTrashCmd(t, trashBody, "table", true)

	if strings.Contains(got, "0001") {
		t.Errorf("table emits a zero timestamp:\n%s", got)
	}
	if !strings.Contains(got, "2 volume(s) in trash") {
		t.Errorf("missing the count line:\n%s", got)
	}
	if !strings.Contains(got, "11 Aug 2026") {
		t.Errorf("real deleted_at not rendered:\n%s", got)
	}
	if !strings.Contains(got, "Deleted:   -\n") {
		t.Errorf("absent timestamp not rendered as %q:\n%s", "-", got)
	}
	// No expiry countdown without a deletion date to count from.
	if strings.Count(got, "Expires:") != 1 {
		t.Errorf("Expires count = %d, want 1 (only the dated row):\n%s", strings.Count(got, "Expires:"), got)
	}
}

// lipgloss renders escapes into a buffer regardless of where the buffer goes;
// agent mode and a non-terminal destination must both suppress them.
func TestTrashTableHasNoANSIWhenNotATerminal(t *testing.T) {
	t.Parallel()

	got := runTrashCmd(t, trashBody, "table", true)
	if strings.ContainsRune(got, '\033') {
		t.Errorf("table output carries ANSI escapes:\n%q", got)
	}
}

func TestTrashEmpty(t *testing.T) {
	t.Parallel()

	if got := runTrashCmd(t, `[]`, "table", true); !strings.Contains(got, "Trash is empty.") {
		t.Errorf("got %q", got)
	}
	if got := runTrashCmd(t, `[]`, "json", true); strings.TrimSpace(got) != "[]" {
		t.Errorf("empty json = %q, want []", got)
	}
}

// Trashed volumes report a monthly_price, so the table must carry the disclaimer
// that the web console — not this output — is authoritative for charges.
func TestTrashTableCarriesPriceDisclaimer(t *testing.T) {
	t.Parallel()

	got := runTrashCmd(t, trashBody, "table", true)
	if !strings.Contains(got, cmdutil.PriceDisclaimer) {
		t.Errorf("missing the price disclaimer:\n%s", got)
	}
}
