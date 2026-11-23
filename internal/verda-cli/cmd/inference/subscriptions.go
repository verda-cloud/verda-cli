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
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdSubscriptions(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "subscriptions",
		Aliases: []string{"subscription", "subs"},
		Short:   "List and manage the project's model subscriptions",
		Long: cmdutil.LongDesc(`
			A project must be subscribed to a model in a service tier before it
			can call that model. Without a subcommand, lists the subscriptions.
		`),
		Example: cmdutil.Examples(`
			verda inference subscriptions
			verda inf subscriptions enable <model>
			verda inf subscriptions disable <model> --tier standard --yes
			verda inf subscriptions status
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSubscriptionsList(cmd, f, ioStreams)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List the project's model subscriptions",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runSubscriptionsList(cmd, f, ioStreams)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show whether inference is active or suspended for the project",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runSubscriptionStatus(cmd, f, ioStreams)
			},
		},
		newCmdSubscriptionEnable(f, ioStreams),
		newCmdSubscriptionDisable(f, ioStreams),
	)
	return cmd
}

func validateTier(tier string) error {
	if !slices.Contains(verda.InferenceServiceTiers, tier) {
		return cmdutil.NewValidationError("tier", "must be one of "+strings.Join(verda.InferenceServiceTiers, ", "))
	}
	return nil
}

func listSubscriptions(ctx context.Context, f cmdutil.Factory, client *verda.Client) ([]verda.InferenceSubscription, error) {
	return callAPI(ctx, f, "Loading subscriptions...", client.Inference.ListSubscriptions)
}

func runSubscriptionsList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	subs, err := listSubscriptions(cmd.Context(), f, client)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d subscription(s):", len(subs)), subs)
	if subs == nil {
		subs = []verda.InferenceSubscription{}
	}
	if wrote, err := writeStructured(f, ioStreams, subs); wrote {
		return err
	}
	// Status is context only; the list still prints if it fails.
	if st, err := callAPI(cmd.Context(), f, "Loading status...", client.Inference.GetStatus); err == nil {
		_, _ = fmt.Fprintf(ioStreams.Out, "  Inference: %s\n\n", statusLine(st))
	}
	if len(subs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No subscriptions. Subscribe with: verda inference subscriptions enable <model>")
		return nil
	}
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "MODEL\tTIER\tQUOTA TIER\tRPM\tTPM\tSINCE")
	for i := range subs {
		s := &subs[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n", s.ModelCode, s.ServiceTier, s.QuotaTier,
			limit(s.RPM), limit(s.TPM), s.CreatedAt.Format(time.DateOnly))
	}
	return tw.Flush()
}

func statusLine(st *verda.InferenceTenantStatus) string {
	if st.Status == verda.InferenceTenantStatusActive {
		return st.Status
	}
	line := st.Status
	if st.SuspendReason != nil {
		line += " (" + *st.SuspendReason + ")"
	}
	if st.PublicComment != nil && *st.PublicComment != "" {
		line += ": " + *st.PublicComment
	}
	return line
}

func runSubscriptionStatus(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	st, err := callAPI(cmd.Context(), f, "Loading status...", client.Inference.GetStatus)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", st)
	if wrote, err := writeStructured(f, ioStreams, st); wrote {
		return err
	}
	printStatus(ioStreams.Out, st)
	return nil
}

func printStatus(w io.Writer, st *verda.InferenceTenantStatus) {
	_, _ = fmt.Fprintf(w, "  Status:           %s\n", statusLine(st))
	_, _ = fmt.Fprintf(w, "  Minimum balance:  %s\n", strconv.FormatBool(st.HasMinimumBalance))
}

type subscriptionOptions struct {
	Tier string
	Yes  bool
}

func newCmdSubscriptionEnable(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &subscriptionOptions{}
	cmd := &cobra.Command{
		Use:     "enable [<model>]",
		Aliases: []string{"subscribe"},
		Short:   "Subscribe the project to a model",
		Long: cmdutil.LongDesc(`
			Subscribe the project to a model in a service tier (default standard).
			Subscribing again to the same model and tier keeps one subscription.
		`),
		Example: cmdutil.Examples(`
			verda inference subscriptions enable <model>
			verda inf subscriptions enable <model> --tier flex
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubscriptionEnable(cmd, f, ioStreams, opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.Tier, "tier", tierStandard, "Service tier: "+strings.Join(verda.InferenceServiceTiers, ", "))
	return cmd
}

func runSubscriptionEnable(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *subscriptionOptions, args []string) error {
	if err := validateTier(opts.Tier); err != nil {
		return err
	}
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
		code, err = pickModel(cmd.Context(), f, client, "Select a model to subscribe to", func(m *verda.InferenceModel) bool {
			return m.AcceptNewSubscriptions && m.Status != verda.InferenceModelStatusComingSoon && slices.Contains(m.ServiceTiers, opts.Tier)
		})
		if err != nil || code == "" {
			return err
		}
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request:", map[string]string{"model": code, "tier": opts.Tier})
	sub, err := callAPI(cmd.Context(), f, "Subscribing...", func(ctx context.Context) (*verda.InferenceSubscription, error) {
		return client.Inference.Subscribe(ctx, code, opts.Tier)
	})
	if err != nil {
		return err
	}
	if wrote, err := writeStructured(f, ioStreams, sub); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Subscribed to %s (%s tier).\n", sub.ModelCode, sub.ServiceTier)
	return nil
}

func newCmdSubscriptionDisable(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &subscriptionOptions{}
	cmd := &cobra.Command{
		Use:     "disable [<model>]",
		Aliases: []string{"unsubscribe"},
		Short:   "Cancel the project's subscription to a model",
		Long: cmdutil.LongDesc(`
			Cancel the project's subscription to a model in a service tier
			(default standard). Requests to that model in that tier are rejected
			afterwards. Agent mode requires --yes.
		`),
		Example: cmdutil.Examples(`
			verda inference subscriptions disable
			verda inf subscriptions disable <model> --yes
			verda --agent inf subscriptions disable <model> --tier flex --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubscriptionDisable(cmd, f, ioStreams, opts, args)
		},
	}
	cmd.Flags().StringVar(&opts.Tier, "tier", tierStandard, "Service tier: "+strings.Join(verda.InferenceServiceTiers, ", "))
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runSubscriptionDisable(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *subscriptionOptions, args []string) error {
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("disable")
	}
	if err := validateTier(opts.Tier); err != nil {
		return err
	}
	if len(args) == 0 && !interactiveTTY(f) {
		return cmdutil.NewMissingFlagsError([]string{argModel})
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	code, tier := "", opts.Tier
	if len(args) > 0 {
		code = args[0]
	} else {
		code, tier, err = pickSubscription(ctx, f, client)
		if err != nil || code == "" {
			return err
		}
	}
	if !opts.Yes {
		ok, err := confirmDanger(ctx, f, ioStreams,
			fmt.Sprintf("Unsubscribe from %s (%s tier)", code, tier),
			"Requests to this model in this tier will be rejected.",
			"Unsubscribe?")
		if err != nil || !ok {
			return err
		}
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request:", map[string]string{"model": code, "tier": tier})
	if _, err := callAPI(ctx, f, "Unsubscribing...", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, client.Inference.Unsubscribe(ctx, code, tier)
	}); err != nil {
		return err
	}
	result := map[string]string{"model": code, "tier": tier, "action": "unsubscribe", "status": "completed"}
	if wrote, err := writeStructured(f, ioStreams, result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Unsubscribed from %s (%s tier).\n", code, tier)
	return nil
}

func pickSubscription(ctx context.Context, f cmdutil.Factory, client *verda.Client) (code, tier string, _ error) {
	subs, err := listSubscriptions(ctx, f, client)
	if err != nil {
		return "", "", err
	}
	if len(subs) == 0 {
		return "", "", errors.New("the project has no subscriptions")
	}
	labels := make([]string, 0, len(subs)+1)
	for i := range subs {
		labels = append(labels, fmt.Sprintf("%s  (%s)", subs[i].ModelCode, subs[i].ServiceTier))
	}
	labels = append(labels, "Cancel")
	idx, err := f.Prompter().Select(ctx, "Select a subscription to cancel", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", "", nil
		}
		return "", "", err
	}
	if idx >= len(subs) {
		return "", "", nil
	}
	return subs[idx].ModelCode, subs[idx].ServiceTier, nil
}

// confirmDanger: red warning + Confirm. (false, nil) on decline or Esc/Ctrl+C.
func confirmDanger(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, heading, detail, prompt string) (bool, error) {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", warn.Render(heading))
	if detail != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", detail)
	}
	ok, err := f.Prompter().Confirm(ctx, prompt)
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return false, nil
		}
		return false, err
	}
	if !ok {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
	}
	return ok, nil
}
