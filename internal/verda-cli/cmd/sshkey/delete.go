package sshkey

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	ID string
}

// NewCmdDelete creates the ssh-key delete cobra command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"rm"},
		Short:   "Delete an SSH key",
		Long: cmdutil.LongDesc(`
			Delete an SSH key from your account. In interactive mode you will be
			prompted to select a key and confirm deletion. Use --id for
			non-interactive use.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda ssh-key delete

			# Non-interactive
			verda ssh-key delete --id abc-123
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.ID, "id", "", "SSH key ID to delete")

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	keyID := opts.ID
	keyName := keyID

	if keyID == "" { //nolint:nestif // Interactive prompt flow requires nested conditionals.
		// Interactive: list keys and let user select.
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
			return err
		}

		if len(keys) == 0 {
			_, _ = fmt.Fprintln(ioStreams.Out, "No SSH keys found.")
			return nil
		}

		labels := make([]string, 0, len(keys)+1)
		for _, k := range keys {
			labels = append(labels, fmt.Sprintf("%s  %s  %s", k.Name, k.ID, k.Fingerprint))
		}
		labels = append(labels, "Cancel")

		idx, err := prompter.Select(ctx, "Select SSH key to delete", labels)
		if err != nil {
			return nil
		}
		if idx == len(keys) {
			return nil
		}

		keyID = keys[idx].ID
		keyName = keys[idx].Name
	}

	// Confirm deletion.
	confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Are you sure you want to delete SSH key %q?", keyName))
	if err != nil || !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
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

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted SSH key: %s (%s)\n", keyName, keyID)
	return nil
}
