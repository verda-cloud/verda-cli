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

package startupscript

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
			if cmdutil.IsPromptCancel(err) {
				return nil // User pressed Esc/Ctrl+C.
			}
			return err
		}
		if name == "" {
			return errors.New("name is required")
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
		content, err = promptScriptContent(ctx, prompter)
		if err != nil {
			return err
		}
		if content == "" {
			return nil // User canceled or left input blank.
		}
	}

	if strings.TrimSpace(content) == "" {
		return errors.New("script content is required")
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

// promptScriptContent asks the user for the script source and collects the
// content. Returns ("", nil) when the user cancels.
func promptScriptContent(ctx context.Context, prompter tui.Prompter) (string, error) {
	sourceIdx, err := prompter.Select(ctx, "Script source", []string{
		"Load from file",
		"Paste content",
	}, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil // User pressed Esc/Ctrl+C.
		}
		return "", err
	}

	if sourceIdx == 0 { // Load from file
		return promptScriptFromFile(ctx, prompter)
	}

	// Paste content
	content, err := prompter.Editor(ctx, "Script content",
		tui.WithEditorDefault("#!/bin/bash\n\n# Your startup script here\n"),
		tui.WithFileExt(".sh"))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil // User pressed Esc/Ctrl+C.
		}
		return "", err
	}
	return content, nil
}

// promptScriptFromFile asks for a file path and reads the script from it.
// Returns ("", nil) when the user cancels or leaves the path blank.
func promptScriptFromFile(ctx context.Context, prompter tui.Prompter) (string, error) {
	path, err := prompter.TextInput(ctx, "File path")
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil // User pressed Esc/Ctrl+C.
		}
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("reading script file: %w", err)
	}
	return string(data), nil
}
