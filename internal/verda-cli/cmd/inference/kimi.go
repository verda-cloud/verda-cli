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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// kimiFallbackContext: max_context_size is required; used only when the API omits context_length.
const kimiFallbackContext int64 = 131072

func kimiHome(env *hostEnv) string {
	if dir := env.Getenv("KIMI_CODE_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(env.Home, ".kimi-code")
}

func kimiConfigPath(env *hostEnv) string { return filepath.Join(kimiHome(env), "config.toml") }

// kimiReady: never create the Kimi Code home ourselves; a config written before
// its first launch would skip the ~/.kimi legacy-migration prompt.
func kimiReady(env *hostEnv) error {
	if st, err := os.Stat(kimiHome(env)); err == nil && st.IsDir() {
		return nil
	}
	return fmt.Errorf("%s does not exist yet: run `kimi` once (or `kimi migrate`), then re-run `verda inference setup kimi`",
		env.tildePath(kimiHome(env)))
}

func kimiOwns(key []string) bool {
	if len(key) < 2 {
		return false
	}
	return key[0] == "providers" && key[1] == providerID ||
		key[0] == "models" && strings.HasPrefix(key[1], providerID+"/")
}

func kimiBlock(c *agentContext) string {
	tables := []string{renderTOMLTable(tomlKey("providers", providerID), []tomlKV{
		{"type", "openai"},
		{"base_url", c.openAIBaseURL()},
		{"api_key", c.key},
	})}
	for i := range c.models {
		m := &c.models[i]
		ctxLen := kimiFallbackContext
		if m.ContextLength != nil && *m.ContextLength > 0 {
			ctxLen = *m.ContextLength
		} else if c.warn != nil {
			c.warn(fmt.Sprintf("kimi: %s has no published context length; using %d", m.Code, kimiFallbackContext))
		}
		kvs := []tomlKV{
			{"provider", providerID},
			{"model", m.EndpointModel},
			{"display_name", m.Name},
			{"max_context_size", ctxLen},
		}
		if m.MaxOutputLength != nil && *m.MaxOutputLength > 0 {
			kvs = append(kvs, tomlKV{"max_output_size", *m.MaxOutputLength})
		}
		tables = append(tables, renderTOMLTable(tomlKey("models", providerID+"/"+m.Code), kvs))
	}
	return strings.Join(tables, "\n")
}

func writeKimi(c *agentContext) ([]fileChange, error) {
	path := kimiConfigPath(c.env)
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return nil, err
	}
	out, err := editTOMLFile(path, old, kimiOwns, kimiBlock(c))
	if err != nil {
		return nil, err
	}
	return []fileChange{{
		Path: path, Kind: fileKindPatched, Old: old, Exists: exists, New: out,
		Mode: 0o600, Created: c.created(path, exists),
	}}, nil
}

func cleanKimi(c *agentContext) ([]fileChange, error) {
	return cleanTOML(c, kimiConfigPath(c.env), kimiOwns)
}

func cleanTOML(c *agentContext, path string, owns func([]string) bool) ([]fileChange, error) {
	old, exists, err := readFileIfExists(path)
	if err != nil || !exists {
		return nil, err
	}
	out, err := editTOMLFile(path, old, owns, "")
	if err != nil {
		return nil, err
	}
	ch := fileChange{Path: path, Kind: fileKindPatched, Old: old, Exists: true, New: out, Mode: 0o600}
	if c.created(path, true) && strings.TrimSpace(string(out)) == "" {
		ch.Delete = true
	}
	return []fileChange{ch}, nil
}

func kimiKey(c *agentContext) (string, error) {
	path := kimiConfigPath(c.env)
	data, exists, err := readFileIfExists(path)
	if err != nil || !exists {
		return "", errors.Join(err, errors.New("kimi config missing"))
	}
	doc, err := parseTOMLDoc(path, data)
	if err != nil {
		return "", err
	}
	p, ok := tomlTableAt(doc, "providers", providerID)
	if !ok {
		return "", errors.New("providers.verda missing")
	}
	key, _ := p["api_key"].(string)
	if key == "" {
		return "", errors.New("providers.verda.api_key missing")
	}
	return key, nil
}

func kimiHints(c *agentContext) []string {
	return []string{
		"kimi -m " + providerID + "/" + c.primaryModel().Code,
		"or /model inside kimi and pick a " + providerID + "/<model> entry",
	}
}
