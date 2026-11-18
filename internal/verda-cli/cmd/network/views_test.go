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

package network

import (
	"bytes"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNetworkViewOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()
	v := newNetworkView(&verda.PrivateNetwork{ID: "pn_1", Name: "n", Mode: "custom"})
	for _, format := range []string{"json", "yaml"} {
		var b bytes.Buffer
		if _, err := cmdutil.WriteStructured(&b, format, v); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if out := b.String(); strings.Contains(out, "created_at") || strings.Contains(out, "0001-01-01") {
			t.Errorf("%s carries a zero created_at: %s", format, out)
		}
	}
}
