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
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func (r *setupRun) result(dryRun bool, shell *shellResult) *setupResult {
	res := &setupResult{
		DryRun: dryRun, Profile: r.profile, BaseURL: r.baseURL, Models: modelCodes(r.models),
		Subscriptions: r.subs, Agents: []agentResult{}, Shell: shell, Warnings: r.warnings,
	}
	for _, p := range r.plans {
		ar := agentResult{
			Agent: p.def.Name, KeyAction: p.keyAction, KeyReason: p.reason, KeyLabel: p.label,
			Models: modelCodes(r.models), Files: fileResults(p.changes, p.prev), Start: p.def.startHints(p.actx),
		}
		if rec := r.state.agent(r.profile, p.def.Name); rec != nil && (!dryRun || p.keyAction == keyActionReuse) {
			ar.KeyID = rec.KeyID
			if !dryRun {
				ar.Files = fileResults(p.changes, rec)
			}
		}
		res.Agents = append(res.Agents, ar)
	}
	return res
}

// fileResults reports each change; rec supplies backups made (or to be made) on first touch.
func fileResults(changes []fileChange, rec *agentRecord) []fileResult {
	out := make([]fileResult, 0, len(changes))
	for i := range changes {
		c := &changes[i]
		fr := fileResult{Path: c.Path, Action: c.action()}
		if fr2 := rec.file(c.Path); fr2 != nil {
			fr.Backup = fr2.Backup
		} else if c.Exists && !c.Created && !c.Delete && fr.Action != actionUnchanged {
			fr.Backup = c.Path + backupSuffix
		}
		out = append(out, fr)
	}
	return out
}

func (r *setupRun) output(res *setupResult) error {
	if wrote, err := writeStructured(r.f, r.io, res); wrote {
		return err
	}
	if res.DryRun {
		_, _ = fmt.Fprintln(r.io.Out, "Dry run: nothing was changed. Setup would:")
		printSetupPlan(r.io.Out, r.env, res)
		return nil
	}
	printWarnings(r.io.ErrOut, res.Warnings)
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	for i := range res.Agents {
		a := &res.Agents[i]
		d := lookupAgent(a.Agent)
		_, _ = fmt.Fprintf(r.io.Out, "%s %s is connected to Verda (key %q, models: %s)\n",
			ok.Render("✓"), d.Display, a.KeyLabel, strings.Join(a.Models, ", "))
		for _, f := range a.Files {
			if f.Action != actionUnchanged {
				_, _ = fmt.Fprintf(r.io.Out, "    %s %s\n", f.Action, r.env.tildePath(f.Path))
			}
		}
		for _, s := range a.Start {
			_, _ = fmt.Fprintf(r.io.Out, "    start: %s\n", s)
		}
	}
	printShell(r.io.Out, r.env, res.Shell)
	return nil
}

func printWarnings(w io.Writer, warnings []string) {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	for _, m := range warnings {
		_, _ = fmt.Fprintln(w, warn.Render("warning: "+m))
	}
}

func printShell(w io.Writer, env *hostEnv, s *shellResult) {
	if s == nil {
		return
	}
	switch {
	case s.Action == "print" && len(s.Aliases) > 0:
		_, _ = fmt.Fprintln(w, "\nYour shell is not zsh or bash; add these aliases to its config:")
		for _, a := range s.Aliases {
			_, _ = fmt.Fprintln(w, "  "+a)
		}
	case s.Action != actionUnchanged && len(s.Aliases) > 0:
		_, _ = fmt.Fprintf(w, "\nAliases are in %s. Open a new shell or run: source %s\n", env.tildePath(s.RCFile), env.tildePath(s.RCFile))
	}
}

func printSetupPlan(w io.Writer, env *hostEnv, res *setupResult) {
	_, _ = fmt.Fprintf(w, "  Profile %s, endpoint %s\n", res.Profile, res.BaseURL)
	for _, s := range res.Subscriptions {
		if s.Action == subActionSubscribe {
			_, _ = fmt.Fprintf(w, "  subscribe the project to %s (%s tier)\n", s.Model, s.Tier)
		}
	}
	for i := range res.Agents {
		a := &res.Agents[i]
		d := lookupAgent(a.Agent)
		_, _ = fmt.Fprintf(w, "  %s:\n", d.Display)
		_, _ = fmt.Fprintf(w, "    %s\n", keyPlanLine(a))
		for _, f := range a.Files {
			line := fmt.Sprintf("    %s %s", f.Action, env.tildePath(f.Path))
			if f.Backup != "" && f.Action != actionUnchanged {
				line += "  (backup " + env.tildePath(f.Backup) + ")"
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}
	if s := res.Shell; s != nil && s.Action != actionUnchanged {
		target := "print the aliases"
		if s.RCFile != "" {
			target = s.Action + " the alias block in " + env.tildePath(s.RCFile)
		}
		_, _ = fmt.Fprintf(w, "  shell: %s\n", target)
	}
}

func keyPlanLine(a *agentResult) string {
	var line string
	switch a.KeyAction {
	case keyActionReuse:
		line = fmt.Sprintf("keep API key %q", a.KeyLabel)
	case keyActionRotate:
		line = fmt.Sprintf("create a new API key %q, then delete the old one", a.KeyLabel)
	case keyActionReplace:
		line = fmt.Sprintf("create a new API key %q and delete the old one", a.KeyLabel)
	default:
		line = fmt.Sprintf("create API key %q", a.KeyLabel)
	}
	if a.KeyReason != "" {
		line += " (" + a.KeyReason + ")"
	}
	return line
}

func (r *setupRun) confirm(shell *shellResult) (bool, error) {
	_, _ = fmt.Fprintln(r.io.ErrOut, "\nSetup will:")
	printSetupPlan(r.io.ErrOut, r.env, r.result(true, shell))
	_, _ = fmt.Fprintln(r.io.ErrOut)
	ok, err := r.f.Prompter().Confirm(r.cmd.Context(), "Apply these changes?")
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			_, _ = fmt.Fprintln(r.io.ErrOut, "Canceled.")
			return false, nil
		}
		return false, err
	}
	if !ok {
		_, _ = fmt.Fprintln(r.io.ErrOut, "Canceled.")
	}
	return ok, nil
}
