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

import "reflect"

type viewSlot struct {
	id      string
	view    View
	subs    map[reflect.Type]bool
	last    string // current render output
	printed string // last output sent to terminal
}

// MessageBus routes messages between the engine and views.
type MessageBus struct {
	slots []viewSlot
}

// NewMessageBus creates an empty message bus.
func NewMessageBus() *MessageBus {
	return &MessageBus{}
}

// Register adds a view to the bus.
func (b *MessageBus) Register(id string, v View) {
	subs := make(map[reflect.Type]bool)
	for _, t := range v.Subscribe() {
		subs[t] = true
	}
	b.slots = append(b.slots, viewSlot{
		id:   id,
		view: v,
		subs: subs,
	})
}

// Broadcast sends a message to ALL views (engine-level events).
// Processes any published messages from views (chained delivery).
func (b *MessageBus) Broadcast(msg any) {
	var pending []any
	for i := range b.slots {
		render, published := b.slots[i].view.Update(msg)
		b.slots[i].last = render
		pending = append(pending, published...)
	}
	b.deliverPending(pending)
}

// Publish sends messages only to views that subscribed to those types.
// Processes any published messages from views (chained delivery).
func (b *MessageBus) Publish(from string, msgs []any) {
	var pending []any
	for _, msg := range msgs {
		msgType := reflect.TypeOf(msg)
		for i := range b.slots {
			if b.slots[i].id == from {
				continue
			}
			if b.slots[i].subs[msgType] {
				render, published := b.slots[i].view.Update(msg)
				b.slots[i].last = render
				pending = append(pending, published...)
			}
		}
	}
	b.deliverPending(pending)
}

// deliverPending processes chained messages (published by views during Update).
func (b *MessageBus) deliverPending(msgs []any) {
	for len(msgs) > 0 {
		var next []any
		for _, msg := range msgs {
			msgType := reflect.TypeOf(msg)
			for i := range b.slots {
				if b.slots[i].subs[msgType] {
					render, published := b.slots[i].view.Update(msg)
					b.slots[i].last = render
					next = append(next, published...)
				}
			}
		}
		msgs = next
	}
}

// RenderAll returns the last rendered output of each view in order.
func (b *MessageBus) RenderAll() []string {
	renders := make([]string, len(b.slots))
	for i, s := range b.slots {
		renders[i] = s.last
	}
	return renders
}

// RenderChanged returns outputs only for views whose render changed
// since the last call to RenderChanged. Unchanged views return "".
func (b *MessageBus) RenderChanged() []string {
	renders := make([]string, len(b.slots))
	for i := range b.slots {
		if b.slots[i].last != b.slots[i].printed {
			renders[i] = b.slots[i].last
			b.slots[i].printed = b.slots[i].last
		}
	}
	return renders
}
