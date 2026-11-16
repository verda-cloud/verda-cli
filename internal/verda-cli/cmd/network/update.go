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

package network

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	changeRename      = "rename"
	changeMakeDefault = "make-default"
	stepChanges       = "changes"
)

// newEngine builds the wizard engine; a var so tests can inject results.
var newEngine = func(prompter tui.Prompter, status tui.Status, out io.Writer) *wizard.Engine {
	return wizard.NewEngine(prompter, status, wizard.WithOutput(out))
}

// updateOptions holds the requested changes; nameSet/defaultSet mark what to send.
type updateOptions struct {
	Name       string
	Default    bool
	nameSet    bool
	defaultSet bool
}

func (o *updateOptions) hasChanges() bool { return o.nameSet || o.defaultSet }

// NewCmdUpdate creates the network update command.
func NewCmdUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &updateOptions{}
	cmd := &cobra.Command{
		Use:   "update [<network>]",
		Short: "Rename a private network or make it the default",
		Long: cmdutil.LongDesc(`
			Rename a private network and/or make it the default network.
			--default only sets: to change the default, make another
			network the default instead (--default=false is rejected).

			On a terminal with no change flags, pick the changes from a
			list. In scripts and agent mode, pass --name and/or --default.
		`),
		Example: cmdutil.Examples(`
			verda network update prod --name production
			verda network update pn_d90ky5tk --default
			verda network update
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.nameSet = cmd.Flags().Changed(flagName)
			opts.defaultSet = cmd.Flags().Changed(flagDefault)
			return runUpdate(cmd, f, ioStreams, opts, args)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, flagName, "", "New name (1-64 characters)")
	flags.BoolVar(&opts.Default, flagDefault, false, "Make this the default network")
	return cmd
}

// validateUpdateFlags checks flag values before any API call.
func validateUpdateFlags(opts *updateOptions, kind string) error {
	opts.Name = strings.TrimSpace(opts.Name)
	if opts.defaultSet && !opts.Default {
		return cmdutil.NewValidationError("default", "--default only sets; make another "+kind+" the default instead")
	}
	if opts.nameSet {
		if err := validateName(opts.Name); err != nil {
			return cmdutil.NewValidationError("name", err.Error())
		}
	}
	return nil
}

// missingUpdateFlags: off a terminal an update needs a target and a change flag.
func missingUpdateFlags(opts *updateOptions, args []string, target string) error {
	if opts.hasChanges() {
		return nil
	}
	var missing []string
	if len(args) == 0 {
		missing = append(missing, target)
	}
	return cmdutil.NewMissingOneOfFlagsError(missing, []string{"--" + flagName, "--" + flagDefault})
}

func runUpdate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *updateOptions, args []string) error {
	if err := validateUpdateFlags(opts, "network"); err != nil {
		return err
	}
	interactive := interactiveTTY(f)
	if !interactive {
		if err := missingUpdateFlags(opts, args, argNetwork); err != nil {
			return err
		}
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, err := resolveNetworkArg(cmd, f, ioStreams, client, args)
	if err != nil || network == nil {
		return err
	}

	if !opts.hasChanges() {
		if err := runChangeFlow(cmd.Context(), f, ioStreams, opts, network.Name, network.IsDefault); err != nil {
			return err
		}
		ok, err := confirmUpdate(cmd.Context(), f, ioStreams, opts, "network", network.Name)
		if err != nil || !ok {
			return err
		}
	}

	req := verda.UpdatePrivateNetworkRequest{}
	if opts.nameSet {
		req.Name = &opts.Name
	}
	if opts.defaultSet {
		req.IsDefault = &opts.Default
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	updated, err := cmdutil.WithSpinner(ctx, f.Status(), "Updating private network...", func(ctx context.Context) (*verda.PrivateNetwork, error) {
		return client.PrivateNetworks.Update(ctx, network.ID, req)
	})
	if err != nil {
		return err
	}
	view := cmdutil.NewNetworkView(updated)
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Updated private network %s (%s)%s\n", view.Name, view.ID, defaultSuffix(view.IsDefault))
	return nil
}

func defaultSuffix(isDefault bool) string {
	if isDefault {
		return ", default"
	}
	return ""
}

// runChangeFlow asks which changes to make, then the new name; it fills opts.
func runChangeFlow(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *updateOptions, currentName string, isDefault bool) error {
	choices := changeChoices(isDefault)
	var picked []string
	flow := &wizard.Flow{
		Name: "update",
		Steps: []wizard.Step{
			{
				Name:        stepChanges,
				Description: "What do you want to change?",
				Prompt:      wizard.MultiSelectPrompt,
				Required:    true,
				Loader:      wizard.StaticChoices(choices...),
				Setter: func(v any) {
					picked, _ = v.([]string)
					opts.defaultSet = slices.Contains(picked, changeMakeDefault)
					opts.Default = opts.defaultSet
				},
				Resetter: func() { picked, opts.defaultSet, opts.Default = nil, false, false },
				IsSet:    func() bool { return false },
				Value:    func() any { return picked },
				MinError: func(int) string { return "pick at least one change" },
			},
			{
				Name:        flagName,
				Description: "New name (now " + currentName + ")",
				Prompt:      wizard.TextInputPrompt,
				Required:    true,
				DependsOn:   []string{stepChanges},
				ShouldSkip:  func(c map[string]any) bool { return !hasChange(c[stepChanges], changeRename) },
				Validate:    func(v any) error { return validateName(v.(string)) },
				Setter: func(v any) {
					name, _ := v.(string)
					opts.Name = strings.TrimSpace(name)
					opts.nameSet = true
				},
				Resetter: func() { opts.Name, opts.nameSet = "", false },
				IsSet:    func() bool { return false },
				Value:    func() any { return opts.Name },
			},
		},
	}
	return newEngine(f.Prompter(), f.Status(), ioStreams.ErrOut).Run(ctx, flow)
}

// changeChoices: "Make default" is hidden when the target already is the default.
func changeChoices(isDefault bool) []wizard.Choice {
	choices := []wizard.Choice{{Label: "Rename", Value: changeRename}}
	if !isDefault {
		choices = append(choices, wizard.Choice{Label: "Make default", Value: changeMakeDefault})
	}
	return choices
}

func hasChange(v any, change string) bool {
	picked, _ := v.([]string)
	return slices.Contains(picked, change)
}

// confirmUpdate prints the summary and asks; false on decline or a clean cancel.
func confirmUpdate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *updateOptions, kind, name string) (bool, error) {
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Update %s %s:\n", kind, name)
	if opts.nameSet {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "    Rename:        %s → %s\n", name, opts.Name)
	}
	if opts.defaultSet {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "    Make default:  yes (replaces the current default %s)\n", kind)
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	ok, err := f.Prompter().Confirm(ctx, "Apply these changes?")
	if err != nil && !cmdutil.IsPromptCancel(err) {
		return false, err
	}
	if err != nil || !ok {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
		return false, nil //nolint:nilerr // intentional: prompter cancel is a clean exit
	}
	return true, nil
}
