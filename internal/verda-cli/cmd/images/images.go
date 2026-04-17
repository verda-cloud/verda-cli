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

package images

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type listOptions struct {
	InstanceType string
	Category     string
}

// NewCmdImages creates the images command.
func NewCmdImages(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "images",
		Aliases: []string{"image", "img"},
		Short:   "List available OS images",
		Long: cmdutil.LongDesc(`
			List available OS images. Optionally filter by instance type
			to show only compatible images, or by category.
		`),
		Example: cmdutil.Examples(`
			# List all images
			verda images

			# Images compatible with a specific instance type
			verda images --type 1V100.6V

			# Filter by category
			verda images --category ubuntu

			# JSON output for scripting
			verda images -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImages(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.InstanceType, "type", "", "Filter images compatible with this instance type")
	flags.StringVar(&opts.Category, "category", "", "Filter by category (e.g., ubuntu, pytorch)")

	return cmd
}

func runImages(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *listOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading images...")
	}

	var images []verda.Image
	if opts.InstanceType != "" {
		images, err = client.Images.GetImagesByInstanceType(ctx, opts.InstanceType)
	} else {
		images, err = client.Images.Get(ctx)
	}
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	// Filter out cluster images.
	filtered := images[:0]
	for i := range images {
		if images[i].IsCluster {
			continue
		}
		if opts.Category != "" && !strings.EqualFold(images[i].Category, opts.Category) {
			continue
		}
		filtered = append(filtered, images[i])
	}
	images = filtered

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d image(s):", len(images)), images)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), images); wrote {
		return err
	}

	if len(images) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No images found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d image(s) found\n\n", len(images))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-38s  %-45s  %-12s  %s\n", "IMAGE TYPE", "NAME", "CATEGORY", "DETAILS")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-38s  %-45s  %-12s  %s\n", "----------", "----", "--------", "-------")
	for i := range images {
		details := strings.Join(images[i].Details, ", ")
		def := ""
		if images[i].IsDefault {
			def = " *"
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-38s  %-45s  %-12s  %s%s\n",
			images[i].ImageType, images[i].Name, images[i].Category, details, def)
	}
	return nil
}
