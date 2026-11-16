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

// Package network implements `verda network`: private networks, subnets and routes.
package network

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	outputTable = "table"
	flagName    = "name"
	flagDefault = "default"
	flagYes     = "yes"
	flagNetwork = "network"
	argNetwork  = "<network>"
	timeLayout  = "2006-01-02 15:04"
	cancelLabel = "Cancel"
	canceledMsg = "Canceled."
	noneMark    = "-"
	defaultMark = "*"
)

// NewCmdNetwork creates the `verda network` parent command.
func NewCmdNetwork(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "network",
		Aliases:     []string{"networks", "net", "private-network", "vpc"},
		Short:       "Manage private networks, subnets and routes",
		Annotations: map[string]string{cmdutil.TagAnnotation: "beta"},
		Long: cmdutil.LongDesc(`
			Manage private networks (VPC). A network is global; its subnets
			live in one location each. Instances on the same network reach
			each other over private IPs. A route sends a subnet's internet
			traffic through one of your instances (a NAT gateway).

			The default network is the one a VM joins when it names no
			network. Arguments accept an ID (pn_..., sn_...) or a name.
		`),
		Example: cmdutil.Examples(`
			verda network list
			verda network create --name prod
			verda network describe prod
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdList(f, ioStreams),
		NewCmdDescribe(f, ioStreams),
	)
	return cmd
}

// isInteractiveTerminal: prompts read stdin and draw on stderr, so all three
// streams must be terminals. A var so tests can drive the interactive paths.
var isInteractiveTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd()) && cmdutil.IsStdoutTerminal() && cmdutil.IsStderrTerminal()
}

// interactiveTTY: prompts only on a terminal, outside agent mode, with table output.
func interactiveTTY(f cmdutil.Factory) bool {
	return isInteractiveTerminal() && !f.AgentMode() && f.OutputFormat() == outputTable
}

func spinner(ctx context.Context, f cmdutil.Factory, msg string) func() {
	status := f.Status()
	if status == nil {
		return func() {}
	}
	sp, _ := status.Spinner(ctx, msg)
	return func() {
		if sp != nil {
			sp.Stop("")
		}
	}
}

// resolveNetworkArg: args[0] by ID or name; else a picker on a TTY; else a missing-arg error.
// A nil network with a nil error means the user canceled the picker.
func resolveNetworkArg(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, args []string) (*verda.PrivateNetwork, error) {
	if len(args) > 0 {
		ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
		defer cancel()
		stop := spinner(ctx, f, "Loading private networks...")
		n, err := cmdutil.ResolveNetwork(ctx, client, args[0])
		stop()
		return n, err
	}
	if !interactiveTTY(f) {
		return nil, cmdutil.NewMissingFlagsError([]string{argNetwork})
	}
	return selectNetwork(cmd.Context(), f, ioStreams, client)
}

// selectNetwork prompts for a network; cancel or an empty list → nil, nil.
func selectNetwork(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client) (*verda.PrivateNetwork, error) {
	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	networks, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading private networks...", func(ctx context.Context) ([]verda.PrivateNetwork, error) {
		return client.PrivateNetworks.Get(ctx)
	})
	if err != nil {
		return nil, err
	}
	if len(networks) == 0 {
		printNoNetworks(ioStreams)
		return nil, nil
	}

	labels := make([]string, 0, len(networks)+1)
	for i := range networks {
		labels = append(labels, networkLabel(&networks[i]))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select private network", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	if idx < 0 || idx >= len(networks) {
		return nil, nil
	}
	return &networks[idx], nil
}

func networkLabel(n *verda.PrivateNetwork) string {
	label := fmt.Sprintf("%s  (%s, %s)", n.Name, n.ID, n.Mode)
	if n.IsDefault {
		label += "  default"
	}
	return label
}

func printNoNetworks(ioStreams cmdutil.IOStreams) {
	_, _ = fmt.Fprintln(ioStreams.Out, "No private networks found.")
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda network create")
}

func defaultColumn(isDefault bool) string {
	if isDefault {
		return defaultMark
	}
	return ""
}
