package startupscript

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type addOptions struct {
	Name   string
	File   string
	Script string
}

// NewCmdAdd creates the startup-script add cobra command.
func NewCmdAdd(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &addOptions{}

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a startup script",
		Long: cmdutil.LongDesc(`
			Add a new startup script to your account. In interactive mode you will
			be prompted for the script name and then asked to load from a file or
			paste the content. Use flags for non-interactive use.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda startup-script add

			# From file
			verda startup-script add --name setup --file ./init.sh

			# Inline script
			verda startup-script add --name setup --script "#!/bin/bash\napt update"
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Script name")
	flags.StringVar(&opts.File, "file", "", "Path to a script file")
	flags.StringVar(&opts.Script, "script", "", "Inline script content")

	return cmd
}

func runAdd(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *addOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	name := opts.Name
	if name == "" {
		name, err = prompter.TextInput(ctx, "Script name")
		if err != nil {
			return nil //nolint:nilerr
		}
		if name == "" {
			return fmt.Errorf("name is required")
		}
	}

	var content string
	switch {
	case opts.File != "":
		data, err := os.ReadFile(opts.File)
		if err != nil {
			return fmt.Errorf("reading script file: %w", err)
		}
		content = string(data)
	case opts.Script != "":
		content = opts.Script
	default:
		// Interactive: ask user to load from file or paste content.
		sourceIdx, err := prompter.Select(ctx, "Script source", []string{
			"Load from file",
			"Paste content",
		})
		if err != nil {
			return nil //nolint:nilerr
		}

		switch sourceIdx {
		case 0: // Load from file
			path, err := prompter.TextInput(ctx, "File path")
			if err != nil || strings.TrimSpace(path) == "" {
				return nil //nolint:nilerr
			}
			data, err := os.ReadFile(strings.TrimSpace(path))
			if err != nil {
				return fmt.Errorf("reading script file: %w", err)
			}
			content = string(data)
		case 1: // Paste content
			content, err = prompter.Editor(ctx, "Script content (Ctrl+D to finish)",
				tui.WithEditorDefault("#!/bin/bash\n\n# Your startup script here\n"),
				tui.WithFileExt(".sh"))
			if err != nil {
				return nil //nolint:nilerr
			}
		}
	}

	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("script content is required")
	}

	req := &verda.CreateStartupScriptRequest{
		Name:   name,
		Script: content,
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	createCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(createCtx, "Adding startup script...")
	}
	script, err := client.StartupScripts.AddStartupScript(createCtx, req)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Added startup script: %s (%s)\n", script.Name, script.ID)
	return nil
}
