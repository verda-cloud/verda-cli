package volume

import (
	"bytes"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func strPtr(s string) *string { return &s }

func TestRenderVolumeSummary(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:       "vol-abc",
		Name:     "data-volume",
		Size:     500,
		Type:     "NVMe",
		Status:   "attached",
		Location: "FIN-01",
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("renderVolumeSummary produced empty output")
	}
}

func TestRenderVolumeSummaryWithInstance(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:         "vol-def",
		Name:       "os-vol",
		Size:       100,
		Type:       "NVMe",
		Status:     "attached",
		Location:   "FIN-01",
		IsOSVolume: true,
		InstanceID: strPtr("inst-123"),
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("renderVolumeSummary produced empty output")
	}
}

func TestRenderVolumeSummaryDetached(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:       "vol-ghi",
		Name:     "spare-vol",
		Size:     200,
		Type:     "HDD",
		Status:   "detached",
		Location: "FIN-03",
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	if buf.Len() == 0 {
		t.Fatal("renderVolumeSummary produced empty output for detached volume")
	}
}
