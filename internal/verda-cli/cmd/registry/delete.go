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

package registry

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// deleteOptions bundles flag state for `verda registry delete`. Mirrors
// the shape of `vm delete`: positional target + --yes for scripting and
// agent mode, no filter flags (there is no cross-project analog of
// `vm --all`).
type deleteOptions struct {
	Profile         string
	CredentialsFile string
	Yes             bool
}

// deleteAction labels the two operations the command performs. They show
// up in structured output (deleteResult.Action) and in debug traces.
const (
	deleteActionRepository = "delete_repository"
	deleteActionArtifact   = "delete_artifact"
)

// deleteResult is the structured payload emitted in agent mode (JSON/YAML).
// Fields are optional per action:
//
//   - delete_repository sets Repository + DeletedArtifacts (best-effort
//     count observed pre-delete; 0 when the artifact count couldn't be
//     resolved, e.g. on a transient /artifacts 5xx).
//   - delete_artifact sets Repository + Reference and, when we resolved
//     the underlying digest before deletion, Digest and RemovedTags.
//
// The envelope stays stable even if we later add per-action fields —
// agents should key on Action to decide which sub-schema applies.
type deleteResult struct {
	Action           string   `json:"action" yaml:"action"`
	Repository       string   `json:"repository" yaml:"repository"`
	Reference        string   `json:"reference,omitempty" yaml:"reference,omitempty"`
	Digest           string   `json:"digest,omitempty" yaml:"digest,omitempty"`
	RemovedTags      []string `json:"removed_tags,omitempty" yaml:"removed_tags,omitempty"`
	DeletedArtifacts int      `json:"deleted_artifacts,omitempty" yaml:"deleted_artifacts,omitempty"`
	Status           string   `json:"status" yaml:"status"`
}

// NewCmdDelete creates the `verda registry delete` command.
//
// Positional forms mirror the rest of the registry surface (`tags`,
// `copy`):
//
//	delete REPOSITORY             -> whole-repo delete
//	delete REPOSITORY:TAG         -> artifact delete via tag
//	delete REPOSITORY@DIGEST      -> artifact delete via digest
//	delete                        -> interactive (pick repo, then sub-menu)
//
// With no arg on a TTY the command enters an interactive flow that
// mirrors the Harbor web UI's two delete dialogs (screenshot 1 "Delete
// image", screenshot 2 "Delete image repository"). In agent mode a
// positional target is required AND --yes is mandatory; missing --yes
// surfaces a CONFIRMATION_REQUIRED AgentError so automations can retry.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{Profile: defaultProfileName}

	cmd := &cobra.Command{
		Use:     "delete [REPOSITORY[:TAG|@DIGEST]]",
		Aliases: []string{"del", "rm"},
		Short:   "Delete a repository or a single image",
		Long: cmdutil.LongDesc(`
			Delete a repository or a single image (artifact) from the active
			Verda project.

			A bare repository argument removes the repository and every
			artifact / tag under it (equivalent to Harbor's "Delete image
			repository" button). Appending :TAG or @DIGEST scopes the
			delete to a single artifact — the same semantics as the web
			UI's per-row "Delete image": the manifest and every tag that
			pointed at it are removed in one call.

			Without an argument on a terminal, delete enters an interactive
			flow: pick a repository, then choose between deleting selected
			images or the whole repository. Piping or redirecting stdout
			disables the interactive flow — scripts must always pass an
			explicit target. Agent mode (--agent) requires --yes for every
			destructive operation, matching the safety contract used by
			` + "`verda vm delete`" + `.
		`),
		Example: cmdutil.Examples(`
			# Interactive: pick a repo, then delete images or the whole repo
			verda registry delete

			# Delete a whole repository (all artifacts, all tags)
			verda registry delete library/hello-world

			# Delete a single artifact by tag
			verda registry delete library/hello-world:latest

			# Delete a single artifact by digest
			verda registry delete library/hello-world@sha256:d1a8d0a4eeb6

			# Skip confirmation (required in agent mode)
			verda registry delete library/hello-world --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var target string
			if len(args) == 1 {
				target = args[0]
			}
			return runDelete(cmd, f, ioStreams, opts, target)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared Verda credentials file")
	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation (required in agent mode)")

	return cmd
}

// runDelete is the RunE body, split out for testability. Pre-flight
// parity with ls/tags/push/copy: load creds, check expiry, build the
// lister through the swap point, run under the factory's timeout.
func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions, target string) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}
	if creds.ProjectID == "" {
		return &cmdutil.AgentError{
			Code: kindRegistryNotConfigured,
			Message: "Active credential has no project id. Re-run `verda registry configure` " +
				"with the docker-login string from the Verda UI.",
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	lister := buildHarborLister(creds, RetryConfig{})

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	if target == "" {
		if f.AgentMode() {
			// No target + agent mode is ambiguous: the picker isn't
			// available and deleting "something" without specifying it
			// would be a footgun. Fail closed with a clear validation
			// error instead.
			return &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "verda registry delete requires a target (REPOSITORY, REPOSITORY:TAG, or REPOSITORY@DIGEST) in agent mode.",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		if !isTerminalFn(ioStreams.Out) {
			// Piped / redirected stdout: refuse to be interactive
			// because the user can't see our prompts. Symmetric with
			// the AgentMode branch above.
			return &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "verda registry delete needs a target when stdout is not a terminal. Pass REPOSITORY[:TAG|@DIGEST].",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		return runDeleteInteractive(ctx, f, ioStreams, lister, creds)
	}

	return runDeleteTarget(ctx, f, ioStreams, lister, creds, target, opts.Yes)
}

// classifyTarget inspects the raw positional argument and decides whether
// it points at a repository, a tagged artifact, or a digested artifact.
// The classification lives here (not in refname.go) because refname's
// Normalize defaults Tag to "latest" for bare repo references — that
// default is correct for push/copy but would smuggle an unintended tag
// delete into `registry delete library/hello-world`.
//
// We only inspect the path's last segment so host:port and other colons
// earlier in the string can't mis-classify the target.
func classifyTarget(raw string) (isArtifact, isDigest bool) {
	lastSlash := strings.LastIndexByte(raw, '/')
	last := raw
	if lastSlash >= 0 {
		last = raw[lastSlash+1:]
	}
	if strings.ContainsRune(last, '@') {
		return true, true
	}
	if strings.ContainsRune(last, ':') {
		return true, false
	}
	return false, false
}

// runDeleteTarget handles the positional / scripted path: parse the
// target, dispatch to repo-or-artifact delete. Shared by CLI users who
// type a target explicitly AND by the interactive picker after the user
// selects "Delete this repository".
func runDeleteTarget(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, target string, yes bool) error {
	isArtifact, _ := classifyTarget(target)

	ref, err := Normalize(target, creds)
	if err != nil {
		return &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  err.Error(),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	if ref.Project != "" && ref.Project != creds.ProjectID {
		// Cross-project deletes aren't something the robot credential
		// could service even if we let it through — refusing here keeps
		// the error message specific ("wrong project") instead of a
		// confusing 403 from Harbor.
		return &cmdutil.AgentError{
			Code: kindRegistryInvalidReference,
			Message: fmt.Sprintf(
				"Reference points at project %q but the active credential is for project %q. Use credentials for %q or rewrite the reference.",
				ref.Project, creds.ProjectID, ref.Project,
			),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	if isArtifact {
		reference := ref.Digest
		if reference == "" {
			reference = ref.Tag
		}
		return deleteArtifactFlow(ctx, f, ioStreams, lister, creds, ref.Repository, reference, yes)
	}
	return deleteRepositoryFlow(ctx, f, ioStreams, lister, creds, ref.Repository, yes)
}

// deleteRepositoryFlow implements the "Delete image repository" dialog
// from the Harbor UI — a single red-warning + info-box + confirm. We
// fetch the artifact count on a best-effort basis so the confirmation
// surfaces the blast radius ("this image repository holds N image"),
// matching the UI. A failing count lookup degrades gracefully to a
// generic "all artifacts" wording.
func deleteRepositoryFlow(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, repoName string, yes bool) error {
	artifactCount := -1
	if arts, err := lister.ListArtifacts(ctx, creds.ProjectID, repoName); err == nil {
		artifactCount = len(arts)
	}

	if f.AgentMode() {
		if !yes {
			return cmdutil.NewConfirmationRequiredError("delete")
		}
	} else {
		confirmed, err := confirmDeleteRepository(ctx, f, ioStreams, repoName, artifactCount, yes)
		if err != nil {
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete repository:", map[string]any{
		"project":        creds.ProjectID,
		"repository":     repoName,
		"artifact_count": artifactCount,
	})

	err := cmdutil.RunWithSpinner(ctx, f.Status(), fmt.Sprintf("Deleting repository %s...", repoName), func() error {
		return lister.DeleteRepository(ctx, creds.ProjectID, repoName)
	})
	if err != nil {
		return translateErrorWithExpiry(err, creds)
	}

	if f.AgentMode() {
		result := deleteResult{
			Action:     deleteActionRepository,
			Repository: repoName,
			Status:     "completed",
		}
		if artifactCount >= 0 {
			result.DeletedArtifacts = artifactCount
		}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		return nil
	}

	if artifactCount >= 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted repository %s (%d artifact(s) removed).\n", repoName, artifactCount)
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted repository %s.\n", repoName)
	}
	return nil
}

// deleteArtifactFlow implements the "Delete image" dialog from the
// Harbor UI. When reference is a tag, we best-effort resolve the
// backing digest + sibling tags via ListArtifacts so the confirmation
// can show the same "digest / tag / size" row the web UI shows, and so
// the agent-mode payload carries those fields. On lookup failure we
// proceed without the context (Harbor's DELETE is still safe — the
// confirmation just shows less info).
func deleteArtifactFlow(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, repoName, reference string, yes bool) error {
	art := lookupArtifact(ctx, lister, creds.ProjectID, repoName, reference)

	if f.AgentMode() {
		if !yes {
			return cmdutil.NewConfirmationRequiredError("delete")
		}
	} else {
		confirmed, err := confirmDeleteArtifact(ctx, f, ioStreams, repoName, reference, art, yes)
		if err != nil {
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete artifact:", map[string]any{
		"project":    creds.ProjectID,
		"repository": repoName,
		"reference":  reference,
	})

	err := cmdutil.RunWithSpinner(ctx, f.Status(), fmt.Sprintf("Deleting image %s...", reference), func() error {
		return lister.DeleteArtifact(ctx, creds.ProjectID, repoName, reference)
	})
	if err != nil {
		return translateErrorWithExpiry(err, creds)
	}

	if f.AgentMode() {
		result := deleteResult{
			Action:     deleteActionArtifact,
			Repository: repoName,
			Reference:  reference,
			Status:     "completed",
		}
		if art != nil {
			result.Digest = art.Digest
			result.RemovedTags = append([]string(nil), art.Tags...)
		}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted image %s from %s.\n", reference, repoName)
	return nil
}

// lookupArtifact returns the ArtifactInfo for reference (digest or tag)
// inside repoName, or nil on any failure. It's always a best-effort
// enrichment for the confirmation dialog — a nil return is handled
// downstream, never propagated as an error.
func lookupArtifact(ctx context.Context, lister RepositoryLister, projectName, repoName, reference string) *ArtifactInfo {
	arts, err := lister.ListArtifacts(ctx, projectName, repoName)
	if err != nil {
		return nil
	}
	isDigest := strings.HasPrefix(reference, "sha256:")
	for i := range arts {
		if isDigest && strings.HasPrefix(arts[i].Digest, reference) {
			return &arts[i]
		}
		for _, t := range arts[i].Tags {
			if t == reference {
				return &arts[i]
			}
		}
	}
	return nil
}

// runDeleteInteractive: pick a repo from the lister, then ask the user
// what to do inside that repo. The inner action menu matches the two
// Harbor UI affordances — "Delete image(s)" (multi-select over
// artifacts) and "Delete this repository" (wipe the whole thing). The
// picker loops: after a successful delete we refresh the repo list so
// the user sees the state they just mutated.
//
// Cancellation (Esc / Ctrl-C on any prompt) is a clean exit — no
// spurious error is surfaced. Errors from the delete calls themselves
// keep the loop alive, so one failure doesn't throw the user out of
// the picker.
func runDeleteInteractive(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials) error {
	prompter := f.Prompter()

	for {
		repos, err := lister.ListRepositories(ctx, creds.ProjectID)
		if err != nil {
			return translateErrorWithExpiry(err, creds)
		}
		if len(repos) == 0 {
			_, _ = fmt.Fprintf(ioStreams.Out, "No repositories found in project %s.\n", creds.ProjectID)
			return nil
		}
		sortRepos(repos)

		labels := make([]string, 0, len(repos)+1)
		for i := range repos {
			labels = append(labels, formatRepoRow(&repos[i]))
		}
		labels = append(labels, "Exit")

		idx, err := prompter.Select(ctx, "Select repository to manage (type to filter)", labels)
		if err != nil {
			return nil //nolint:nilerr // intentional: prompter cancel is a clean exit
		}
		if idx == len(repos) {
			return nil
		}

		selected := repos[idx]
		exit, err := runDeleteRepoMenu(ctx, f, ioStreams, lister, creds, selected)
		if err != nil {
			// Surface the error but keep the outer loop alive so the
			// user can recover (same contract as ls_test's
			// "ArtifactsErrorStaysInLoop").
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", err)
			continue
		}
		if exit {
			return nil
		}
	}
}

// runDeleteRepoMenu renders the inner "what do you want to do in this
// repo?" menu and drives the two sub-flows. Returns (exit, error):
// exit=true means the user picked Exit on this menu (propagate all the
// way out of the interactive command); err is a non-recoverable lister
// failure.
//
// "Back to repository list" returns (false, nil) so the caller loops.
func runDeleteRepoMenu(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, repo RepositoryInfo) (bool, error) {
	prompter := f.Prompter()

	const (
		menuImages = iota
		menuRepo
		menuBack
		menuExit
	)
	choices := []string{
		"Delete image(s) from " + repo.Name,
		fmt.Sprintf("Delete repository %s (all images)", repo.Name),
		"Back to repository list",
		"Exit",
	}

	for {
		idx, err := prompter.Select(ctx,
			fmt.Sprintf("What would you like to delete in %s?", repo.Name), choices)
		if err != nil {
			return true, nil //nolint:nilerr // intentional: prompter cancel is a clean exit
		}
		switch idx {
		case menuImages:
			if err := runDeleteImagesInteractive(ctx, f, ioStreams, lister, creds, repo); err != nil {
				return false, err
			}
			// Fall through to re-prompt; artifact counts may have
			// changed.
			continue
		case menuRepo:
			if err := deleteRepositoryFlow(ctx, f, ioStreams, lister, creds, repo.Name, false); err != nil {
				return false, err
			}
			// A successful repo delete means there's nothing left to do
			// in this repo — bounce back to the outer picker.
			return false, nil
		case menuBack:
			return false, nil
		case menuExit:
			return true, nil
		}
	}
}

// runDeleteImagesInteractive lists artifacts in repo and runs a
// MultiSelect picker over them. Ctrl-A (the built-in "select all" in
// our bubbletea MultiSelect) is surfaced in the prompt label so users
// discover it even when they skip the hint bar.
//
// The confirm step matches the "Delete image" dialog from the web UI:
// one row per selected artifact, red warning, yes/no.
func runDeleteImagesInteractive(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, repo RepositoryInfo) error {
	arts, err := lister.ListArtifacts(ctx, creds.ProjectID, repo.Name)
	if err != nil {
		return err
	}
	if len(arts) == 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "%s has no images to delete.\n", repo.Name)
		return nil
	}

	labels := make([]string, 0, len(arts))
	for i := range arts {
		labels = append(labels, formatArtifactRow(&arts[i]))
	}

	prompter := f.Prompter()
	indices, err := prompter.MultiSelect(ctx,
		"Select image(s) to delete (space toggles, ctrl+a selects all, enter confirms)",
		labels)
	if err != nil {
		// User canceled the picker — back to the menu.
		return nil //nolint:nilerr // intentional: prompter cancel is a clean exit
	}
	if len(indices) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No images selected.")
		return nil
	}

	selected := make([]ArtifactInfo, 0, len(indices))
	for _, i := range indices {
		if i >= 0 && i < len(arts) {
			selected = append(selected, arts[i])
		}
	}

	confirmed, err := confirmDeleteImagesBatch(ctx, f, ioStreams, repo.Name, selected)
	if err != nil {
		return err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}

	// Delete one-by-one. Harbor does not expose a batch-delete
	// endpoint for artifacts, so the only choice is a loop. Errors on
	// individual artifacts are collected and reported at the end; we
	// don't abort the whole batch on a single 404/412, because users
	// generally want the survivors deleted even if one row is pinned
	// by a retention rule.
	var failures []string
	var succeeded int
	for i := range selected {
		a := &selected[i]
		ref := a.Digest
		if ref == "" && len(a.Tags) > 0 {
			ref = a.Tags[0]
		}
		if ref == "" {
			failures = append(failures, "(unresolved artifact, no digest or tag)")
			continue
		}
		err := cmdutil.RunWithSpinner(ctx, f.Status(),
			fmt.Sprintf("Deleting %s...", shortDigest(a.Digest)),
			func() error {
				return lister.DeleteArtifact(ctx, creds.ProjectID, repo.Name, ref)
			})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", shortDigest(a.Digest), translateErrorWithExpiry(err, creds)))
			continue
		}
		succeeded++
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %d image(s) from %s.\n", succeeded, repo.Name)
	if len(failures) > 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "%d image(s) failed:\n", len(failures))
		for _, line := range failures {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  - %s\n", line)
		}
	}
	return nil
}

// confirmDeleteArtifact renders the single-row dialog that mirrors
// screenshot 1 from the design review:
//
//	Delete image
//	DIGEST                   TAG     SIZE
//	sha256:d1a8d0a4eeb6...   latest  3.92 KiB
//	This action cannot be undone.
//	? Delete image? (y/N)
//
// The caller does not hold a Confirm-prompt mock state when this fires
// in agent mode — deleteArtifactFlow short-circuits before calling us.
func confirmDeleteArtifact(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	repoName, reference string, art *ArtifactInfo, yes bool) (bool, error) {
	if yes {
		return true, nil
	}

	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s  %s\n\n",
		warnStyle.Render("Delete image"), repoName)

	digest := reference
	tag := "--"
	size := "--"
	if art != nil {
		if art.Digest != "" {
			digest = art.Digest
		}
		if len(art.Tags) > 0 {
			tag = strings.Join(art.Tags, ", ")
		}
		if art.Size > 0 {
			size = formatBytes(art.Size)
		}
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-22s  %-30s  %s\n", "DIGEST", "TAG", "SIZE")
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-22s  %-30s  %s\n\n",
		shortDigest(digest), truncateField(tag, 30), size)

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", warnStyle.Render("This action cannot be undone."))

	return f.Prompter().Confirm(ctx, fmt.Sprintf("Delete image %s?", reference))
}

// confirmDeleteImagesBatch renders the multi-row variant of screenshot
// 1 — one DIGEST/TAG/SIZE line per selected artifact. Matches the
// "Delete image" dialog's layout; confirmation is a single yes/no.
func confirmDeleteImagesBatch(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	repoName string, arts []ArtifactInfo) (bool, error) {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s  %s  (%d row(s))\n\n",
		warnStyle.Render("Delete image(s)"), repoName, len(arts))

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-22s  %-30s  %s\n", "DIGEST", "TAG", "SIZE")
	for i := range arts {
		tag := untaggedLabel
		if len(arts[i].Tags) > 0 {
			tag = strings.Join(arts[i].Tags, ", ")
		}
		size := "--"
		if arts[i].Size > 0 {
			size = formatBytes(arts[i].Size)
		}
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-22s  %-30s  %s\n",
			shortDigest(arts[i].Digest), truncateField(tag, 30), size)
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", warnStyle.Render("This action cannot be undone."))

	return f.Prompter().Confirm(ctx,
		fmt.Sprintf("Delete %d image(s) from %s?", len(arts), repoName))
}

// confirmDeleteRepository renders the dialog that mirrors screenshot 2:
//
//	⚠ Delete image repository  library/hello-world
//	ℹ This image repository holds 1 image
//	This will permanently delete the entire image repository.
//	This action cannot be undone.
//	? Delete library/hello-world? (y/N)
//
// artifactCount < 0 means we couldn't look it up — we omit the "holds
// N image" line in that case rather than lie.
func confirmDeleteRepository(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	repoName string, artifactCount int, yes bool) (bool, error) {
	if yes {
		return true, nil
	}

	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s  %s\n\n",
		warnStyle.Render("⚠ Delete image repository"), repoName)

	if artifactCount >= 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s %d %s\n\n",
			infoStyle.Render("ℹ"), artifactCount, pluralImageNoun(artifactCount))
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "  This will permanently delete the entire image repository.")
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", warnStyle.Render("This action cannot be undone."))

	return f.Prompter().Confirm(ctx, fmt.Sprintf("Delete %s?", repoName))
}

// formatArtifactRow is the single-line label used in the image
// MultiSelect. Narrow enough to fit in 80 cols next to the picker's
// checkbox chrome; tags drive the "what am I about to delete?" signal,
// so they get the widest slot.
func formatArtifactRow(a *ArtifactInfo) string {
	tag := untaggedLabel
	if len(a.Tags) > 0 {
		tag = strings.Join(a.Tags, ", ")
	}
	size := "--"
	if a.Size > 0 {
		size = formatBytes(a.Size)
	}
	return fmt.Sprintf("%-22s  %-30s  %10s",
		shortDigest(a.Digest), truncateField(tag, 30), size)
}

// truncateField narrows a field to width, adding "..." when truncated.
// Used by the confirmation dialogs and the MultiSelect labels so long
// tag lists don't break the table layout. Pure; no I/O.
func truncateField(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}

// pluralImageNoun picks "image" or "images" for an artifact count.
// Mirrors the Harbor UI phrasing ("holds 1 image" / "holds 3 images").
func pluralImageNoun(n int) string {
	if n == 1 {
		return "image"
	}
	return "images"
}

// sortRepos sorts repos in place by display name. Factored out because
// both ls and delete's interactive path want the same deterministic
// ordering. Insertion sort is fine here — the expected list length is
// tens of rows, not thousands, and the allocation-free shape keeps the
// hot-path dependency footprint small.
func sortRepos(repos []RepositoryInfo) {
	for i := 1; i < len(repos); i++ {
		for j := i; j > 0 && repos[j-1].Name > repos[j].Name; j-- {
			repos[j-1], repos[j] = repos[j], repos[j-1]
		}
	}
}
