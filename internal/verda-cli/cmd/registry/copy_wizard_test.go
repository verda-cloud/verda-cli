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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

// readCaptureRegistry records the ref passed to Read, then errors so the copy
// short-circuits before any network I/O. Used to assert the wizard built the
// expected source reference and reached the real copy path.
type readCaptureRegistry struct {
	Registry
	gotRef string
}

func (r *readCaptureRegistry) Read(_ context.Context, ref string) (v1.Image, error) {
	r.gotRef = ref
	return nil, errors.New("intercept: no network")
}

// headNotFoundRegistry answers every Head with a not-found AgentError so the
// overwrite guard treats the destination as absent and proceeds.
type headNotFoundRegistry struct {
	Registry
	gotRef string
}

func (h *headNotFoundRegistry) Head(_ context.Context, ref string) (*v1.Descriptor, error) {
	h.gotRef = ref
	return nil, &cmdutil.AgentError{Code: kindRegistryTagNotFound, Message: "not found"}
}

func TestCopyCommandPreview(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		opts *copyOptions
		want string
	}{
		{
			"single public",
			[]string{"docker.io/library/nginx:1.25", "vccr.io/p/library/nginx:1.25"},
			&copyOptions{SrcAuth: srcAuthDockerConfig},
			"verda registry copy docker.io/library/nginx:1.25 vccr.io/p/library/nginx:1.25",
		},
		{
			"all tags",
			[]string{"docker.io/library/nginx", "vccr.io/p/library/nginx"},
			&copyOptions{SrcAuth: srcAuthDockerConfig, AllTags: true},
			"verda registry copy docker.io/library/nginx vccr.io/p/library/nginx --all-tags",
		},
		{
			"basic auth",
			[]string{"private.example.com/app:v1", "vccr.io/p/app:v1"},
			&copyOptions{SrcAuth: srcAuthBasic, SrcUsername: "jdoe"},
			"verda registry copy private.example.com/app:v1 vccr.io/p/app:v1 --src-auth basic --src-username jdoe --src-password-stdin",
		},
		{
			"anonymous",
			[]string{"src:v1", "dst:v1"},
			&copyOptions{SrcAuth: srcAuthAnonymous},
			"verda registry copy src:v1 dst:v1 --src-auth anonymous",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := copyCommandPreview(c.args, c.opts); got != c.want {
				t.Errorf("preview = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCopyWizard_RunsCopyWithCollectedInputs drives the full wizard (source ->
// access -> scope -> destination -> confirm) and asserts it reaches the real
// copy with the source it collected. Registries are stubbed so no network I/O
// occurs.
func TestCopyWizard_RunsCopyWithCollectedInputs(t *testing.T) {
	writeCopyCredsFile(t, "vccr.io", "proj")

	src := &readCaptureRegistry{}
	prevSrc := sourceRegistryBuilder
	sourceRegistryBuilder = func(_ authn.Authenticator, _ RetryConfig) Registry { return src }
	t.Cleanup(func() { sourceRegistryBuilder = prevSrc })

	dst := &headNotFoundRegistry{}
	prevClient := clientBuilder
	clientBuilder = func(_ *options.RegistryCredentials, _ RetryConfig) Registry { return dst }
	t.Cleanup(func() { clientBuilder = prevClient })

	mock := tuitest.New().
		AddTextInput("docker.io/library/nginx:1.25"). // source image
		AddSelect(0).                                 // access: public / docker login
		AddSelect(0).                                 // scope: just this tag
		AddTextInput("").                             // destination: accept default
		AddConfirm(true)                              // confirm
	f := cmdutil.NewTestFactory(mock)
	streams, _, _ := copyStreams("")

	opts := &copyOptions{Retries: 5, Progress: progressAuto, SrcAuth: srcAuthDockerConfig}
	cmd := NewCmdCopy(f, streams)
	cmd.SetContext(context.Background())

	// The copy fails at the stubbed source Read (no network) — that's expected;
	// we only assert the wizard reached the copy with the inputs it gathered.
	_ = runCopyWizard(cmd, f, streams, opts)

	if !strings.Contains(src.gotRef, "library/nginx:1.25") {
		t.Errorf("source Read ref = %q, want it to contain library/nginx:1.25", src.gotRef)
	}
	// The default destination was synthesized under the active VCR project.
	if !strings.Contains(dst.gotRef, "vccr.io/proj/library/nginx:1.25") {
		t.Errorf("destination Head ref = %q, want vccr.io/proj/library/nginx:1.25", dst.gotRef)
	}
}

// TestCopy_NoArgsNonTTYUsageError: with no positional args and no terminal, the
// wizard must not launch — the command returns a usage error so scripts fail
// loudly instead of hanging on a prompt.
func TestCopy_NoArgsNonTTYUsageError(t *testing.T) {
	withForcedTTY(t, false)
	writeCopyCredsFile(t, "vccr.io", "proj")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams /* no args */)
	if err == nil {
		t.Fatal("expected a usage error with no args on a non-terminal")
	}
	if !strings.Contains(err.Error(), "requires a source") {
		t.Errorf("unexpected error: %v", err)
	}
}
