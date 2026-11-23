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
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	keyActionReuse   = "reuse"
	keyActionCreate  = "create"
	keyActionRotate  = "rotate"
	keyActionReplace = "replace"

	subActionSubscribe = "subscribe"
	subActionExisting  = "existing"

	placeholderKey = "<new key>"
	maxKeyLabel    = 128
	argAgent       = "agent"
)

type setupOptions struct {
	Models       []string
	Yes          bool
	DryRun       bool
	Rotate       bool
	Experimental bool
	InferenceURL string
}

func newCmdSetup(f cmdutil.Factory, ioStreams cmdutil.IOStreams, envFn func() (*hostEnv, error)) *cobra.Command {
	opts := &setupOptions{}
	cmd := &cobra.Command{
		Use:   "setup [agent...]",
		Short: "Connect local coding agents (OpenCode, Kimi Code) to Verda models",
		Long: cmdutil.LongDesc(`
			Connect coding agents on this machine to Verda Inference in one step.
			For every chosen agent, setup subscribes the project to the chosen
			models (standard tier) if needed, creates one API key labeled
			"verda-cli <agent> (<hostname>)", and adds a "verda" provider to the
			agent's config.

			Setup only adds: it never changes the agent's default model or other
			providers. A config file that existed before is backed up once to
			<file>.verda-bak. Re-running keeps the existing key and rewrites only
			the verda entries; --rotate replaces the key. Undo with
			"verda inference remove <agent>".

			Supported agents: opencode (~/.config/opencode/opencode.json[c]) and
			Kimi Code (kimi, $KIMI_CODE_HOME or ~/.kimi-code/config.toml; run
			kimi once first). Without arguments on a terminal, setup offers a
			checklist with installed agents preselected.

			The endpoint defaults to https://inference.verda.com; override it with
			--inference-url, VERDA_INFERENCE_BASE_URL, "inference.base-url" in
			~/.verda/config.yaml, or verda_inference_base_url in the profile.
		`),
		Example: cmdutil.Examples(`
			# Interactive: pick agents and models
			verda inference setup

			# Non-interactive
			verda inf setup opencode kimi --model <model> --yes

			# Preview without changing anything
			verda inf setup opencode --model <model> --dry-run

			# Replace the agent's key
			verda inf setup opencode --rotate --yes
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := envFn()
			if err != nil {
				return err
			}
			r := &setupRun{cmd: cmd, f: f, io: ioStreams, opts: opts, env: env, interactive: interactiveTTY(f)}
			return r.run(args)
		},
	}
	flags := cmd.Flags()
	flags.StringArrayVar(&opts.Models, "model", nil, "Model code to add (repeatable); defaults to the models of a previous setup")
	flags.BoolVar(&opts.Yes, "yes", false, "Apply without confirmation (required when not interactive)")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Show what would change without writing files or calling mutating APIs")
	flags.BoolVar(&opts.Rotate, "rotate", false, "Create a new key, rewrite the configs, then delete the old key")
	flags.StringVar(&opts.InferenceURL, "inference-url", "", "Inference endpoint base URL (default "+defaultInferenceBaseURL+")")
	flags.BoolVar(&opts.Experimental, "experimental-agents", false, "Also offer codex and claude-code (need the Global Router)")
	_ = flags.MarkHidden("experimental-agents")
	return cmd
}

type setupRun struct {
	cmd         *cobra.Command
	f           cmdutil.Factory
	io          cmdutil.IOStreams
	opts        *setupOptions
	env         *hostEnv
	interactive bool

	client   *verda.Client
	profile  string
	baseURL  string
	state    *agentsState
	agents   []*agentDef
	models   []verda.InferenceModel
	subs     []subscriptionChange
	keyIDs   map[string]bool
	plans    []*agentPlan
	warnings []string
}

type agentPlan struct {
	def       *agentDef
	prev      *agentRecord
	actx      *agentContext
	keyAction string
	reason    string
	reuseKey  string
	oldKeyID  string
	label     string
	changes   []fileChange
}

type setupResult struct {
	DryRun        bool                 `json:"dry_run"`
	Profile       string               `json:"profile"`
	BaseURL       string               `json:"base_url"`
	Models        []string             `json:"models"`
	Subscriptions []subscriptionChange `json:"subscriptions"`
	Agents        []agentResult        `json:"agents"`
	Shell         *shellResult         `json:"shell,omitempty"`
	Warnings      []string             `json:"warnings,omitempty"`
}

type subscriptionChange struct {
	Model  string `json:"model"`
	Tier   string `json:"tier"`
	Action string `json:"action"`
}

type agentResult struct {
	Agent     string       `json:"agent"`
	KeyAction string       `json:"key_action"`
	KeyReason string       `json:"key_reason,omitempty"`
	KeyID     string       `json:"key_id,omitempty"`
	KeyLabel  string       `json:"key_label"`
	Models    []string     `json:"models"`
	Files     []fileResult `json:"files"`
	Start     []string     `json:"start,omitempty"`
}

type fileResult struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Backup string `json:"backup,omitempty"`
}

type shellResult struct {
	RCFile  string   `json:"rc_file,omitempty"`
	Action  string   `json:"action"`
	Aliases []string `json:"aliases"`
}

func (r *setupRun) warn(msg string) { r.warnings = append(r.warnings, msg) }

func (r *setupRun) run(args []string) error {
	if ok, err := r.resolveInputs(args); err != nil || !ok {
		return err
	}
	if ok, err := r.plan(); err != nil || !ok {
		return err
	}
	shell, rc, err := r.shellChange(r.state.allAgentsWith(r.agentNamesSelected()))
	if err != nil {
		return err
	}
	if r.opts.DryRun {
		return r.output(r.result(true, shell))
	}
	if !r.opts.Yes {
		if ok, err := r.confirm(shell); err != nil || !ok {
			return err
		}
	}
	return r.apply(shell, rc)
}

// resolveInputs validates flags and picks agents; ok=false is a clean cancel.
func (r *setupRun) resolveInputs(args []string) (ok bool, err error) {
	if r.agents, err = resolveAgentArgs(args, r.opts.Experimental); err != nil {
		return false, err
	}
	if len(r.agents) == 0 && !r.interactive {
		return false, cmdutil.NewMissingFlagsError([]string{argAgent})
	}
	if r.baseURL, err = resolveBaseURL(r.opts.InferenceURL, r.f.Options()); err != nil {
		return false, err
	}
	if !r.opts.Yes && !r.opts.DryRun && !r.interactive {
		return false, cmdutil.NewConfirmationRequiredError("setup")
	}
	r.profile = profileName(r.f)
	if r.state, err = loadState(r.env.statePath()); err != nil {
		return false, err
	}
	if len(r.agents) == 0 {
		if r.agents, err = r.pickAgents(); err != nil || len(r.agents) == 0 {
			return false, err
		}
	}
	if err := r.checkOwnership(); err != nil {
		return false, err
	}
	return r.checkReady()
}

// checkReady: interactive runs skip agents that are not ready; otherwise it is an error.
func (r *setupRun) checkReady() (ok bool, err error) {
	ready := r.agents[:0:0]
	for _, d := range r.agents {
		if d.ready == nil {
			ready = append(ready, d)
			continue
		}
		reason := d.ready(r.env)
		if reason == nil {
			ready = append(ready, d)
			continue
		}
		if !r.interactive {
			return false, &cmdutil.AgentError{
				Code:     "AGENT_NOT_READY",
				Message:  d.Name + ": " + reason.Error(),
				Details:  map[string]any{"agent": d.Name, "reason": reason.Error()},
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		_, _ = fmt.Fprintf(r.io.ErrOut, "Skipping %s: %v\n", d.Display, reason)
	}
	r.agents = ready
	return len(ready) > 0, nil
}

// plan reads models, subscriptions and keys; ok=false is a clean cancel.
func (r *setupRun) plan() (ok bool, err error) {
	if r.client, err = r.f.VerdaClient(); err != nil {
		return false, err
	}
	if err := r.resolveModels(); err != nil || len(r.models) == 0 {
		return false, err
	}
	if err := r.resolveSubscriptions(); err != nil {
		return false, err
	}
	return true, r.buildPlans()
}

func (r *setupRun) agentNamesSelected() []string {
	names := make([]string, len(r.agents))
	for i, d := range r.agents {
		names[i] = d.Name
	}
	return names
}

// resolveAgentArgs validates names before anything touches the API.
func resolveAgentArgs(args []string, experimental bool) ([]*agentDef, error) {
	var out []*agentDef
	for _, a := range args {
		d := lookupAgent(strings.ToLower(a))
		if d == nil || d.Experimental && !experimental {
			if d != nil {
				return nil, cmdutil.NewValidationError(argAgent, d.Name+" needs the Inference Global Router, which is not available yet")
			}
			return nil, cmdutil.NewValidationError(argAgent, fmt.Sprintf("unknown agent %q; supported: %s", a, strings.Join(agentNames(experimental), ", ")))
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *setupRun) pickAgents() ([]*agentDef, error) {
	defs := make([]*agentDef, 0, len(agentDefs))
	labels := make([]string, 0, len(agentDefs))
	var defaults []int
	for _, d := range agentDefs {
		if d.Experimental && !r.opts.Experimental {
			continue
		}
		note := "not found on PATH"
		if r.env.installed(d.Binary) {
			note = "installed"
			if d.ready == nil || d.ready(r.env) == nil {
				defaults = append(defaults, len(defs))
			}
		}
		if r.state.agent(r.profile, d.Name) != nil {
			note += ", set up"
		}
		if d.ready != nil && d.ready(r.env) != nil {
			note += ", not initialized (run it once first)"
		}
		defs = append(defs, d)
		labels = append(labels, fmt.Sprintf("%s (%s)  %s", d.Display, d.Name, note))
	}
	idx, err := r.f.Prompter().MultiSelect(r.cmd.Context(), "Connect which coding agents to Verda?", labels,
		tui.WithMultiSelectDefaults(defaults), tui.WithMinSelections(1), tui.WithMultiSelectShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*agentDef, 0, len(idx))
	for _, i := range idx {
		out = append(out, defs[i])
	}
	return out, nil
}

func (r *setupRun) checkOwnership() error {
	for _, d := range r.agents {
		if owner := r.state.owner(d.Name); owner != "" && owner != r.profile {
			return cmdutil.NewValidationError(argAgent, fmt.Sprintf(
				"%s is already set up for profile %q; run `verda --auth.profile %s inference remove %s` first",
				d.Name, owner, owner, d.Name))
		}
	}
	return nil
}

func setupCandidate(m *verda.InferenceModel) bool {
	return strings.EqualFold(m.Category, "text") &&
		(m.Status == verda.InferenceModelStatusAvailable || m.Status == verda.InferenceModelStatusBeta) &&
		slices.Contains(m.ServiceTiers, tierStandard)
}

func (r *setupRun) previousModels() []string {
	var codes []string
	for _, d := range r.agents {
		if rec := r.state.agent(r.profile, d.Name); rec != nil {
			for _, c := range rec.Models {
				if !slices.Contains(codes, c) {
					codes = append(codes, c)
				}
			}
		}
	}
	return codes
}

func (r *setupRun) resolveModels() error {
	catalog, err := listModels(r.cmd.Context(), r.f, r.client)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(r.io.ErrOut, r.f.Debug(), fmt.Sprintf("API response: %d model(s):", len(catalog)), modelCodes(catalog))
	wanted := r.opts.Models
	switch {
	case len(wanted) > 0:
	case r.interactive:
		return r.pickModels(catalog)
	default:
		if wanted = r.previousModels(); len(wanted) == 0 {
			return cmdutil.NewMissingFlagsError([]string{argModel})
		}
	}
	for _, w := range wanted {
		i := slices.IndexFunc(catalog, func(m verda.InferenceModel) bool { return m.Code == w || m.EndpointModel == w })
		if i < 0 {
			return cmdutil.NewValidationError(argModel, fmt.Sprintf("unknown model %q; see `verda inference models`", w))
		}
		m := catalog[i]
		if m.Status == verda.InferenceModelStatusComingSoon {
			return cmdutil.NewValidationError(argModel, m.Code+" is not available yet")
		}
		if !slices.ContainsFunc(r.models, func(x verda.InferenceModel) bool { return x.Code == m.Code }) {
			r.models = append(r.models, m)
		}
	}
	return nil
}

func (r *setupRun) pickModels(catalog []verda.InferenceModel) error {
	prev := r.previousModels()
	cands := make([]verda.InferenceModel, 0, len(catalog))
	labels := make([]string, 0, len(catalog))
	var defaults []int
	firstAvailable := -1
	for i := range catalog {
		m := &catalog[i]
		if !setupCandidate(m) {
			continue
		}
		if slices.Contains(prev, m.Code) {
			defaults = append(defaults, len(cands))
		}
		if firstAvailable < 0 && m.Status == verda.InferenceModelStatusAvailable {
			firstAvailable = len(cands)
		}
		labels = append(labels, fmt.Sprintf("%s (%s)  %s  context %s", m.Name, m.Code, m.Status, tokens(m.ContextLength)))
		cands = append(cands, *m)
	}
	if len(cands) == 0 {
		return errors.New("no text models are available for coding agents")
	}
	if len(defaults) == 0 {
		defaults = []int{max(firstAvailable, 0)}
	}
	idx, err := r.f.Prompter().MultiSelect(r.cmd.Context(), "Which models should the agents get?", labels,
		tui.WithMultiSelectDefaults(defaults), tui.WithMinSelections(1), tui.WithMultiSelectShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil
		}
		return err
	}
	for _, i := range idx {
		r.models = append(r.models, cands[i])
	}
	return nil
}

func (r *setupRun) resolveSubscriptions() error {
	subs, err := listSubscriptions(r.cmd.Context(), r.f, r.client)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(r.io.ErrOut, r.f.Debug(), fmt.Sprintf("API response: %d subscription(s):", len(subs)), subs)
	for i := range r.models {
		m := &r.models[i]
		action := subActionExisting
		if !slices.ContainsFunc(subs, func(s verda.InferenceSubscription) bool {
			return s.ModelCode == m.Code && s.ServiceTier == tierStandard
		}) {
			if !m.AcceptNewSubscriptions {
				return cmdutil.NewValidationError(argModel, m.Code+" does not accept new subscriptions")
			}
			action = subActionSubscribe
		}
		r.subs = append(r.subs, subscriptionChange{Model: m.Code, Tier: tierStandard, Action: action})
	}
	return nil
}

func keyLabel(agent, host string) string {
	label := "verda-cli " + agent + " (" + host + ")"
	for utf8.RuneCountInString(label) > maxKeyLabel {
		r := []rune(host)
		host = string(r[:len(r)-1])
		label = "verda-cli " + agent + " (" + host + ")"
	}
	return label
}

func (r *setupRun) buildPlans() error {
	keys, err := listKeys(r.cmd.Context(), r.f, r.client)
	if err != nil {
		return err
	}
	r.keyIDs = map[string]bool{}
	for i := range keys {
		r.keyIDs[keys[i].ID] = true
	}
	for _, d := range r.agents {
		p := &agentPlan{def: d, prev: r.state.agent(r.profile, d.Name), label: keyLabel(d.Name, r.env.hostname())}
		p.actx = &agentContext{ctx: r.cmd.Context(), env: r.env, baseURL: r.baseURL, models: r.models, prev: p.prev, warn: r.warn}
		r.planKey(p)
		p.actx.key = placeholderKey
		if p.keyAction == keyActionReuse {
			p.actx.key = p.reuseKey
		}
		if p.changes, err = d.changes(p.actx); err != nil {
			return fmt.Errorf("%s: %w", d.Name, err)
		}
		r.plans = append(r.plans, p)
	}
	return nil
}

func (r *setupRun) planKey(p *agentPlan) {
	if p.prev == nil {
		p.keyAction = keyActionCreate
		return
	}
	alive := r.keyIDs[p.prev.KeyID]
	if alive {
		p.oldKeyID = p.prev.KeyID
		p.label = p.prev.KeyLabel
	}
	key, err := p.def.readKey(&agentContext{ctx: r.cmd.Context(), env: r.env, prev: p.prev})
	switch {
	case r.opts.Rotate:
		p.keyAction = keyActionRotate
	case !alive:
		p.keyAction, p.reason = keyActionCreate, "the previous key no longer exists"
	case err != nil || key == "":
		p.keyAction, p.reason = keyActionReplace, "the stored key could not be read"
	default:
		p.keyAction, p.reuseKey, p.oldKeyID = keyActionReuse, key, ""
	}
}

// shellChange regenerates the alias block for every agent in names.
func (r *setupRun) shellChange(names []string) (*shellResult, *fileChange, error) {
	aliases := make([]string, 0, len(names))
	for _, n := range names {
		d := lookupAgent(n)
		if d == nil || d.alias == nil {
			continue
		}
		a, err := d.alias(&agentContext{env: r.env})
		if err != nil {
			return nil, nil, err
		}
		aliases = append(aliases, a)
	}
	return planRC(r.env, r.state, aliases)
}

func planRC(env *hostEnv, state *agentsState, aliases []string) (*shellResult, *fileChange, error) {
	path := state.RCFile
	if path == "" {
		if len(aliases) == 0 {
			return nil, nil, nil
		}
		path = rcFileFor(env, func(p string) bool { _, ok, _ := readFileIfExists(p); return ok })
	}
	if path == "" {
		return &shellResult{Action: "print", Aliases: aliases}, nil, nil
	}
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return nil, nil, err
	}
	ch := &fileChange{Path: path, Kind: fileKindPatched, Old: old, Exists: exists,
		New: []byte(replaceRCBlock(string(old), renderRCBlock(aliases))), Mode: 0o644, Created: !exists}
	return &shellResult{RCFile: path, Action: ch.action(), Aliases: aliases}, ch, nil
}

// applyRC writes the block; the rc file is backed up the first time only.
func applyRC(state *agentsState, ch *fileChange, aliases []string) error {
	if ch == nil {
		return nil
	}
	if ch.action() != actionUnchanged {
		if ch.Exists && state.RCFile == "" {
			if _, err := backupOnce(ch.Path, ch.Old); err != nil {
				return err
			}
		}
		if err := writeFileAtomic(ch.Path, ch.New, ch.Mode, false); err != nil {
			return err
		}
	}
	state.RCFile = ""
	if len(aliases) > 0 {
		state.RCFile = ch.Path
	}
	return nil
}

func (r *setupRun) apply(shell *shellResult, rc *fileChange) error {
	ctx := r.cmd.Context()
	for _, s := range r.subs {
		if s.Action != subActionSubscribe {
			continue
		}
		cmdutil.DebugJSON(r.io.ErrOut, r.f.Debug(), "Subscribe:", s)
		if _, err := callAPI(ctx, r.f, "Subscribing to "+s.Model+"...", func(ctx context.Context) (*verda.InferenceSubscription, error) {
			return r.client.Inference.Subscribe(ctx, s.Model, s.Tier)
		}); err != nil {
			return fmt.Errorf("subscribing to %s: %w", s.Model, err)
		}
	}
	for _, p := range r.plans {
		if err := r.applyAgent(ctx, p); err != nil {
			return fmt.Errorf("%s: %w", p.def.Name, err)
		}
	}
	if shell != nil {
		if err := applyRC(r.state, rc, shell.Aliases); err != nil {
			return err
		}
		if err := saveState(r.env.statePath(), r.state); err != nil {
			return err
		}
	}
	return r.output(r.result(false, shell))
}

func (r *setupRun) applyAgent(ctx context.Context, p *agentPlan) error {
	key, keyID := p.reuseKey, ""
	if p.prev != nil {
		keyID = p.prev.KeyID
	}
	var created *verda.CreatedInferenceAPIKey
	if p.keyAction != keyActionReuse {
		req := &verda.CreateInferenceAPIKeyRequest{Label: p.label}
		cmdutil.DebugJSON(r.io.ErrOut, r.f.Debug(), "Create API key:", req)
		var err error
		created, err = callAPI(ctx, r.f, "Creating API key for "+p.def.Name+"...", func(ctx context.Context) (*verda.CreatedInferenceAPIKey, error) {
			return r.client.Inference.CreateAPIKey(ctx, req)
		})
		if err != nil {
			return err
		}
		key, keyID, p.label = created.Key, created.ID, created.Label
	}
	p.actx.key = key
	changes, err := p.def.changes(p.actx)
	var recs []fileRecord
	if err == nil {
		recs, err = applyChanges(changes, p.prev)
	}
	if err != nil {
		if created != nil {
			r.deleteKey(ctx, created.ID)
		}
		return err
	}
	p.changes = changes
	r.state.setAgent(r.profile, p.def.Name, &agentRecord{
		KeyID: keyID, KeyLabel: p.label, Models: modelCodes(r.models), BaseURL: r.baseURL,
		Files: recs, UpdatedAt: time.Now().UTC(),
	})
	if err := saveState(r.env.statePath(), r.state); err != nil {
		return err
	}
	if p.oldKeyID != "" && p.oldKeyID != keyID {
		r.deleteKey(ctx, p.oldKeyID)
	}
	return nil
}

// deleteKey is best effort: a failure leaves an unused key, reported as a warning.
func (r *setupRun) deleteKey(ctx context.Context, id string) {
	cmdutil.DebugJSON(r.io.ErrOut, r.f.Debug(), "Delete API key:", map[string]string{"id": id})
	_, err := callAPI(ctx, r.f, "Deleting old API key...", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, r.client.Inference.DeleteAPIKey(ctx, id)
	})
	if err != nil && !isNotFound(err) {
		r.warn(fmt.Sprintf("could not delete API key %s: %v; delete it with `verda inference keys delete %s`", id, err, id))
	}
}
