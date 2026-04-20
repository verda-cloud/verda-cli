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

package skills

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

func TestRunStatus_Installed(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "skills.json")
	_ = SaveState(statePath, &State{
		Version:     "1.0.0",
		InstalledAt: time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC),
		Agents:      []string{"claude-code", "cursor"},
	})

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{
		statePath: statePath,
	}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}

	output := out.String()
	if !bytes.Contains(out.Bytes(), []byte("1.0.0")) {
		t.Fatalf("expected installed version in output, got:\n%s", output)
	}
	if !bytes.Contains(out.Bytes(), []byte("Claude Code")) {
		t.Fatalf("expected Claude Code in output, got:\n%s", output)
	}
}

func TestRunStatus_NotInstalled(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "skills.json")

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{statePath: statePath}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("not installed")) {
		t.Fatalf("expected 'not installed' message, got:\n%s", out.String())
	}
}

func TestRunStatus_JSON(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "skills.json")
	_ = SaveState(statePath, &State{
		Version: "1.0.0",
		Agents:  []string{"claude-code"},
	})

	mock := tuitest.New()
	f := cmdutil.NewTestFactory(mock)
	f.OutputFormatOverride = "json"
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &statusOptions{statePath: statePath}

	if err := runStatus(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"version"`)) {
		t.Fatalf("expected JSON output, got:\n%s", out.String())
	}
}
