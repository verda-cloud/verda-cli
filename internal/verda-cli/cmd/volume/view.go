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
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func renderVolumeSummary(w interface{ Write([]byte) (int, error) }, vol *verda.Volume) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	status := vol.Status
	if vol.IsOSVolume {
		status = "Main OS"
	}

	_, _ = fmt.Fprintf(w, "\n%s  %s\n\n", bold.Render(vol.Name), dim.Render(status))
	_, _ = fmt.Fprintf(w, "  %s  %s\n", dim.Render("ID:      "), vol.ID)
	_, _ = fmt.Fprintf(w, "  %s  %dGB\n", dim.Render("Size:    "), vol.Size)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", dim.Render("Type:    "), vol.Type)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", dim.Render("Location:"), vol.Location)
	if vol.InstanceID != nil && *vol.InstanceID != "" {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", dim.Render("Instance:"), *vol.InstanceID)
	}
	_, _ = fmt.Fprintln(w)
}
