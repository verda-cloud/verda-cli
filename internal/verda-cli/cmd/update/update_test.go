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

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// These tests override the package-level apiBase, so they must not run
// in parallel with each other (other tests in this package never read it).

const fakeTag = "v9.9.9"

type fakeRelease struct {
	tamperSums bool // sums entry carries a hash that does not match the archive
	sumsStatus int  // serve this non-200 status from the sums endpoint instead of a body
	omitSums   bool // checksum file absent from the release asset list
}

// setupFakeRelease hosts a fake GitHub release API plus asset downloads and
// points apiBase at it. Returns the binary payload embedded in the archive.
func setupFakeRelease(t *testing.T, fr fakeRelease) []byte {
	t.Helper()

	binaryContent := []byte("fake verda binary payload")
	binaryName := platformBinaryName()
	ext := "tar.gz"
	if runtime.GOOS == osWindows {
		ext = zipExt
	}
	assetName := fmt.Sprintf("verda_9.9.9_%s_%s.%s", runtime.GOOS, runtime.GOARCH, ext)
	sumsName := "verda_9.9.9_SHA256SUMS"
	archive := buildArchive(t, ext, binaryName, binaryContent)

	sum := sha256.Sum256(archive)
	hash := hex.EncodeToString(sum[:])
	if fr.tamperSums {
		sum = sha256.Sum256([]byte("not the archive"))
		hash = hex.EncodeToString(sum[:])
	}
	sumsBody := fmt.Sprintf("%s  %s\n", hash, assetName)

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/"+repo+"/releases/tags/"+fakeTag, func(w http.ResponseWriter, _ *http.Request) {
		assets := []ghAsset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/dl/" + assetName},
		}
		if !fr.omitSums {
			assets = append(assets, ghAsset{Name: sumsName, BrowserDownloadURL: srv.URL + "/dl/" + sumsName})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ghRelease{TagName: fakeTag, Assets: assets})
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/dl/"+sumsName, func(w http.ResponseWriter, _ *http.Request) {
		if fr.sumsStatus != 0 {
			w.WriteHeader(fr.sumsStatus)
			return
		}
		_, _ = w.Write([]byte(sumsBody))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldBase := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = oldBase })

	return binaryContent
}

// stubExecutable reroutes the old-binary migration probe so a successful
// runUpdate under test never touches the real test binary.
func stubExecutable(t *testing.T) {
	t.Helper()
	old := executablePath
	executablePath = func() (string, error) { return "", nil }
	t.Cleanup(func() { executablePath = old })
}

func buildArchive(t *testing.T, ext, name string, content []byte) []byte {
	t.Helper()
	if ext == zipExt {
		return buildZipArchive(t, name, content)
	}
	return buildTarGzArchive(t, name, content)
}

func buildTarGzArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZipArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRunUpdateVerifiedHappyPath(t *testing.T) {
	binary := setupFakeRelease(t, fakeRelease{})
	home := t.TempDir()
	t.Setenv("VERDA_HOME", home)
	stubExecutable(t)

	f := &cmdutil.TestFactory{}
	var out, errOut bytes.Buffer
	ioStreams := cmdutil.IOStreams{In: bytes.NewReader(nil), Out: &out, ErrOut: &errOut}

	err := runUpdate(context.Background(), f, ioStreams, fakeTag, false)
	if err != nil {
		t.Fatalf("runUpdate error: %v", err)
	}

	dst := filepath.Join(home, "bin", platformBinaryName())
	got, err := os.ReadFile(dst) // #nosec G304 -- dst is under t.TempDir()
	if err != nil {
		t.Fatalf("reading installed binary: %v", err)
	}
	if !bytes.Equal(got, binary) {
		t.Errorf("installed binary content mismatch")
	}
	if !strings.Contains(out.String(), "Updated to "+fakeTag) {
		t.Errorf("expected 'Updated to %s' in output, got %q", fakeTag, out.String())
	}
}

func TestRunUpdateAbortsOnChecksumMismatch(t *testing.T) {
	setupFakeRelease(t, fakeRelease{tamperSums: true})
	home := t.TempDir()
	t.Setenv("VERDA_HOME", home)

	// Pre-existing install: must be left untouched by the aborted update.
	binDir := filepath.Join(home, "bin")
	dst := filepath.Join(binDir, platformBinaryName())
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G306 -- fixture must be executable like a real install
	if err := os.WriteFile(dst, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	f := &cmdutil.TestFactory{}
	var out, errOut bytes.Buffer
	ioStreams := cmdutil.IOStreams{In: bytes.NewReader(nil), Out: &out, ErrOut: &errOut}

	err := runUpdate(context.Background(), f, ioStreams, fakeTag, false)
	if err == nil {
		t.Fatal("expected checksum verification error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum verification failed") {
		t.Errorf("error should report checksum verification failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--skip-verify") {
		t.Errorf("error should name --skip-verify as escape hatch, got: %v", err)
	}

	got, err := os.ReadFile(dst) // #nosec G304 -- dst is under t.TempDir()
	if err != nil {
		t.Fatalf("old binary missing after aborted update: %v", err)
	}
	if string(got) != "old binary" {
		t.Errorf("old binary was modified: %q", got)
	}
}

func TestRunUpdateAbortsOnChecksumFetchFailure(t *testing.T) {
	setupFakeRelease(t, fakeRelease{sumsStatus: http.StatusInternalServerError})
	home := t.TempDir()
	t.Setenv("VERDA_HOME", home)

	f := &cmdutil.TestFactory{}
	var out, errOut bytes.Buffer
	ioStreams := cmdutil.IOStreams{In: bytes.NewReader(nil), Out: &out, ErrOut: &errOut}

	err := runUpdate(context.Background(), f, ioStreams, fakeTag, false)
	if err == nil {
		t.Fatal("expected error when checksum endpoint fails, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error should surface the fetch failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--skip-verify") {
		t.Errorf("error should name --skip-verify as escape hatch, got: %v", err)
	}

	// Fail closed: abort happened before the install directory was prepared.
	if _, statErr := os.Stat(filepath.Join(home, "bin")); !os.IsNotExist(statErr) {
		t.Errorf("bin directory should not exist after aborted update, stat err = %v", statErr)
	}
}

func TestRunUpdateSkipVerifyProceeds(t *testing.T) {
	binary := setupFakeRelease(t, fakeRelease{tamperSums: true})
	home := t.TempDir()
	t.Setenv("VERDA_HOME", home)
	stubExecutable(t)

	// JSON format doubles as the --agent output-contract assertion.
	f := &cmdutil.TestFactory{OutputFormatOverride: "json"}
	var out, errOut bytes.Buffer
	ioStreams := cmdutil.IOStreams{In: bytes.NewReader(nil), Out: &out, ErrOut: &errOut}

	err := runUpdate(context.Background(), f, ioStreams, fakeTag, true)
	if err != nil {
		t.Fatalf("runUpdate with skip-verify should proceed despite bad sums: %v", err)
	}

	dst := filepath.Join(home, "bin", platformBinaryName())
	got, err := os.ReadFile(dst) // #nosec G304 -- dst is under t.TempDir()
	if err != nil {
		t.Fatalf("reading installed binary: %v", err)
	}
	if !bytes.Equal(got, binary) {
		t.Errorf("installed binary content mismatch")
	}

	var res updateResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("stdout should be pure JSON in json/agent mode, got %q: %v", out.String(), err)
	}
	if !res.Updated || res.Version != fakeTag || res.ChecksumVerified {
		t.Errorf("unexpected JSON result: %+v", res)
	}
	if res.Path != dst {
		t.Errorf("result path = %q, want %q", res.Path, dst)
	}
}
