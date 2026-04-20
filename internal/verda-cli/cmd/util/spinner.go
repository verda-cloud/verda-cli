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

	"github.com/verda-cloud/verdagostack/pkg/tui"
)

// WithSpinner runs fn while showing a spinner message. If status is nil or the
// spinner cannot be created, fn is executed directly without visual feedback.
func WithSpinner[T any](ctx context.Context, status tui.Status, msg string, fn func() (T, error)) (T, error) {
	if status == nil {
		return fn()
	}
	sp, err := status.Spinner(ctx, msg)
	if err != nil {
		return fn() // fallback: run without spinner
	}
	result, fnErr := fn()
	sp.Stop("")
	return result, fnErr
}

// RunWithSpinner runs fn while showing a spinner message. It is a convenience
// wrapper around [WithSpinner] for functions that return only an error.
func RunWithSpinner(ctx context.Context, status tui.Status, msg string, fn func() error) error {
	_, err := WithSpinner(ctx, status, msg, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}
