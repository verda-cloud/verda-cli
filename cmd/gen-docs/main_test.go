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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/docgen"
)

// TestRun builds the docs from the real command tree, as the release job does, so a
// generator failure surfaces in CI rather than first in a release.
func TestRun(t *testing.T) {
	// t.Setenv restores HOME and VERDA_HOME; other VERDA_* that isolateEnv unsets stay unset.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERDA_HOME", t.TempDir())

	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if err := run(dir, "v1.2.3"); err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	index := readFile(t, filepath.Join(first, docgen.IndexFile))
	if !strings.Contains(index, "> Verda CLI **v1.2.3**") {
		t.Errorf("commands.md does not name the release; got:\n%s", index)
	}
	readFile(t, filepath.Join(first, docgen.ReferenceDir, "vm.md"))

	// Each run gets a fresh temp home, so any home-derived help text differs between runs.
	for _, name := range listFiles(t, first) {
		a, b := readFile(t, filepath.Join(first, name)), readFile(t, filepath.Join(second, name))
		if a != b {
			t.Errorf("%s differs between runs; help text must not depend on the environment", name)
		}
	}
}

func TestRun_RejectsBadVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERDA_HOME", t.TempDir())
	out := t.TempDir()
	for _, version := range []string{"", "latest"} {
		if err := run(out, version); err == nil {
			t.Errorf("run(%q) = nil, want an error", version)
		}
	}
	if entries, _ := os.ReadDir(out); len(entries) > 0 {
		t.Errorf("a rejected run must write nothing; got %d entries", len(entries))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		names = append(names, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("got %d generated files, want an index and reference pages", len(names))
	}
	return names
}
