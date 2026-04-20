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

// Package registry's loginparse.go provides a pure-function parser for the
// `docker login ...` command-line string the Verda web UI emits after a user
// provisions a container-registry credential. The parser is consumed by the
// `configure` wizard, which asks the user to paste the command and extracts
// the username, secret, host, and project UUID.
//
// Scope notes:
//
//   - This is not a general-purpose `docker` flag parser. Only -u/--username,
//     -p/--password, and the trailing positional host are recognized.
//   - `--password-stdin` is not accepted: the web UI always emits a
//     `-p <secret>` form, and supporting stdin is the configure command's job
//     (not this pure-function parser's).
//   - The username format `vcr-<project-id>+<name>` embeds the project UUID.
//     When the registry host also carries a `/uuid` suffix, we require it to
//     match — mismatch means the user pasted credentials from a different
//     project, which we surface as an error rather than silently trusting one
//     side.
package registry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// parsedLogin is the structured form of a `docker login ...` string.
type parsedLogin struct {
	Username  string
	Secret    string
	Host      string // host only, e.g. "vccr.io" (no scheme, no path)
	ProjectID string // extracted from Username (vcr-<project-id>+<name>)
}

// usernameRe matches `vcr-<project-id>+<name>`. The project-id capture
// tolerates hyphens, digits, and both letter cases so UUIDs parse cleanly.
// The name portion accepts any non-`+` characters to match the web UI, which
// allows free-form credential names.
var usernameRe = regexp.MustCompile(`^vcr-([a-zA-Z0-9-]+)\+[^+]+$`)

// parseDockerLogin parses a `docker login ...` command string as pasted from
// the Verda web UI's credential-created screen. Returns a populated
// parsedLogin on success, or an error describing what was wrong with the
// input so the wizard can prompt the user to try again.
func parseDockerLogin(raw string) (parsedLogin, error) {
	tokens := strings.Fields(raw)
	if len(tokens) < 2 || tokens[0] != "docker" || tokens[1] != "login" {
		return parsedLogin{}, errors.New("not a docker login command")
	}

	var (
		username string
		secret   string
		host     string
	)

	i := 2
	for i < len(tokens) {
		tok := tokens[i]
		switch tok {
		case "-u", "--username":
			if i+1 >= len(tokens) {
				return parsedLogin{}, fmt.Errorf("flag %s requires a value", tok)
			}
			username = tokens[i+1]
			i += 2
		case "-p", "--password":
			if i+1 >= len(tokens) {
				return parsedLogin{}, fmt.Errorf("flag %s requires a value", tok)
			}
			secret = tokens[i+1]
			i += 2
		default:
			// First unclaimed positional is the host. Later positionals are
			// unexpected; ignore them silently so a stray copy-pasted suffix
			// doesn't surprise-break the paste.
			if host == "" {
				host = tok
			}
			i++
		}
	}

	if username == "" {
		return parsedLogin{}, errors.New("missing username (-u/--username)")
	}
	if secret == "" {
		return parsedLogin{}, errors.New("missing password (-p/--password)")
	}
	if host == "" {
		return parsedLogin{}, errors.New("missing registry host")
	}
	if strings.Contains(host, "://") {
		return parsedLogin{}, fmt.Errorf("registry host must not include a scheme: %q", host)
	}

	m := usernameRe.FindStringSubmatch(username)
	if m == nil {
		return parsedLogin{}, errors.New("username must be in format vcr-<project-id>+<name>")
	}
	projectID := m[1]

	// Host may arrive as "vccr.io" or "vccr.io/<project-id>". Split on the
	// last slash so we're resilient to future path-prefix changes; the
	// project-id must be the last path segment per current UI output.
	if idx := strings.LastIndex(host, "/"); idx >= 0 {
		hostOnly := host[:idx]
		hostProject := host[idx+1:]
		if hostOnly == "" {
			return parsedLogin{}, errors.New("registry host is empty")
		}
		if hostProject != projectID {
			return parsedLogin{}, fmt.Errorf(
				"project UUID in username (%q) does not match host path (%q) — credentials may be from a different project",
				projectID, hostProject,
			)
		}
		host = hostOnly
	}

	return parsedLogin{
		Username:  username,
		Secret:    secret,
		Host:      host,
		ProjectID: projectID,
	}, nil
}
