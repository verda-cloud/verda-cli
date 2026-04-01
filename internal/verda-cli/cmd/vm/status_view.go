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

// Spinner frames for the waiting animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// renderInstanceView renders a styled instance summary.
// When waiting is true, the header shows an animated spinner and elapsed time.
func renderInstanceView(inst *verda.Instance, spinnerFrame string, elapsed time.Duration, waiting bool) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold := lipgloss.NewStyle().Bold(true)
	statusStyle := lipgloss.NewStyle().Foreground(statusColor(inst.Status))

	// Header: hostname  instance-type  ● status ⠋ 15s
	var header string
	if waiting {
		header = fmt.Sprintf("%s  %s  %s %s %s",
			bold.Render(inst.Hostname),
			dim.Render(inst.InstanceType),
			statusStyle.Render("● "+inst.Status),
			statusStyle.Render(spinnerFrame),
			dim.Render(formatElapsed(elapsed)))
	} else {
		header = fmt.Sprintf("%s  %s  %s",
			bold.Render(inst.Hostname),
			dim.Render(inst.InstanceType),
			statusStyle.Render("● "+inst.Status))
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "\n%s\n\n", header)

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

	labelStyle := dim
	for _, l := range lines {
		_, _ = fmt.Fprintf(&b, "  %s  %s\n", labelStyle.Render(fmt.Sprintf("%-15s", l.label)), l.value)
	}

	// SSH hint
	if inst.Status == verda.StatusRunning && inst.IP != nil && *inst.IP != "" {
		sshStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)
		_, _ = fmt.Fprintf(&b, "\n  %s\n", sshStyle.Render(fmt.Sprintf("ssh root@%s", *inst.IP)))
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
	return fmt.Sprintf("%dm%ds", s/60, s%60)
}

// pollInstanceStatus polls the instance status until it reaches a terminal state.
// Shows an animated spinner and elapsed timer while waiting.
func pollInstanceStatus(ctx context.Context, w io.Writer, client *verda.Client, instanceID string) error {
	clearLines := func(n int) {
		for range n {
			_, _ = fmt.Fprintf(w, "\033[A\033[2K")
		}
	}

	var lastLineCount int
	var lastInst *verda.Instance
	startTime := time.Now()
	frame := 0

	// Ticker for spinner animation (100ms).
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Ticker for API polling.
	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Initial fetch.
	inst, err := client.Instances.GetByID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("polling instance status: %w", err)
	}
	lastInst = inst

	for {
		// Render current state.
		waiting := !terminalStatuses[lastInst.Status]
		output := renderInstanceView(lastInst, spinnerFrames[frame%len(spinnerFrames)], time.Since(startTime), waiting)
		lineCount := strings.Count(output, "\n")

		if lastLineCount > 0 {
			clearLines(lastLineCount)
		}
		_, _ = fmt.Fprint(w, output)
		lastLineCount = lineCount

		if !waiting {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			frame++
		case <-pollTicker.C:
			frame++
			inst, err := client.Instances.GetByID(ctx, instanceID)
			if err != nil {
				return fmt.Errorf("polling instance status: %w", err)
			}
			lastInst = inst
		}
	}
}
