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

package serverless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	scaleStatusAccepted  = "accepted"
	scaleStatusCompleted = "completed"

	defaultScaleWaitTimeout = 60 * time.Second

	// sameAsReadNote marks a diff row whose value equals a read that can lag a recent write.
	sameAsReadNote = "(same as last read; still sent)"
)

type containerScaleOptions struct {
	MinReplicas    int
	MaxReplicas    int
	Concurrency    int
	RequestTTL     time.Duration
	ScaleUpDelay   time.Duration
	ScaleDownDelay time.Duration
	QueuePreset    string
	QueueLoad      float64
	CPUUtil        string
	GPUUtil        string
	Yes            bool
	Wait           cmdutil.WaitOptions
}

type containerScaleResult struct {
	Name      string                         `json:"name"`
	Status    string                         `json:"status"`
	Previous  *verda.ContainerScalingOptions `json:"previous"`
	Expected  *verda.ContainerScalingOptions `json:"expected"`
	Current   *verda.ContainerScalingOptions `json:"current,omitempty"`
	WaitError string                         `json:"wait_error,omitempty"`
}

func newCmdContainerScale(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &containerScaleOptions{}
	cmd := &cobra.Command{
		Use:   "scale [<name>]",
		Short: "Change the scaling options of a container deployment",
		Long: cmdutil.LongDesc(`
			Change the scaling options of a container deployment. Only the options
			you set change; everything else keeps its current value.

			Raising --min-replicas keeps that many replicas running and billed even
			with no traffic. --max-replicas caps the billed replicas under load.

			Without option flags on a terminal, an interactive flow shows the current
			options and asks which to change. Every change is confirmed; --yes skips
			the confirmation and is required in agent mode and without a terminal.

			The API accepts the change at once, but reads show it only after about
			5-20 seconds. --wait polls until the new options are visible. Replicas
			then start or stop on their own; watch them with
			'verda container replicas'.
		`),
		Example: cmdutil.Examples(`
			# Interactive: pick a deployment and the options to change
			verda container scale

			# Keep one replica warm and allow up to five
			verda container scale my-api --min-replicas 1 --max-replicas 5

			# Scale to zero when idle, and wait until reads show the change
			verda container scale my-api --min-replicas 0 --yes --wait

			# Turn on the CPU trigger at 70%, turn off the GPU trigger
			verda container scale my-api --cpu-util 70 --gpu-util off
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContainerScale(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&opts.MinReplicas, "min-replicas", 0, fmt.Sprintf("Minimum replicas (0..%d; 0 = scale to zero when idle)", maxReplicas))
	flags.IntVar(&opts.MaxReplicas, "max-replicas", 0, fmt.Sprintf("Maximum replicas (1..%d)", maxReplicas))
	flags.IntVar(&opts.Concurrency, "concurrency", 0, fmt.Sprintf("Concurrent requests per replica (1..%d)", maxConcurrency))
	flags.DurationVar(&opts.RequestTTL, "request-ttl", 0, "How long a queued request may wait before it is dropped (0s..24h, whole seconds)")
	flags.DurationVar(&opts.ScaleUpDelay, "scale-up-delay", 0, "Delay before scaling up (0s..1h, whole seconds)")
	flags.DurationVar(&opts.ScaleDownDelay, "scale-down-delay", 0, "Delay before scaling down (0s..1h, whole seconds)")
	flags.StringVar(&opts.QueuePreset, "queue-preset", "", "Queue-load preset: instant | balanced | cost-saver | custom")
	flags.Float64Var(&opts.QueueLoad, "queue-load", 0, fmt.Sprintf("Queue-load threshold (%d..%d, fractions allowed); implies --queue-preset custom", minQueueLoad, maxQueueLoad))
	flags.StringVar(&opts.CPUUtil, "cpu-util", "", fmt.Sprintf("CPU utilization trigger: threshold %% (%d..%d) to turn it on, or off", minUtilThreshold, maxUtilThreshold))
	flags.StringVar(&opts.GPUUtil, "gpu-util", "", fmt.Sprintf("GPU utilization trigger: threshold %% (%d..%d) to turn it on, or off; not on CPU compute", minUtilThreshold, maxUtilThreshold))
	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode and without a terminal)")
	flags.BoolVar(&opts.Wait.Wait, "wait", false, "Wait until reads show the new options (not until replicas are ready)")
	flags.DurationVar(&opts.Wait.Timeout, "wait-timeout", defaultScaleWaitTimeout, "Maximum time to wait with --wait")

	return cmd
}

func runContainerScale(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerScaleOptions, args []string) error {
	change, err := opts.change(cmd.Flags())
	if err != nil {
		return err
	}
	if opts.Wait.Wait && opts.Wait.Timeout <= 0 {
		return cmdutil.NewValidationError("--wait-timeout", "must be greater than 0")
	}

	interactive := interactiveTTY(f)
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if !interactive {
		if err := checkScaleNonInteractive(name, change, opts.Yes); err != nil {
			return err
		}
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	if name == "" {
		if name, err = selectContainerDeployment(cmd.Context(), f, ioStreams); err != nil || name == "" {
			return err
		}
	}

	cur, err := loadContainerScaling(cmd.Context(), f, client, name)
	if err != nil {
		return err
	}

	if change.Empty() {
		renderScalingCard(ioStreams.ErrOut, name, cur)
		if change, err = runScaleWizard(cmd.Context(), f, ioStreams, cur); err != nil || change == nil {
			return err
		}
	}

	req, err := change.Request()
	if err != nil {
		return flagError(err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Scaling request:", req)

	call := &scaleCall{f: f, ioStreams: ioStreams, client: client, name: name, cur: cur, req: req}
	if ok, err := call.confirm(cmd.Context(), opts.Yes); err != nil || !ok {
		return err
	}
	return call.apply(cmd.Context(), opts.Wait)
}

// scaleCall is one scale request against one deployment, after flags or the wizard.
type scaleCall struct {
	f         cmdutil.Factory
	ioStreams cmdutil.IOStreams
	client    *verda.Client
	name      string
	cur       *verda.ContainerScalingOptions
	req       *verda.UpdateScalingOptionsRequest
}

// confirm shows the diff (table output) and asks unless --yes; cancel → false, nil.
func (c *scaleCall) confirm(ctx context.Context, yes bool) (bool, error) {
	if c.f.OutputFormat() == outputTable {
		renderScalingDiff(c.ioStreams.ErrOut, c.name, c.cur, c.req)
	}
	if yes {
		return true, nil
	}
	confirmed, err := c.f.Prompter().Confirm(ctx, fmt.Sprintf("Apply scaling change to %s?", c.name), tui.WithConfirmDefault(false))
	if err != nil {
		if isPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(c.ioStreams.ErrOut, "Canceled.")
	}
	return confirmed, nil
}

// apply sends the PATCH, optionally waits, then writes one result.
func (c *scaleCall) apply(ctx context.Context, w cmdutil.WaitOptions) error {
	// Bounded here, after the prompt, so think-time can't drain the API budget.
	apiCtx, cancel := context.WithTimeout(ctx, c.f.Options().Timeout)
	defer cancel()
	expected, err := cmdutil.WithSpinner(apiCtx, c.f.Status(), "Updating scaling for "+c.name+"...", func(ctx context.Context) (*verda.ContainerScalingOptions, error) {
		return c.client.ContainerDeployments.UpdateDeploymentScaling(ctx, c.name, c.req)
	})
	if err != nil {
		return fmt.Errorf("updating scaling: %w", err)
	}
	cmdutil.DebugJSON(c.ioStreams.ErrOut, c.f.Debug(), "Scaling response:", expected)

	result := containerScaleResult{Name: c.name, Status: scaleStatusAccepted, Previous: c.cur, Expected: expected}
	var waitErr error
	if w.Wait {
		waitErr = c.wait(ctx, w.Timeout, &result)
	}

	if wrote, werr := cmdutil.WriteStructured(c.ioStreams.Out, c.f.OutputFormat(), result); wrote {
		if werr != nil {
			return werr
		}
		return waitErr
	}
	renderScaleResult(c.ioStreams.Out, &result)
	return waitErr
}

// scaleOptionFlags: a non-interactive scale needs at least one.
var scaleOptionFlags = []string{
	"--min-replicas", "--max-replicas", "--concurrency", "--request-ttl", "--scale-up-delay",
	"--scale-down-delay", "--queue-preset", "--queue-load", "--cpu-util", "--gpu-util",
}

// checkScaleNonInteractive: without a TTY (agent, pipe, -o json) nothing may prompt.
func checkScaleNonInteractive(name string, change *scaling.Change, yes bool) error {
	var missing []string
	if name == "" {
		missing = append(missing, "<name>")
	}
	if change.Empty() {
		return cmdutil.NewMissingOneOfFlagsError(missing, scaleOptionFlags)
	}
	if len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	if !yes {
		e := cmdutil.NewConfirmationRequiredError("scale")
		e.Message = "scale changes billed capacity and requires --yes without an interactive terminal"
		return e
	}
	return nil
}

func loadContainerScaling(ctx context.Context, f cmdutil.Factory, client *verda.Client, name string) (*verda.ContainerScalingOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	cur, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading scaling options...", func(ctx context.Context) (*verda.ContainerScalingOptions, error) {
		return client.ContainerDeployments.GetDeploymentScaling(ctx, name)
	})
	if err != nil {
		return nil, fmt.Errorf("fetching scaling options: %w", err)
	}
	return cur, nil
}

// wait fills result on success; its error wraps API errors only, never ctx (main treats that as a clean cancel).
func (c *scaleCall) wait(ctx context.Context, timeout time.Duration, result *containerScaleResult) error {
	var w io.Writer
	if c.f.OutputFormat() == outputTable {
		_, _ = fmt.Fprintf(c.ioStreams.ErrOut, "Change accepted for %q; waiting for reads to show it (Ctrl+C stops waiting, the change stays).\n", c.name)
		if cmdutil.IsStderrTerminal() {
			w = c.ioStreams.ErrOut
		}
	}
	got, err := scaling.Wait(ctx, w, c.client, c.name, c.req, timeout)
	if err != nil {
		result.WaitError = err.Error()
		return fmt.Errorf("scaling change for %q was accepted, but --wait could not confirm it (%w); check with 'verda container describe %s'", c.name, err, c.name)
	}
	result.Status = scaleStatusCompleted
	result.Current = got
	return nil
}

// change converts the set flags to a scaling.Change; unset flags stay nil, so
// an explicit --min-replicas 0 is sent.
func (o *containerScaleOptions) change(fs *pflag.FlagSet) (*scaling.Change, error) {
	c := &scaling.Change{}
	intFlags := []struct {
		name string
		v    int
		dst  **int
	}{
		{"min-replicas", o.MinReplicas, &c.MinReplicas},
		{"max-replicas", o.MaxReplicas, &c.MaxReplicas},
		{"concurrency", o.Concurrency, &c.Concurrency},
	}
	for _, fl := range intFlags {
		if fs.Changed(fl.name) {
			*fl.dst = new(fl.v)
		}
	}

	durFlags := []struct {
		name string
		v    time.Duration
		maxD time.Duration
		dst  **int
	}{
		{"request-ttl", o.RequestTTL, maxContainerRequestTTL, &c.QueueTTLSeconds},
		{"scale-up-delay", o.ScaleUpDelay, maxScalingDelay, &c.ScaleUpDelaySeconds},
		{"scale-down-delay", o.ScaleDownDelay, maxScalingDelay, &c.ScaleDownDelaySeconds},
	}
	for _, fl := range durFlags {
		if !fs.Changed(fl.name) {
			continue
		}
		secs, err := wholeSeconds(fl.v, fl.maxD)
		if err != nil {
			return nil, cmdutil.NewValidationError("--"+fl.name, err.Error())
		}
		*fl.dst = &secs
	}

	switch {
	case fs.Changed("queue-load"):
		c.QueueLoad = new(o.QueueLoad)
	case fs.Changed("queue-preset"):
		n, err := resolveQueueLoad(o.QueuePreset, 0)
		if err != nil {
			return nil, cmdutil.NewValidationError("--queue-preset", err.Error())
		}
		q := float64(n)
		c.QueueLoad = &q
	}

	var err error
	if fs.Changed("cpu-util") {
		if c.CPUUtil, err = parseUtilFlag("--cpu-util", o.CPUUtil); err != nil {
			return nil, err
		}
	}
	if fs.Changed("gpu-util") {
		if c.GPUUtil, err = parseUtilFlag("--gpu-util", o.GPUUtil); err != nil {
			return nil, err
		}
	}
	if err := c.Validate(); err != nil {
		return nil, flagError(err)
	}
	return c, nil
}

// flagError renders a scaling.FieldError with its flag name.
func flagError(err error) error {
	var fe *scaling.FieldError
	if errors.As(err, &fe) {
		return cmdutil.NewValidationError("--"+fe.Field, fe.Message)
	}
	return err
}

// wholeSeconds: the API takes integer seconds; a fraction is rejected, not truncated.
func wholeSeconds(d, maxD time.Duration) (int, error) {
	if d%time.Second != 0 {
		return 0, errors.New("must be whole seconds")
	}
	if d < 0 || d > maxD {
		return 0, fmt.Errorf("must be between 0s and %s", shortDuration(maxD))
	}
	return int(d / time.Second), nil
}

// parseUtilFlag: N turns the trigger on at N, "off" or 0 turns it off.
func parseUtilFlag(flag, s string) (*int, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == utilOff {
		return new(0), nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, cmdutil.NewValidationError(flag, fmt.Sprintf("must be %d..%d or off", minUtilThreshold, maxUtilThreshold))
	}
	return &n, nil
}

func runScaleWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, cur *verda.ContainerScalingOptions, opts ...wizard.EngineOption) (*scaling.Change, error) {
	change := &scaling.Change{}
	engine := wizard.NewEngine(f.Prompter(), f.Status(), append([]wizard.EngineOption{wizard.WithOutput(ioStreams.ErrOut)}, opts...)...)
	if err := engine.Run(ctx, buildScaleFlow(cur, change)); err != nil {
		if isPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	if change.Empty() {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No changes.")
		return nil, nil
	}
	return change, nil
}

// --- Rendering ---

type scalingDiffRow struct{ label, from, to string }

// scalingDiff lists the fields req sets, as current -> requested.
func scalingDiff(cur *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) []scalingDiffRow {
	var rows []scalingDiffRow
	addInt := func(label string, want *int, have int) {
		if want != nil {
			rows = append(rows, scalingDiffRow{label, strconv.Itoa(have), strconv.Itoa(*want)})
		}
	}
	addSecs := func(label string, want *verda.ScalingPolicy, have *verda.ScalingPolicy) {
		if want != nil {
			rows = append(rows, scalingDiffRow{label, fmtSeconds(policySeconds(have)), fmtSeconds(want.DelaySeconds)})
		}
	}
	addInt("Min replicas", req.MinReplicaCount, cur.MinReplicaCount)
	addInt("Max replicas", req.MaxReplicaCount, cur.MaxReplicaCount)
	addInt("Concurrency", req.ConcurrentRequestsPerReplica, cur.ConcurrentRequestsPerReplica)
	if req.QueueMessageTTLSeconds != nil {
		rows = append(rows, scalingDiffRow{"Request TTL", fmtSeconds(cur.QueueMessageTTLSeconds), fmtSeconds(*req.QueueMessageTTLSeconds)})
	}
	addSecs("Scale-up delay", req.ScaleUpPolicy, cur.ScaleUpPolicy)
	addSecs("Scale-down delay", req.ScaleDownPolicy, cur.ScaleDownPolicy)

	t := req.ScalingTriggers
	if t == nil {
		return rows
	}
	ct := scaling.Triggers(cur)
	if t.QueueLoad != nil {
		rows = append(rows, scalingDiffRow{"Queue load", queueLoadLabel(ct.QueueLoad), queueLoadLabel(t.QueueLoad)})
	}
	if t.CPUUtilization != nil {
		rows = append(rows, scalingDiffRow{"CPU trigger", utilLabel(ct.CPUUtilization), utilLabel(t.CPUUtilization)})
	}
	if t.GPUUtilization != nil {
		rows = append(rows, scalingDiffRow{"GPU trigger", utilLabel(ct.GPUUtilization), utilLabel(t.GPUUtilization)})
	}
	return rows
}

func renderScalingDiff(w io.Writer, name string, cur *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s\n", header.Render("Scaling change for "+name))
	for _, r := range scalingDiff(cur, req) {
		var same string
		if r.from == r.to {
			same = " " + dim.Render(sameAsReadNote)
		}
		_, _ = fmt.Fprintf(w, "  %s %s %s %s%s\n", label.Render(padLabel(r.label)), dim.Render(r.from), dim.Render("→"), r.to, same)
	}
	if m := req.MinReplicaCount; m != nil && *m > 0 && *m > cur.MinReplicaCount {
		_, _ = fmt.Fprintf(w, "\n  %s\n", warn.Render(fmt.Sprintf("%d replica(s) will run and be billed even with no traffic.", *m)))
	}
	if msg := scaling.BoundWarning(cur, req); msg != "" {
		_, _ = fmt.Fprintf(w, "\n  %s\n", warn.Render(msg))
	}
	_, _ = fmt.Fprintln(w)
}

func renderScaleResult(w io.Writer, r *containerScaleResult) {
	if r.Status == scaleStatusCompleted {
		_, _ = fmt.Fprintf(w, "Scaling change completed for %q (reads show it).\n", r.Name)
	} else {
		_, _ = fmt.Fprintf(w, "Scaling change accepted for %q. Reads can show the old values for about 20 s.\n", r.Name)
	}
	_, _ = fmt.Fprintf(w, "Replicas start or stop on their own; watch them with: verda container replicas %s\n", r.Name)
}

// renderScalingCard prints the current scaling options; shared with describe.
func renderScalingCard(w io.Writer, name string, sc *verda.ContainerScalingOptions) {
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	_, _ = fmt.Fprintf(w, "\n  %s\n", header.Render("Current scaling for "+name))
	writeScalingLines(w, "  ", sc)
	_, _ = fmt.Fprintln(w)
}

func writeScalingLines(w io.Writer, indent string, sc *verda.ContainerScalingOptions) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	t := scaling.Triggers(sc)
	rows := []scalingDiffRow{
		{"Replicas", fmt.Sprintf("%d..%d", sc.MinReplicaCount, sc.MaxReplicaCount), ""},
		{"Concurrency", strconv.Itoa(sc.ConcurrentRequestsPerReplica), ""},
		{"Queue load", queueLoadLabel(t.QueueLoad), ""},
		{"CPU trigger", utilLabel(t.CPUUtilization), ""},
		{"GPU trigger", utilLabel(t.GPUUtilization), ""},
		{"Scale-up delay", fmtSeconds(policySeconds(sc.ScaleUpPolicy)), ""},
		{"Scale-down delay", fmtSeconds(policySeconds(sc.ScaleDownPolicy)), ""},
		{"Request TTL", fmtSeconds(sc.QueueMessageTTLSeconds), ""},
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%s%s %s\n", indent, label.Render(padLabel(r.label)), r.from)
	}
}

// padLabel pads before styling: width verbs count ANSI escapes, so padding a rendered label misaligns.
func padLabel(s string) string { return fmt.Sprintf("%-16s", s) }

func policySeconds(p *verda.ScalingPolicy) int {
	if p == nil {
		return 0
	}
	return p.DelaySeconds
}

func fmtSeconds(s int) string {
	return shortDuration(time.Duration(s) * time.Second)
}

// queueLoadLabel reverses the preset mapping for display; exact threshold wins.
func queueLoadLabel(q *verda.QueueLoadTrigger) string {
	if q == nil {
		return "-"
	}
	name := presetCustom
	for _, p := range queuePresets {
		if float64(p.threshold) == q.Threshold {
			name = p.name
			break
		}
	}
	return name + " (" + strconv.FormatFloat(q.Threshold, 'f', -1, 64) + ")"
}

func utilLabel(u *verda.UtilizationTrigger) string {
	if u == nil || !u.Enabled {
		return utilOff
	}
	return fmt.Sprintf("on at %d%%", u.Threshold)
}
