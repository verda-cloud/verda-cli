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

package inference

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// hostEnv is everything setup/remove read from the machine; tests swap it for
// temp dirs so no test can touch a real agent config.
type hostEnv struct {
	Home     string
	VerdaDir string
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Hostname func() (string, error)
	// Version returns `<bin> --version` output.
	Version func(ctx context.Context, bin string) (string, error)
}

func defaultHostEnv() (*hostEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return nil, err
	}
	return &hostEnv{
		Home:     home,
		VerdaDir: verdaDir,
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Hostname: os.Hostname,
		Version:  binaryVersion,
	}, nil
}

func binaryVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output() //nolint:gosec // bin is a fixed agent binary name
	return string(out), err
}

func (e *hostEnv) statePath() string { return filepath.Join(e.VerdaDir, "inference-agents.yaml") }

func (e *hostEnv) agentsDir() string { return filepath.Join(e.VerdaDir, "agents") }

func (e *hostEnv) keyFile(agent string) string {
	return filepath.Join(e.agentsDir(), agent+".key")
}

// tildePath renders path as ~/… when it is under the home dir.
func (e *hostEnv) tildePath(path string) string {
	if rel, err := filepath.Rel(e.Home, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

func (e *hostEnv) installed(bin string) bool {
	if e.LookPath == nil {
		return false
	}
	_, err := e.LookPath(bin)
	return err == nil
}

func (e *hostEnv) hostname() string {
	if e.Hostname == nil {
		return "unknown-host"
	}
	h, err := e.Hostname()
	if err != nil || h == "" {
		return "unknown-host"
	}
	return strings.TrimSuffix(h, ".local")
}
