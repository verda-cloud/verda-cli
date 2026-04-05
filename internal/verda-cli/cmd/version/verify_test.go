package version

import (
	"bytes"
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

	body := `aaa111  verda_linux_amd64/verda
bbb222  verda_darwin_arm64/verda
ccc333  verda_windows_amd64/verda.exe
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
	if err := os.WriteFile(path, content, 0644); err != nil {
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
		fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	client := srv.Client()
	body, err := fetchChecksums(client, srv.URL)
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
	_, err := fetchChecksums(client, srv.URL)
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
	if err := os.WriteFile(binPath, content, 0755); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(h[:])

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	checksumBody := fmt.Sprintf("%s  verda_%s_%s/verda\n", expectedHash, goos, goarch)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	result, err := verifyBinary(srv.Client(), binPath, srv.URL, goos, goarch)
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
	if err := os.WriteFile(binPath, []byte("tampered binary"), 0755); err != nil {
		t.Fatal(err)
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	checksumBody := fmt.Sprintf("%s  verda_%s_%s/verda\n", "0000000000000000000000000000000000000000000000000000000000000000", goos, goarch)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksumBody)
	}))
	defer srv.Close()

	result, err := verifyBinary(srv.Client(), binPath, srv.URL, goos, goarch)
	if err != nil {
		t.Fatalf("verifyBinary error: %v", err)
	}
	if result.Match {
		t.Error("expected match=false")
	}
}

func TestRunVerifyDevBuild(t *testing.T) {
	t.Parallel()

	var outBuf, errBuf bytes.Buffer

	err := runVerify(&outBuf, &errBuf, "", nil, "v0.0.0-dev", "", "")
	if err == nil {
		t.Fatal("expected error for dev build")
	}
	if !strings.Contains(errBuf.String(), "development build") {
		t.Errorf("expected warning about development build, got: %q", errBuf.String())
	}
}
