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

package volume

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdTrash creates the volume trash command.
func NewCmdTrash(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "trash",
		Short: "List volumes in trash",
		Long: cmdutil.LongDesc(`
			List deleted volumes that can still be restored (within 96 hours).
		`),
		Example: cmdutil.Examples(`
			verda volume trash
			verda vol trash
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrash(cmd, f, ioStreams)
		},
	}
}

func runTrash(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading trash...")
	}
	volumes, err := client.Volumes.GetVolumesInTrash(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d trashed volume(s):", len(volumes)), volumes)

	if len(volumes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "Trash is empty.")
		return nil
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold := lipgloss.NewStyle().Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "  %d volume(s) in trash\n\n", len(volumes))

	for i := range volumes {
		v := &volumes[i]
		volType := "Block"
		if v.IsOSVolume {
			volType = "OS"
		}

		permStatus := ""
		if v.IsPermanentlyDeleted {
			permStatus = warnStyle.Render("  (permanently deleted)")
		}

		_, _ = fmt.Fprintf(&b, "  %s  %s%s\n", bold.Render(v.Name), dim.Render(volType), permStatus)
		_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("ID:      "), v.ID)
		_, _ = fmt.Fprintf(&b, "    %s  %dGB %s\n", dim.Render("Size:    "), v.Size, v.Type)
		_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Location:"), v.Location)
		_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Contract:"), v.Contract)
		if v.MonthlyPrice > 0 {
			_, _ = fmt.Fprintf(&b, "    %s  $%.2f/mo (%s)\n", dim.Render("Price:   "), v.MonthlyPrice, v.Currency)
		}
		_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Deleted: "), v.DeletedAt.Format("2 Jan 2006, 15:04"))

		if !v.IsPermanentlyDeleted && !v.DeletedAt.IsZero() {
			expiresAt := v.DeletedAt.Add(96 * time.Hour)
			remaining := time.Until(expiresAt)
			if remaining > 0 {
				_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Expires: "), fmt.Sprintf("%s (%s remaining)", expiresAt.Format("2 Jan 2006, 15:04"), formatDuration(remaining)))
			}
		}
		_, _ = fmt.Fprintln(&b)
	}

	// Use pager for scrollable output when list is long.
	if status := f.Status(); status != nil {
		return status.Pager(cmd.Context(), b.String(), tui.WithPagerTitle(fmt.Sprintf("Trash (%d volumes)", len(volumes))))
	}
	_, _ = fmt.Fprint(ioStreams.Out, b.String())
	return nil
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	if h >= 24 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	return fmt.Sprintf("%dh", h)
}
