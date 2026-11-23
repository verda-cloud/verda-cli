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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const (
	agentOpenCode   = "opencode"
	agentKimi       = "kimi"
	agentCodex      = "codex"
	agentClaudeCode = "claude-code"

	providerID = "verda"

	actionCreate    = "create"
	actionUpdate    = "update"
	actionDelete    = "delete"
	actionRestore   = "restore"
	actionUnchanged = "unchanged"
)

// fileChange is one file setup/remove would write; Old/Exists describe the
// file before the change so dry-run and backups need no second read.
type fileChange struct {
	Path   string
	Kind   string
	Old    []byte
	Exists bool
	New    []byte
	Delete bool
	// RestoreFrom: on delete, put this backup back instead.
	RestoreFrom string
	Mode        fs.FileMode
	// Secret files are always 0600, even when they already exist.
	Secret  bool
	Created bool
}

func (c *fileChange) action() string {
	switch {
	case c.Delete && c.RestoreFrom != "":
		return actionRestore
	case c.Delete && !c.Exists:
		return actionUnchanged
	case c.Delete:
		return actionDelete
	case !c.Exists:
		return actionCreate
	case bytes.Equal(c.Old, c.New):
		return actionUnchanged
	}
	return actionUpdate
}

type agentContext struct {
	ctx     context.Context
	env     *hostEnv
	baseURL string
	models  []verda.InferenceModel
	key     string
	prev    *agentRecord
	warn    func(string)
	// openCodeV2 is set by writeOpenCode for the start hints.
	openCodeV2 bool
}

func (c *agentContext) openAIBaseURL() string { return c.baseURL + "/v1" }

// primaryModel: agents that take a single model use the first selected one.
func (c *agentContext) primaryModel() *verda.InferenceModel { return &c.models[0] }

func (c *agentContext) created(path string, exists bool) bool {
	if !exists {
		return true
	}
	if rec := c.prev.file(path); rec != nil {
		return rec.Created
	}
	return false
}

type agentDef struct {
	Name    string
	Display string
	Binary  string
	Aliases []string
	// Experimental agents need /v1/responses or /v1/messages (Global Router).
	Experimental bool
	// KeyInFile: the key lives in ~/.verda/agents/<name>.key; else inline in a config.
	KeyInFile bool

	// ready reports why the agent cannot be set up on this machine yet; nil = ready.
	ready      func(env *hostEnv) error
	write      func(c *agentContext) ([]fileChange, error)
	clean      func(c *agentContext) ([]fileChange, error)
	inlineKey  func(c *agentContext) (string, error)
	startHints func(c *agentContext) []string
	alias      func(c *agentContext) (string, error)
}

var agentDefs = []*agentDef{
	{
		Name: agentOpenCode, Display: "OpenCode", Binary: "opencode", KeyInFile: true,
		write: writeOpenCode, clean: cleanOpenCode, startHints: openCodeHints,
	},
	{
		Name: agentKimi, Display: "Kimi Code", Binary: "kimi",
		ready: kimiReady, write: writeKimi, clean: cleanKimi, inlineKey: kimiKey, startHints: kimiHints,
	},
	{
		Name: agentCodex, Display: "Codex", Binary: "codex", Experimental: true, KeyInFile: true,
		write: writeCodex, clean: cleanCodex, startHints: codexHints, alias: codexAlias,
	},
	{
		Name: agentClaudeCode, Display: "Claude Code", Binary: "claude", Aliases: []string{"claude"}, Experimental: true,
		write: writeClaudeCode, clean: cleanClaudeCode, inlineKey: claudeCodeKey, startHints: claudeCodeHints, alias: claudeCodeAlias,
	},
}

func lookupAgent(name string) *agentDef {
	for _, d := range agentDefs {
		if d.Name == name || slices.Contains(d.Aliases, name) {
			return d
		}
	}
	return nil
}

func agentNames(experimental bool) []string {
	var names []string
	for _, d := range agentDefs {
		if !d.Experimental || experimental {
			names = append(names, d.Name)
		}
	}
	return names
}

// readKey recovers the plaintext key a previous setup stored for this agent.
func (d *agentDef) readKey(c *agentContext) (string, error) {
	if d.KeyInFile {
		data, exists, err := readFileIfExists(c.env.keyFile(d.Name))
		if err != nil || !exists {
			return "", errors.Join(err, errors.New("key file missing"))
		}
		return strings.TrimSpace(string(data)), nil
	}
	return d.inlineKey(c)
}

// changes is every file setup writes for this agent, key file included.
func (d *agentDef) changes(c *agentContext) ([]fileChange, error) {
	var out []fileChange
	if d.KeyInFile {
		kc, err := ownedChange(c, c.env.keyFile(d.Name), []byte(c.key))
		if err != nil {
			return nil, err
		}
		out = append(out, kc)
	}
	rest, err := d.write(c)
	if err != nil {
		return nil, err
	}
	return append(out, rest...), nil
}

func (d *agentDef) cleanChanges(c *agentContext) ([]fileChange, error) {
	out, err := d.clean(c)
	if err != nil {
		return nil, err
	}
	if d.KeyInFile {
		kc, err := deleteOwnedChange(c, c.env.keyFile(d.Name))
		if err != nil {
			return nil, err
		}
		out = append(out, kc)
	}
	return out, nil
}

func ownedChange(c *agentContext, path string, content []byte) (fileChange, error) {
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return fileChange{}, err
	}
	return fileChange{
		Path: path, Kind: fileKindOwned, Old: old, Exists: exists, New: content,
		Mode: 0o600, Secret: true, Created: c.created(path, exists),
	}, nil
}

func deleteOwnedChange(c *agentContext, path string) (fileChange, error) {
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return fileChange{}, err
	}
	ch := fileChange{Path: path, Kind: fileKindOwned, Old: old, Exists: exists, Delete: true}
	if rec := c.prev.file(path); rec != nil && rec.Backup != "" {
		ch.RestoreFrom = rec.Backup
	}
	return ch, nil
}

// applyChanges writes changes in order; a pre-existing file is backed up the
// first time verda modifies it.
func applyChanges(changes []fileChange, prev *agentRecord) ([]fileRecord, error) {
	recs := make([]fileRecord, 0, len(changes))
	for i := range changes {
		c := &changes[i]
		if c.Delete {
			if err := applyDelete(c); err != nil {
				return recs, err
			}
			continue
		}
		rec := fileRecord{Path: c.Path, Kind: c.Kind, Created: c.Created}
		prevRec := prev.file(c.Path)
		if prevRec != nil {
			rec.Backup = prevRec.Backup
		}
		if c.action() != actionUnchanged {
			if c.Exists && prevRec == nil && !c.Created {
				backup, err := backupOnce(c.Path, c.Old)
				if err != nil {
					return recs, err
				}
				rec.Backup = backup
			}
			if err := writeFileAtomic(c.Path, c.New, c.Mode, c.Secret); err != nil {
				return recs, err
			}
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func applyDelete(c *fileChange) error {
	if c.RestoreFrom == "" {
		return removeFileIfExists(c.Path)
	}
	data, exists, err := readFileIfExists(c.RestoreFrom)
	if err != nil {
		return err
	}
	if !exists {
		return removeFileIfExists(c.Path)
	}
	mode := fs.FileMode(0o600)
	if st, err := os.Stat(c.RestoreFrom); err == nil {
		mode = st.Mode().Perm()
	}
	if err := writeFileAtomic(c.Path, data, mode, true); err != nil {
		return err
	}
	return removeFileIfExists(c.RestoreFrom)
}

// shellPath quotes path for use inside a single-quoted alias.
func shellPath(env *hostEnv, path string) (string, error) {
	if strings.ContainsAny(path, "'\"$`\\\n") {
		return "", fmt.Errorf("cannot build a shell alias for path %q: it contains shell metacharacters", path)
	}
	if rel, err := filepath.Rel(env.Home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return `"$HOME/` + filepath.ToSlash(rel) + `"`, nil
	}
	return `"` + path + `"`, nil
}

func modelCodes(models []verda.InferenceModel) []string {
	codes := make([]string, len(models))
	for i := range models {
		codes[i] = models[i].Code
	}
	return codes
}
