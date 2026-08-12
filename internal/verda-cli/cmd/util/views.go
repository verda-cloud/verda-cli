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

package util

import (
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// View types own the JSON/YAML contract for resources whose API payloads have
// optional fields. Marshaling an SDK struct directly turns "the API sent
// nothing" into Go's zero value, and a zero time.Time is not harmless: it
// serializes as 0001-01-01T00:00:00Z, which an age-based reaper
// ("older than 2h ⇒ delete") reads as ancient and acts on. Absent is safe,
// wrong is dangerous — so absent stays absent.
//
// Keys mirror the SDK's own json tags exactly ("key", not "public_key"). yaml
// tags are mandatory: the encoder is go.yaml.in/yaml/v3 (output.go), which
// ignores json tags and would otherwise emit "createdat".

// nilIfZero maps the zero time to nil so an omitempty tag can drop the field.
// A pointer to a zero value would still marshal.
func nilIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// SSHKeyView is the JSON/YAML shape for one SSH key.
type SSHKeyView struct {
	ID          string     `json:"id"                     yaml:"id"`
	Name        string     `json:"name"                   yaml:"name"`
	PublicKey   string     `json:"key"                    yaml:"key"`
	Fingerprint string     `json:"fingerprint,omitempty"  yaml:"fingerprint,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"   yaml:"created_at,omitempty"`
}

// NewSSHKeyView converts one SDK SSH key to its output shape.
func NewSSHKeyView(k *verda.SSHKey) SSHKeyView {
	return SSHKeyView{
		ID:          k.ID,
		Name:        k.Name,
		PublicKey:   k.PublicKey,
		Fingerprint: k.Fingerprint,
		CreatedAt:   nilIfZero(k.CreatedAt),
	}
}

// NewSSHKeyViews converts a slice of SDK SSH keys, preserving order.
func NewSSHKeyViews(keys []verda.SSHKey) []SSHKeyView {
	views := make([]SSHKeyView, len(keys))
	for i := range keys {
		views[i] = NewSSHKeyView(&keys[i])
	}
	return views
}

// StartupScriptView is the JSON/YAML shape for one startup script.
type StartupScriptView struct {
	ID        string     `json:"id"                    yaml:"id"`
	Name      string     `json:"name"                  yaml:"name"`
	Script    string     `json:"script"                yaml:"script"`
	CreatedAt *time.Time `json:"created_at,omitempty"  yaml:"created_at,omitempty"`
}

// NewStartupScriptView converts one SDK startup script to its output shape.
func NewStartupScriptView(s *verda.StartupScript) StartupScriptView {
	return StartupScriptView{
		ID:        s.ID,
		Name:      s.Name,
		Script:    s.Script,
		CreatedAt: nilIfZero(s.CreatedAt),
	}
}

// NewStartupScriptViews converts a slice of SDK startup scripts, preserving order.
func NewStartupScriptViews(scripts []verda.StartupScript) []StartupScriptView {
	views := make([]StartupScriptView, len(scripts))
	for i := range scripts {
		views[i] = NewStartupScriptView(&scripts[i])
	}
	return views
}

// InstanceView is the JSON/YAML shape for one instance. Every field of
// verda.Instance is mirrored with its own json key — TestInstanceViewCoversSDKFields
// fails if the SDK grows a field this view would silently drop.
//
// Nested types (CPU, GPU, …) are reused from the SDK: they carry no zero-time
// field, so there is nothing to omit, and re-declaring them would be four more
// structs to keep in sync.
type InstanceView struct {
	ID              string                `json:"id"                   yaml:"id"`
	IP              *string               `json:"ip"                   yaml:"ip"`
	Status          string                `json:"status"               yaml:"status"`
	CreatedAt       *time.Time            `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	CPU             verda.InstanceCPU     `json:"cpu"                  yaml:"cpu"`
	GPU             verda.InstanceGPU     `json:"gpu"                  yaml:"gpu"`
	GPUMemory       verda.InstanceMemory  `json:"gpu_memory"           yaml:"gpu_memory"`
	Memory          verda.InstanceMemory  `json:"memory"               yaml:"memory"`
	Storage         verda.InstanceStorage `json:"storage"              yaml:"storage"`
	Hostname        string                `json:"hostname"             yaml:"hostname"`
	Description     string                `json:"description"          yaml:"description"`
	Location        string                `json:"location"             yaml:"location"`
	PricePerHour    verda.FlexibleFloat   `json:"price_per_hour"       yaml:"price_per_hour"`
	IsSpot          bool                  `json:"is_spot"              yaml:"is_spot"`
	InstanceType    string                `json:"instance_type"        yaml:"instance_type"`
	Image           string                `json:"image"                yaml:"image"`
	OSName          string                `json:"os_name"              yaml:"os_name"`
	StartupScriptID *string               `json:"startup_script_id"    yaml:"startup_script_id"`
	SSHKeyIDs       []string              `json:"ssh_key_ids"          yaml:"ssh_key_ids"`
	OSVolumeID      *string               `json:"os_volume_id"         yaml:"os_volume_id"`
	JupyterToken    string                `json:"jupyter_token"        yaml:"jupyter_token"`
	Contract        string                `json:"contract"             yaml:"contract"`
	Pricing         string                `json:"pricing"              yaml:"pricing"`
	VolumeIDs       []string              `json:"volume_ids"           yaml:"volume_ids"`
}

// NewInstanceView converts one SDK instance to its output shape.
func NewInstanceView(i *verda.Instance) InstanceView {
	return InstanceView{
		ID:              i.ID,
		IP:              i.IP,
		Status:          i.Status,
		CreatedAt:       nilIfZero(i.CreatedAt),
		CPU:             i.CPU,
		GPU:             i.GPU,
		GPUMemory:       i.GPUMemory,
		Memory:          i.Memory,
		Storage:         i.Storage,
		Hostname:        i.Hostname,
		Description:     i.Description,
		Location:        i.Location,
		PricePerHour:    i.PricePerHour,
		IsSpot:          i.IsSpot,
		InstanceType:    i.InstanceType,
		Image:           i.Image,
		OSName:          i.OSName,
		StartupScriptID: i.StartupScriptID,
		SSHKeyIDs:       i.SSHKeyIDs,
		OSVolumeID:      i.OSVolumeID,
		JupyterToken:    i.JupyterToken,
		Contract:        i.Contract,
		Pricing:         i.Pricing,
		VolumeIDs:       i.VolumeIDs,
	}
}

// NewInstanceViews converts a slice of SDK instances, preserving order.
func NewInstanceViews(instances []verda.Instance) []InstanceView {
	views := make([]InstanceView, len(instances))
	for i := range instances {
		views[i] = NewInstanceView(&instances[i])
	}
	return views
}

// JobDeploymentShortView is the JSON/YAML shape for one batch-job deployment
// summary. Serverless is a hidden feature; the view exists so the surface does
// not carry the same zero-timestamp defect when it ships.
type JobDeploymentShortView struct {
	Name      string                  `json:"name"                 yaml:"name"`
	CreatedAt *time.Time              `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	Compute   *verda.ContainerCompute `json:"compute"              yaml:"compute"`
}

// NewJobDeploymentShortView converts one SDK job deployment summary.
func NewJobDeploymentShortView(j *verda.JobDeploymentShortInfo) JobDeploymentShortView {
	return JobDeploymentShortView{
		Name:      j.Name,
		CreatedAt: nilIfZero(j.CreatedAt),
		Compute:   j.Compute,
	}
}

// NewJobDeploymentShortViews converts a slice, preserving order.
func NewJobDeploymentShortViews(jobs []verda.JobDeploymentShortInfo) []JobDeploymentShortView {
	views := make([]JobDeploymentShortView, len(jobs))
	for i := range jobs {
		views[i] = NewJobDeploymentShortView(&jobs[i])
	}
	return views
}

// TimeColumn renders a timestamp for table output. An absent value prints as
// "-" rather than 0001-01-01, so a human reading the table sees "unknown"
// instead of a plausible-looking date.
func TimeColumn(t *time.Time, layout string) string {
	if t == nil {
		return "-"
	}
	return t.Format(layout)
}

// TextColumn renders a possibly-empty string for table output, so an absent
// value is visible as "-" instead of a blank the eye slides over.
func TextColumn(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
