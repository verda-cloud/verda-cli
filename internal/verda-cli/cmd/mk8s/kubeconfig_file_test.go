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
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"
)

func skipModeTestOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// assertMode checks the POSIX mode; Windows has none, so only that check is skipped.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

func TestSanitizeFileName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"alice":          "alice",
		"a_b-c9":         "a_b-c9",
		"alice@corp.com": "alice-corp-com",
		"a b/c\\d":       "a-b-c-d",
		"../etc":         "---etc",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewKubeconfigTarget_DefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tgt, err := newKubeconfigTarget("", "prod", "alice@corp.com")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".kube", "verda-prod.alice-corp-com.yaml")
	if tgt.path != want {
		t.Errorf("path = %q, want %q", tgt.path, want)
	}
	if !tgt.defaultDir {
		t.Error("default path must create its parent")
	}

	other, _ := newKubeconfigTarget("", "prod", "bob")
	if other.path == tgt.path {
		t.Error("different common names must give different files")
	}

	custom, _ := newKubeconfigTarget("/x/y.yaml", "prod", "alice")
	if custom.path != "/x/y.yaml" || custom.defaultDir {
		t.Errorf("custom target = %+v", custom)
	}
}

func TestKubeconfigTarget_DefaultCreatesKubeDir0700(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tgt, err := newKubeconfigTarget("", "prod", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Check(false); err != nil {
		t.Fatalf("Check with missing ~/.kube: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kube")); err == nil {
		t.Fatal("Check must not create ~/.kube")
	}
	if err := tgt.Write([]byte(kcPrivateKeyMarker), false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(home, ".kube"), 0o700)
	assertMode(t, tgt.path, 0o600)
	if got := readFile(t, tgt.path); got != kcPrivateKeyMarker {
		t.Errorf("content = %q", got)
	}
}

func TestKubeconfigTarget_ExistingKubeDirKeepsMode(t *testing.T) {
	skipModeTestOnWindows(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	kube := filepath.Join(home, ".kube")
	if err := os.Mkdir(kube, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(kube, 0o755); err != nil {
		t.Fatal(err)
	}

	tgt, _ := newKubeconfigTarget("", "prod", "alice")
	if err := tgt.Write([]byte("x"), false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, kube, 0o755)
	assertMode(t, tgt.path, 0o600)
}

func TestKubeconfigTarget_CustomParentMustExist(t *testing.T) {
	t.Parallel()
	tgt := kubeconfigTarget{path: filepath.Join(t.TempDir(), "missing", "kc.yaml")}
	if err := tgt.Check(false); err == nil {
		t.Fatal("Check must fail when the parent directory is missing")
	}
	if err := tgt.Write([]byte("x"), false); err == nil {
		t.Fatal("Write must fail when the parent directory is missing")
	}
	if _, err := os.Stat(filepath.Dir(tgt.path)); err == nil {
		t.Error("a custom --file parent must not be created")
	}
}

func TestKubeconfigTarget_ParentIsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parent := filepath.Join(dir, "afile")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (kubeconfigTarget{path: filepath.Join(parent, "kc.yaml")}).Check(false); err == nil {
		t.Fatal("a parent that is a file must be rejected")
	}
}

func TestKubeconfigTarget_NewFile0600(t *testing.T) {
	t.Parallel()
	tgt := kubeconfigTarget{path: filepath.Join(t.TempDir(), "kc.yaml")}
	if err := tgt.Write([]byte(kcPrivateKeyMarker), false); err != nil {
		t.Fatal(err)
	}
	assertMode(t, tgt.path, 0o600)
	if got := readFile(t, tgt.path); got != kcPrivateKeyMarker {
		t.Errorf("content = %q", got)
	}
}

func TestKubeconfigTarget_NoOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	tgt := kubeconfigTarget{path: path}

	err := tgt.Check(false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Check error = %v, want mention of --force", err)
	}
	if err := tgt.Write([]byte("new"), false); err == nil {
		t.Fatal("Write must not overwrite without force")
	}
	if got := readFile(t, path); got != "original" {
		t.Errorf("file changed to %q", got)
	}
}

func TestKubeconfigTarget_ForceReplacesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "kc.yaml")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil { //nolint:gosec // test: a permissive old file must end up 0600
		t.Fatal(err)
	}
	tgt := kubeconfigTarget{path: path}
	if err := tgt.Check(true); err != nil {
		t.Fatalf("Check with force: %v", err)
	}
	if err := tgt.Write([]byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "new" {
		t.Errorf("content = %q", got)
	}
	assertMode(t, path, 0o600)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestKubeconfigTarget_ForceOnNewFileCreates(t *testing.T) {
	t.Parallel()
	tgt := kubeconfigTarget{path: filepath.Join(t.TempDir(), "kc.yaml")}
	if err := tgt.Write([]byte("x"), true); err != nil {
		t.Fatal(err)
	}
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

func TestNewKubeconfigTarget_NoCollisions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// With '-' as the separator, the first two pairs would both be verda-a-b-c.yaml.
	// Names that differ only in replaced characters (b.c vs b-c) still share a
	// file for one cluster; only the cluster/common-name boundary is guaranteed.
	pairs := [][2]string{{"a-b", "c"}, {"a", "b-c"}, {"a-b-c", ""}, {"", "a-b-c"}, {"a", ""}, {"", "a"}}
	seen := map[string][2]string{}
	for _, p := range pairs {
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

func TestKubeconfigTarget_CreateRefusesPlantedSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	link := filepath.Join(dir, "kc.yaml")
	if err := os.Symlink(victim, link); err != nil { // dangling: O_EXCL must still refuse
		t.Fatal(err)
	}
	tgt := kubeconfigTarget{path: link}
	if err := tgt.Check(true); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("Check on a dangling symlink = %v, want symlink error", err)
	}
	if err := tgt.create([]byte(kcPrivateKeyMarker)); err == nil {
		t.Error("create must refuse a symlink planted after Check")
	}
	if _, err := os.Stat(victim); err == nil {
		t.Error("the symlink target was created")
	}
}

func TestKubeconfigTarget_UnwritableDirFailsCheck(t *testing.T) {
	t.Parallel()
	skipModeTestOnWindows(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // test: a read-only directory
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // test cleanup

	tgt := kubeconfigTarget{path: filepath.Join(dir, "kc.yaml")}
	if err := tgt.Check(false); err == nil || !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("Check in a read-only directory = %v, want a cannot-write error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Check left files behind: %v", entries)
	}
}

func TestKubeconfigTarget_UnwritableHomeFailsCheckWithoutKubeDir(t *testing.T) {
	skipModeTestOnWindows(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0o500); err != nil { //nolint:gosec // test: a read-only home
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) }) //nolint:gosec // test cleanup
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tgt, err := newKubeconfigTarget("", "prod", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Check(false); err == nil {
		t.Fatal("Check must fail when ~/.kube is missing and the home directory is not writable")
	}
}

func TestKubeconfigTarget_RejectsDirectory(t *testing.T) {
	t.Parallel()
	tgt := kubeconfigTarget{path: t.TempDir()}
	for _, force := range []bool{false, true} {
		if err := tgt.Check(force); err == nil || !strings.Contains(err.Error(), "directory") {
			t.Errorf("Check(force=%v) = %v, want directory error", force, err)
		}
	}
}

func TestKubeconfigView_NeverContainsYAML(t *testing.T) {
	t.Parallel()
	notAfter := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	k := &verda.ManagedKubernetesKubeconfig{
		ID:          "prod-kc-abc",
		DisplayName: "ci",
		CommonName:  "alice",
		Groups:      []string{"system:masters"},
		Status:      verda.ManagedKubernetesKubeconfigStatusReady,
		NotAfter:    &notAfter,
		Kubeconfig:  "client-key-data: " + kcPrivateKeyMarker,
	}
	v := newKubeconfigView(k, "/home/a/.kube/kc.yaml")

	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	y, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"json": string(j), "yaml": string(y)} {
		if strings.Contains(out, kcPrivateKeyMarker) || strings.Contains(out, "client-key-data") {
			t.Errorf("%s leaks the kubeconfig YAML: %s", name, out)
		}
		if !strings.Contains(out, "prod-kc-abc") || !strings.Contains(out, "/home/a/.kube/kc.yaml") {
			t.Errorf("%s misses metadata: %s", name, out)
		}
	}

	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "display_name", "common_name", "groups", "status", "not_after", "file"} {
		if _, ok := m[key]; !ok {
			t.Errorf("view JSON misses %q", key)
		}
	}
	if len(m) != 7 {
		t.Errorf("view has %d fields, want exactly 7: %v", len(m), m)
	}
}

func TestKubeconfigView_StableShape(t *testing.T) {
	t.Parallel()
	// A pending kubeconfig: no groups, no expiry, nothing written yet.
	v := newKubeconfigView(&verda.ManagedKubernetesKubeconfig{ID: "c-kc-1", CommonName: "alice", Status: "pending"}, "")
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "display_name", "common_name", "groups", "status", "not_after"} {
		if _, ok := m[key]; !ok {
			t.Errorf("pending view JSON misses %q: %s", key, j)
		}
	}
	if g, ok := m["groups"].([]any); !ok || len(g) != 0 {
		t.Errorf("groups = %v, want []", m["groups"])
	}
	if m["not_after"] != nil {
		t.Errorf("not_after = %v, want null", m["not_after"])
	}
	if _, ok := m["file"]; ok {
		t.Errorf("file must be absent before a write: %s", j)
	}
}
