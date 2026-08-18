package bubbletea

import (
	"bytes"
	"os"
	"testing"
)

// fdWriter is the shape the CLI hands the prompter: a writer that filters ANSI
// on the way out while still exposing the underlying descriptor.
type fdWriter struct {
	bytes.Buffer
	fd uintptr
}

func (w *fdWriter) Fd() uintptr                { return w.fd }
func (w *fdWriter) Read(_ []byte) (int, error) { return 0, nil }
func (w *fdWriter) Close() error               { return nil }

// rendersToTerminal must follow the fd, not the concrete type. Matching only
// *os.File made it answer false for every wrapped stream, which silently
// disabled every spinner, progress bar and pager on a real terminal.
func TestRendersToTerminalSeesThroughAWrapper(t *testing.T) {
	t.Parallel()

	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no controlling terminal available: %v", err)
	}
	defer func() { _ = tty.Close() }()

	if !rendersToTerminal(tty) {
		t.Fatal("a raw *os.File tty was not detected as a terminal")
	}
	if !rendersToTerminal(&fdWriter{fd: tty.Fd()}) {
		t.Error("a wrapper forwarding a tty fd was not detected as a terminal")
	}
}

// The non-terminal answer has to stay false, or piped runs start launching
// alt-screen programs against a pipe.
func TestRendersToTerminalRejectsNonTerminals(t *testing.T) {
	t.Parallel()

	if rendersToTerminal(&bytes.Buffer{}) {
		t.Error("a bytes.Buffer was treated as a terminal")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	if rendersToTerminal(w) {
		t.Error("a pipe was treated as a terminal")
	}
	if rendersToTerminal(&fdWriter{fd: w.Fd()}) {
		t.Error("a wrapper forwarding a pipe fd was treated as a terminal")
	}
}
