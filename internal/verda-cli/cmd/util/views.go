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
