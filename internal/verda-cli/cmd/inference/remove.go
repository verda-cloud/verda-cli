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
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type removeOptions struct {
	Yes    bool
	DryRun bool
}

type removeResult struct {
	DryRun   bool                `json:"dry_run"`
	Profile  string              `json:"profile"`
	Agents   []removeAgentResult `json:"agents"`
	Shell    *shellResult        `json:"shell,omitempty"`
	Warnings []string            `json:"warnings,omitempty"`
}

type removeAgentResult struct {
	Agent      string       `json:"agent"`
	KeyID      string       `json:"key_id,omitempty"`
	KeyLabel   string       `json:"key_label,omitempty"`
	KeyDeleted bool         `json:"key_deleted"`
	Files      []fileResult `json:"files"`
}

type removePlan struct {
	def     *agentDef
	rec     *agentRecord
	changes []fileChange
}

func newCmdRemove(f cmdutil.Factory, ioStreams cmdutil.IOStreams, envFn func() (*hostEnv, error)) *cobra.Command {
	opts := &removeOptions{}
	cmd := &cobra.Command{
		Use:     "remove [agent...]",
		Aliases: []string{"uninstall"},
		Short:   "Disconnect coding agents from Verda and delete their API keys",
		Long: cmdutil.LongDesc(`
			Undo "verda inference setup" for the given agents: remove the verda
			provider and models from the agent config, delete the files setup
			created, drop the agent's shell alias, and delete the agent's API key.
			Everything else in the agent config stays as it is. Agent mode
			requires --yes.
		`),
		Example: cmdutil.Examples(`
			verda inference remove opencode
			verda inf remove kimi --yes
			verda inf remove opencode --dry-run
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := envFn()
			if err != nil {
				return err
			}
			return runRemove(cmd, f, ioStreams, opts, env, args)
		},
	}
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation (required when not interactive)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would change without changing anything")
	return cmd
}

func runRemove(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *removeOptions, env *hostEnv, args []string) error {
	state, err := loadState(env.statePath())
	if err != nil {
		return err
	}
	profile := profileName(f)
	defs, err := resolveRemoveAgents(cmd.Context(), f, ioStreams, opts, state, profile, args)
	if err != nil || len(defs) == 0 {
		return err
	}
	plans, err := planRemove(env, state, profile, defs)
	if err != nil {
		return err
	}
	shell, rc, err := planRC(env, state, remainingAliases(env, state, defs))
	if err != nil {
		return err
	}
	res := &removeResult{DryRun: opts.DryRun, Profile: profile, Shell: shell}
	if opts.DryRun {
		res.Agents = removeResults(plans)
		return writeRemoveResult(f, ioStreams, env, res)
	}
	if !opts.Yes {
		if ok, err := confirmRemove(cmd.Context(), f, ioStreams, plans); err != nil || !ok {
			return err
		}
	}
	deleted, warnings, err := applyRemove(cmd.Context(), f, ioStreams, env, state, profile, plans)
	res.Warnings = warnings
	if err != nil {
		return err
	}
	if shell != nil {
		if err := applyRC(state, rc, shell.Aliases); err != nil {
			return err
		}
		if err := saveState(env.statePath(), state); err != nil {
			return err
		}
	}
	res.Agents = removeResults(plans)
	for i := range res.Agents {
		res.Agents[i].KeyDeleted = deleted[res.Agents[i].Agent]
	}
	return writeRemoveResult(f, ioStreams, env, res)
}

func resolveRemoveAgents(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *removeOptions,
	state *agentsState, profile string, args []string,
) ([]*agentDef, error) {
	interactive := interactiveTTY(f)
	if !opts.Yes && !opts.DryRun && !interactive {
		return nil, cmdutil.NewConfirmationRequiredError("remove")
	}
	defs, err := resolveAgentArgs(args, true)
	if err != nil || len(defs) > 0 {
		return defs, err
	}
	if !interactive {
		return nil, cmdutil.NewMissingFlagsError([]string{argAgent})
	}
	return pickSetUpAgents(ctx, f, ioStreams, state, profile)
}

func pickSetUpAgents(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, state *agentsState, profile string) ([]*agentDef, error) {
	names := state.agentNames(profile)
	if len(names) == 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "No agents are set up for profile %q.\n", profile)
		return nil, nil
	}
	idx, err := f.Prompter().MultiSelect(ctx, "Disconnect which agents from Verda?", names,
		tui.WithMinSelections(1), tui.WithMultiSelectShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	defs := make([]*agentDef, 0, len(idx))
	for _, i := range idx {
		if d := lookupAgent(names[i]); d != nil {
			defs = append(defs, d)
		}
	}
	return defs, nil
}

func planRemove(env *hostEnv, state *agentsState, profile string, defs []*agentDef) ([]*removePlan, error) {
	plans := make([]*removePlan, 0, len(defs))
	for _, d := range defs {
		if owner := state.owner(d.Name); owner != "" && owner != profile {
			return nil, cmdutil.NewValidationError(argAgent, fmt.Sprintf(
				"%s was set up for profile %q; run `verda --auth.profile %s inference remove %s`", d.Name, owner, owner, d.Name))
		}
		rec := state.agent(profile, d.Name)
		changes, err := d.cleanChanges(&agentContext{env: env, prev: rec})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name, err)
		}
		plans = append(plans, &removePlan{def: d, rec: rec, changes: append(changes, strayOwnedFiles(rec, changes)...)})
	}
	return plans, nil
}

// strayOwnedFiles: files setup created at paths the current env no longer
// points to (e.g. CODEX_HOME changed since setup).
func strayOwnedFiles(rec *agentRecord, changes []fileChange) []fileChange {
	if rec == nil {
		return nil
	}
	out := make([]fileChange, 0, len(rec.Files))
	for _, fr := range rec.Files {
		if fr.Kind != fileKindOwned || slices.ContainsFunc(changes, func(c fileChange) bool { return c.Path == fr.Path }) {
			continue
		}
		_, exists, _ := readFileIfExists(fr.Path)
		out = append(out, fileChange{Path: fr.Path, Kind: fileKindOwned, Exists: exists, Delete: true, RestoreFrom: fr.Backup})
	}
	return out
}

func remainingAliases(env *hostEnv, state *agentsState, removed []*agentDef) []string {
	var aliases []string
	for _, n := range state.allAgents() {
		d := lookupAgent(n)
		if d == nil || d.alias == nil || slices.Contains(removed, d) {
			continue
		}
		if a, err := d.alias(&agentContext{env: env}); err == nil {
			aliases = append(aliases, a)
		}
	}
	return aliases
}

func confirmRemove(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, plans []*removePlan) (bool, error) {
	names := make([]string, 0, len(plans))
	keys := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.def.Display)
		if p.rec != nil && p.rec.KeyID != "" {
			keys = append(keys, fmt.Sprintf("%q", p.rec.KeyLabel))
		}
	}
	detail := "Their verda provider entries are removed; other settings stay."
	if len(keys) > 0 {
		detail += " API keys deleted: " + strings.Join(keys, ", ") + "."
	}
	return confirmDanger(ctx, f, ioStreams, "Disconnect "+strings.Join(names, ", ")+" from Verda", detail, "Remove?")
}

func applyRemove(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, env *hostEnv, state *agentsState,
	profile string, plans []*removePlan,
) (deleted map[string]bool, warnings []string, _ error) {
	deleted = map[string]bool{}
	for _, p := range plans {
		if _, err := applyChanges(p.changes, p.rec); err != nil {
			return deleted, warnings, fmt.Errorf("%s: %w", p.def.Name, err)
		}
		if p.rec != nil && p.rec.KeyID != "" {
			ok, warn := deleteAgentKey(ctx, f, ioStreams, p.rec.KeyID)
			deleted[p.def.Name] = ok
			if warn != "" {
				warnings = append(warnings, warn)
			}
		}
		state.removeAgent(profile, p.def.Name)
		if err := saveState(env.statePath(), state); err != nil {
			return deleted, warnings, err
		}
	}
	return deleted, warnings, nil
}

// deleteAgentKey: a key the API no longer has counts as deleted.
func deleteAgentKey(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, id string) (deleted bool, warning string) {
	client, err := f.VerdaClient()
	if err == nil {
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete API key:", map[string]string{"id": id})
		_, err = callAPI(ctx, f, "Deleting API key...", func(ctx context.Context) (struct{}, error) {
			return struct{}{}, client.Inference.DeleteAPIKey(ctx, id)
		})
	}
	if err == nil || isNotFound(err) {
		return true, ""
	}
	return false, fmt.Sprintf("could not delete API key %s: %v; delete it with `verda inference keys delete %s`", id, err, id)
}

func removeResults(plans []*removePlan) []removeAgentResult {
	out := make([]removeAgentResult, 0, len(plans))
	for _, p := range plans {
		ar := removeAgentResult{Agent: p.def.Name, Files: []fileResult{}}
		if p.rec != nil {
			ar.KeyID, ar.KeyLabel = p.rec.KeyID, p.rec.KeyLabel
		}
		for i := range p.changes {
			c := &p.changes[i]
			ar.Files = append(ar.Files, fileResult{Path: c.Path, Action: c.action(), Backup: c.RestoreFrom})
		}
		out = append(out, ar)
	}
	return out
}

func writeRemoveResult(f cmdutil.Factory, ioStreams cmdutil.IOStreams, env *hostEnv, res *removeResult) error {
	if wrote, err := writeStructured(f, ioStreams, res); wrote {
		return err
	}
	printWarnings(ioStreams.ErrOut, res.Warnings)
	if res.DryRun {
		_, _ = fmt.Fprintln(ioStreams.Out, "Dry run: nothing was changed. Remove would:")
	}
	for i := range res.Agents {
		a := &res.Agents[i]
		d := lookupAgent(a.Agent)
		switch {
		case res.DryRun && a.KeyID != "":
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s: delete API key %q\n", d.Display, a.KeyLabel)
		case res.DryRun:
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s: no API key recorded\n", d.Display)
		case a.KeyDeleted:
			_, _ = fmt.Fprintf(ioStreams.Out, "Disconnected %s from Verda; deleted API key %q.\n", d.Display, a.KeyLabel)
		default:
			_, _ = fmt.Fprintf(ioStreams.Out, "Disconnected %s from Verda.\n", d.Display)
		}
		for _, fr := range a.Files {
			if fr.Action != actionUnchanged {
				_, _ = fmt.Fprintf(ioStreams.Out, "    %s %s\n", fr.Action, env.tildePath(fr.Path))
			}
		}
	}
	if s := res.Shell; s != nil && s.RCFile != "" && s.Action != actionUnchanged {
		_, _ = fmt.Fprintf(ioStreams.Out, "    update %s (aliases)\n", env.tildePath(s.RCFile))
	}
	return nil
}
