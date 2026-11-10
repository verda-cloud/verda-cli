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

// Command gen-docs writes docs/commands.md and docs/reference/ from the verda
// command tree. The release job runs it via `make docs VERSION=vX.Y.Z`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/docgen"
)

func main() {
	out := flag.String("out", "docs", "Docs directory to write commands.md and reference/ into")
	version := flag.String("version", "", "Release the docs describe, e.g. v1.2.3 (required)")
	flag.Parse()
	if err := run(*out, *version); err != nil {
		fmt.Fprintln(os.Stderr, "gen-docs:", err)
		os.Exit(1)
	}
}

func run(out, version string) error {
	cleanup, err := isolateEnv()
	if err != nil {
		return err
	}
	defer cleanup()
	var discard bytes.Buffer
	root, _ := cmd.NewRootCommand(cmdutil.IOStreams{In: &bytes.Buffer{}, Out: &discard, ErrOut: &discard})
	files, err := docgen.Generate(root, version)
	if err != nil {
		return err
	}
	// Stale pages of removed commands must not survive a regeneration.
	if err := os.RemoveAll(filepath.Join(out, docgen.ReferenceDir)); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // committed public docs
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil { //nolint:gosec // committed public docs
			return err
		}
	}
	return nil
}

// isolateEnv: no inherited VERDA_* (batchjob gate off) and a throwaway home, so building
// the tree can't touch a real config dir; TestRun catches any home-derived help text.
func isolateEnv() (cleanup func(), err error) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "VERDA_") {
			_ = os.Unsetenv(name)
		}
	}
	home, err := os.MkdirTemp("", "verda-gen-docs-")
	if err != nil {
		return nil, err
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("VERDA_HOME", filepath.Join(home, ".verda"))
	return func() { _ = os.RemoveAll(home) }, nil
}
