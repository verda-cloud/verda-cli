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
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdLocations creates `verda mk8s locations`.
func NewCmdLocations(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "locations",
		Aliases: []string{"location", "loc"},
		Short:   "List locations enabled for managed Kubernetes",
		Long: longDesc(`
			List the locations where this project can create managed Kubernetes
			clusters. Use one of them with -l when you create a cluster. A
			project without access to managed Kubernetes gets an error that
			says so.
		`),
		Example: cmdutil.Examples(`
			verda mk8s locations

			# A JSON array of location codes, for scripts
			verda mk8s locations -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLocations(cmd, f, ioStreams)
		},
	}
}

func runLocations(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	res, err := apiCall(cmd.Context(), f, "Loading locations...", client.ManagedKubernetes.GetEnabledLocations)
	if err != nil {
		return err
	}
	codes := res.Locations
	if codes == nil {
		codes = []string{}
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d location(s):", len(codes)), codes)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), codes); wrote {
		return err
	}
	if len(codes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "Managed Kubernetes is enabled for this project, but no location is configured.")
		return nil
	}
	_, _ = fmt.Fprintln(ioStreams.Out, "LOCATION")
	for _, c := range codes {
		_, _ = fmt.Fprintln(ioStreams.Out, c)
	}
	return nil
}
