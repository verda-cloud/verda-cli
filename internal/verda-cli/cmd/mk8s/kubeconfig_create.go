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
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagCommonName  = "common-name"
	flagAdmin       = "admin"
	flagGroup       = "group"
	flagDisplayName = "display-name"
	flagDuration    = "duration"

	// groupAdmin is the group that grants cluster-admin.
	groupAdmin = "system:masters"

	defaultKubeconfigWaitTimeout = 3 * time.Minute

	kubeconfigAgainWarning = "Running create again issues another certificate."
)

type kubeconfigCreateOptions struct {
	poolScope
	kubeconfigFileFlags
	CommonName  string
	Admin       bool
	Groups      []string
	DisplayName string
	Duration    string
	WaitTimeout time.Duration

	displayNameSet, durationSet bool
}

// NewCmdKubeconfigCreate creates `verda mk8s kubeconfig create`.
func NewCmdKubeconfigCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &kubeconfigCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kubeconfig and write it to a file",
		Long: longDesc(`
			Issue a client certificate for a cluster and write the kubeconfig to
			a file with mode 0600. The command always waits until the
			kubeconfig is ready, for at most --wait-timeout. The YAML holds a
			private key: it goes to the file only, never to structured output
			or --debug.

			The default file is ~/.kube/verda-<cluster>.<common-name>.yaml;
			~/.kube is created with mode 0700 if it is missing. The directory of
			a --file path must exist. The command checks the file before it
			issues a certificate, and stops if the file exists unless you pass
			--force. It never writes through a symlink. --file - writes only
			the YAML to stdout, for a pipe; it cannot be combined with
			-o json|yaml or --agent. A file you redirect stdout to gets the
			mode of your umask, not 0600.

			Access: --admin adds the group system:masters (full control of the
			cluster) and --group adds another group; repeat --group for more,
			and combine it with --admin if you like. In scripts, agent mode
			and -o json|yaml, --cluster, --common-name and at least one of
			--admin and --group are required. On a terminal, the CLI asks for
			each value that you do not give, then shows a summary and asks
			for confirmation.

			A certificate cannot be revoked before it expires: deleting the
			kubeconfig does not disable copies already downloaded. Deleting the
			cluster invalidates them. Without --duration the API issues it for
			90 days; the minimum is 1h. Use a short --duration for admin
			credentials.

			A create is sent once and is never retried. If it fails with a
			server or network error, the kubeconfig may exist anyway: check
			kubeconfig list before you run it again. If the wait ends before the
			kubeconfig is ready, or the file cannot be written, the error gives
			the kubeconfig ID and the kubeconfig save command to run; running
			create again issues another certificate.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster and answer the prompts (terminal only)
			verda mk8s kubeconfig create

			# Admin access, written to ~/.kube/verda-prod.alice.yaml
			verda mk8s kubeconfig create --cluster prod --common-name alice --admin

			# Two groups, valid for 30 days, written to a file you choose
			verda mk8s kubeconfig create --cluster prod --common-name ci --group developers --group viewers --duration 720h --file ./ci.yaml

			# Replace an existing file
			verda mk8s kubeconfig create --cluster prod --common-name alice --admin --force

			# Agent mode: JSON metadata with the path in "file", never the YAML
			verda --agent mk8s kubeconfig create --cluster prod -l FIN-03 --common-name ci --group developers --file ./ci.yaml

			# Only the YAML, on stdout, piped to a secret store
			verda mk8s kubeconfig create --cluster prod --common-name ci --group developers --file - | gh secret set KUBECONFIG
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.displayNameSet = cmd.Flags().Changed(flagDisplayName)
			opts.durationSet = cmd.Flags().Changed(flagDuration)
			return runKubeconfigCreate(cmd, f, ioStreams, opts)
		},
	}
	opts.poolScope.addFlags(cmd)
	opts.kubeconfigFileFlags.addFlags(cmd)
	flags := cmd.Flags()
	flags.StringVar(&opts.CommonName, flagCommonName, "", "User name in the cluster: the common name of the certificate (not system:*)")
	flags.BoolVar(&opts.Admin, flagAdmin, false, "Grant full control of the cluster (adds the group "+groupAdmin+")")
	flags.StringArrayVar(&opts.Groups, flagGroup, nil, "Kubernetes group of the user, for RBAC (not system:*); repeat the flag for more groups")
	flags.StringVar(&opts.DisplayName, flagDisplayName, "", "Label for the kubeconfig, at most 128 characters")
	flags.StringVar(&opts.Duration, flagDuration, "", "Lifetime of the certificate in Go duration syntax, at least 1h, for example 720h (default: 90 days, set by the API). It cannot be revoked before it expires, only by deleting the cluster: use a short lifetime for admin credentials")
	flags.DurationVar(&opts.WaitTimeout, flagWaitTimeout, defaultKubeconfigWaitTimeout, "Maximum time to wait until the kubeconfig is ready")
	return cmd
}

func (o *kubeconfigCreateOptions) trim() {
	o.Cluster, o.Location = strings.TrimSpace(o.Cluster), strings.TrimSpace(o.Location)
	o.CommonName, o.DisplayName, o.Duration = strings.TrimSpace(o.CommonName), strings.TrimSpace(o.DisplayName), strings.TrimSpace(o.Duration)
	o.Groups = cleanGroups(o.Groups)
}

// cleanGroups trims, drops blanks and duplicates, and keeps the order.
func cleanGroups(in []string) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		if g = strings.TrimSpace(g); g != "" && !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

func (o *kubeconfigCreateOptions) hasAccess() bool { return o.Admin || len(o.Groups) > 0 }

// groups is the access to request: the admin group first, then --group values.
func (o *kubeconfigCreateOptions) groups() []string {
	if !o.Admin {
		return slices.Clone(o.Groups)
	}
	return cleanGroups(append([]string{groupAdmin}, o.Groups...))
}

// request leaves duration out when unset: the API default applies.
func (o *kubeconfigCreateOptions) request() verda.CreateManagedKubernetesKubeconfigRequest {
	return verda.CreateManagedKubernetesKubeconfigRequest{
		CommonName:  o.CommonName,
		Groups:      o.groups(),
		DisplayName: o.DisplayName,
		Duration:    o.Duration,
	}
}

// missingError is the error for what a script must give, or nil. A terminal
// asks instead, except for a blank --cluster, which no command accepts.
func (o *kubeconfigCreateOptions) missingError(f cmdutil.Factory, cmd *cobra.Command) error {
	missing := o.missing(f, cmd)
	if !interactiveTTY(f) {
		if o.CommonName == "" {
			missing = append(missing, "--"+flagCommonName)
		}
		if !o.hasAccess() {
			return cmdutil.NewMissingOneOfFlagsError(missing, []string{"--" + flagAdmin, "--" + flagGroup})
		}
	}
	if len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	return nil
}

func runKubeconfigCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *kubeconfigCreateOptions) error {
	opts.trim()
	if err := opts.validate(cmd, f); err != nil {
		return err
	}
	if opts.WaitTimeout <= 0 {
		return cmdutil.UsageErrorf(cmd, "--%s must be greater than 0", flagWaitTimeout)
	}
	if err := opts.missingError(f, cmd); err != nil {
		return err
	}
	// Cheap input checks first, then the file, then whatever prompts or calls the
	// API. On a terminal the request and the default path are known only after the prompts.
	interactive := interactiveTTY(f)
	var (
		req  verda.CreateManagedKubernetesKubeconfigRequest
		dest *kubeconfigDest
		err  error
	)
	if interactive {
		// A named file that is taken stops the command before any prompt or API call.
		if opts.File != "" && opts.File != kubeconfigStdout {
			if err := (kubeconfigTarget{path: opts.File}).checkFree(opts.Force); err != nil {
				return asUsage(cmd, err)
			}
		}
	} else if req, dest, err = opts.scriptedInput(cmd); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	target, err := opts.resolve(cmd, f, ioStreams, client)
	if err != nil || target == nil {
		return err
	}
	if interactive {
		adminFlag := opts.Admin
		if ok, err := promptKubeconfigCreate(cmd.Context(), f, ioStreams, opts, target); err != nil || !ok {
			return err
		}
		if req, err = opts.validRequest(cmd); err != nil {
			return err
		}
		if dest, err = opts.destination(cmd, target.Name, opts.CommonName); err != nil {
			return err
		}
		if ok, err := confirmKubeconfigCreate(cmd.Context(), f, ioStreams, target, &req, dest, adminFlag); err != nil || !ok {
			return err
		}
	}
	return createKubeconfig(cmd, f, ioStreams, client, target, opts, &req, dest)
}

// scriptedInput validates everything a script gave, before any API call: the
// request, then the file (the cluster name is the one given, names are exact).
func (o *kubeconfigCreateOptions) scriptedInput(cmd *cobra.Command) (verda.CreateManagedKubernetesKubeconfigRequest, *kubeconfigDest, error) {
	req, err := o.validRequest(cmd)
	if err != nil {
		return req, nil, err
	}
	dest, err := o.destination(cmd, o.Cluster, o.CommonName)
	return req, dest, err
}

// validRequest builds the request and rejects what the API would refuse.
func (o *kubeconfigCreateOptions) validRequest(cmd *cobra.Command) (verda.CreateManagedKubernetesKubeconfigRequest, error) {
	req := o.request()
	if err := req.Validate(); err != nil {
		return req, cmdutil.UsageErrorf(cmd, "invalid kubeconfig: %s", strings.TrimSuffix(err.Error(), "."))
	}
	return req, nil
}

func createKubeconfig(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, target *clusterTarget, opts *kubeconfigCreateOptions, req *verda.CreateManagedKubernetesKubeconfigRequest, dest *kubeconfigDest) error {
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	created, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating kubeconfig...", func(ctx context.Context) (*verda.ManagedKubernetesKubeconfig, error) {
		return client.ManagedKubernetes.CreateKubeconfig(ctx, target.Location, target.Name, *req)
	})
	if err != nil {
		return createFailed(f, ioStreams, kindKubeconfig, kubeconfigListCmd(target), err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", newKubeconfigView(created, ""))

	saveCmd := kubeconfigSaveCmd(target, created.ID, dest, opts.Force)
	ready, err := waitForKubeconfig(cmd.Context(), f, ioStreams, client, target, created.ID, opts.WaitTimeout)
	if err != nil {
		return kubeconfigRecovery(f, kubeconfigWaitError(err, target, created, ready, opts.WaitTimeout, saveCmd), created.ID, saveCmd)
	}
	if err := dest.write(ioStreams, ready.Kubeconfig, opts.Force); err != nil {
		err = fmt.Errorf("kubeconfig %s is ready but writing it failed: %w. Fix the cause and run `%s`. %s", created.ID, err, saveCmd, kubeconfigAgainWarning)
		return kubeconfigRecovery(f, err, created.ID, saveCmd)
	}
	return reportKubeconfigWritten(f, ioStreams, target, dest, ready, dest.replaced)
}

// waitForKubeconfig polls until the kubeconfig is ready with its YAML. It
// returns the last read, which holds the YAML: keep it out of every output.
func waitForKubeconfig(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, id string, timeout time.Duration) (*verda.ManagedKubernetesKubeconfig, error) {
	if f.OutputFormat() == outputTable {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Kubeconfig %s created; waiting until it is ready (Ctrl+C stops waiting, the kubeconfig stays).\n", id)
	}
	return waitFor(ctx, f, waitWriter(f, ioStreams), timeout,
		func(ctx context.Context) (*verda.ManagedKubernetesKubeconfig, error) {
			return client.ManagedKubernetes.GetKubeconfig(ctx, t.Location, t.Name, id)
		}, kubeconfigReady)
}

// kubeconfigWaitError says what to do after a wait that did not end in a ready kubeconfig.
func kubeconfigWaitError(err error, t *clusterTarget, created, last *verda.ManagedKubernetesKubeconfig, timeout time.Duration, saveCmd string) error {
	subject := fmt.Sprintf("kubeconfig %s of cluster %s (%s)", created.ID, t.Name, t.Location)
	tail := fmt.Sprintf("Run `%s` once it is ready. %s", saveCmd, kubeconfigAgainWarning)
	if readErr, ok := errors.AsType[*pollReadError](err); ok {
		return fmt.Errorf("%s was created, but its status could not be read (%w). %s", subject, readErr.err, tail)
	}
	seen := created.Status
	if last != nil {
		seen = last.Status
	}
	return fmt.Errorf("timed out after %s waiting for %s to become ready (last status: %s). It was created and is still being issued. %s", timeout, subject, seen, tail)
}

// kubeconfigRecovery adds the kubeconfig ID and the save command to the agent
// error details; people get them in the message.
func kubeconfigRecovery(f cmdutil.Factory, err error, id, saveCmd string) error {
	if !f.AgentMode() {
		return err
	}
	ae := *cmdutil.ClassifyError(err)
	details := make(map[string]any, len(ae.Details)+3)
	maps.Copy(details, ae.Details)
	details["kubeconfig_id"] = id
	details["save_command"] = saveCmd
	details["warning"] = kubeconfigAgainWarning
	ae.Details = details
	ae.Message = err.Error() // the recovery text, not the classified API message
	return &ae
}
