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

package registry

import (
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// clientBuilder is the package-level swap point. Tests reassign it to
// return a fake Registry backed by an in-process test server. Production
// code funnels through buildClient → clientBuilder →
// newGGCRRegistryWithRetry.
//
// The builder accepts a RetryConfig so push's --retries flag can flow
// into the transport. Call sites that don't yet expose a retries knob
// (ls, tags, login) pass RetryConfig{}, which disables retries.
var clientBuilder = newGGCRRegistryWithRetry

// daemonListerBuilder is the parallel swap point for the local Docker
// daemon lister. Tests (push, Task 19) reassign it to return a fake
// DaemonLister that enumerates a canned image set without touching the
// host's docker socket. Production code funnels through NewDaemonLister.
var daemonListerBuilder = NewDaemonLister

// sourceLoaderBuilder is the swap point for resolving a user-supplied
// image reference (daemon ref / OCI layout dir / tarball file) into a
// v1.Image. It takes a ping function (supplied by push) so auto-detect
// can probe the daemon without a circular import between source.go and
// the push command. Tests reassign it to return a fake SourceLoader.
var sourceLoaderBuilder = NewDefaultSourceLoader

// harborListerBuilder is the swap point for the Harbor REST client used
// by `ls`. Tests reassign it to return a fake RepositoryLister with a
// canned repository set. Production code funnels through
// buildHarborLister -> harborListerBuilder -> newHarborClient.
//
// A separate swap point (rather than reusing clientBuilder) mirrors the
// interface split: Registry is ggcr/Docker-v2 shaped; RepositoryLister
// is Harbor-REST shaped. See harbor.go for the rationale.
var harborListerBuilder = newHarborClient

// buildClient returns a Registry for the given credentials and retry
// policy, routed through clientBuilder so tests can substitute a fake.
// Pass RetryConfig{} to disable retries.
func buildClient(creds *options.RegistryCredentials, cfg RetryConfig) Registry {
	return clientBuilder(creds, cfg)
}

// buildHarborLister returns a RepositoryLister for the given credentials
// and retry policy, routed through harborListerBuilder so tests can
// substitute a fake.
func buildHarborLister(creds *options.RegistryCredentials, cfg RetryConfig) RepositoryLister {
	return harborListerBuilder(creds, cfg)
}

// loadCredsFromFactory loads registry credentials for the resolved profile
// (see resolveProfile).
func loadCredsFromFactory(_ cmdutil.Factory, profileOverride, fileOverride string) (*options.RegistryCredentials, error) {
	profile := resolveProfile(profileOverride)
	path := credentialsFilePath(fileOverride)
	return options.LoadRegistryCredentialsForProfile(path, profile)
}

// resolveProfile picks the credentials profile a registry command acts on:
// an explicit --profile flag wins, else the CLI's active profile (VERDA_PROFILE
// or auth.profile in the config), else "default".
//
// Registry commands are in skipCredentialResolution, so — unlike `verda vm` —
// they don't inherit the active profile from options.Complete(). Resolving it
// here keeps `registry ls/tags/push/...` consistent with `registry configure`
// and the rest of the CLI: a user who ran `verda auth use <profile>` (or set
// VERDA_PROFILE) gets that profile for registry too. Idempotent — resolving an
// already-explicit profile returns it unchanged.
//
// The "default" fallback also avoids ini.v1 loading its synthetic DEFAULT
// section for a blank profile, which would surface a spurious "not configured"
// error right after a successful `verda registry configure`.
func resolveProfile(flagProfile string) string {
	if p := options.ActiveProfile(flagProfile); p != "" {
		return p
	}
	return defaultProfileName
}
