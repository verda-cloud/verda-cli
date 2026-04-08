package status

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestRunStatusJSON(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "i1", "status": "running", "location": "FIN-01", "price_per_hour": 0.10, "is_spot": false, "hostname": "gpu-1"},
			{"id": "i2", "status": "offline", "location": "FIN-01", "price_per_hour": 0.0, "hostname": "gpu-2"},
		})
	})
	mux.HandleFunc("GET /volumes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "v1", "status": "attached", "size": 100, "base_hourly_cost": 0.014},
		})
	})
	mux.HandleFunc("GET /balance", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"amount": 500.0, "currency": "USD"})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test"),
		verda.WithClientSecret("test"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var out bytes.Buffer
	f := &cmdutil.TestFactory{
		ClientOverride:       client,
		OutputFormatOverride: "json",
	}
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &bytes.Buffer{}}

	cmd := NewCmdStatus(f, ioStreams)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var dashboard Dashboard
	if err := json.Unmarshal(out.Bytes(), &dashboard); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, out.String())
	}

	if dashboard.Instances.Total != 2 {
		t.Fatalf("expected 2 total instances, got %d", dashboard.Instances.Total)
	}
	if dashboard.Instances.Running != 1 {
		t.Fatalf("expected 1 running, got %d", dashboard.Instances.Running)
	}
	if dashboard.Volumes.Attached != 1 {
		t.Fatalf("expected 1 attached volume, got %d", dashboard.Volumes.Attached)
	}
	if dashboard.Financials.Balance != 500.0 {
		t.Fatalf("expected balance $500, got $%.2f", dashboard.Financials.Balance)
	}
}

func TestRunStatusTable(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "i1", "status": "running", "location": "FIN-01", "price_per_hour": 0.10, "hostname": "gpu-1"},
		})
	})
	mux.HandleFunc("GET /volumes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("GET /balance", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"amount": 100.0, "currency": "USD"})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test"),
		verda.WithClientSecret("test"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var out bytes.Buffer
	f := &cmdutil.TestFactory{
		ClientOverride: client,
	}
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &bytes.Buffer{}}

	cmd := NewCmdStatus(f, ioStreams)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Verda Cloud Status") {
		t.Fatalf("expected dashboard header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "running") {
		t.Fatalf("expected 'running' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Balance") {
		t.Fatalf("expected 'Balance' in output, got:\n%s", output)
	}
}
