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

package serverless

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newFileSecretServer(t *testing.T) (*httptest.Server, *fileSecretRecorder) {
	t.Helper()
	rec := &fileSecretRecorder{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("GET /file-secrets", func(w http.ResponseWriter, _ *http.Request) {
		rec.mu.Lock()
		data := rec.listResponse
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})

	mux.HandleFunc("POST /file-secrets", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.createBody = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})

	mux.HandleFunc("DELETE /file-secrets/{name}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.deletedName = r.PathValue("name")
		rec.deleteForce = r.URL.Query().Get("force") == "true"
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

type fileSecretRecorder struct {
	mu           sync.Mutex
	listResponse []verda.FileSecret
	createBody   []byte
	deletedName  string
	deleteForce  bool
}

func TestFileSecretList_Table(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	rec.listResponse = []verda.FileSecret{
		{Name: "tls-bundle", SecretType: "file", FileNames: []string{"cert.pem", "key.pem"}, CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
		{Name: "ca-cert", SecretType: "file", FileNames: []string{"ca.pem"}, CreatedAt: time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)},
	}
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret list failed: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"NAME", "TYPE", "FILES", "CREATED", "tls-bundle", "cert.pem, key.pem", "ca-cert", "ca.pem"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestFileSecretList_JSON(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	rec.listResponse = []verda.FileSecret{
		{Name: "tls-bundle", SecretType: "file", FileNames: []string{"cert.pem"}, CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
	}
	f := newTestFactory(t, srv.URL)

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret list failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "tls-bundle") {
		t.Errorf("expected tls-bundle in JSON output, got:\n%s", stdout.String())
	}
}

func TestFileSecretList_Empty(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	rec.listResponse = []verda.FileSecret{}
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret list failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "No file secrets found.") {
		t.Errorf("expected empty message, got:\n%s", stdout.String())
	}
}

func TestFileSecretCreate_Flags(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert-content"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"file-secret", "create",
		"--name", "my-certs",
		"--file", "cert.pem=" + certPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.createBody
	rec.mu.Unlock()
	if body == nil {
		t.Fatalf("server did not receive POST /file-secrets")
	}

	var got struct {
		Name  string `json:"name"`
		Files []struct {
			Name          string `json:"file_name"`
			Base64Content string `json:"base64_content"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got.Name != "my-certs" {
		t.Errorf("name: got %q, want my-certs", got.Name)
	}
	if len(got.Files) != 1 {
		t.Fatalf("files: got %d, want 1", len(got.Files))
	}
	if got.Files[0].Name != "cert.pem" {
		t.Errorf("file name: got %q, want cert.pem", got.Files[0].Name)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("cert-content"))
	if got.Files[0].Base64Content != wantB64 {
		t.Errorf("base64: got %q, want %q", got.Files[0].Base64Content, wantB64)
	}
}

func TestFileSecretCreate_AgentMode_MissingFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "create"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("expected MISSING_REQUIRED_FLAGS, got %q", agentErr.Code)
	}
}

func TestFileSecretDelete_WithYes(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "delete", "my-certs", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret delete failed: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.deletedName != "my-certs" {
		t.Errorf("deleted name: got %q, want my-certs", rec.deletedName)
	}
}

func TestFileSecretDelete_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	srv, _ := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "delete", "my-certs"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("expected CONFIRMATION_REQUIRED, got %q", agentErr.Code)
	}
}

func TestFileSecretDelete_Force(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "delete", "my-certs", "--yes", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret delete --force failed: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !rec.deleteForce {
		t.Error("expected force=true in DELETE request")
	}
}

func TestFileSecretCreate_MultipleFiles(t *testing.T) {
	t.Parallel()
	srv, rec := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	if err := os.WriteFile(certPath, []byte("cert-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key-data"), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"file-secret", "create",
		"--name", "tls-bundle",
		"--file", "cert.pem=" + certPath,
		"--file", "key.pem=" + keyPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.createBody
	rec.mu.Unlock()

	var got struct {
		Name  string `json:"name"`
		Files []struct {
			Name          string `json:"file_name"`
			Base64Content string `json:"base64_content"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if len(got.Files) != 2 {
		t.Fatalf("files: got %d, want 2", len(got.Files))
	}
	wantCert := base64.StdEncoding.EncodeToString([]byte("cert-data"))
	wantKey := base64.StdEncoding.EncodeToString([]byte("key-data"))
	if got.Files[0].Base64Content != wantCert {
		t.Errorf("file[0] base64: got %q, want %q", got.Files[0].Base64Content, wantCert)
	}
	if got.Files[1].Base64Content != wantKey {
		t.Errorf("file[1] base64: got %q, want %q", got.Files[1].Base64Content, wantKey)
	}
}

func TestFileSecretCreate_JSONOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{
		"file-secret", "create",
		"--name", "my-certs",
		"--file", "cert.pem=" + certPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret create failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["name"] != "my-certs" {
		t.Errorf("name: got %v, want my-certs", got["name"])
	}
	if got["action"] != "created" {
		t.Errorf("action: got %v, want created", got["action"])
	}
	if got["files"] != float64(1) {
		t.Errorf("files: got %v, want 1", got["files"])
	}
}

func TestParseFileFlags(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "cert.pem")
		if err := os.WriteFile(certPath, []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
		files, err := parseFileFlags([]string{"cert.pem=" + certPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Name != "cert.pem" {
			t.Errorf("name: got %q", files[0].Name)
		}
		want := base64.StdEncoding.EncodeToString([]byte("hello"))
		if files[0].Base64Content != want {
			t.Errorf("base64: got %q, want %q", files[0].Base64Content, want)
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := parseFileFlags([]string{"no-equals-sign"})
		if err == nil || !strings.Contains(err.Error(), "expected name=path") {
			t.Fatalf("expected name=path error, got %v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		_, err := parseFileFlags([]string{"=/some/path"})
		if err == nil || !strings.Contains(err.Error(), "expected name=path") {
			t.Fatalf("expected name=path error, got %v", err)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := parseFileFlags([]string{"cert.pem="})
		if err == nil || !strings.Contains(err.Error(), "expected name=path") {
			t.Fatalf("expected name=path error, got %v", err)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := parseFileFlags([]string{"cert.pem=/nonexistent/path"})
		if err == nil || !strings.Contains(err.Error(), "reading file") {
			t.Fatalf("expected reading file error, got %v", err)
		}
	})
}

func TestFileSecretDelete_JSONOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "delete", "my-certs", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret delete failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["name"] != "my-certs" {
		t.Errorf("name: got %q, want my-certs", got["name"])
	}
	if got["action"] != "deleted" {
		t.Errorf("action: got %q, want deleted", got["action"])
	}
}

func TestFileSecretCreate_AgentMode_PartialFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "create", "--name", "my-certs"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code: got %q, want MISSING_REQUIRED_FLAGS", agentErr.Code)
	}
	missing, ok := agentErr.Details["missing"].([]string)
	if !ok {
		t.Fatalf("expected Details[missing] to be []string, got %T", agentErr.Details["missing"])
	}
	if len(missing) != 1 || missing[0] != "--file" {
		t.Errorf("expected missing=[--file], got %v", missing)
	}
}

func TestFileSecretDelete_TableOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"file-secret", "delete", "my-certs", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret delete failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted file secret") {
		t.Errorf("expected 'Deleted file secret' in output; got:\n%s", stdout.String())
	}
}

func TestFileSecretCreate_TableOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newFileSecretServer(t)
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{
		"file-secret", "create",
		"--name", "my-certs",
		"--file", "cert.pem=" + certPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("file-secret create failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Created file secret") {
		t.Errorf("expected 'Created file secret' in output; got:\n%s", stdout.String())
	}
}
