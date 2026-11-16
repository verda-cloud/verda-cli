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

//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	isolatedProfile = "test"
	isolatedTimeout = 2 * time.Minute
)

var (
	credentialsOnce sync.Once
	credentialsPath string
)

// sharedCredentialsFile is resolved once in the test process: the parent's
// VERDA_SHARED_CREDENTIALS_FILE, else ~/.verda/credentials.
func sharedCredentialsFile(t *testing.T) string {
	t.Helper()
	credentialsOnce.Do(func() {
		if p := os.Getenv("VERDA_SHARED_CREDENTIALS_FILE"); p != "" {
			credentialsPath = p
			return
		}
		if home, err := os.UserHomeDir(); err == nil {
			credentialsPath = filepath.Join(home, ".verda", "credentials")
		}
	})
	if credentialsPath == "" {
		t.Skip("cannot resolve the credentials file for the [test] profile")
	}
	return credentialsPath
}

// isolatedEnv drops every VERDA_* variable from parent (they override --auth.profile),
// then points VERDA_HOME at home and the credentials at credFile only.
func isolatedEnv(parent []string, home, credFile string) []string {
	env := make([]string, 0, len(parent)+2)
	for _, kv := range parent {
		if strings.HasPrefix(kv, "VERDA_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "VERDA_HOME="+home, "VERDA_SHARED_CREDENTIALS_FILE="+credFile)
}

// runIsolated runs the binary with the [test] profile, a temp config dir and a temp
// working directory, so neither the real ~/.verda nor a stray config.yaml is used.
func runIsolated(t *testing.T, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), isolatedTimeout)
	defer cancel()

	full := append([]string{"--auth.profile", isolatedProfile}, args...)
	cmd := exec.CommandContext(ctx, verdaBin(), full...)
	cmd.Dir = t.TempDir()
	cmd.Env = isolatedEnv(os.Environ(), t.TempDir(), sharedCredentialsFile(t))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	r := cliResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running verda %s: %v", strings.Join(full, " "), err)
		}
		r.ExitCode = exitErr.ExitCode()
	}
	return r
}

// Conflicting VERDA_* variables in the parent must not reach the child.
func TestIsolatedEnvStripsVerdaOverrides(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"HOME=/home/someone",
		"VERDA_AUTH_CLIENT_ID=wrong-id",
		"VERDA_AUTH_TOKEN=wrong-token",
		"VERDA_AUTH_CREDENTIALS_FILE=/tmp/wrong",
		"VERDA_PROFILE=prod",
		"VERDA_CLIENT_SECRET=wrong-secret",
		"VERDA_HOME=/home/someone/.verda",
	}
	env := isolatedEnv(parent, "/tmp/iso-home", "/tmp/creds")

	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k := range got {
		if strings.HasPrefix(k, "VERDA_") && k != "VERDA_HOME" && k != "VERDA_SHARED_CREDENTIALS_FILE" {
			t.Errorf("child env keeps %s", k)
		}
	}
	if got["VERDA_HOME"] != "/tmp/iso-home" || got["VERDA_SHARED_CREDENTIALS_FILE"] != "/tmp/creds" {
		t.Errorf("VERDA_HOME=%q VERDA_SHARED_CREDENTIALS_FILE=%q", got["VERDA_HOME"], got["VERDA_SHARED_CREDENTIALS_FILE"])
	}
	if got["PATH"] != "/usr/bin" || got["HOME"] != "/home/someone" {
		t.Error("non-VERDA variables must pass through")
	}
	if n := strings.Count(strings.Join(env, "\n"), "VERDA_HOME="); n != 1 {
		t.Errorf("VERDA_HOME appears %d times", n)
	}
}
