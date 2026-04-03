package util

import (
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// IOStreams provides the standard names for iostreams, making it easy to
// test commands that read/write to stdin, stdout, and stderr.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// NewStdIOStreams returns an IOStreams wired to os.Stdin, os.Stdout, and os.Stderr.
func NewStdIOStreams() IOStreams {
	return IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
}

// IsStdoutTerminal returns true if stdout is a terminal (not piped/redirected).
func IsStdoutTerminal() bool {
	return term.IsTerminal(os.Stdout.Fd())
}
