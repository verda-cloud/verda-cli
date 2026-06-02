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
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSetUsageTemplate_TagAnnotation verifies a command's TagAnnotation renders
// as "name (tag)" in the grouped help, while untagged commands stay plain.
func TestSetUsageTemplate_TagAnnotation(t *testing.T) {
	t.Parallel()
	tagged := &cobra.Command{Use: "s3", Short: "Manage S3 object storage",
		Annotations: map[string]string{TagAnnotation: "beta"}}
	plain := &cobra.Command{Use: "volume", Short: "Manage volumes"}
	root := &cobra.Command{Use: "verda"}
	groups := CommandGroups{{Message: "Resource Commands:", Commands: []*cobra.Command{tagged, plain}}}
	groups.Add(root)
	SetUsageTemplate(root, groups)

	tmpl := root.UsageTemplate()
	if !strings.Contains(tmpl, "s3 (beta)") {
		t.Errorf("tagged command should render as \"s3 (beta)\"; got:\n%s", tmpl)
	}
	if strings.Contains(tmpl, "volume (") {
		t.Errorf("untagged command must not get a tag; got:\n%s", tmpl)
	}
}
