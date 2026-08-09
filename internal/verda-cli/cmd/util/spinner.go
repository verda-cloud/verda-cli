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
	"context"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// WithSpinner runs fn while showing a spinner message. If status is nil or the
// spinner cannot be created, fn is executed directly without visual feedback.
//
// fn receives a context derived from ctx: Ctrl+C on the spinner quits the UI
// and cancels it, so the guarded operation aborts instead of running to
// completion unseen (surfacing as context.Canceled through fn's error).
func WithSpinner[T any](ctx context.Context, status tui.Status, msg string, fn func(context.Context) (T, error)) (T, error) {
	if status == nil {
		return fn(ctx)
	}
	sp, err := status.Spinner(ctx, msg)
	if err != nil {
		return fn(ctx) // fallback: run without spinner
	}
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		if sp.Interrupted() {
			cancel()
		}
	}()
	result, fnErr := fn(opCtx)
	sp.Stop("")
	return result, fnErr
}

// RunWithSpinner runs fn while showing a spinner message. It is a convenience
// wrapper around [WithSpinner] for functions that return only an error.
func RunWithSpinner(ctx context.Context, status tui.Status, msg string, fn func(context.Context) error) error {
	_, err := WithSpinner(ctx, status, msg, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}
