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
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdClusterList creates `verda mk8s cluster list`.
func NewCmdClusterList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var location string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List clusters",
		Long: longDesc(`
			List the managed Kubernetes clusters in your project, in every
			location or, with -l, in one.
		`),
		Example: cmdutil.Examples(`
			verda mk8s cluster list
			verda mk8s cluster list -l FIN-03

			# Names only, for scripts
			verda mk8s cluster ls -o json | jq -r '.[].name'
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClusterList(cmd, f, ioStreams, location)
		},
	}
	addLocationFlag(cmd, &location, "List only the clusters in this location")
	return cmd
}

func runClusterList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, location string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	clusters, err := listClusters(cmd.Context(), f, client)
	if err != nil {
		return err
	}
	if location != "" {
		clusters = slices.DeleteFunc(clusters, func(c verda.ManagedKubernetesCluster) bool { return c.Location != location })
	}
	views := newClusterViews(clusters)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d cluster(s):", len(views)), views)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		printNoClusters(ioStreams, location)
		return nil
	}

	now := time.Now()
	for _, withEndpoint := range []bool{true, false} {
		var buf bytes.Buffer
		if err := writeClusterTable(&buf, views, now, withEndpoint); err != nil {
			return err
		}
		if !withEndpoint || widestLine(buf.String()) <= clusterTableWidth {
			_, err := ioStreams.Out.Write(buf.Bytes())
			return err
		}
	}
	return nil
}

// clusterTableWidth: the endpoint holds the project ID, so its column is
// dropped when a row is wider; -o json and describe always show it.
const clusterTableWidth = 120

func writeClusterTable(w io.Writer, views []ClusterView, now time.Time, withEndpoint bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "NAME\tLOCATION\tVERSION\tPHASE\t"
	if withEndpoint {
		header += "ENDPOINT\t"
	}
	_, _ = fmt.Fprintln(tw, header+"AGE")
	for i := range views {
		v := &views[i]
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t", v.Name, v.Location, v.KubernetesVersion, v.Phase)
		if withEndpoint {
			row += cmdutil.TextColumn(strings.TrimPrefix(v.ExternalEndpoint, "https://")) + "\t"
		}
		_, _ = fmt.Fprintln(tw, row+ageColumn(v.CreatedAt, now))
	}
	return tw.Flush()
}

func widestLine(s string) int {
	widest := 0
	for line := range strings.SplitSeq(s, "\n") {
		widest = max(widest, utf8.RuneCountInString(line))
	}
	return widest
}
