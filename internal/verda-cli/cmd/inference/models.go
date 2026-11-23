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
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const argModel = "model"

func newCmdModels(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"model"},
		Short:   "List inference models",
		Long: cmdutil.LongDesc(`
			List the inference model catalog. CODE identifies a model in CLI
			commands and subscriptions; ENDPOINT MODEL is the value to send as
			"model" in inference requests.
		`),
		Example: cmdutil.Examples(`
			verda inference models
			verda inf models get <code>
			verda --agent inf models -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsList(cmd, f, ioStreams)
		},
	}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List inference models",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsList(cmd, f, ioStreams)
		},
	}
	get := &cobra.Command{
		Use:   "get [<code>]",
		Short: "Show an inference model and the project's rate limits for it",
		Example: cmdutil.Examples(`
			verda inference models get <code>
			verda --agent inf models get <code> -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelGet(cmd, f, ioStreams, args)
		},
	}
	cmd.AddCommand(list, get)
	return cmd
}

func listModels(ctx context.Context, f cmdutil.Factory, client *verda.Client) ([]verda.InferenceModel, error) {
	return callAPI(ctx, f, "Loading models...", client.Inference.ListModels)
}

func runModelsList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	models, err := listModels(cmd.Context(), f, client)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d model(s):", len(models)), models)
	if models == nil {
		models = []verda.InferenceModel{}
	}
	if wrote, err := writeStructured(f, ioStreams, models); wrote {
		return err
	}
	if len(models) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No inference models found.")
		return nil
	}
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CODE\tNAME\tSTATUS\tCATEGORY\tCONTEXT\tENDPOINT MODEL\tTIERS")
	for i := range models {
		m := &models[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", m.Code, m.Name, m.Status, cmdutil.TextColumn(m.Category),
			tokens(m.ContextLength), m.EndpointModel, strings.Join(m.ServiceTiers, ","))
	}
	return tw.Flush()
}

func runModelGet(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) error {
	if len(args) == 0 && !interactiveTTY(f) {
		return cmdutil.NewMissingFlagsError([]string{argModel})
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	code := ""
	if len(args) > 0 {
		code = args[0]
	} else {
		code, err = pickModel(cmd.Context(), f, client, "Select a model", nil)
		if err != nil || code == "" {
			return err
		}
	}
	model, err := callAPI(cmd.Context(), f, "Loading model...", func(ctx context.Context) (*verda.InferenceModelDetail, error) {
		return client.Inference.GetModel(ctx, code)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", model)
	if wrote, err := writeStructured(f, ioStreams, model); wrote {
		return err
	}
	printModel(ioStreams.Out, model)
	return nil
}

func printModel(w io.Writer, m *verda.InferenceModelDetail) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(tw, "  %s\t%s\n", k, v) }
	row("Code:", m.Code)
	row("Name:", m.Name)
	row("Endpoint model:", m.EndpointModel)
	row("Provider:", cmdutil.TextColumn(m.ProviderName))
	row("Status:", m.Status)
	row("Category:", cmdutil.TextColumn(m.Category))
	row("Context length:", tokens(m.ContextLength))
	row("Max output:", tokens(m.MaxOutputLength))
	row("Service tiers:", strings.Join(m.ServiceTiers, ", "))
	row("New subscriptions:", strconv.FormatBool(m.AcceptNewSubscriptions))
	row("Price per token:", "input "+cmdutil.TextColumn(m.Pricing.Input)+", output "+cmdutil.TextColumn(m.Pricing.Output))
	row("Quota tier:", strconv.Itoa(m.QuotaTier))
	_ = tw.Flush()
	if len(m.ServiceTierQuota) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\n  Rate limits:")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  TIER\tRPM\tTPM\tMAX ENQUEUED TOKENS")
	for _, q := range m.ServiceTierQuota {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", q.ServiceTier, limit(q.Quota.RPM), limit(q.Quota.TPM), limit(q.Quota.MaxEnqueuedTokens))
	}
	_ = tw.Flush()
}

func tokens(n *int64) string {
	if n == nil || *n <= 0 {
		return "-"
	}
	if *n >= 1000 && *n%1024 == 0 {
		return strconv.FormatInt(*n/1024, 10) + "K"
	}
	if *n >= 1000 {
		return strconv.FormatInt(*n/1000, 10) + "K"
	}
	return strconv.FormatInt(*n, 10)
}

func limit(n *int64) string {
	if n == nil {
		return "unlimited"
	}
	return strconv.FormatInt(*n, 10)
}

// pickModel shows a model picker; keep filters the list (nil keeps all). "" = canceled.
func pickModel(ctx context.Context, f cmdutil.Factory, client *verda.Client, prompt string, keep func(*verda.InferenceModel) bool) (string, error) {
	models, err := listModels(ctx, f, client)
	if err != nil {
		return "", err
	}
	codes := make([]string, 0, len(models))
	labels := make([]string, 0, len(models)+1)
	for i := range models {
		m := &models[i]
		if keep != nil && !keep(m) {
			continue
		}
		codes = append(codes, m.Code)
		labels = append(labels, fmt.Sprintf("%s  (%s)  %s", m.Name, m.Code, m.Status))
	}
	if len(codes) == 0 {
		return "", errors.New("no matching inference models")
	}
	labels = append(labels, "Cancel")
	idx, err := f.Prompter().Select(ctx, prompt, labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil
		}
		return "", err
	}
	if idx >= len(codes) {
		return "", nil
	}
	return codes[idx], nil
}
