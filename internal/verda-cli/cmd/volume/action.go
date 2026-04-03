package volume

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type volumeAction struct {
	Label      string
	ConfirmMsg string
	WarningMsg string
	Prepare    func(ctx context.Context) error // collect user input before spinner
	Execute    func(ctx context.Context, client *verda.Client, vol *verda.Volume) error
}

// NewCmdAction creates the volume action command.
func NewCmdAction(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var volumeID string
	var waitOpts cmdutil.WaitOptions

	cmd := &cobra.Command{
		Use:   "action",
		Short: "Perform actions on a volume",
		Long: cmdutil.LongDesc(`
			Select a volume and perform an action: detach, rename,
			resize, clone, or delete.
		`),
		Example: cmdutil.Examples(`
			verda volume action
			verda vol action --id abc-123
			verda vol action --id abc-123 --wait
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVolumeAction(cmd, f, ioStreams, volumeID, waitOpts)
		},
	}

	cmd.Flags().StringVar(&volumeID, "id", "", "Volume ID to act on")
	waitOpts.AddFlags(cmd.Flags(), false) // --wait defaults to false for volume action
	return cmd
}

//nolint:gocyclo // Interactive CLI command with prompts, confirmation, and spinner — inherently complex.
func runVolumeAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, volumeID string, waitOpts cmdutil.WaitOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	if volumeID == "" {
		selected, err := selectVolume(ctx, f, ioStreams, client)
		if err != nil {
			return err
		}
		if selected == "" {
			return nil
		}
		volumeID = selected
	}

	vol, err := client.Volumes.GetVolume(ctx, volumeID)
	if err != nil {
		return fmt.Errorf("fetching volume: %w", err)
	}

	renderVolumeSummary(ioStreams.Out, vol)

	// Build actions, some need user input collected before execute.
	actions := buildVolumeActions(ctx, prompter, client, vol)
	if len(actions) == 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "No actions available for this volume.\n")
		return nil
	}

	labels := make([]string, 0, len(actions)+1)
	for _, a := range actions {
		labels = append(labels, a.Label)
	}
	labels = append(labels, "Cancel")

	idx, err := prompter.Select(ctx, "Select action", labels)
	if err != nil {
		return nil
	}
	if idx == len(actions) {
		return nil
	}

	action := actions[idx]
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	if action.ConfirmMsg != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", action.ConfirmMsg)
	}
	if action.WarningMsg != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", warnStyle.Render(action.WarningMsg))
	}
	if action.ConfirmMsg != "" || action.WarningMsg != "" {
		_, _ = fmt.Fprintln(ioStreams.ErrOut)
		confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Would you like to continue? (%s on %s)", action.Label, vol.Name))
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	// Collect user input before spinner.
	if action.Prepare != nil {
		if err := action.Prepare(ctx); err != nil {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Action: %s on volume:", action.Label), map[string]string{
		"volume_id": vol.ID,
		"name":      vol.Name,
		"status":    vol.Status,
	})

	actionCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(actionCtx, fmt.Sprintf("%s %s...", action.Label, vol.Name))
	}
	err = action.Execute(actionCtx, client, vol)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Done: %s on %s (%s)\n", action.Label, vol.Name, vol.ID)

	if waitOpts.Wait && action.Label != "Delete" {
		_, err := cmdutil.PollVolumeStatus(ctx, ioStreams.ErrOut, client, vol.ID, waitOpts)
		if err != nil {
			return err
		}
	}
	return nil
}

func buildVolumeActions(ctx context.Context, prompter tui.Prompter, client *verda.Client, vol *verda.Volume) []volumeAction {
	isAttached := vol.InstanceID != nil && *vol.InstanceID != ""
	var actions []volumeAction

	if isAttached {
		instanceID := *vol.InstanceID
		actions = append(actions, volumeAction{
			Label:      "Detach",
			ConfirmMsg: "Detaching the volume will make it unavailable to the instance.",
			Execute: func(ctx context.Context, c *verda.Client, v *verda.Volume) error {
				return c.Volumes.DetachVolume(ctx, v.ID, verda.VolumeDetachRequest{InstanceID: instanceID})
			},
		})
	}

	var newName string
	actions = append(actions, volumeAction{
		Label: "Rename",
		Prepare: func(ctx context.Context) error {
			n, err := prompter.TextInput(ctx, "New name", tui.WithDefault(vol.Name))
			if err != nil || strings.TrimSpace(n) == "" {
				return errors.New("canceled")
			}
			newName = strings.TrimSpace(n)
			return nil
		},
		Execute: func(ctx context.Context, c *verda.Client, v *verda.Volume) error {
			return c.Volumes.RenameVolume(ctx, v.ID, verda.VolumeRenameRequest{Name: newName})
		},
	})

	var newSize int
	actions = append(actions, volumeAction{
		Label: "Resize (grow only)",
		Prepare: func(ctx context.Context) error {
			sizeStr, err := prompter.TextInput(ctx, fmt.Sprintf("New size in GiB (current: %d)", vol.Size))
			if err != nil || strings.TrimSpace(sizeStr) == "" {
				return errors.New("canceled")
			}
			s, err := strconv.Atoi(strings.TrimSpace(sizeStr))
			if err != nil || s <= vol.Size {
				return fmt.Errorf("new size must be greater than %d", vol.Size)
			}
			newSize = s
			return nil
		},
		Execute: func(ctx context.Context, c *verda.Client, v *verda.Volume) error {
			return c.Volumes.ResizeVolume(ctx, v.ID, verda.VolumeResizeRequest{Size: newSize})
		},
	})

	var cloneName string
	actions = append(actions, volumeAction{
		Label: "Clone",
		Prepare: func(ctx context.Context) error {
			n, err := prompter.TextInput(ctx, "Clone name", tui.WithDefault(vol.Name+"-clone"))
			if err != nil || strings.TrimSpace(n) == "" {
				return errors.New("canceled")
			}
			cloneName = strings.TrimSpace(n)
			return nil
		},
		Execute: func(ctx context.Context, c *verda.Client, v *verda.Volume) error {
			_, cloneErr := c.Volumes.CloneVolume(ctx, v.ID, verda.VolumeCloneRequest{Name: cloneName})
			return cloneErr
		},
	}, volumeAction{
		Label:      "Delete",
		ConfirmMsg: "Deleted storage can be restored within 96 hours.",
		WarningMsg: "This action cannot be undone after the recovery period.",
		Execute: func(ctx context.Context, c *verda.Client, v *verda.Volume) error {
			return c.Volumes.DeleteVolume(ctx, v.ID, false)
		},
	})

	return actions
}

func selectVolume(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client) (string, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading volumes...")
	}
	volumes, err := client.Volumes.ListVolumes(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return "", err
	}

	if len(volumes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No volumes found.")
		return "", nil
	}

	labels := make([]string, 0, len(volumes)+1)
	for i := range volumes {
		status := volumes[i].Status
		if volumes[i].IsOSVolume {
			status = "OS"
		}
		labels = append(labels, fmt.Sprintf("%-25s  %-10s  %5dGB  %-6s  %s", volumes[i].Name, status, volumes[i].Size, volumes[i].Type, volumes[i].Location))
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select volume (type to filter)", labels)
	if err != nil {
		return "", nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
	}
	if idx == len(volumes) {
		return "", nil
	}
	return volumes[idx].ID, nil
}
