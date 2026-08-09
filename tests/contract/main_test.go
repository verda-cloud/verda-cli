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

// Package contract contains hermetic black-box contract tests: they drive the
// real verda binary against an in-process mock Verda API (see mockapi) and
// assert the machine-facing guarantees documented in docs/agent-errors.md
// (stdout/stderr separation, structured errors, exit codes, --yes gates).
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

// verdaBin is the path to the binary built once by TestMain. Empty when the
// suite runs under -short (tests skip).
var verdaBin string

// testClientSecret must be unique enough that finding it in output is never
// incidental — debug redaction tests grep stderr for it.
const (
	testClientID     = "contract-test-client-id"
	testClientSecret = "contract-test-secret-DO-NOT-LEAK"
)

const cliTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root, err := repoRoot(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract: locate repo root: %v\n", err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "verda-contract-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract: mktemp: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	verdaBin = filepath.Join(dir, "verda")
	build := exec.CommandContext(ctx, "go", "build", "-C", root, "-o", verdaBin, "./cmd/verda/") // #nosec G204 -- args are fixed literals; output path is a t.TempDir-managed dir
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "contract: go build ./cmd/verda: %v\n%s\n", err, out)
		os.Exit(1)
	}
	// Pay the first-launch cost up front: macOS holds a freshly written unsigned
	// binary in dyld for ~60s (provenance/Gatekeeper check) while every parallel
	// test would otherwise blow its per-command timeout waiting on the loader.
	warm := exec.CommandContext(ctx, verdaBin, "--version")
	if out, err := warm.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "contract: warm-up run failed: %v\n%s\n", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// repoRoot resolves the module root so the build works regardless of the
// package directory go test runs from.
func repoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

// cliResult holds one CLI invocation's captured output and wall-clock time.
type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// newServer returns a fresh mock API registered for cleanup.
func newServer(t *testing.T) *mockapi.Server {
	t.Helper()
	srv := mockapi.New()
	t.Cleanup(srv.Close)
	return srv
}

// runCLI executes the built binary against srv with hermetic env:
// VERDA_HOME and cwd point at fresh temp dirs (no user config, no cwd
// config.yaml poisoning), inherited VERDA_* vars are stripped, and mock
// credentials are injected. Stdin is /dev/null so a regression that blocks
// on interactive input hangs at the per-command timeout instead of passing.
func runCLI(t *testing.T, srv *mockapi.Server, args ...string) cliResult {
	t.Helper()
	return runCLIEnv(t, srv, nil, args...)
}

// runCLIEnv is runCLI plus extra env entries appended after the hermetic
// baseline (so they win over the stripped inherited vars — e.g.
// VERDA_REGISTRY_CREDENTIALS_FILE for registry commands).
func runCLIEnv(t *testing.T, srv *mockapi.Server, extraEnv []string, args ...string) cliResult {
	t.Helper()
	if verdaBin == "" {
		t.Skip("contract suite requires building the binary (disabled with -short)")
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	fullArgs := append([]string{"--base-url", srv.URL()}, args...)
	cmd := exec.CommandContext(ctx, verdaBin, fullArgs...) // #nosec G204 -- verdaBin is the harness-built binary under t.TempDir
	cmd.Env = append(cliEnv(t), extraEnv...)
	cmd.Dir = t.TempDir()
	cmd.Stdin = devNull
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("verda %s timed out after %s\nstdout: %s\nstderr: %s",
			strings.Join(fullArgs, " "), cliTimeout, stdout.String(), stderr.String())
	}

	res := cliResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: duration}
	if runErr == nil {
		return res
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("verda %s failed to run: %v", strings.Join(fullArgs, " "), runErr)
	}
	res.ExitCode = exitErr.ExitCode()
	return res
}

// cliEnv strips inherited VERDA_* variables, then sets an isolated config
// home and mock credentials.
func cliEnv(t *testing.T) []string {
	t.Helper()
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "VERDA_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"VERDA_HOME="+t.TempDir(),
		"VERDA_CLIENT_ID="+testClientID,
		"VERDA_CLIENT_SECRET="+testClientSecret,
	)
}

// agentErrorEnvelope mirrors docs/agent-errors.md: {"error": {...}} on stderr.
type agentErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

// requireExit fails unless the result has the wanted exit code, dumping both
// streams for debugging.
func requireExit(t *testing.T, r cliResult, want int) {
	t.Helper()
	if r.ExitCode != want {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", r.ExitCode, want, r.Stdout, r.Stderr)
	}
}

// parseAgentError validates and decodes the structured error envelope.
func parseAgentError(t *testing.T, r cliResult) agentErrorEnvelope {
	t.Helper()
	var env agentErrorEnvelope
	if err := json.Unmarshal([]byte(r.Stderr), &env); err != nil {
		t.Fatalf("stderr is not the agent error envelope: %v\nstderr: %s\nstdout: %s", err, r.Stderr, r.Stdout)
	}
	if env.Error.Code == "" {
		t.Fatalf("agent error envelope has empty code\nstderr: %s", r.Stderr)
	}
	return env
}

// requireCleanJSON asserts stdout parses as JSON into target and carries no
// terminal escape bytes.
func requireCleanJSON(t *testing.T, r cliResult, target any) {
	t.Helper()
	if i := strings.IndexByte(r.Stdout, 0x1b); i >= 0 {
		t.Fatalf("stdout contains ANSI escape at byte %d: %q", i, r.Stdout[max(0, i-20):i])
	}
	if err := json.Unmarshal([]byte(r.Stdout), target); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s\nstderr: %s", err, r.Stdout, r.Stderr)
	}
}
