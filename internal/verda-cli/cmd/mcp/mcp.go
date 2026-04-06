package mcp

import (
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdMCP creates the parent `verda mcp` command.
func NewCmdMCP(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI agent integration",
		Long: cmdutil.LongDesc(`
			Model Context Protocol (MCP) server that exposes Verda Cloud
			operations as structured tools for AI agents.

			Requires valid credentials — run "verda auth login" first.
		`),
	}

	cmd.AddCommand(NewCmdServe(f, ioStreams))

	return cmd
}

// NewCmdServe creates the `verda mcp serve` command.
func NewCmdServe(f cmdutil.Factory, _ cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start MCP server on stdio",
		Long: cmdutil.LongDesc(`
			Start a Model Context Protocol (MCP) server that communicates
			over stdin/stdout. Configure in your AI agent:

			  {
			    "mcpServers": {
			      "verda": {
			        "command": "verda",
			        "args": ["mcp", "serve"]
			      }
			    }
			  }
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Defer credential resolution and client creation to the
			// first tool call so the MCP handshake completes instantly.
			opts := f.Options()
			server := NewLazyServer(func() (*verda.Client, error) {
				opts.Complete()
				if err := opts.Validate(); err != nil {
					return nil, err
				}
				return f.VerdaClient()
			})
			return server.ServeStdio(cmd.Context())
		},
	}
}
