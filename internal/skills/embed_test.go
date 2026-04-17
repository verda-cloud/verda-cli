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

package skills

import "testing"

func TestManifestData(t *testing.T) {
	t.Parallel()
	data := ManifestData()
	if len(data) == 0 {
		t.Fatal("expected non-empty manifest data")
	}
}

func TestReadSkillFile(t *testing.T) {
	t.Parallel()
	content, err := ReadSkillFile("verda-cloud.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestReadSkillFile_NotFound(t *testing.T) {
	t.Parallel()
	_, err := ReadSkillFile("nonexistent.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
