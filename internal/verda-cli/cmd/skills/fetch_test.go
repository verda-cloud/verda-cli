package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testManifestJSON = `{
	"version":"1.2.0",
	"skills":["verda-cloud.md","verda-commands.md"],
	"agents":{
		"claude-code":{"display_name":"Claude Code","scope":"global","target":"~/.claude/skills/","method":"copy"},
		"cursor":{"display_name":"Cursor","scope":"project","target":".cursor/rules/","method":"copy"},
		"codex":{"display_name":"Codex","scope":"project","target":"AGENTS.md","method":"append"}
	}
}`

func TestFetchManifest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verda-cloud/verda-ai-skills/main/manifest.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(testManifestJSON))
	}))
	defer srv.Close()

	f := &fetcher{baseURL: srv.URL, client: srv.Client()}
	m, err := f.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != "1.2.0" {
		t.Fatalf("expected version 1.2.0, got %q", m.Version)
	}
	if len(m.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(m.Skills))
	}
	if len(m.Agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(m.Agents))
	}
	cc := m.Agents["claude-code"]
	if cc.Name != "claude-code" {
		t.Fatalf("expected agent Name 'claude-code', got %q", cc.Name)
	}
	if cc.DisplayName != "Claude Code" {
		t.Fatalf("expected display name 'Claude Code', got %q", cc.DisplayName)
	}
}

func TestFetchSkillFile(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verda-cloud/verda-ai-skills/main/skills/verda-cloud.md" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("# Verda Cloud Skill\ntest content"))
	}))
	defer srv.Close()

	f := &fetcher{baseURL: srv.URL, client: srv.Client()}
	content, err := f.FetchSkillFile(context.Background(), "verda-cloud.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "# Verda Cloud Skill\ntest content" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestFetchManifest_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := &fetcher{baseURL: srv.URL, client: srv.Client()}
	_, err := f.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestManifestAgentNames(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Agents: map[string]*Agent{
			"cursor":      {Name: "cursor"},
			"claude-code": {Name: "claude-code"},
			"codex":       {Name: "codex"},
		},
	}
	names := m.AgentNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "claude-code" {
		t.Fatalf("expected claude-code first, got %q", names[0])
	}
}

func TestAgentTargetDir_Copy(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "cursor", Target: ".cursor/rules/", Method: "copy"}
	if dir := a.TargetDir(); dir != ".cursor/rules/" {
		t.Fatalf("expected '.cursor/rules/', got %q", dir)
	}
}

func TestAgentTargetDir_Append(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "codex", Target: "AGENTS.md", Method: methodAppend}
	if dir := a.TargetDir(); dir != "." {
		t.Fatalf("expected '.', got %q", dir)
	}
	if f := a.TargetFile(); f != "AGENTS.md" {
		t.Fatalf("expected 'AGENTS.md', got %q", f)
	}
}

func TestAgentDisplayLabel(t *testing.T) {
	t.Parallel()
	a := &Agent{DisplayName: "Claude Code", Target: "~/.claude/skills/"}
	label := a.DisplayLabel()
	if label != "Claude Code (~/.claude/skills/)" {
		t.Fatalf("unexpected label: %q", label)
	}
}

func TestExpandHome(t *testing.T) {
	t.Parallel()
	if p := expandHome(".cursor/rules/"); p != ".cursor/rules/" {
		t.Fatalf("expected unchanged path, got %q", p)
	}
	expanded := expandHome("~/.claude/skills/")
	if expanded == "~/.claude/skills/" {
		t.Fatal("expected ~ to be expanded")
	}
}
