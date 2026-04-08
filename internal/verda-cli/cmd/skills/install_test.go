package skills

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verda-cloud/verda-ai-skills/main/manifest.json":
			w.Write([]byte(`{"version":"1.0.0","skills":["verda-cloud.md","verda-commands.md"],"agents":{"claude-code":{"display_name":"Claude Code","scope":"global","target":"~/.claude/skills/","method":"copy"}}}`))
		case "/verda-cloud/verda-ai-skills/main/skills/verda-cloud.md":
			w.Write([]byte("# Verda Cloud\nDecision engine content"))
		case "/verda-cloud/verda-ai-skills/main/skills/verda-commands.md":
			w.Write([]byte("# Verda Commands\nCommand reference content"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstallCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillFiles := map[string]string{
		"verda-cloud.md":    "# Verda Cloud\ntest",
		"verda-commands.md": "# Commands\ntest",
	}
	agent := &Agent{
		Name: "test-agent", DisplayName: "Test Agent",
		Scope: "global", Method: "copy", Target: dir,
	}
	if err := installForAgent(agent, skillFiles); err != nil {
		t.Fatalf("install error: %v", err)
	}
	for name, content := range skillFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != content {
			t.Fatalf("content mismatch for %s", name)
		}
	}
}

func TestInstallAppend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillFiles := map[string]string{"verda-cloud.md": "# Verda Cloud\ntest"}
	agent := &Agent{
		Name: "codex", DisplayName: "Codex",
		Scope: "project", Method: methodAppend,
		Target: filepath.Join(dir, "AGENTS.md"),
	}
	if err := installForAgent(agent, skillFiles); err != nil {
		t.Fatalf("install error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !bytes.Contains(data, []byte(markerStart)) {
		t.Fatal("expected start marker")
	}
	if !bytes.Contains(data, []byte(markerEnd)) {
		t.Fatal("expected end marker")
	}
	if !bytes.Contains(data, []byte("# Verda Cloud")) {
		t.Fatal("expected skill content")
	}
}

func TestInstallAppend_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := &Agent{
		Name: "codex", Scope: "project", Method: methodAppend,
		Target: filepath.Join(dir, "AGENTS.md"),
	}
	_ = installForAgent(agent, map[string]string{"verda-cloud.md": "# V1"})
	_ = installForAgent(agent, map[string]string{"verda-cloud.md": "# V2"})

	data, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if bytes.Count(data, []byte(markerStart)) != 1 {
		t.Fatalf("expected exactly 1 start marker, got content:\n%s", data)
	}
	if !bytes.Contains(data, []byte("# V2")) {
		t.Fatal("expected updated content")
	}
}

func TestRunInstall_NonInteractive(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	defer srv.Close()

	targetDir := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "skills.json")

	mock := tuitest.New().AddConfirm(true)
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &installOptions{
		agents:    []string{"claude-code"},
		statePath: statePath,
		fetcher:   &fetcher{baseURL: srv.URL, client: srv.Client()},
		agentOverrides: map[string]*Agent{
			"claude-code": {
				Name: "claude-code", DisplayName: "Claude Code",
				Scope: "global", Method: "copy", Target: targetDir,
			},
		},
	}

	if err := runInstall(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("install error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "verda-cloud.md")); err != nil {
		t.Fatal("verda-cloud.md not installed")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "verda-commands.md")); err != nil {
		t.Fatal("verda-commands.md not installed")
	}
	state, _ := LoadState(statePath)
	if state.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %q", state.Version)
	}
	if !state.HasAgent("claude-code") {
		t.Fatal("expected claude-code in state")
	}
}

func TestRunInstall_UserCancels(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	defer srv.Close()

	mock := tuitest.New().AddConfirm(false)
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &installOptions{
		agents:    []string{"claude-code"},
		statePath: filepath.Join(t.TempDir(), "skills.json"),
		fetcher:   &fetcher{baseURL: srv.URL, client: srv.Client()},
	}

	if err := runInstall(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("expected nil on cancel, got: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Canceled")) {
		t.Fatal("expected canceled message")
	}
}
