package util

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// CheckErr prints a user-friendly error to stderr and exits with code 1.
func CheckErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// UsageErrorf creates a formatted usage error that hints the user to run --help.
func UsageErrorf(cmd *cobra.Command, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nSee '%s --help' for help and examples", msg, cmd.CommandPath())
}

// DefaultSubCommandRun prints help when a parent command is invoked without a subcommand.
func DefaultSubCommandRun(out io.Writer) func(c *cobra.Command, args []string) {
	return func(c *cobra.Command, args []string) {
		c.SetOut(out)
		c.SetErr(out)
		RequireNoArguments(c, args)
		_ = c.Help()
	}
}

// RequireNoArguments prints a usage error and exits if extra arguments are present.
func RequireNoArguments(c *cobra.Command, args []string) {
	if len(args) > 0 {
		CheckErr(UsageErrorf(c, "unknown command %q", strings.Join(args, " ")))
	}
}
