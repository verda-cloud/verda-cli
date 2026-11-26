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

package mk8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	accessAdminLabel  = "Admin (full control of the cluster)"
	accessCustomLabel = "Custom groups"

	revokeNote = "A downloaded certificate cannot be revoked before it expires; deleting the kubeconfig does not disable it."
)

// currentUserName pre-fills the common name; a var so tests can set it.
var currentUserName = func() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Windows gives DOMAIN\user.
		return u.Username[strings.LastIndex(u.Username, `\`)+1:]
	}
	return os.Getenv("USER")
}

// kubeconfigAsker: each ask reports ok=false after "Canceled." on Esc or Ctrl+C.
type kubeconfigAsker struct {
	ctx       context.Context
	ioStreams cmdutil.IOStreams
	prompter  tui.Prompter
}

func (a *kubeconfigAsker) text(msg string, opts ...tui.TextInputOption) (v string, ok bool, err error) {
	v, err = a.prompter.TextInput(a.ctx, msg, opts...)
	if err != nil {
		ok, err = a.canceled(err)
		return "", ok, err
	}
	return strings.TrimSpace(v), true, nil
}

func (a *kubeconfigAsker) selectOne(msg string, labels []string) (idx int, ok bool, err error) {
	idx, err = a.prompter.Select(a.ctx, msg, labels, pickerHints()...)
	if err != nil {
		ok, err = a.canceled(err)
		return 0, ok, err
	}
	return idx, true, nil
}

func (a *kubeconfigAsker) canceled(err error) (ok bool, retErr error) {
	if !cmdutil.IsPromptCancel(err) {
		return false, err
	}
	printCanceled(a.ioStreams)
	return false, nil
}

// promptKubeconfigCreate asks for each value its flag did not give; false means canceled.
func promptKubeconfigCreate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *kubeconfigCreateOptions, t *clusterTarget) (bool, error) {
	a := &kubeconfigAsker{ctx: ctx, ioStreams: ioStreams, prompter: f.Prompter()}
	steps := []func() (bool, error){
		func() (bool, error) { return a.askCommonName(opts) },
		func() (bool, error) { return a.askAccess(opts) },
		func() (bool, error) { return a.askDisplayName(opts) },
		func() (bool, error) { return a.askDuration(opts) },
		func() (bool, error) { return a.askFile(opts, t) },
	}
	for _, step := range steps {
		if ok, err := step(); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func nonBlank(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(what + " must not be empty")
		}
		return nil
	}
}

func (a *kubeconfigAsker) askCommonName(opts *kubeconfigCreateOptions) (bool, error) {
	if opts.CommonName != "" {
		return true, nil
	}
	v, ok, err := a.text("Common name (the user name in the cluster)", tui.WithDefault(currentUserName()), tui.WithValidation(nonBlank("common name")))
	if ok {
		opts.CommonName = v
	}
	return ok, err
}

func (a *kubeconfigAsker) askAccess(opts *kubeconfigCreateOptions) (bool, error) {
	if opts.hasAccess() {
		return true, nil
	}
	_, _ = fmt.Fprintf(a.ioStreams.ErrOut, "%s\n", revokeNote)
	idx, ok, err := a.selectOne("Access", []string{accessAdminLabel, accessCustomLabel})
	if !ok {
		return false, err
	}
	if idx == 0 {
		opts.Admin = true
		return true, nil
	}
	v, ok, err := a.text("Group names, separated by commas", tui.WithValidation(func(s string) error {
		if len(cleanGroups(strings.Split(s, ","))) == 0 {
			return errors.New("enter at least one group")
		}
		return nil
	}))
	if ok {
		opts.Groups = cleanGroups(strings.Split(v, ","))
	}
	return ok, err
}

func (a *kubeconfigAsker) askDisplayName(opts *kubeconfigCreateOptions) (bool, error) {
	if opts.displayNameSet {
		return true, nil
	}
	v, ok, err := a.text("Display name (optional)")
	if ok {
		opts.DisplayName = v
	}
	return ok, err
}

// askDuration: the API default is a hint only; an empty answer leaves duration out.
func (a *kubeconfigAsker) askDuration(opts *kubeconfigCreateOptions) (bool, error) {
	if opts.durationSet {
		return true, nil
	}
	v, ok, err := a.text("Duration (optional, for example 720h)", tui.WithPlaceholder("default: 90 days"), tui.WithValidation(validateDurationInput))
	if ok {
		opts.Duration = v
	}
	return ok, err
}

func validateDurationInput(s string) error {
	req := verda.CreateManagedKubernetesKubeconfigRequest{CommonName: "x", Duration: strings.TrimSpace(s)}
	if err := req.Validate(); err != nil {
		return errors.New(strings.TrimSuffix(err.Error(), "."))
	}
	return nil
}

func (a *kubeconfigAsker) askFile(opts *kubeconfigCreateOptions, t *clusterTarget) (bool, error) {
	if opts.fileSet {
		return true, nil
	}
	textOpts := []tui.TextInputOption{tui.WithValidation(kubeconfigFileValidator(t.Name, opts.CommonName, opts.Force))}
	if def, err := newKubeconfigTarget("", t.Name, opts.CommonName); err == nil {
		textOpts = append(textOpts, tui.WithDefault(def.path))
	}
	v, ok, err := a.text("File to write the kubeconfig to", textOpts...)
	if !ok {
		return false, err
	}
	opts.File, opts.fileSet = expandHome(v), true
	return true, nil
}

// expandHome: no shell expands a leading ~/ in a prompt answer.
func expandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}

// kubeconfigFileValidator refuses "-": it would print the private key on the terminal.
func kubeconfigFileValidator(cluster, commonName string, force bool) func(string) error {
	return func(s string) error {
		s = strings.TrimSpace(s)
		if s == kubeconfigStdout {
			return errors.New("enter a file path; use --file - to print the YAML to stdout")
		}
		target, err := newKubeconfigTarget(expandHome(s), cluster, commonName)
		if err != nil {
			return err
		}
		return target.Check(force)
	}
}

// confirmKubeconfigCreate: adminFlag means the access prompt, which prints the note, did not run.
func confirmKubeconfigCreate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, req *verda.CreateManagedKubernetesKubeconfigRequest, dest *kubeconfigDest, adminFlag bool) (bool, error) {
	display, duration, file := req.DisplayName, req.Duration, dest.path()
	if display == "" {
		display = "-"
	}
	if duration == "" {
		duration = "API default (90 days)"
	}
	if dest.stdout {
		file = "stdout"
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\nCreate kubeconfig\n  Cluster:       %s (%s)\n  Common name:   %s\n  Groups:        %s\n  Display name:  %s\n  Duration:      %s\n  File:          %s\n",
		t.Name, t.Location, req.CommonName, strings.Join(req.Groups, ", "), display, duration, file)
	if adminFlag {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n%s\n", revokeNote)
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	return askConfirm(ctx, ioStreams, f.Prompter(), "Create kubeconfig?")
}
