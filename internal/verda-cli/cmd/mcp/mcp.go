// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdMCP creates the parent `verda mcp` command.
func NewCmdMCP(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI agent integration (beta)",
		Long: cmdutil.LongDesc(`
			[Beta] Model Context Protocol (MCP) server that exposes Verda Cloud
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
			fmt.Fprintln(os.Stderr, "Verda MCP server starting...")

			// Defer credential resolution and client creation to the
			// first tool call so the MCP handshake completes instantly.
			opts := f.Options()
			server := NewLazyServer(func() (*verda.Client, error) {
				fmt.Fprintln(os.Stderr, "Verda MCP: authenticating...")
				opts.Complete()
				if err := opts.Validate(); err != nil {
					return nil, err
				}
				return f.VerdaClient()
			})
			fmt.Fprintln(os.Stderr, "Verda MCP server listening on stdio")
			return server.ServeStdio(cmd.Context())
		},
	}
}
