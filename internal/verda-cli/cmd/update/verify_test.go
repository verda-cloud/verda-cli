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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseChecksumLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantHex string
		wantKey string
		wantOK  bool
	}{
		{
			name:    "valid linux amd64",
			line:    "abc123def456  verda_linux_amd64/verda",
			wantHex: "abc123def456",
			wantKey: "verda_linux_amd64/verda",
			wantOK:  true,
		},
		{
			name:    "valid darwin arm64",
			line:    "deadbeef  verda_darwin_arm64/verda",
			wantHex: "deadbeef",
			wantKey: "verda_darwin_arm64/verda",
			wantOK:  true,
		},
		{
			name:    "valid windows",
			line:    "cafebabe  verda_windows_amd64/verda.exe",
			wantHex: "cafebabe",
			wantKey: "verda_windows_amd64/verda.exe",
			wantOK:  true,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "comment line",
			line:   "# this is a comment",
			wantOK: false,
		},
		{
			name:   "single field",
			line:   "abc123def456",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hex, key, ok := parseChecksumLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("parseChecksumLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if ok {
				if hex != tt.wantHex {
					t.Errorf("hex = %q, want %q", hex, tt.wantHex)
				}
				if key != tt.wantKey {
					t.Errorf("key = %q, want %q", key, tt.wantKey)
				}
			}
		})
	}
}

func TestFindMatchingChecksum(t *testing.T) {
	t.Parallel()

	// GoReleaser dist directories include arch variant suffixes
	body := `aaa111  verda_linux_amd64_v1/verda
bbb222  verda_darwin_arm64_v8.0/verda
ccc333  verda_windows_amd64_v1/verda.exe
`

	tests := []struct {
		name     string
		goos     string
		goarch   string
		wantHash string
		wantErr  bool
	}{
		{
			name:     "linux amd64",
			goos:     "linux",
			goarch:   "amd64",
			wantHash: "aaa111",
		},
		{
			name:     "darwin arm64",
			goos:     "darwin",
			goarch:   "arm64",
			wantHash: "bbb222",
		},
		{
			name:     "windows amd64",
			goos:     "windows",
			goarch:   "amd64",
			wantHash: "ccc333",
		},
		{
			name:    "unknown platform",
			goos:    "freebsd",
			goarch:  "riscv64",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := findMatchingChecksum(body, tt.goos, tt.goarch)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantHash {
				t.Errorf("got %q, want %q", got, tt.wantHash)
			}
		})
	}
}

func TestHashFile(t *testing.T) {
	t.Parallel()

	// Create a temp file with known content.
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	content := []byte("hello world\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}

	// Compute expected hash.
	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])

	got, err := hashFile(path)
	if err != nil {
		t.Fatalf("hashFile error: %v", err)
	}
	if got != want {
		t.Errorf("hashFile = %q, want %q", got, want)
	}
}

func TestHashFileNotFound(t *testing.T) {
	t.Parallel()
	_, err := hashFile("/nonexistent/path/to/file")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestChecksumURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    string
	}{
		{
			version: "v1.2.3",
			want:    "https://github.com/verda-cloud/verda-cli/releases/download/v1.2.3/verda_1.2.3_binary_SHA256SUMS",
		},
		{
			version: "v0.5.0",
			want:    "https://github.com/verda-cloud/verda-cli/releases/download/v0.5.0/verda_0.5.0_binary_SHA256SUMS",
		},
		{
			version: "1.3.0",
			want:    "https://github.com/verda-cloud/verda-cli/releases/download/v1.3.0/verda_1.3.0_binary_SHA256SUMS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			got := checksumURL(tt.version)
			if got != tt.want {
				t.Errorf("checksumURL(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestFetchChecksums(t *testing.T) {
	t.Parallel()

	checksumBody := "abc123  verda_linux_amd64/verda\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	client := srv.Client()
	body, err := fetchChecksums(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("fetchChecksums error: %v", err)
	}
	if body != checksumBody {
		t.Errorf("body = %q, want %q", body, checksumBody)
	}
}

func TestFetchChecksums404(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	_, err := fetchChecksums(context.Background(), client, srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestVerifyBinaryMatch(t *testing.T) {
	t.Parallel()

	// Create a temp binary with known content.
	dir := t.TempDir()
	binPath := filepath.Join(dir, "verda")
	content := []byte("fake binary content")
	if err := os.WriteFile(binPath, content, 0600); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(h[:])

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	checksumBody := fmt.Sprintf("%s  verda_%s_%s_v1/verda\n", expectedHash, goos, goarch)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	result, err := verifyBinary(context.Background(), srv.Client(), binPath, srv.URL, goos, goarch)
	if err != nil {
		t.Fatalf("verifyBinary error: %v", err)
	}
	if !result.Match {
		t.Error("expected match=true")
	}
	if result.ActualHash != expectedHash {
		t.Errorf("actual hash = %q, want %q", result.ActualHash, expectedHash)
	}
	if result.ExpectedHash != expectedHash {
		t.Errorf("expected hash = %q, want %q", result.ExpectedHash, expectedHash)
	}
}

func TestVerifyBinaryMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "verda")
	if err := os.WriteFile(binPath, []byte("tampered binary"), 0600); err != nil {
		t.Fatal(err)
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	checksumBody := fmt.Sprintf("%s  verda_%s_%s/verda\n", "0000000000000000000000000000000000000000000000000000000000000000", goos, goarch)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	result, err := verifyBinary(context.Background(), srv.Client(), binPath, srv.URL, goos, goarch)
	if err != nil {
		t.Fatalf("verifyBinary error: %v", err)
	}
	if result.Match {
		t.Error("expected match=false")
	}
}

func TestFindMatchingChecksumEdgeCases(t *testing.T) {
	t.Parallel()

	// Keys without a GoReleaser variant suffix (e.g. "verda_linux_arm64/verda")
	// must match too.
	body := "fff666  verda_linux_arm64/verda\n"
	got, err := findMatchingChecksum(body, "linux", "arm64")
	if err != nil {
		t.Fatalf("unexpected error for suffix-less key: %v", err)
	}
	if got != "fff666" {
		t.Errorf("got %q, want %q", got, "fff666")
	}

	// A key that merely starts with the os_arch prefix (no "/" or "_" boundary)
	// must not match.
	body = "zzz999  verda_linux_amd64evil/verda\n"
	if _, err := findMatchingChecksum(body, "linux", "amd64"); err == nil {
		t.Error("expected error for prefix-collision key, got match")
	}

	// Body with only comments and blanks yields no match.
	body = "# comment\n\n   \n"
	if _, err := findMatchingChecksum(body, "linux", "amd64"); err == nil {
		t.Error("expected error for comment-only body")
	}
}

func TestFindArchiveChecksum(t *testing.T) {
	t.Parallel()

	body := `# goreleaser archive sums
aaa111  verda_1.0.0_linux_amd64.tar.gz
bbb222  verda_1.0.0_linux_amd64.deb
ccc333  verda_1.0.0_darwin_arm64.tar.gz
`

	tests := []struct {
		name     string
		artifact string
		wantHash string
		wantErr  bool
	}{
		{name: "exact archive", artifact: "verda_1.0.0_linux_amd64.tar.gz", wantHash: "aaa111"},
		{name: "same prefix different ext", artifact: "verda_1.0.0_linux_amd64.deb", wantHash: "bbb222"},
		{name: "other platform", artifact: "verda_1.0.0_darwin_arm64.tar.gz", wantHash: "ccc333"},
		{name: "prefix without ext must not match", artifact: "verda_1.0.0_linux_amd64", wantErr: true},
		{name: "unknown artifact", artifact: "verda_2.0.0_linux_amd64.tar.gz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := findArchiveChecksum(body, tt.artifact)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got match %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantHash {
				t.Errorf("got %q, want %q", got, tt.wantHash)
			}
		})
	}
}

func TestVerifyArchiveChecksum(t *testing.T) {
	t.Parallel()

	data := []byte("archive payload")
	sum := sha256.Sum256(data)
	goodBody := hex.EncodeToString(sum[:]) + "  verda_1.0.0_linux_amd64.tar.gz\n"

	if err := verifyArchiveChecksum(data, goodBody, "verda_1.0.0_linux_amd64.tar.gz"); err != nil {
		t.Errorf("expected match, got error: %v", err)
	}

	badBody := strings.Replace(goodBody, hex.EncodeToString(sum[:]), "0000000000000000000000000000000000000000000000000000000000000000", 1)
	err := verifyArchiveChecksum(data, badBody, "verda_1.0.0_linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should describe the mismatch, got: %v", err)
	}

	if err := verifyArchiveChecksum(data, goodBody, "verda_9.9.9_windows_amd64.zip"); err == nil {
		t.Error("expected error for artifact missing from sums file")
	}
}

func TestRunVerifyDevBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
	}{
		{"with v prefix", "v0.0.0-dev"},
		{"without v prefix", "0.0.0-dev"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var outBuf, errBuf bytes.Buffer
			err := runVerify(context.Background(), &outBuf, &errBuf, "", nil, tt.version, "", "")
			if err == nil {
				t.Fatal("expected error for dev build")
			}
			if !strings.Contains(errBuf.String(), "development build") {
				t.Errorf("expected warning about development build, got: %q", errBuf.String())
			}
		})
	}
}
