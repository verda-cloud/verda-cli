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
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagLocation = "location"
	flagYes      = "yes"
	argCluster   = "<cluster>"
	cancelLabel  = "Cancel"
	canceledMsg  = "Canceled."
)

// clusterTarget: names are unique per project, but every API path also needs the location.
type clusterTarget struct {
	Name     string
	Location string
}

func addLocationFlag(cmd *cobra.Command, location *string, usage string) {
	cmd.Flags().VarP((*locationCode)(location), flagLocation, "l", usage)
}

// locationCode: codes are upper case (FIN-03), so -l fin-03 matches them in pickers and API paths.
type locationCode string

func (l *locationCode) String() string { return string(*l) }

func (l *locationCode) Set(s string) error {
	*l = locationCode(strings.ToUpper(strings.TrimSpace(s)))
	return nil
}

func (l *locationCode) Type() string { return "string" }

// checkClusterArg runs before f.VerdaClient(), so bad input fails the same
// with or without credentials. label is <cluster> or --cluster.
func checkClusterArg(f cmdutil.Factory, name string, given bool, label string) error {
	if clusterArgMissing(f, name, given) {
		return cmdutil.NewMissingFlagsError([]string{label})
	}
	return nil
}

// clusterArgMissing: the cluster was not given where it has to be, or was blank.
func clusterArgMissing(f cmdutil.Factory, name string, given bool) bool {
	blank := given && strings.TrimSpace(name) == ""
	return blank || (!given && !interactiveTTY(f))
}

// resolveCluster: name and location make no call, a name alone one List, no
// name a picker. A nil target with a nil error means canceled or nothing to pick.
func resolveCluster(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, name, location string) (*clusterTarget, error) {
	name, location = strings.TrimSpace(name), strings.TrimSpace(location)
	if name != "" && location != "" {
		return &clusterTarget{Name: name, Location: location}, nil
	}
	clusters, err := listClusters(cmd.Context(), f, client)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return pickCluster(cmd.Context(), f, ioStreams, clusters, location)
	}
	return matchCluster(cmd, clusters, name)
}

func argOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func describeCmd(name, location string) string {
	return fmt.Sprintf("verda mk8s cluster describe %s -l %s", name, location)
}

func getCluster(ctx context.Context, f cmdutil.Factory, client *verda.Client, t *clusterTarget) (*verda.ManagedKubernetesCluster, error) {
	return apiCall(ctx, f, "Loading cluster...", func(ctx context.Context) (*verda.ManagedKubernetesCluster, error) {
		return client.ManagedKubernetes.Get(ctx, t.Location, t.Name)
	})
}

// apiCall runs one API call bounded by --timeout, under a spinner, with its error mapped.
func apiCall[T any](ctx context.Context, f cmdutil.Factory, msg string, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	v, err := cmdutil.WithSpinner(ctx, f.Status(), msg, fn)
	return v, mapErr(err)
}

func apiRun(ctx context.Context, f cmdutil.Factory, msg string, fn func(context.Context) error) error {
	_, err := apiCall(ctx, f, msg, func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) })
	return err
}

func listClusters(ctx context.Context, f cmdutil.Factory, client *verda.Client) ([]verda.ManagedKubernetesCluster, error) {
	return apiCall(ctx, f, "Loading clusters...", func(ctx context.Context) ([]verda.ManagedKubernetesCluster, error) {
		return client.ManagedKubernetes.List(ctx)
	})
}

// matchCluster returns plain errors, not AgentErrors (main prints those as JSON
// even for a person); ClassifyError still gives NOT_FOUND and VALIDATION_ERROR.
func matchCluster(cmd *cobra.Command, clusters []verda.ManagedKubernetesCluster, name string) (*clusterTarget, error) {
	var locations []string
	for i := range clusters {
		if clusters[i].Name == name {
			locations = append(locations, clusters[i].Location)
		}
	}
	switch len(locations) {
	case 0:
		return nil, &verda.APIError{StatusCode: http.StatusNotFound, Message: fmt.Sprintf("cluster %q not found", name)}
	case 1:
		return &clusterTarget{Name: name, Location: locations[0]}, nil
	default:
		return nil, cmdutil.UsageErrorf(cmd, "cluster %q exists in several locations (%s); choose one with -l", name, strings.Join(locations, ", "))
	}
}

func pickCluster(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, clusters []verda.ManagedKubernetesCluster, location string) (*clusterTarget, error) {
	var shown []verda.ManagedKubernetesCluster
	for i := range clusters {
		if location == "" || clusters[i].Location == location {
			shown = append(shown, clusters[i])
		}
	}
	if len(shown) == 0 {
		printNoClusters(ioStreams, location)
		return nil, nil
	}
	labels := make([]string, 0, len(shown)+1)
	for i := range shown {
		labels = append(labels, fmt.Sprintf("%s  (%s, %s)", shown[i].Name, shown[i].Location, shown[i].Phase))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select cluster", labels, pickerHints()...)
	switch {
	case err == nil && idx >= 0 && idx < len(shown):
		return &clusterTarget{Name: shown[idx].Name, Location: shown[idx].Location}, nil
	case err != nil && !cmdutil.IsPromptCancel(err):
		return nil, err
	}
	// The Cancel row, Esc and Ctrl+C all end the command cleanly.
	printCanceled(ioStreams)
	return nil, nil
}

// pickerHints: outside a wizard Esc ends the command, so the bar says cancel, not back.
func pickerHints() []tui.SelectOption {
	return []tui.SelectOption{tui.WithShowHints(true), tui.WithSelectRelabel("esc", "esc cancel")}
}

func printNoClusters(ioStreams cmdutil.IOStreams, location string) {
	if location != "" {
		_, _ = fmt.Fprintf(ioStreams.Out, "No clusters found in %s.\n", location)
		return
	}
	_, _ = fmt.Fprintln(ioStreams.Out, "No clusters found.")
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda mk8s cluster create")
}
