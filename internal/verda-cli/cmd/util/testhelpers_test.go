package util

import "github.com/spf13/cobra"

func stubCommand() *cobra.Command {
	return &cobra.Command{Use: "test"}
}
