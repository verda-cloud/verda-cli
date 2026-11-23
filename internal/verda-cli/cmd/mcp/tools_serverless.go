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

package mcp

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Shared plumbing for the serverless tools (containers, batch jobs, catalog).
// Request assembly lives in cmd/serverless and is imported, never mirrored:
// the scratch mount, :latest rejection and queue-preset math must not drift
// between the CLI and MCP.

const (
	statusAccepted  = "accepted"
	statusCompleted = "completed"

	// deploymentWaitTimeout bounds wait: true on pause/resume (image pull is not involved).
	deploymentWaitTimeout  = 5 * time.Minute
	deploymentPollInterval = 3 * time.Second
	// defaultScaleWaitTimeout mirrors the CLI --wait-timeout default for scale.
	defaultScaleWaitTimeout = 60 * time.Second
	// statusReadTimeout bounds the best-effort status/scaling reads on describe and create.
	statusReadTimeout = 10 * time.Second
)

// flagToArg renames CLI flags whose MCP argument differs beyond dash→underscore.
var flagToArg = map[string]string{
	"registry-creds":   "registry_credentials",
	"env-secret":       "env_secrets",
	"secret-mount":     "secret_mounts",
	"scale-up-delay":   "scale_up_delay_seconds",
	"scale-down-delay": "scale_down_delay_seconds",
	"request-ttl":      "request_ttl_seconds",
	"deadline":         "deadline_seconds",
}

// argName converts a CLI flag ("--max-replicas", "min-replicas") to its MCP argument name.
func argName(flag string) string {
	f := strings.TrimPrefix(flag, "--")
	if i := strings.IndexByte(f, '='); i >= 0 {
		f = f[:i]
	}
	if a, ok := flagToArg[f]; ok {
		return a
	}
	return strings.ReplaceAll(f, "-", "_")
}

// messageFields maps the serverless validators' non-flag messages to the argument they concern.
var messageFields = []struct{ prefix, field string }{
	{"deployment name", "name"},
	{"container image", "image"},
	{"invalid env", "env"},
	{"invalid secret mount", "secret_mounts"},
}

// serverlessArgError renders a cmd/serverless or scaling validation error as
// VALIDATION_ERROR with details.field in argument form. Other errors pass through.
func serverlessArgError(err error) error {
	var fe *scaling.FieldError
	if errors.As(err, &fe) {
		return invalidArgError(argName(fe.Field), fe.Message)
	}
	var ae *cmdutil.AgentError
	if errors.As(err, &ae) {
		return err
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "--") {
		flag, rest, _ := strings.Cut(msg, " ")
		field := argName(flag)
		return invalidArgError(field, argVocabulary(field, rest))
	}
	for _, m := range messageFields {
		if strings.HasPrefix(msg, m.prefix) {
			return invalidArgError(m.field, msg)
		}
	}
	return invalidArgError("request", msg)
}

var (
	flagTokenRe = regexp.MustCompile(`--[a-z][a-z-]*`)
	durationRe  = regexp.MustCompile(`\b\d+[hms](?:\d+[ms])*\b`)
)

// argVocabulary rewrites a CLI validator message for MCP: other --flag tokens
// become argument names, and for *_seconds arguments Go durations ("10m",
// "168h") become seconds, since that is the unit the agent passes.
func argVocabulary(field, reason string) string {
	reason = flagTokenRe.ReplaceAllStringFunc(reason, argName)
	if !strings.HasSuffix(field, "_seconds") {
		return reason
	}
	return durationRe.ReplaceAllStringFunc(reason, func(tok string) string {
		d, err := time.ParseDuration(tok)
		if err != nil {
			return tok
		}
		return strconv.Itoa(int(d.Seconds())) + "s"
	})
}

// argReader collects typed arguments and latches the first error, so handlers
// with many optional arguments stay linear instead of one if-err per field.
type argReader struct {
	a   map[string]any
	err error
}

func (r *argReader) set(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *argReader) reqStr(name string) string {
	v, err := requiredString(r.a, name)
	r.set(err)
	return v
}

func (r *argReader) str(name string) string {
	v, err := optionalString(r.a, name)
	r.set(err)
	return v
}

func (r *argReader) boolean(name string) bool {
	v, err := optionalBool(r.a, name)
	r.set(err)
	return v
}

func (r *argReader) strs(name string) []string {
	v, err := optionalStringSlice(r.a, name)
	r.set(err)
	return v
}

func (r *argReader) enum(name string, allowed ...string) string {
	v, err := optionalEnum(r.a, name, allowed...)
	r.set(err)
	return v
}

func (r *argReader) intPtr(name string) *int {
	v, err := optionalIntPtr(r.a, name)
	r.set(err)
	return v
}

func (r *argReader) floatPtr(name string) *float64 {
	v, err := optionalFloatPtr(r.a, name)
	r.set(err)
	return v
}

// setInt overrides dst only when the argument was present: 0 is a legal value
// for min_replicas and "trigger off" for cpu_util, so absence must not reset.
func setInt(dst, v *int) {
	if v != nil {
		*dst = *v
	}
}

func setSeconds(dst *time.Duration, v *int) {
	if v != nil {
		*dst = time.Duration(*v) * time.Second
	}
}

// waitForStatus polls read until it reports want; the last observed status is
// returned with any error. Like scaling.Wait, a transient read failure is "not
// yet" (the deadline bounds retries); a permanent 4xx ends the wait at once.
func waitForStatus(ctx context.Context, want string, timeout time.Duration, read func(ctx context.Context) (string, error)) (string, error) {
	last := "unknown"
	return cmdutil.Poll(ctx, nil, deploymentPollInterval, cmdutil.WaitOptions{Wait: true, Timeout: timeout},
		func(ctx context.Context) (string, bool, error) {
			s, err := read(ctx)
			if scaling.Permanent(err) {
				return last, false, err
			}
			if err != nil {
				return last, false, nil //nolint:nilerr // intentional: transient read failure is "not yet"
			}
			last = s
			return s, s == want, nil
		})
}

// readStatus is the best-effort status read on describe/create: a failure
// lands in the result, never fails the tool.
func readStatus(ctx context.Context, read func(ctx context.Context) (string, error)) (status, statusErr string) {
	ctx, cancel := context.WithTimeout(ctx, statusReadTimeout)
	defer cancel()
	s, err := read(ctx)
	if err != nil {
		return "", err.Error()
	}
	return s, ""
}
