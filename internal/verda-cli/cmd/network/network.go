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

// Package network implements `verda network`: private networks.
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
	argNetwork  = "<network-id>"
	timeLayout  = "2006-01-02 15:04"
	cancelLabel = "Cancel"
	canceledMsg = "Canceled."
	defaultMark = "*"
)

// NewCmdNetwork creates the `verda network` parent command.
func NewCmdNetwork(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "network",
		Aliases:     []string{"networks", "net", "private-network", "vpc"},
		Short:       "Manage private networks",
		Annotations: map[string]string{cmdutil.TagAnnotation: "beta"},
		Long: cmdutil.LongDesc(`
			Manage private networks (VPC). A network is global; its subnets
			live in one location each. Instances on the same network reach
			each other over private IPs.

			The default network is the one a VM joins when it names no
			network. Arguments and --network take an ID.
			After network create, use subnet create to add a subnet in a
			location. Use delete to delete a network and its subnets. Use
			subnet delete to delete one subnet.
		`),
		Example: cmdutil.Examples(`
			verda network list
			verda network create --name prod
			verda network subnet create --network $NETWORK_ID -l FIN-03
			verda network subnet list --network $NETWORK_ID
			verda network delete $NETWORK_ID
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdList(f, ioStreams),
		NewCmdCreate(f, ioStreams),
		NewCmdDelete(f, ioStreams),
		NewCmdSubnet(f, ioStreams),
	)
	return cmd
}

// isInteractiveTerminal: prompts need stdin, stdout and stderr on a TTY; a var so tests can fake one.
var isInteractiveTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd()) && cmdutil.IsStdoutTerminal() && cmdutil.IsStderrTerminal()
}

// interactiveTTY: prompts only on a terminal, outside agent mode, with table output.
func interactiveTTY(f cmdutil.Factory) bool {
	return isInteractiveTerminal() && !f.AgentMode() && f.OutputFormat() == outputTable
}

// checkNetworkArg: off a TTY, <network-id> is required. Call it before
// f.VerdaClient() so bad input fails the same with or without credentials.
func checkNetworkArg(f cmdutil.Factory, args []string) error {
	if len(args) == 0 && !interactiveTTY(f) {
		return cmdutil.NewMissingFlagsError([]string{argNetwork})
	}
	return nil
}

// resolveNetworkArg: args[0] by ID; else a picker. Call checkNetworkArg first.
// A nil network with a nil error means the user canceled the picker.
func resolveNetworkArg(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, args []string) (*verda.PrivateNetwork, error) {
	if len(args) > 0 {
		ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
		defer cancel()
		return cmdutil.WithSpinner(ctx, f.Status(), "Loading private networks...", func(ctx context.Context) (*verda.PrivateNetwork, error) {
			return client.PrivateNetworks.GetByID(ctx, args[0])
		})
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
	if err != nil && !cmdutil.IsPromptCancel(err) {
		return nil, err
	}
	if err != nil || idx < 0 || idx >= len(networks) {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
		return nil, nil
	}
	return &networks[idx], nil
}

// networkByID returns nil when no network has this ID.
func networkByID(networks []verda.PrivateNetwork, id string) *verda.PrivateNetwork {
	for i := range networks {
		if networks[i].ID == id {
			return &networks[i]
		}
	}
	return nil
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
