package main

import (
	"fmt"
	"os"

	cmd "github/verda-cloud/verda-cli/internal/verda-cli/cmd"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func main() {
	root := cmd.NewRootCommand(cmdutil.NewStdIOStreams())
	if err := root.Execute(); err != nil {
		// In agent mode, always emit structured JSON errors.
		ae := cmdutil.ClassifyError(err)
		if ae != nil {
			cmdutil.WriteAgentError(os.Stderr, ae)
			os.Exit(ae.ExitCode)
		}
		// Normal mode: plain text error.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
