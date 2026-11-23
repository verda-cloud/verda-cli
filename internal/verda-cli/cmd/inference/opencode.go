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

package inference

import (
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/tailscale/hujson"
)

const (
	openCodeSchema   = "https://opencode.ai/config.json"
	openCodeSkeleton = "{\n  \"$schema\": \"" + openCodeSchema + "\"\n}\n"
	// v2 keys providers by ID under `providers`; v1 (`provider`, npm/options) is deprecated but still read.
	openCodeV2Package = "@opencode/ai/providers/openai-compatible"
	openCodeV1Package = "@ai-sdk/openai-compatible"
)

var (
	openCodeV1Path = []string{"provider", providerID}
	openCodeV2Path = []string{"providers", providerID}
	versionMajorRe = regexp.MustCompile(`(\d+)\.\d+`)
)

type openCodeLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

type openCodeSettings struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
}

type openCodeModelV2 struct {
	ModelID string         `json:"modelID"`
	Name    string         `json:"name,omitempty"`
	Limit   *openCodeLimit `json:"limit,omitempty"`
}

type openCodeProviderV2 struct {
	Name     string                     `json:"name"`
	Package  string                     `json:"package"`
	Settings openCodeSettings           `json:"settings"`
	Models   map[string]openCodeModelV2 `json:"models"`
}

type openCodeModelV1 struct {
	Name  string         `json:"name,omitempty"`
	Limit *openCodeLimit `json:"limit,omitempty"`
}

type openCodeProviderV1 struct {
	Name    string                     `json:"name"`
	NPM     string                     `json:"npm"`
	Options openCodeSettings           `json:"options"`
	Models  map[string]openCodeModelV1 `json:"models"`
}

func openCodeConfigPath(env *hostEnv) string {
	dir := env.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(env.Home, ".config")
	}
	dir = filepath.Join(dir, "opencode")
	jsonc := filepath.Join(dir, "opencode.jsonc")
	if _, exists, _ := readFileIfExists(jsonc); exists {
		return jsonc
	}
	return filepath.Join(dir, "opencode.json")
}

// openCodeV2: the installed binary decides; if its version is unknown, follow
// the file (v2 only when it already uses `providers`).
func openCodeV2(c *agentContext, doc *hujson.Value) bool {
	if c.env.Version != nil {
		if out, err := c.env.Version(c.ctx, "opencode"); err == nil {
			if m := versionMajorRe.FindStringSubmatch(out); m != nil {
				major, _ := strconv.Atoi(m[1])
				return major >= 2
			}
		}
	}
	return jsoncHasPath(doc, "providers")
}

func openCodeLimits(c *agentContext, i int) *openCodeLimit {
	m := &c.models[i]
	if m.ContextLength == nil || m.MaxOutputLength == nil {
		return nil
	}
	return &openCodeLimit{Context: *m.ContextLength, Output: *m.MaxOutputLength}
}

func openCodeValue(c *agentContext, v2 bool) any {
	settings := openCodeSettings{
		BaseURL: c.openAIBaseURL(),
		APIKey:  "{file:" + c.env.tildePath(c.env.keyFile(agentOpenCode)) + "}",
	}
	if v2 {
		p := openCodeProviderV2{Name: "Verda", Package: openCodeV2Package, Settings: settings, Models: map[string]openCodeModelV2{}}
		for i := range c.models {
			m := &c.models[i]
			p.Models[m.Code] = openCodeModelV2{ModelID: m.EndpointModel, Name: m.Name, Limit: openCodeLimits(c, i)}
		}
		return p
	}
	// v1 sends the models map key as the model ID.
	p := openCodeProviderV1{Name: "Verda", NPM: openCodeV1Package, Options: settings, Models: map[string]openCodeModelV1{}}
	for i := range c.models {
		m := &c.models[i]
		p.Models[m.EndpointModel] = openCodeModelV1{Name: m.Name, Limit: openCodeLimits(c, i)}
	}
	return p
}

func writeOpenCode(c *agentContext) ([]fileChange, error) {
	path := openCodeConfigPath(c.env)
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return nil, err
	}
	src := old
	if !exists {
		src = []byte(openCodeSkeleton)
	}
	doc, err := parseJSONC(path, src)
	if err != nil {
		return nil, err
	}
	v2 := openCodeV2(c, doc)
	c.openCodeV2 = v2
	set, stale := openCodeV1Path, openCodeV2Path
	if v2 {
		set, stale = stale, set
	}
	removeJSONCPath(doc, stale)
	if err := setJSONCPath(doc, set, openCodeValue(c, v2)); err != nil {
		return nil, err
	}
	return []fileChange{{
		Path: path, Kind: fileKindPatched, Old: old, Exists: exists, New: doc.Pack(),
		Mode: 0o600, Created: c.created(path, exists),
	}}, nil
}

func cleanOpenCode(c *agentContext) ([]fileChange, error) {
	path := openCodeConfigPath(c.env)
	old, exists, err := readFileIfExists(path)
	if err != nil || !exists {
		return nil, err
	}
	doc, err := parseJSONC(path, old)
	if err != nil {
		return nil, err
	}
	removeJSONCPath(doc, openCodeV1Path)
	removeJSONCPath(doc, openCodeV2Path)
	out := doc.Pack()
	ch := fileChange{Path: path, Kind: fileKindPatched, Old: old, Exists: true, New: out, Mode: 0o600}
	if c.created(path, true) && string(out) == openCodeSkeleton {
		ch.Delete = true
	}
	return []fileChange{ch}, nil
}

func openCodeHints(c *agentContext) []string {
	m := c.primaryModel()
	ref := providerID + "/" + m.EndpointModel
	if c.openCodeV2 {
		ref = providerID + "/" + m.Code
	}
	return []string{
		"opencode, then /models and pick " + ref,
		"opencode run --model " + ref + ` "hello"`,
	}
}
