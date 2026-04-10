package vm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// testHarness bundles a mock API server, a TestFactory, and captured IOStreams
// for use in orchestration tests.
type testHarness struct {
	Server    *httptest.Server
	Factory   *cmdutil.TestFactory
	IOStreams cmdutil.IOStreams
	Stdout    *bytes.Buffer
	Stderr    *bytes.Buffer
}

// newTestHarness creates a test harness with an httptest server whose routes
// are defined by the caller via the provided mux. The server is registered for
// cleanup when the test finishes.
func newTestHarness(t *testing.T, mux *http.ServeMux) *testHarness {
	t.Helper()

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("newTestHarness: failed to create client: %v", err)
	}

	var stdout, stderr bytes.Buffer

	return &testHarness{
		Server: srv,
		Factory: &cmdutil.TestFactory{
			ClientOverride:       client,
			AgentModeOverride:    true,
			OutputFormatOverride: "json",
		},
		IOStreams: cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr},
		Stdout:    &stdout,
		Stderr:    &stderr,
	}
}

// baseMux returns an http.ServeMux pre-configured with the OAuth token
// endpoint that the SDK client requires for authentication.
func baseMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	return mux
}
