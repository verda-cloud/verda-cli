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

package inference

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdKeys(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "keys",
		Aliases: []string{"key", "api-keys"},
		Short:   "List and manage inference API keys",
		Long: cmdutil.LongDesc(`
			Inference API keys (dcs_…) authenticate inference requests with
			"Authorization: Bearer <key>". Without a subcommand, lists the keys
			(masked).
		`),
		Example: cmdutil.Examples(`
			verda inference keys
			verda inf keys create --label my-app
			verda inf keys delete <id> --yes
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runKeysList(cmd, f, ioStreams)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List inference API keys",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runKeysList(cmd, f, ioStreams)
			},
		},
		newCmdKeyCreate(f, ioStreams),
		newCmdKeyDelete(f, ioStreams),
	)
	return cmd
}

func listKeys(ctx context.Context, f cmdutil.Factory, client *verda.Client) ([]verda.InferenceAPIKey, error) {
	return callAPI(ctx, f, "Loading API keys...", client.Inference.ListAPIKeys)
}

func runKeysList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	keys, err := listKeys(cmd.Context(), f, client)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d key(s):", len(keys)), keys)
	if keys == nil {
		keys = []verda.InferenceAPIKey{}
	}
	if wrote, err := writeStructured(f, ioStreams, keys); wrote {
		return err
	}
	if len(keys) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No inference API keys. Create one with: verda inference keys create --label <label>")
		return nil
	}
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tLABEL\tKEY\tCREATED")
	for i := range keys {
		k := &keys[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", k.ID, k.Label, k.MaskedKey, k.CreatedAt.Format(time.DateOnly))
	}
	return tw.Flush()
}

// createdKeyView is the one place the plaintext key is serialized; the SDK
// type redacts it under fmt, so it is never passed to DebugJSON.
type createdKeyView struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	MaskedKey string    `json:"masked_key"`
	CreatedAt time.Time `json:"created_at"`
	Key       string    `json:"key"`
}

type keyCreateOptions struct {
	Label string
}

func newCmdKeyCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &keyCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an inference API key",
		Long: cmdutil.LongDesc(`
			Create an inference API key. The key is printed once and cannot be
			retrieved again; store it right away. In JSON output the plaintext is
			in the "key" field.
		`),
		Example: cmdutil.Examples(`
			verda inference keys create --label my-app
			verda --agent inf keys create --label ci -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runKeyCreate(cmd, f, ioStreams, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Label, "label", "", "Key label shown in listings (1-128 characters)")
	return cmd
}

func runKeyCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *keyCreateOptions) error {
	if opts.Label == "" && !interactiveTTY(f) {
		return cmdutil.NewMissingFlagsError([]string{"label"})
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	label := opts.Label
	if label == "" {
		host, _ := os.Hostname()
		label, err = f.Prompter().TextInput(ctx, "Key label", tui.WithDefault("verda-cli ("+host+")"))
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				return nil
			}
			return err
		}
	}
	req := &verda.CreateInferenceAPIKeyRequest{Label: label}
	if err := req.Validate(); err != nil {
		return cmdutil.NewValidationError("label", err.Error())
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	key, err := callAPI(ctx, f, "Creating API key...", func(ctx context.Context) (*verda.CreatedInferenceAPIKey, error) {
		return client.Inference.CreateAPIKey(ctx, req)
	})
	if err != nil {
		return err
	}
	view := createdKeyView{ID: key.ID, Label: key.Label, MaskedKey: key.MaskedKey, CreatedAt: key.CreatedAt, Key: key.Key}
	if wrote, err := writeStructured(f, ioStreams, view); wrote {
		return err
	}
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Created inference API key %q (%s).\n\n", key.Label, key.ID)
	_, _ = fmt.Fprintln(ioStreams.Out, key.Key)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n%s\n", warn.Render("Store it now: this is the only time the key is shown."))
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "Send it as \"Authorization: Bearer <key>\" to "+defaultInferenceBaseURL+"/v1.")
	return nil
}

type keyDeleteOptions struct {
	Yes bool
}

func newCmdKeyDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &keyDeleteOptions{}
	cmd := &cobra.Command{
		Use:     "delete [<id>]",
		Aliases: []string{"rm"},
		Short:   "Delete an inference API key",
		Long: cmdutil.LongDesc(`
			Delete an inference API key; requests made with it are rejected
			afterwards. Without an id, pick the key interactively. Agent mode
			requires an id and --yes.
		`),
		Example: cmdutil.Examples(`
			verda inference keys delete
			verda inf keys delete <id> --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeyDelete(cmd, f, ioStreams, opts, args)
		},
	}
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runKeyDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *keyDeleteOptions, args []string) error {
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	if len(args) == 0 && !interactiveTTY(f) {
		return cmdutil.NewMissingFlagsError([]string{"id"})
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	var id, label string
	if len(args) > 0 {
		id, label = args[0], args[0]
	} else {
		id, label, err = pickKey(ctx, f, client)
		if err != nil || id == "" {
			return err
		}
	}
	if !opts.Yes {
		ok, err := confirmDanger(ctx, f, ioStreams, "Delete inference API key "+label,
			"Requests made with this key will be rejected. This cannot be undone.", "Delete key?")
		if err != nil || !ok {
			return err
		}
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deleting API key:", map[string]string{"id": id})
	if _, err := callAPI(ctx, f, "Deleting API key...", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, client.Inference.DeleteAPIKey(ctx, id)
	}); err != nil {
		return err
	}
	result := map[string]string{"id": id, "action": "delete", "status": "completed"}
	if wrote, err := writeStructured(f, ioStreams, result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted inference API key %s.\n", label)
	return nil
}

func pickKey(ctx context.Context, f cmdutil.Factory, client *verda.Client) (id, label string, _ error) {
	keys, err := listKeys(ctx, f, client)
	if err != nil {
		return "", "", err
	}
	if len(keys) == 0 {
		return "", "", errors.New("the project has no inference API keys")
	}
	labels := make([]string, 0, len(keys)+1)
	for i := range keys {
		labels = append(labels, fmt.Sprintf("%s  %s  %s", keys[i].Label, keys[i].MaskedKey, keys[i].ID))
	}
	labels = append(labels, "Cancel")
	idx, err := f.Prompter().Select(ctx, "Select a key to delete", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", "", nil
		}
		return "", "", err
	}
	if idx >= len(keys) {
		return "", "", nil
	}
	return keys[idx].ID, fmt.Sprintf("%q (%s)", keys[idx].Label, keys[idx].ID), nil
}
