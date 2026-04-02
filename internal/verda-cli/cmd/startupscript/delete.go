package startupscript

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	ID string
}

// NewCmdDelete creates the startup-script delete cobra command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"rm"},
		Short:   "Delete a startup script",
		Long: cmdutil.LongDesc(`
			Delete a startup script from your account. In interactive mode you
			will be prompted to select a script and confirm deletion. Use --id
			for non-interactive use.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda startup-script delete

			# Non-interactive
			verda startup-script delete --id abc-123
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.ID, "id", "", "Startup script ID to delete")

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	scriptID := opts.ID
	scriptName := scriptID

	if scriptID == "" {
		// Interactive: list scripts and let user select.
		listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
		defer cancel()

		var sp interface{ Stop(string) }
		if status := f.Status(); status != nil {
			sp, _ = status.Spinner(listCtx, "Loading startup scripts...")
		}
		scripts, err := client.StartupScripts.GetAllStartupScripts(listCtx)
		if sp != nil {
			sp.Stop("")
		}
		if err != nil {
			return err
		}

		if len(scripts) == 0 {
			_, _ = fmt.Fprintln(ioStreams.Out, "No startup scripts found.")
			return nil
		}

		labels := make([]string, len(scripts))
		for i, s := range scripts {
			labels[i] = fmt.Sprintf("%s  %s", s.Name, s.ID)
		}
		labels = append(labels, "Cancel")

		idx, err := prompter.Select(ctx, "Select startup script to delete", labels)
		if err != nil {
			return nil //nolint:nilerr
		}
		if idx == len(scripts) {
			return nil
		}

		scriptID = scripts[idx].ID
		scriptName = scripts[idx].Name
	}

	// Confirm deletion.
	confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Are you sure you want to delete startup script %q?", scriptName))
	if err != nil || !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
		return nil
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deleting startup script:", map[string]string{"id": scriptID, "name": scriptName})

	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp2 interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp2, _ = status.Spinner(deleteCtx, "Deleting startup script...")
	}
	err = client.StartupScripts.DeleteStartupScript(deleteCtx, scriptID)
	if sp2 != nil {
		sp2.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted startup script: %s (%s)\n", scriptName, scriptID)
	return nil
}
