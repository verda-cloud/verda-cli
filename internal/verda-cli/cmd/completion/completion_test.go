package completion

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestCompletionBash(t *testing.T) {
	t.Parallel()
	testShellCompletion(t, "bash", "bash completion V2")
}

func TestCompletionZsh(t *testing.T) {
	t.Parallel()
	testShellCompletion(t, "zsh", "compdef")
}

func TestCompletionFish(t *testing.T) {
	t.Parallel()
	testShellCompletion(t, "fish", "complete")
}

func TestCompletionPowershell(t *testing.T) {
	t.Parallel()
	testShellCompletion(t, "powershell", "Register-ArgumentCompleter")
}

func TestCompletionRejectsInvalidShell(t *testing.T) {
	t.Parallel()

	root := rootWithCompletion()
	root.SetArgs([]string{"completion", "invalid"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error for invalid shell argument")
	}
}

func TestCompletionRequiresArg(t *testing.T) {
	t.Parallel()

	root := rootWithCompletion()
	root.SetArgs([]string{"completion"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error when no shell argument provided")
	}
}

func testShellCompletion(t *testing.T, shell, expectedContent string) {
	t.Helper()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &bytes.Buffer{}}

	root := &cobra.Command{Use: "verda"}
	root.AddCommand(NewCmdCompletion(ioStreams))
	root.SetArgs([]string{"completion", shell})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion %s error: %v", shell, err)
	}

	if !strings.Contains(buf.String(), expectedContent) {
		t.Fatalf("expected %s completion to contain %q, got %d bytes", shell, expectedContent, buf.Len())
	}
}

func rootWithCompletion() *cobra.Command {
	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &bytes.Buffer{}}
	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdCompletion(ioStreams))
	return root
}
