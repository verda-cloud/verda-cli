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

package status

import (
	"bytes"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNewCmdStatusHasCorrectUse(t *testing.T) {
	t.Parallel()

	f := cmdutil.NewTestFactory(nil)
	ioStreams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	cmd := NewCmdStatus(f, ioStreams)

	if cmd.Use != "status" {
		t.Fatalf("expected Use 'status', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Fatal("expected Short description to be non-empty")
	}
}
