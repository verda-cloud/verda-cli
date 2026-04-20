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

// buildClient returns a Registry for the given credentials and retry
// policy, routed through clientBuilder so tests can substitute a fake.
// Pass RetryConfig{} to disable retries.
func buildClient(creds *options.RegistryCredentials, cfg RetryConfig) Registry {
	return clientBuilder(creds, cfg)
}

// loadCredsFromFactory loads registry credentials for the current
// profile, applying the s3-style fallback to the default profile name
// when Profile is empty.
//
// Registry commands are exempt from Options.Complete() (see
// cmd/cmd.go skipCredentialResolution), so AuthOptions.Profile is never
// auto-resolved. Without the fallback, an unset profile would make
// ini.v1 load the synthetic DEFAULT section instead of the user's
// [default] section, surfacing a spurious "not configured" error right
// after a successful `verda registry configure`.
func loadCredsFromFactory(f cmdutil.Factory, profileOverride, fileOverride string) (*options.RegistryCredentials, error) {
	profile := profileOverride
	if profile == "" {
		if opts := f.Options(); opts != nil && opts.AuthOptions != nil {
			profile = opts.AuthOptions.Profile
		}
	}
	if profile == "" {
		profile = defaultProfileName
	}
	path := credentialsFilePath(fileOverride)
	return options.LoadRegistryCredentialsForProfile(path, profile)
}
