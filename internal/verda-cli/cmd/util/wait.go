package util

import (
	"context"
	"fmt"
	"io"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/pflag"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// WaitOptions holds flags for polling async operations.
type WaitOptions struct {
	Wait    bool
	Timeout time.Duration
}

// AddFlags registers --wait and --wait-timeout on the given flag set.
func (w *WaitOptions) AddFlags(fs *pflag.FlagSet, waitDefault bool) {
	fs.BoolVar(&w.Wait, "wait", waitDefault, "Wait for the operation to complete")
	fs.DurationVar(&w.Timeout, "wait-timeout", 5*time.Minute, "Maximum time to wait for the operation")
}

// PollFunc is called periodically. It returns the current status string and
// whether polling is done.
type PollFunc func(ctx context.Context) (status string, done bool, err error)

// Poll polls until done or timeout, showing an animated status line on w.
// If w is nil, polling runs silently.
func Poll(ctx context.Context, w io.Writer, interval time.Duration, opts WaitOptions, pollFn PollFunc) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	startTime := time.Now()
	frame := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	pollTicker := time.NewTicker(interval)
	defer pollTicker.Stop()

	// Initial check.
	status, done, err := pollFn(ctx)
	if err != nil {
		return status, err
	}
	if done {
		return status, nil
	}

	lastStatus := status

	for {
		if w != nil {
			spinner := spinnerFrames[frame%len(spinnerFrames)]
			elapsed := formatPollElapsed(time.Since(startTime))
			line := fmt.Sprintf("\r\033[2K%s Waiting (%s) %s",
				accentStyle.Render(spinner),
				lastStatus,
				dimStyle.Render(elapsed))
			_, _ = fmt.Fprint(w, line)
		}

		select {
		case <-ctx.Done():
			if w != nil {
				_, _ = fmt.Fprintf(w, "\r\033[2K")
			}
			return lastStatus, fmt.Errorf("timed out waiting (last status: %s)", lastStatus)
		case <-ticker.C:
			frame++
		case <-pollTicker.C:
			frame++
			status, done, err := pollFn(ctx)
			if err != nil {
				if w != nil {
					_, _ = fmt.Fprintf(w, "\r\033[2K")
				}
				return status, err
			}
			lastStatus = status
			if done {
				if w != nil {
					_, _ = fmt.Fprintf(w, "\r\033[2K")
				}
				return status, nil
			}
		}
	}
}

// PollInstanceStatus polls an instance until it reaches one of the expected
// statuses (or a terminal status).
func PollInstanceStatus(ctx context.Context, w io.Writer, client *verda.Client, instanceID string, opts WaitOptions, expectStatus ...string) (*verda.Instance, error) {
	target := ""
	if len(expectStatus) > 0 {
		target = expectStatus[0]
	}

	var lastInst *verda.Instance
	pollFn := func(ctx context.Context) (string, bool, error) {
		inst, err := client.Instances.GetByID(ctx, instanceID)
		if err != nil {
			return "", false, fmt.Errorf("polling instance: %w", err)
		}
		lastInst = inst
		msg := InstanceStatusMessage(inst.Status)
		if target != "" {
			return msg, inst.Status == target || inst.Status == verda.StatusError || inst.Status == verda.StatusNotFound, nil
		}
		return msg, InstanceTerminalStatuses[inst.Status], nil
	}

	_, err := Poll(ctx, w, 5*time.Second, opts, pollFn)
	return lastInst, err
}

// PollVolumeStatus polls a volume until it reaches one of the expected statuses.
func PollVolumeStatus(ctx context.Context, w io.Writer, client *verda.Client, volumeID string, opts WaitOptions, expectStatus ...string) (*verda.Volume, error) {
	target := ""
	if len(expectStatus) > 0 {
		target = expectStatus[0]
	}

	var lastVol *verda.Volume
	pollFn := func(ctx context.Context) (string, bool, error) {
		vol, err := client.Volumes.GetVolume(ctx, volumeID)
		if err != nil {
			return "", false, fmt.Errorf("polling volume: %w", err)
		}
		lastVol = vol
		if target != "" {
			return vol.Status, vol.Status == target, nil
		}
		return vol.Status, VolumeTerminalStatuses[vol.Status], nil
	}

	_, err := Poll(ctx, w, 5*time.Second, opts, pollFn)
	return lastVol, err
}

func formatPollElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}
