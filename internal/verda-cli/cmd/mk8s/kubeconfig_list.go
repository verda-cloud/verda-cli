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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const argKubeconfigRef = "<id>"

// NewCmdKubeconfigList creates `verda mk8s kubeconfig list`.
func NewCmdKubeconfigList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the kubeconfigs of a cluster",
		Long: longDesc(`
			List the kubeconfigs of a cluster: their IDs, who each one is for,
			its groups, its status and when it expires. EXPIRES is "-" while a
			kubeconfig is pending. The list never shows the kubeconfig YAML.

			Without --cluster, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			verda mk8s kubeconfig list --cluster prod

			# JSON for scripts: expiry is in not_after
			verda mk8s kubeconfig ls --cluster prod -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runKubeconfigList(cmd, f, ioStreams, scope)
		},
	}
	scope.addFlags(cmd)
	return cmd
}

func runKubeconfigList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope) error {
	if missing := scope.missing(f, cmd); len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	target, err := scope.resolve(cmd, f, ioStreams, client)
	if err != nil || target == nil {
		return err
	}
	views, err := listKubeconfigViews(cmd.Context(), f, client, target)
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d kubeconfig(s):", len(views)), views)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		printNoKubeconfigs(ioStreams, target.Name)
		return nil
	}

	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDISPLAY NAME\tCOMMON NAME\tGROUPS\tSTATUS\tEXPIRES")
	for i := range views {
		v := &views[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.ID, cmdutil.TextColumn(v.DisplayName), v.CommonName, groupsColumn(v.Groups), v.Status, expiresColumn(v.NotAfter))
	}
	return tw.Flush()
}

// listKubeconfigViews and getKubeconfigView convert inside the call, so an SDK
// struct (with the YAML and its private key) never reaches a printer or the debug log.
func listKubeconfigViews(ctx context.Context, f cmdutil.Factory, client *verda.Client, t *clusterTarget) ([]KubeconfigView, error) {
	return apiCall(ctx, f, "Loading kubeconfigs...", func(ctx context.Context) ([]KubeconfigView, error) {
		list, err := client.ManagedKubernetes.ListKubeconfigs(ctx, t.Location, t.Name)
		if err != nil {
			return nil, err
		}
		views := make([]KubeconfigView, 0, len(list))
		for i := range list {
			views = append(views, newKubeconfigView(&list[i], ""))
		}
		return views, nil
	})
}

func getKubeconfigView(ctx context.Context, f cmdutil.Factory, client *verda.Client, t *clusterTarget, id string) (*KubeconfigView, error) {
	return apiCall(ctx, f, "Loading kubeconfig...", func(ctx context.Context) (*KubeconfigView, error) {
		k, err := client.ManagedKubernetes.GetKubeconfig(ctx, t.Location, t.Name, id)
		if err != nil {
			return nil, err
		}
		v := newKubeconfigView(k, "")
		return &v, nil
	})
}

// checkKubeconfigTarget fails before any call when --cluster or <id> cannot be
// asked for. It lists everything missing at once.
func checkKubeconfigTarget(f cmdutil.Factory, cmd *cobra.Command, scope *poolScope, args []string) error {
	missing := scope.missing(f, cmd)
	if poolArgMissing(f, args) {
		missing = append(missing, argKubeconfigRef)
	}
	if len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	return nil
}

// resolveKubeconfig: a named id is read when read is set and used as given
// otherwise; with no id a picker lists the cluster's kubeconfigs. A nil result
// with a nil error means the user canceled or the cluster has none.
func resolveKubeconfig(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, args []string, read bool) (*KubeconfigView, error) {
	if len(args) > 0 {
		id := strings.TrimSpace(args[0])
		if !read {
			return &KubeconfigView{ID: id}, nil
		}
		return getKubeconfigView(cmd.Context(), f, client, t, id)
	}
	views, err := listKubeconfigViews(cmd.Context(), f, client, t)
	if err != nil {
		return nil, err
	}
	return pickKubeconfig(cmd.Context(), f, ioStreams, t, views)
}

func pickKubeconfig(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, views []KubeconfigView) (*KubeconfigView, error) {
	if len(views) == 0 {
		printNoKubeconfigs(ioStreams, t.Name)
		return nil, nil
	}
	labels := make([]string, 0, len(views)+1)
	for i := range views {
		v := &views[i]
		labels = append(labels, fmt.Sprintf("%s  (%s, %s)", v.ID, v.CommonName, v.Status))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select kubeconfig", labels, pickerHints()...)
	switch {
	case err == nil && idx >= 0 && idx < len(views):
		return &views[idx], nil
	case err != nil && !cmdutil.IsPromptCancel(err):
		return nil, err
	}
	printCanceled(ioStreams)
	return nil, nil
}

func printNoKubeconfigs(ioStreams cmdutil.IOStreams, cluster string) {
	_, _ = fmt.Fprintf(ioStreams.Out, "No kubeconfigs in cluster %s.\n", cluster)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Create one with: verda mk8s kubeconfig create --cluster %s --common-name <name> --admin\n", cluster)
}

func groupsColumn(groups []string) string {
	return cmdutil.TextColumn(strings.Join(groups, ","))
}

// expiresColumn is "-" while the kubeconfig is pending: the API sets not_after when it issues the certificate.
func expiresColumn(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return timeColumn(*t)
}
