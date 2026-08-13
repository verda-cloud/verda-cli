package bubbletea

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// Pager must not start a Bubble Tea program when its output is not a terminal:
// terminalHeight falls back to 24 there, so any content over ~22 lines would
// launch an alt-screen program against a pipe and block on input forever.
func TestPagerWithoutTerminalPrintsInsteadOfBlocking(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("line\n", 200)
	var dataOut, uiOut bytes.Buffer
	p := New(WithIO(tui.IO{Out: &dataOut, ErrOut: &uiOut, In: strings.NewReader("")}))

	done := make(chan error, 1)
	go func() { done <- p.Pager(context.Background(), content, tui.WithPagerTitle("Trash")) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Pager: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pager blocked on a non-terminal writer; it must print through instead")
	}

	if dataOut.String() != content {
		t.Errorf("content not written to data out: got %d bytes, want %d", dataOut.Len(), len(content))
	}
	if uiOut.Len() != 0 {
		t.Errorf("interactive stream should stay empty, got %q", uiOut.String())
	}
}

// Short content already took the print-through path; keep that behavior pinned.
func TestPagerShortContentPrintsThrough(t *testing.T) {
	t.Parallel()

	var dataOut, uiOut bytes.Buffer
	p := New(WithIO(tui.IO{Out: &dataOut, ErrOut: &uiOut, In: strings.NewReader("")}))

	if err := p.Pager(context.Background(), "one\ntwo\n"); err != nil {
		t.Fatalf("Pager: %v", err)
	}
	if dataOut.String() != "one\ntwo\n" {
		t.Errorf("got %q", dataOut.String())
	}
}
