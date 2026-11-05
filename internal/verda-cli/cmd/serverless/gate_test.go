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

package serverless

import (
	"bytes"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// container is a visible beta; batchjob stays Hidden (and env-gated in
// cmd/cmd.go) until it ships.
func TestServerlessGate(t *testing.T) {
	f := cmdutil.NewTestFactory(nil)
	streams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	c := NewCmdContainer(f, streams)
	if c.Hidden {
		t.Errorf("container command should be visible")
	}
	if got := c.Annotations[cmdutil.TagAnnotation]; got != "beta" {
		t.Errorf("container tag = %q, want beta", got)
	}
	if c := NewCmdBatchjob(f, streams); !c.Hidden {
		t.Errorf("batchjob command should stay Hidden")
	}
}
