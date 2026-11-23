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
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type usageOptions struct {
	From, To, Key string
}

func newCmdUsage(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &usageOptions{}
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show the project's token usage per day and model",
		Long: cmdutil.LongDesc(`
			Show requests and tokens per UTC day and model. Without --from/--to the
			API picks its default range. Dates are YYYY-MM-DD (UTC) or RFC 3339.
		`),
		Example: cmdutil.Examples(`
			verda inference usage
			verda inf usage --from 2026-11-01 --to 2026-11-23
			verda inf usage --key <key-id> -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsage(cmd, f, ioStreams, opts)
		},
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "Start of the range (YYYY-MM-DD or RFC 3339)")
	cmd.Flags().StringVar(&opts.To, "to", "", "End of the range (YYYY-MM-DD or RFC 3339)")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Only usage of this inference API key ID")
	return cmd
}

func parseUsageTime(flag, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, cmdutil.NewValidationError(flag, "expected YYYY-MM-DD or RFC 3339, got "+strconv.Quote(s))
	}
	return t, nil
}

func runUsage(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *usageOptions) error {
	start, err := parseUsageTime("from", opts.From)
	if err != nil {
		return err
	}
	end, err := parseUsageTime("to", opts.To)
	if err != nil {
		return err
	}
	// A bare --to date means "through that day".
	if opts.To != "" && len(opts.To) == len(time.DateOnly) {
		end = end.Add(24*time.Hour - time.Second)
	}
	q := &verda.GetInferenceUsageOptions{Start: start, End: end, APIKeyID: opts.Key}
	if err := q.Validate(); err != nil {
		return cmdutil.NewValidationError("to", err.Error())
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	usage, err := callAPI(cmd.Context(), f, "Loading usage...", func(ctx context.Context) (*verda.InferenceUsage, error) {
		return client.Inference.GetUsage(ctx, q)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", usage)
	if usage.Rows == nil {
		usage.Rows = []verda.InferenceUsageRow{}
	}
	if wrote, err := writeStructured(f, ioStreams, usage); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "  Range: %s to %s\n\n", usage.Range.Start, usage.Range.End)
	if len(usage.Rows) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No usage in this range.")
		return nil
	}
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "DAY\tMODEL\tREQUESTS\tINPUT\tCACHE READ\tOUTPUT\t")
	for i := range usage.Rows {
		r := &usage.Rows[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t\n", r.Day, r.Model, r.Requests, r.InputTokens, r.CacheReadTokens, r.OutputTokens)
	}
	t := usage.Totals
	_, _ = fmt.Fprintf(tw, "TOTAL\t\t%d\t%d\t%d\t%d\t\n", t.Requests, t.InputTokens, t.CacheReadTokens, t.OutputTokens)
	return tw.Flush()
}

type limitsOptions struct {
	AllTiers bool
}

func newCmdLimits(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &limitsOptions{}
	cmd := &cobra.Command{
		Use:     "limits",
		Aliases: []string{"rate-limits"},
		Short:   "Show the project's quota tier and rate limits",
		Example: cmdutil.Examples(`
			verda inference limits
			verda inf limits --all-tiers -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLimits(cmd, f, ioStreams, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.AllTiers, "all-tiers", false, "Show the rate limits of every quota tier")
	return cmd
}

func runLimits(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *limitsOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	var tiers []verda.InferenceTier
	if opts.AllTiers {
		tiers, err = callAPI(cmd.Context(), f, "Loading tiers...", client.Inference.ListTiers)
	} else {
		var t *verda.InferenceTier
		t, err = callAPI(cmd.Context(), f, "Loading rate limits...", client.Inference.GetRateLimits)
		if t != nil {
			tiers = []verda.InferenceTier{*t}
		}
	}
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", tiers)
	var out any = tiers
	if !opts.AllTiers && len(tiers) == 1 {
		out = tiers[0]
	} else if tiers == nil {
		out = []verda.InferenceTier{}
	}
	if wrote, err := writeStructured(f, ioStreams, out); wrote {
		return err
	}
	for i := range tiers {
		if i > 0 {
			_, _ = fmt.Fprintln(ioStreams.Out)
		}
		label := "Project quota tier"
		if opts.AllTiers {
			label = "Quota tier"
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s: %d\n\n", label, tiers[i].Tier)
		if len(tiers[i].RateLimits) == 0 {
			_, _ = fmt.Fprintln(ioStreams.Out, "  No rate limits.")
			continue
		}
		tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  MODEL\tTIER\tRPM\tTPM\tMAX ENQUEUED TOKENS")
		for _, r := range tiers[i].RateLimits {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.ModelCode, r.ServiceTier, limit(r.RPM), limit(r.TPM), limit(r.MaxEnqueuedTokens))
		}
		_ = tw.Flush()
	}
	return nil
}
