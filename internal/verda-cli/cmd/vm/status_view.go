package vm

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const pollInterval = 5 * time.Second

// terminalStatuses are statuses where we stop polling.
var terminalStatuses = map[string]bool{
	verda.StatusRunning:      true,
	verda.StatusOffline:      true,
	verda.StatusError:        true,
	verda.StatusDiscontinued: true,
	verda.StatusNotFound:     true,
	verda.StatusNoCapacity:   true,
}

// statusColor returns a lipgloss color for the instance status.
func statusColor(status string) lipgloss.Color {
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

// renderInstanceView renders a styled instance summary to the writer.
func renderInstanceView(w io.Writer, inst *verda.Instance) {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold := lipgloss.NewStyle().Bold(true)
	statusStyle := lipgloss.NewStyle().Foreground(statusColor(inst.Status))

	// Header: hostname  instance-type  ● Status
	header := fmt.Sprintf("%s  %s  %s",
		bold.Render(inst.Hostname),
		dim.Render(inst.InstanceType),
		statusStyle.Render("● "+inst.Status))

	// Fields
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

	// Render
	_, _ = fmt.Fprintf(w, "\n%s\n\n", header)
	labelStyle := dim
	for _, l := range lines {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", labelStyle.Render(fmt.Sprintf("%-15s", l.label)), l.value)
	}

	// SSH hint
	if inst.Status == verda.StatusRunning && inst.IP != nil && *inst.IP != "" {
		sshStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)
		_, _ = fmt.Fprintf(w, "\n  %s\n", sshStyle.Render(fmt.Sprintf("ssh root@%s", *inst.IP)))
	}

	_, _ = fmt.Fprintln(w)
}

// pollInstanceStatus polls the instance status until it reaches a terminal state.
// It renders the status view in-place, updating on each poll.
func pollInstanceStatus(ctx context.Context, w io.Writer, client *verda.Client, instanceID string) error {
	// Clear line sequence for re-rendering.
	clearScreen := func(lineCount int) {
		for range lineCount {
			_, _ = fmt.Fprintf(w, "\033[A\033[2K") // move up + clear line
		}
	}

	var lastLineCount int

	for {
		inst, err := client.Instances.GetByID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("polling instance status: %w", err)
		}

		// Clear previous render.
		if lastLineCount > 0 {
			clearScreen(lastLineCount)
		}

		// Render and count lines.
		var buf strings.Builder
		renderInstanceView(&buf, inst)
		output := buf.String()
		lastLineCount = strings.Count(output, "\n")

		_, _ = fmt.Fprint(w, output)

		if terminalStatuses[inst.Status] {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
