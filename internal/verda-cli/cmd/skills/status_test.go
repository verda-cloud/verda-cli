package skills

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

func TestRunStatus_Installed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.1.0","skills":["verda-cloud.md"]}`))
	}))
	defer srv.Close()

	statePath := filepath.Join(t.TempDir(), "skills.json")
	_ = SaveState(statePath, &State{
		Version:     "1.0.0",
		InstalledAt: time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC),
		Agents:      []string{"claude-code", "cursor"},
	})

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{
		statePath: statePath,
		fetcher:   &fetcher{baseURL: srv.URL, client: srv.Client()},
	}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}

	output := out.String()
	if !bytes.Contains(out.Bytes(), []byte("1.0.0")) {
		t.Fatalf("expected installed version in output, got:\n%s", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("Claude Code")) {
		t.Fatalf("expected Claude Code in output, got:\n%s", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("1.1.0")) {
		t.Fatalf("expected latest version in output, got:\n%s", output)
	}
}

func TestRunStatus_NotInstalled(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "skills.json")

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{statePath: statePath}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("not installed")) {
		t.Fatalf("expected 'not installed' message, got:\n%s", out.String())
	}
}

func TestRunStatus_JSON(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "skills.json")
	_ = SaveState(statePath, &State{
		Version: "1.0.0",
		Agents:  []string{"claude-code"},
	})

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	f.OutputFormatOverride = "json"
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{statePath: statePath}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"version"`)) {
		t.Fatalf("expected JSON output, got:\n%s", out.String())
	}
}
