package vm

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const (
	pollInterval = 5 * time.Second
	pollTimeout  = 5 * time.Minute
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var terminalStatuses = map[string]bool{
	verda.StatusRunning:      true,
	verda.StatusOffline:      true,
	verda.StatusError:        true,
	verda.StatusDiscontinued: true,
	verda.StatusNotFound:     true,
	verda.StatusNoCapacity:   true,
}

// statusMessage returns a human-friendly message for the instance status.
func statusMessage(status string) string {
	switch status {
	case verda.StatusNew:
		return "Creating instance..."
	case verda.StatusOrdered:
		return "Instance ordered..."
	case verda.StatusProvisioning:
		return "Provisioning instance..."
	case verda.StatusValidating:
		return "Validating instance..."
	case verda.StatusPending:
		return "Waiting for capacity..."
	default:
		return "Waiting..."
	}
}

// statusColor returns a lipgloss color for the instance status.
func statusColor(status string) color.Color {
	switch status {
	case verda.StatusRunning:
		return lipgloss.Color("2") // green
	case verda.StatusProvisioning, verda.StatusOrdered, verda.StatusNew, verda.StatusValidating, verda.StatusPending:
		return lipgloss.Color("3") // yellow
	case verda.StatusError, verda.StatusNoCapacity:
		return lipgloss.Color("1") // red
	case verda.StatusOffline, verda.StatusDiscontinued, verda.StatusDeleting:
		return lipgloss.Color("8") // dim
	default:
		return lipgloss.Color("7") // white
	}
}

// renderInstanceCard renders the final styled instance summary.
// volumes is optional — if provided, they are displayed as a table.
func renderInstanceCard(inst *verda.Instance, volumes ...verda.Volume) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold := lipgloss.NewStyle().Bold(true)
	statusStyle := lipgloss.NewStyle().Foreground(statusColor(inst.Status))

	header := fmt.Sprintf("%s  %s  %s",
		bold.Render(inst.Hostname),
		dim.Render(inst.InstanceType),
		statusStyle.Render("● "+inst.Status))

	lines := []struct{ label, value string }{
		{"Instance ID", inst.ID},
		{"Location", inst.Location},
		{"Instance Type", inst.InstanceType},
		{"Image", inst.OSName},
		{"Contract", inst.Contract},
		{"Pricing", inst.Pricing},
		{"Price", fmt.Sprintf("$%.3f/hr", float64(inst.PricePerHour))},
	}

	if inst.GPU.NumberOfGPUs > 0 {
		lines = append(lines, struct{ label, value string }{
			"Compute", fmt.Sprintf("%dx %s, %dGB VRAM, %dGB RAM",
				inst.GPU.NumberOfGPUs, inst.GPU.Description,
				inst.GPUMemory.SizeInGigabytes, inst.Memory.SizeInGigabytes),
		})
	} else {
		lines = append(lines, struct{ label, value string }{
			"Compute", fmt.Sprintf("%d CPU, %dGB RAM",
				inst.CPU.NumberOfCores, inst.Memory.SizeInGigabytes),
		})
	}

	if inst.IP != nil && *inst.IP != "" {
		lines = append(lines, struct{ label, value string }{"IP Address", *inst.IP})
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "\n%s\n\n", header)
	for _, l := range lines {
		_, _ = fmt.Fprintf(&b, "  %s  %s\n", dim.Render(fmt.Sprintf("%-15s", l.label)), l.value)
	}

	// Storage section.
	if len(volumes) > 0 {
		_, _ = fmt.Fprintf(&b, "\n  %s\n", bold.Render("Storage"))
		for i := range volumes {
			v := &volumes[i]
			volStatus := "Attached"
			if v.IsOSVolume {
				volStatus = "Main OS"
			}
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Name:    "), v.Name)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("ID:      "), v.ID)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Status:  "), volStatus)
			_, _ = fmt.Fprintf(&b, "    %s  %dGB\n", dim.Render("Size:    "), v.Size)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Type:    "), v.Type)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Location:"), v.Location)
			if i < len(volumes)-1 {
				_, _ = fmt.Fprintln(&b)
			}
		}
	}

	if inst.Status == verda.StatusRunning && inst.IP != nil && *inst.IP != "" {
		sshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
		_, _ = fmt.Fprintf(&b, "\n  %s\n", sshStyle.Render("ssh root@"+*inst.IP))
	}

	_, _ = fmt.Fprintln(&b)
	return b.String()
}

// formatElapsed formats a duration as a human-friendly string.
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}

// pollInstanceStatus polls the instance showing an animated status line.
// If expectStatus is non-empty, polls until that specific status is reached.
// If empty, polls until any terminal status (running, offline, error, etc.).
func pollInstanceStatus(ctx context.Context, w io.Writer, client *verda.Client, instanceID string, expectStatus ...string) error {
	var target string
	if len(expectStatus) > 0 {
		target = expectStatus[0]
	}

	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var lastInst *verda.Instance
	startTime := time.Now()
	frame := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Initial fetch.
	inst, err := client.Instances.GetByID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("polling instance status: %w", err)
	}
	lastInst = inst

	isDone := func(status string) bool {
		if target != "" {
			return status == target || status == verda.StatusError || status == verda.StatusNotFound
		}
		return terminalStatuses[status]
	}

	for {
		if isDone(lastInst.Status) {
			_, _ = fmt.Fprintf(w, "\r\033[2K")
			_, _ = fmt.Fprint(w, renderInstanceCard(lastInst))
			return nil
		}

		// Render animated single-line status.
		spinner := spinnerFrames[frame%len(spinnerFrames)]
		elapsed := formatElapsed(time.Since(startTime))
		msg := statusMessage(lastInst.Status)
		line := fmt.Sprintf("\r\033[2K%s %s %s",
			accentStyle.Render(spinner),
			msg,
			dimStyle.Render(elapsed))
		_, _ = fmt.Fprint(w, line)

		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintf(w, "\r\033[2K")
			_, _ = fmt.Fprintf(w, "\nTimed out waiting for status change (last: %s). Use `verda vm list` to check.\n", lastInst.Status)
			return nil
		case <-ticker.C:
			frame++
		case <-pollTicker.C:
			frame++
			inst, err := client.Instances.GetByID(ctx, instanceID)
			if err != nil {
				_, _ = fmt.Fprintln(w)
				return fmt.Errorf("polling instance status: %w", err)
			}
			lastInst = inst
		}
	}
}
