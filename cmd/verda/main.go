package main

import (
	"errors"
	"fmt"
	"os"

	cmd "github/verda-cloud/verda-cli/internal/verda-cli/cmd"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func main() {
	root := cmd.NewRootCommand(cmdutil.NewStdIOStreams())
	if err := root.Execute(); err != nil {
		var ae *cmdutil.AgentError
		if errors.As(err, &ae) {
			cmdutil.WriteAgentError(os.Stderr, ae)
			os.Exit(ae.ExitCode)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
