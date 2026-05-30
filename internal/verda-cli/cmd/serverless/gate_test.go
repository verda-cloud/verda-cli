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

// Serverless is pre-GA: both parents stay Hidden so they don't surface in
// `verda --help` even when VERDA_SERVERLESS_ENABLED registers them. Drop these
// assertions (and the Hidden flags) at GA.
func TestServerlessParentsHiddenPreGA(t *testing.T) {
	f := cmdutil.NewTestFactory(nil)
	streams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	if c := NewCmdContainer(f, streams); !c.Hidden {
		t.Errorf("container command should be Hidden pre-GA")
	}
	if c := NewCmdBatchjob(f, streams); !c.Hidden {
		t.Errorf("batchjob command should be Hidden pre-GA")
	}
}
