package vm

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// formatImageLabel returns a display string for the image field.
// Prefers the image slug; appends the OS name if it differs and is non-empty.
func formatImageLabel(slug, osName string) string {
	if slug == "" {
		return osName
	}
	if osName == "" || osName == slug {
		return slug
	}
	return slug + " (" + osName + ")"
}

// cleanGPUDescription strips the leading "Nx " count prefix from the API GPU
// description when it matches the GPU count, avoiding duplication like "1x 1x H100".
func cleanGPUDescription(gpuCount int, desc string) string {
	prefix := fmt.Sprintf("%dx ", gpuCount)
	return strings.TrimPrefix(desc, prefix)
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
		{"Image", formatImageLabel(inst.Image, inst.OSName)},
		{"Contract", inst.Contract},
		{"Price", fmt.Sprintf("$%.3f/hr", float64(inst.PricePerHour))},
	}

	if inst.GPU.NumberOfGPUs > 0 {
		gpuDesc := cleanGPUDescription(inst.GPU.NumberOfGPUs, inst.GPU.Description)
		lines = append(lines, struct{ label, value string }{
			"Compute", fmt.Sprintf("%dx %s, %dGB VRAM, %dGB RAM",
				inst.GPU.NumberOfGPUs, gpuDesc,
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
			volLabel := v.Status
			if v.IsOSVolume {
				volLabel = v.Status + " (OS)"
			}
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Name:    "), v.Name)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("ID:      "), v.ID)
			_, _ = fmt.Fprintf(&b, "    %s  %s\n", dim.Render("Status:  "), volLabel)
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
