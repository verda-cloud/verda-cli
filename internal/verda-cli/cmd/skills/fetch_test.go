package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchManifest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verda-cloud/verda-ai-skills/main/manifest.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"version":"1.2.0","skills":["verda-cloud.md","verda-commands.md"]}`))
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
