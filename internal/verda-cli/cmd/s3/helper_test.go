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

package s3

import (
	"context"
	"errors"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestBuildClientUsesSwap(t *testing.T) {
	// Do NOT use t.Parallel — we're mutating a package-level var.

	called := false
	orig := clientBuilder
	t.Cleanup(func() { clientBuilder = orig })

	clientBuilder = func(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (API, error) {
		called = true
		return nil, errors.New("fake")
	}

	_, err := buildClient(context.Background(), nil, ClientOverrides{})
	if err == nil || err.Error() != "fake" {
		t.Fatalf("expected fake error, got %v", err)
	}
	if !called {
		t.Fatal("swapped builder not invoked")
	}
}
