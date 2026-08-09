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

package testing

import (
	"context"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func TestConfirm(t *testing.T) {
	p := New().AddConfirm(true).AddConfirm(false)

	v, err := p.Confirm(context.Background(), "proceed?")
	if err != nil || v != true {
		t.Fatalf("expected true, got %v (err=%v)", v, err)
	}

	v, err = p.Confirm(context.Background(), "proceed?")
	if err != nil || v != false {
		t.Fatalf("expected false, got %v (err=%v)", v, err)
	}

	_, err = p.Confirm(context.Background(), "proceed?")
	if err == nil {
		t.Fatal("expected error when queue empty")
	}
}

func TestTextInput(t *testing.T) {
	p := New().AddTextInput("hello")

	v, err := p.TextInput(context.Background(), "name?")
	if err != nil || v != "hello" {
		t.Fatalf("expected hello, got %q (err=%v)", v, err)
	}
}

func TestSelect(t *testing.T) {
	p := New().AddSelect(2)

	v, err := p.Select(context.Background(), "pick one", []string{"a", "b", "c"})
	if err != nil || v != 2 {
		t.Fatalf("expected 2, got %d (err=%v)", v, err)
	}
}

func TestMultiSelect(t *testing.T) {
	p := New().AddMultiSelect([]int{0, 2})

	v, err := p.MultiSelect(context.Background(), "pick many", []string{"a", "b", "c"})
	if err != nil || len(v) != 2 || v[0] != 0 || v[1] != 2 {
		t.Fatalf("expected [0,2], got %v (err=%v)", v, err)
	}
}

func TestPassword(t *testing.T) {
	p := New().AddPassword("secret")

	v, err := p.Password(context.Background(), "password?")
	if err != nil || v != "secret" {
		t.Fatalf("expected secret, got %q (err=%v)", v, err)
	}
}

func TestEditor(t *testing.T) {
	p := New().AddEditor("line1\nline2")

	v, err := p.Editor(context.Background(), "edit:")
	if err != nil || v != "line1\nline2" {
		t.Fatalf("expected multiline, got %q (err=%v)", v, err)
	}
}

func TestSpinnerHandle(t *testing.T) {
	p := New()
	h, err := p.Spinner(context.Background(), "loading...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.UpdateMessage("still loading...")
	h.UpdateMessage("almost done...")
	h.Stop("done!")

	sh := h.(*SpinnerHandle)
	if len(sh.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sh.Messages))
	}
	if sh.Messages[0] != "still loading..." {
		t.Fatalf("expected 'still loading...', got %q", sh.Messages[0])
	}
	if sh.FinalMessage != "done!" {
		t.Fatalf("expected final 'done!', got %q", sh.FinalMessage)
	}
	if !sh.Stopped {
		t.Fatal("expected stopped")
	}
}

func TestProgressHandle(t *testing.T) {
	p := New()
	h, err := p.Progress(context.Background(), "downloading...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.SetPercent(0.5)
	ph := h.(*ProgressHandle)
	if ph.Percent != 0.5 {
		t.Fatalf("expected 0.5, got %f", ph.Percent)
	}

	h.Increment(0.3)
	if ph.Percent != 0.8 {
		t.Fatalf("expected 0.8, got %f", ph.Percent)
	}

	h.Stop("complete!")
	if ph.FinalMessage != "complete!" {
		t.Fatalf("expected 'complete!', got %q", ph.FinalMessage)
	}
	if !ph.Stopped {
		t.Fatal("expected stopped")
	}
}

func TestTable(t *testing.T) {
	p := New()
	err := p.Table(context.Background(),
		[]string{"Name", "Status"},
		[][]string{{"api", "running"}, {"web", "stopped"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Verify interface compliance.
func TestInterfaceCompliance(t *testing.T) {
	var _ tui.Prompter = (*Prompter)(nil)
	var _ tui.Status = (*Prompter)(nil)
}
