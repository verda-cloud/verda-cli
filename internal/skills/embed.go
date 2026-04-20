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

import (
	"embed"
	"io/fs"
)

//go:embed manifest.json
var manifestData []byte

//go:embed files/*
var skillFiles embed.FS

// ManifestData returns the raw embedded manifest JSON.
func ManifestData() []byte { return manifestData }

// ReadSkillFile reads a single skill file from the embedded filesystem.
func ReadSkillFile(name string) (string, error) {
	data, err := fs.ReadFile(skillFiles, "files/"+name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
