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

package sshkey

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	ID  string
	Yes bool
}

// NewCmdDelete creates the ssh-key delete cobra command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete [<id>]",
		Aliases: []string{"rm"},
		Short:   "Delete an SSH key",
		Long: cmdutil.LongDesc(`
			Delete an SSH key from your account. In interactive mode you will be
			prompted to select a key and confirm deletion. Pass the key id as an
			argument or with --id for non-interactive use. Agent mode requires an
			id and --yes.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda ssh-key delete

			# Non-interactive
			verda ssh-key delete abc-123
			verda ssh-key delete --id abc-123

			# Agent mode
			verda --agent ssh-key delete abc-123 --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				if opts.ID != "" {
					return cmdutil.UsageErrorf(cmd, "pass the SSH key id either as an argument or with --id, not both")
				}
				opts.ID = args[0]
			}
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.ID, "id", "", "SSH key ID to delete (alternative to the positional argument)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions (required in agent mode)")

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions) error {
	// Agent mode never prompts: deleting without --yes is an explicit error.
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	keyID := opts.ID
	keyName := keyID

	if keyID == "" {
		// Interactive: list keys and let user select.
		id, name, err := selectKey(ctx, f, ioStreams, prompter, client)
		if err != nil {
			return err
		}
		if id == "" {
			return nil // Canceled or no keys available.
		}
		keyID, keyName = id, name
	}

	// Confirm deletion.
	if !opts.Yes {
		confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Are you sure you want to delete SSH key %q?", keyName))
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
				return nil
			}
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deleting SSH key:", map[string]string{"id": keyID, "name": keyName})

	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp2 interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp2, _ = status.Spinner(deleteCtx, "Deleting SSH key...")
	}
	err = client.SSHKeys.DeleteSSHKey(deleteCtx, keyID)
	if sp2 != nil {
		sp2.Stop("")
	}
	if err != nil {
		return err
	}

	if f.AgentMode() {
		result := map[string]string{
			"id":     keyID,
			"name":   keyName,
			"action": "delete",
			"status": "completed",
		}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted SSH key: %s (%s)\n", keyName, keyID)
	return nil
}

// selectKey lists SSH keys and prompts the user to pick one for deletion.
// Returns zero values when the user cancels or no keys exist.
func selectKey(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, prompter tui.Prompter, client *verda.Client) (keyID, keyName string, _ error) {
	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(listCtx, "Loading SSH keys...")
	}
	keys, err := client.SSHKeys.GetAllSSHKeys(listCtx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return "", "", err
	}

	if len(keys) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No SSH keys found.")
		return "", "", nil
	}

	labels := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		labels = append(labels, fmt.Sprintf("%s  %s  %s", k.Name, k.ID, k.Fingerprint))
	}
	labels = append(labels, "Cancel")

	idx, err := prompter.Select(ctx, "Select SSH key to delete", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", "", nil // Esc/Ctrl+C — clean exit.
		}
		return "", "", err
	}
	if idx == len(keys) {
		return "", "", nil
	}
	return keys[idx].ID, keys[idx].Name, nil
}
