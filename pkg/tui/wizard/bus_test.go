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
	"testing"
)

type testView struct {
	received  []any
	subs      []reflect.Type
	output    string
	publishes []any
}

func (r *testView) Update(msg any) (string, []any) {
	r.received = append(r.received, msg)
	return r.output, r.publishes
}

func (r *testView) Subscribe() []reflect.Type {
	return r.subs
}

func TestBus_BroadcastReachesAllViews(t *testing.T) {
	r1 := &testView{output: "r1"}
	r2 := &testView{output: "r2"}

	bus := NewMessageBus()
	bus.Register("r1", r1)
	bus.Register("r2", r2)

	bus.Broadcast(StepChangedMsg{Current: 1, Total: 5})

	if len(r1.received) != 1 {
		t.Errorf("r1 should receive 1 message, got %d", len(r1.received))
	}
	if len(r2.received) != 1 {
		t.Errorf("r2 should receive 1 message, got %d", len(r2.received))
	}
}

func TestBus_PublishRoutesToSubscribers(t *testing.T) {
	type CustomMsg struct{ Value int }

	r1 := &testView{output: "r1", subs: []reflect.Type{reflect.TypeFor[CustomMsg]()}}
	r2 := &testView{output: "r2"}

	bus := NewMessageBus()
	bus.Register("r1", r1)
	bus.Register("r2", r2)

	bus.Publish("sender", []any{CustomMsg{Value: 42}})

	if len(r1.received) != 1 {
		t.Errorf("r1 should receive CustomMsg, got %d messages", len(r1.received))
	}
	if len(r2.received) != 0 {
		t.Errorf("r2 should NOT receive CustomMsg, got %d messages", len(r2.received))
	}
}

func TestBus_PublishTriggersChain(t *testing.T) {
	type MsgA struct{}
	type MsgB struct{}

	r1 := &testView{
		output:    "r1",
		subs:      []reflect.Type{reflect.TypeFor[MsgA]()},
		publishes: []any{MsgB{}},
	}
	r2 := &testView{
		output: "r2",
		subs:   []reflect.Type{reflect.TypeFor[MsgB]()},
	}

	bus := NewMessageBus()
	bus.Register("r1", r1)
	bus.Register("r2", r2)

	bus.Publish("external", []any{MsgA{}})

	if len(r1.received) != 1 {
		t.Errorf("r1 should receive 1 message, got %d", len(r1.received))
	}
	if len(r2.received) != 1 {
		t.Errorf("r2 should receive chained MsgB, got %d messages", len(r2.received))
	}
}

func TestBus_RenderAll(t *testing.T) {
	r1 := &testView{output: "line1"}
	r2 := &testView{output: "line2"}

	bus := NewMessageBus()
	bus.Register("r1", r1)
	bus.Register("r2", r2)

	// Broadcast to trigger Update and populate last rendered output
	bus.Broadcast(StepChangedMsg{Current: 1, Total: 2})

	renders := bus.RenderAll()
	if len(renders) != 2 || renders[0] != "line1" || renders[1] != "line2" {
		t.Errorf("expected [line1 line2], got %v", renders)
	}
}

func TestBus_ViewToViewPubSub(t *testing.T) {
	type DataReadyMsg struct{ Items int }

	loader := &testView{
		output:    "",
		publishes: []any{DataReadyMsg{Items: 42}},
	}
	summary := &testView{
		output: "waiting",
	}

	bus := NewMessageBus()
	bus.Register("loader", loader)
	bus.Register("summary", summary)

	// Engine broadcasts StepChanged -> loader receives -> publishes DataReadyMsg
	// But summary has no subs for DataReadyMsg so it only gets the broadcast
	bus.Broadcast(StepChangedMsg{Current: 1, Total: 3})

	if len(loader.received) != 1 {
		t.Errorf("loader should receive 1 message, got %d", len(loader.received))
	}
	// summary receives the broadcast only (not the chained DataReadyMsg since it didn't subscribe)
	if len(summary.received) != 1 {
		t.Errorf("summary should receive 1 broadcast, got %d", len(summary.received))
	}

	// Now test with subscription
	subscribedSummary := &testView{
		output: "updated",
		subs:   []reflect.Type{reflect.TypeFor[DataReadyMsg]()},
	}
	bus2 := NewMessageBus()
	bus2.Register("loader", loader)
	bus2.Register("summary", subscribedSummary)

	// Reset loader received
	loader.received = nil
	bus2.Broadcast(StepChangedMsg{Current: 1, Total: 3})

	// subscribedSummary should get broadcast + chained DataReadyMsg
	if len(subscribedSummary.received) != 2 {
		t.Errorf("subscribed summary should receive 2 messages (broadcast + chained), got %d", len(subscribedSummary.received))
	}
}
