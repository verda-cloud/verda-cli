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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
