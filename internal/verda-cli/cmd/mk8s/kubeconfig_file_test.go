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

package mk8s

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skipModeTestOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
}

// The default name keeps every part inside ~/.kube (S5) and never lets two
// clusters share a file: '.' cannot come from a sanitized part.
func TestNewKubeconfigTarget(t *testing.T) {
	home := setHome(t)
	kube := filepath.Join(home, ".kube")
	for name, tc := range map[string]struct {
		file, cluster, cn string
		want              string
		defaultDir        bool
	}{
		"default":         {"", "prod", "alice@corp.com", filepath.Join(kube, "verda-prod.alice-corp-com.yaml"), true},
		"path traversal":  {"", "../etc", `a b/c\d`, filepath.Join(kube, "verda----etc.a-b-c-d.yaml"), true},
		"custom":          {"/x/y.yaml", "prod", "alice", "/x/y.yaml", false},
		"same as default": {filepath.Join(kube, "verda-prod.alice.yaml"), "prod", "alice", filepath.Join(kube, "verda-prod.alice.yaml"), true},
	} {
		tgt, err := newKubeconfigTarget(tc.file, tc.cluster, tc.cn)
		if err != nil || tgt.path != tc.want || tgt.defaultDir != tc.defaultDir {
			t.Errorf("%s: target = %+v, err = %v, want %s", name, tgt, err, tc.want)
		}
	}

	// With '-' as the separator, the first two pairs would both be verda-a-b-c.yaml.
	seen := map[string][2]string{}
	for _, p := range [][2]string{{"a-b", "c"}, {"a", "b-c"}, {"a-b-c", ""}, {"", "a-b-c"}, {"a", ""}, {"", "a"}} {
		tgt, err := newKubeconfigTarget("", p[0], p[1])
		if err != nil {
			t.Fatal(err)
		}
		if prev, dup := seen[tgt.path]; dup {
			t.Errorf("%v and %v both map to %s", prev, p, tgt.path)
		}
		seen[tgt.path] = p
	}
}

// Check fails, before a certificate is issued, where the write would fail later;
// an existing ~/.kube keeps its mode.
func TestKubeconfigTarget_CheckRefuses(t *testing.T) {
	skipModeTestOnWindows(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	readOnly := func(dir string) string {
		if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // test: a read-only directory
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // test cleanup
		return dir
	}

	dir := readOnly(t.TempDir())
	if err := (kubeconfigTarget{path: filepath.Join(dir, "kc.yaml")}).Check(false); err == nil || !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("read-only directory: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Check left files behind: %v", entries)
	}

	readOnly(setHome(t))
	if tgt, err := newKubeconfigTarget("", "prod", "alice"); err != nil || tgt.Check(false) == nil {
		t.Errorf("no ~/.kube and a read-only home: Check must fail (%v)", err)
	}

	kube := filepath.Join(setHome(t), ".kube")
	if err := os.Mkdir(kube, 0o755); err != nil { //nolint:gosec // test: a loose existing ~/.kube
		t.Fatal(err)
	}
	if err := os.Chmod(kube, 0o755); err != nil { //nolint:gosec // test: a loose existing ~/.kube
		t.Fatal(err)
	}
	tgt, _ := newKubeconfigTarget("", "prod", "alice")
	if err := tgt.Write([]byte("x"), false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, kube, 0o755)
	assertMode(t, tgt.path, 0o600)
}

func TestKubeconfigTarget_RejectsSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("victim"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "kc.yaml")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	tgt := kubeconfigTarget{path: link}
	for _, force := range []bool{false, true} {
		if err := tgt.Check(force); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("Check(force=%v) = %v, want symlink error", force, err)
		}
		if err := tgt.Write([]byte(kcPrivateKeyMarker), force); err == nil {
			t.Errorf("Write(force=%v) must fail on a symlink", force)
		}
	}
	if got := readFile(t, victim); got != "victim" {
		t.Errorf("symlink target was written: %q", got)
	}
}

func TestKubeconfigTarget_CreateIsExclusive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A file that appears after Check must not be overwritten by Write without force.
	path := filepath.Join(dir, "kc.yaml")
	tgt := kubeconfigTarget{path: path}
	if err := tgt.Check(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("appeared"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := tgt.create([]byte("new"))
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("create over an existing file = %v, want an error naming --force", err)
	}
	if got := readFile(t, path); got != "appeared" {
		t.Errorf("file changed to %q", got)
	}
}
