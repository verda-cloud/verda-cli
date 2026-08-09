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

package wizard

import (
	"reflect"
	"strings"
	"testing"
)

func TestStepChangedMsg_Fields(t *testing.T) {
	msg := StepChangedMsg{
		Current:   3,
		Total:     12,
		StepName:  "instance-type",
		Collected: map[string]any{"region": "FIN-01"},
	}
	if msg.Current != 3 || msg.Total != 12 {
		t.Error("StepChangedMsg fields not set correctly")
	}
	if msg.StepName != "instance-type" {
		t.Errorf("expected StepName 'instance-type', got %q", msg.StepName)
	}
}

func TestViewDef_HasID(t *testing.T) {
	def := ViewDef{
		ID: "progress",
	}
	if def.ID != "progress" {
		t.Errorf("expected ID 'progress', got %q", def.ID)
	}
}

func TestSubscribeFilter(t *testing.T) {
	subs := []reflect.Type{reflect.TypeFor[StepChangedMsg]()}
	msgType := reflect.TypeFor[StepChangedMsg]()

	found := false
	for _, s := range subs {
		if s == msgType {
			found = true
		}
	}
	if !found {
		t.Error("StepChangedMsg should match subscription")
	}
}

func TestProgressView_Render(t *testing.T) {
	r := NewProgressView()

	out, pub := r.Update(StepChangedMsg{Current: 2, Total: 5, StepName: "gpu"})

	if pub != nil {
		t.Error("progress view should not publish messages")
	}
	if !strings.Contains(out, "Step 2 of 5") {
		t.Errorf("expected 'Step 2 of 5' in output, got: %s", out)
	}
}

func TestProgressView_SingleStepHidden(t *testing.T) {
	r := NewProgressView()

	out, _ := r.Update(StepChangedMsg{Current: 1, Total: 1, StepName: "only"})

	if out != "" {
		t.Errorf("single-step should produce empty output, got: %s", out)
	}
}

func TestProgressView_CustomGradient(t *testing.T) {
	r := NewProgressView(
		WithProgressGradient("#bd93f9", "#ff79c6"),
		WithProgressWidth(20),
	)

	out, _ := r.Update(StepChangedMsg{Current: 3, Total: 6, StepName: "gpu"})

	if !strings.Contains(out, "Step 3 of 6") {
		t.Errorf("expected 'Step 3 of 6' in output, got: %s", out)
	}
}

func TestProgressView_SolidFill(t *testing.T) {
	r := NewProgressView(WithProgressSolidFill("#50fa7b"))

	out, _ := r.Update(StepChangedMsg{Current: 1, Total: 3, StepName: "a"})

	if !strings.Contains(out, "Step 1 of 3") {
		t.Errorf("expected 'Step 1 of 3' in output, got: %s", out)
	}
}

func TestProgressView_PercentMode(t *testing.T) {
	r := NewProgressView(WithProgressPercent())

	out, _ := r.Update(StepChangedMsg{Current: 2, Total: 5, StepName: "gpu"})

	if !strings.Contains(out, "40%") {
		t.Errorf("expected '40%%' in output, got: %s", out)
	}
	if strings.Contains(out, "Step") {
		t.Errorf("percent mode should not show step label, got: %s", out)
	}
}

func TestProgressView_NoLabel(t *testing.T) {
	r := NewProgressView(WithoutProgressStepLabel())

	out, _ := r.Update(StepChangedMsg{Current: 2, Total: 5, StepName: "gpu"})

	if strings.Contains(out, "Step") {
		t.Errorf("should not show step label, got: %s", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("should not show percentage, got: %s", out)
	}
}

func TestProgressView_IgnoresOtherMessages(t *testing.T) {
	r := NewProgressView()

	out, _ := r.Update(CollectedChangedMsg{Key: "x", Value: "y"})

	if out != "" {
		t.Errorf("should ignore non-StepChanged messages, got: %s", out)
	}
}

func TestCustomView_ReactsToCollectedChange(t *testing.T) {
	cost := &costView{}

	bus := NewMessageBus()
	bus.Register("cost", cost)

	bus.Broadcast(CollectedChangedMsg{
		Key:   "instance-type",
		Value: "1H100.80S",
		Collected: map[string]any{
			"instance-type": "1H100.80S",
		},
	})

	renders := bus.RenderAll()
	if !strings.Contains(renders[0], "$3.20/hr") {
		t.Errorf("expected cost display, got: %s", renders[0])
	}
}

type costView struct {
	last string
}

func (r *costView) Update(msg any) (string, []any) {
	if m, ok := msg.(CollectedChangedMsg); ok {
		if m.Collected["instance-type"] == "1H100.80S" {
			r.last = "  Estimated cost: $3.20/hr"
		}
	}
	return r.last, nil
}

func (r *costView) Subscribe() []reflect.Type {
	return nil
}
