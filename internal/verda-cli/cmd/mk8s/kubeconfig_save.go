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
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagFile  = "file"
	flagForce = "force"

	kubeconfigStdout = "-"
)

// shellSafe leaves out ':', the separator of a KUBECONFIG list.
var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=,./-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type kubeconfigFileFlags struct {
	File  string
	Force bool

	fileSet bool
}

func (k *kubeconfigFileFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&k.File, flagFile, "", "File to write the kubeconfig to, or - for stdout (default ~/.kube/verda-<cluster>.<common-name>.yaml)")
	cmd.Flags().BoolVar(&k.Force, flagForce, false, "Replace the file if it exists (never a symlink or a directory)")
}

// validate rejects --file - with structured output: the YAML would share stdout with the JSON.
func (k *kubeconfigFileFlags) validate(cmd *cobra.Command, f cmdutil.Factory) error {
	k.File = strings.TrimSpace(k.File)
	k.fileSet = cmd.Flags().Changed(flagFile)
	if k.File == kubeconfigStdout && (f.AgentMode() || f.OutputFormat() != outputTable) {
		return cmdutil.UsageErrorf(cmd, "--%s %s writes the YAML to stdout and cannot be combined with --agent or -o json|yaml", flagFile, kubeconfigStdout)
	}
	return nil
}

type kubeconfigDest struct {
	stdout   bool
	target   kubeconfigTarget
	replaced bool // set by write
}

// destination checks the target before a certificate is issued; a bad path is a usage error.
func (k *kubeconfigFileFlags) destination(cmd *cobra.Command, cluster, commonName string) (*kubeconfigDest, error) {
	if k.File == kubeconfigStdout {
		return &kubeconfigDest{stdout: true}, nil
	}
	target, err := newKubeconfigTarget(k.File, cluster, commonName)
	if err != nil {
		return nil, err
	}
	if err := target.Check(k.Force); err != nil {
		return nil, asUsage(cmd, err)
	}
	return &kubeconfigDest{target: target}, nil
}

func (d *kubeconfigDest) path() string {
	if d.stdout {
		return kubeconfigStdout
	}
	return d.target.path
}

func (d *kubeconfigDest) write(ioStreams cmdutil.IOStreams, yaml string, force bool) error {
	if d.stdout {
		if _, err := io.WriteString(ioStreams.Out, yaml); err != nil {
			return fmt.Errorf("write kubeconfig to stdout: %w", err)
		}
		return nil
	}
	if force {
		_, statErr := os.Lstat(d.target.path)
		d.replaced = statErr == nil
	}
	return d.target.Write([]byte(yaml), force)
}

func kubeconfigListCmd(t *clusterTarget) string {
	return fmt.Sprintf("verda mk8s kubeconfig list --cluster %s -l %s", shellQuote(t.Name), shellQuote(t.Location))
}

// kubeconfigSaveCmd leaves out the default path (save derives it) and stdout:
// a recovery command must not print the key.
func kubeconfigSaveCmd(t *clusterTarget, id string, d *kubeconfigDest, force bool) string {
	s := fmt.Sprintf("verda mk8s kubeconfig save %s --cluster %s -l %s", shellQuote(id), shellQuote(t.Name), shellQuote(t.Location))
	if !d.stdout && !d.target.defaultDir {
		s += " --file " + shellQuote(d.target.path)
	}
	if force && !d.stdout {
		s += " --force"
	}
	return s
}

// reportKubeconfigWritten never prints the YAML. earlierStillValid: only create
// replaces a file's kubeconfig with a different credential.
func reportKubeconfigWritten(f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, d *kubeconfigDest, k *verda.ManagedKubernetesKubeconfig, earlierStillValid bool) error {
	file := ""
	if !d.stdout {
		file = d.path()
	}
	view := newKubeconfigView(k, file)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Kubeconfig:", view)
	if earlierStillValid {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "The earlier kubeconfig in that file is still valid. Run `%s` and delete it if you no longer need it.\n", kubeconfigListCmd(t))
	}
	if written, err := writeResult(f, ioStreams, view, nil); written {
		return err
	}
	expires := ""
	if k.NotAfter != nil {
		expires = ", valid until " + utcTime(*k.NotAfter)
	}
	if d.stdout {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Kubeconfig %s written to stdout%s.\n", k.ID, expires)
		return nil
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Kubeconfig %s saved to %s%s.\n", k.ID, file, expires)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "To use it:\n  export KUBECONFIG=%s\n  kubectl get nodes\n", shellQuote(file))
	return nil
}

type kubeconfigSaveOptions struct {
	poolScope
	kubeconfigFileFlags
}

// NewCmdKubeconfigSave creates `verda mk8s kubeconfig save`.
func NewCmdKubeconfigSave(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &kubeconfigSaveOptions{}
	cmd := &cobra.Command{
		Use:   "save [<id>]",
		Short: "Write a ready kubeconfig to a file",
		Long: longDesc(`
			Write the YAML of a ready kubeconfig to a file with mode 0600. Use
			it when kubeconfig create stopped waiting before the kubeconfig was
			ready or could not write the file, or to write the file again. It
			issues no new certificate.

			The file rules are those of kubeconfig create: the default file is
			~/.kube/verda-<cluster>.<common-name>.yaml, an existing file is
			replaced only with --force, and nothing is written through a
			symlink. --file - writes only the YAML to stdout, for a pipe; it
			cannot be combined with -o json|yaml or --agent. The YAML holds a
			private key: it is never part of structured output or --debug.

			If the kubeconfig is not ready yet, the command exits with an
			error: run it again in a moment.

			Without <id> or --cluster, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster and the kubeconfig from lists (terminal only)
			verda mk8s kubeconfig save

			# Write ~/.kube/verda-<cluster>.<common-name>.yaml
			verda mk8s kubeconfig save $KUBECONFIG_ID --cluster prod

			# Replace a file you choose
			verda mk8s kubeconfig save $KUBECONFIG_ID --cluster prod --file ./prod.yaml --force

			# Agent mode: JSON metadata with the path in "file", never the YAML
			verda --agent mk8s kubeconfig save $KUBECONFIG_ID --cluster prod -l FIN-03 --file ./prod.yaml

			# Only the YAML, on stdout, piped to a secret store
			verda mk8s kubeconfig save $KUBECONFIG_ID --cluster prod --file - | gh secret set KUBECONFIG
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeconfigSave(cmd, f, ioStreams, opts, args)
		},
	}
	opts.poolScope.addFlags(cmd)
	opts.kubeconfigFileFlags.addFlags(cmd)
	return cmd
}

func runKubeconfigSave(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *kubeconfigSaveOptions, args []string) error {
	if err := opts.validate(cmd, f); err != nil {
		return err
	}
	if err := opts.check(f, cmd, args, argKubeconfigRef); err != nil {
		return err
	}
	client, target, err := opts.connect(cmd, f, ioStreams)
	if err != nil || target == nil {
		return err
	}
	kc, err := resolveKubeconfig(cmd, f, ioStreams, client, target, args, false)
	if err != nil || kc == nil {
		return err
	}
	id := kc.ID
	k, err := apiCall(cmd.Context(), f, "Loading kubeconfig...", func(ctx context.Context) (*verda.ManagedKubernetesKubeconfig, error) {
		return client.ManagedKubernetes.GetKubeconfig(ctx, target.Location, target.Name, id)
	})
	if err != nil {
		return err
	}
	if _, ready := kubeconfigReady(k); !ready {
		return notReadyError(k)
	}
	dest, err := opts.destination(cmd, target.Name, k.CommonName)
	if err != nil {
		return err
	}
	if err := dest.write(ioStreams, k.Kubeconfig, opts.Force); err != nil {
		return err
	}
	return reportKubeconfigWritten(f, ioStreams, target, dest, k, false)
}

func notReadyError(k *verda.ManagedKubernetesKubeconfig) error {
	detail := "status " + k.Status
	if k.Status == verda.ManagedKubernetesKubeconfigStatusReady {
		detail = "status ready, content not available yet"
	}
	return fmt.Errorf("kubeconfig %s is not ready yet (%s); run save again in a moment", k.ID, detail)
}
