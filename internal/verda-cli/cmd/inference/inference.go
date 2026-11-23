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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

const (
	defaultInferenceBaseURL = "https://inference.verda.com"
	inferenceBaseURLSetting = "inference.base-url"
	defaultProfile          = "default"
	tierStandard            = verda.InferenceServiceTierStandard
)

// NewCmdInference creates the `verda inference` command group.
func NewCmdInference(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newCmdInference(f, ioStreams, defaultHostEnv)
}

func newCmdInference(f cmdutil.Factory, ioStreams cmdutil.IOStreams, envFn func() (*hostEnv, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "inference",
		Aliases: []string{"inf"},
		Short:   "Verda Inference: models, subscriptions, API keys, and coding-agent setup",
		Long: cmdutil.LongDesc(`
			Manage Verda Inference for the project of your credentials: browse the
			model catalog, subscribe to models, create inference API keys, check
			usage and rate limits, and connect local coding agents to Verda models.

			The inference API is OpenAI-compatible at https://inference.verda.com/v1
			and Anthropic-compatible at https://inference.verda.com.
		`),
		Example: cmdutil.Examples(`
			# Connect your coding agents to Verda models
			verda inference setup

			# Browse models and subscribe
			verda inf models
			verda inf subscriptions enable <model>

			# Create an API key for your own code
			verda inf keys create --label my-app
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdModels(f, ioStreams),
		newCmdSubscriptions(f, ioStreams),
		newCmdKeys(f, ioStreams),
		newCmdUsage(f, ioStreams),
		newCmdLimits(f, ioStreams),
		newCmdSetup(f, ioStreams, envFn),
		newCmdRemove(f, ioStreams, envFn),
	)
	return cmd
}

// isInteractiveTerminal: prompts need stdin, stdout and stderr on a TTY; a var so tests can fake one.
var isInteractiveTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd()) && cmdutil.IsStdoutTerminal() && cmdutil.IsStderrTerminal()
}

// interactiveTTY: prompts only on a terminal, outside agent mode, with table output.
func interactiveTTY(f cmdutil.Factory) bool {
	return isInteractiveTerminal() && !f.AgentMode() && f.OutputFormat() == clioptions.OutputTable
}

func callAPI[T any](ctx context.Context, f cmdutil.Factory, msg string, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, msg)
	}
	v, err := fn(ctx)
	if sp != nil {
		sp.Stop("")
	}
	return v, err
}

// writeStructured round-trips through JSON so YAML output keeps the SDK's
// snake_case json tags (the YAML encoder ignores them).
func writeStructured(f cmdutil.Factory, ioStreams cmdutil.IOStreams, v any) (bool, error) {
	format := f.OutputFormat()
	if format == "yaml" {
		data, err := json.Marshal(v)
		if err != nil {
			return true, err
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return true, err
		}
		v = generic
	}
	return cmdutil.WriteStructured(ioStreams.Out, format, v)
}

func isNotFound(err error) bool {
	var apiErr *verda.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func profileName(f cmdutil.Factory) string {
	if a := f.Options().AuthOptions; a != nil && a.Profile != "" {
		return a.Profile
	}
	return defaultProfile
}

// resolveBaseURL: --inference-url > VERDA_INFERENCE_BASE_URL / config.yaml
// inference.base-url > profile verda_inference_base_url > production.
func resolveBaseURL(flag string, opts *clioptions.Options) (string, error) {
	raw := flag
	if raw == "" {
		raw = viper.GetString(inferenceBaseURLSetting)
	}
	if raw == "" && opts != nil && opts.AuthOptions != nil {
		if creds, err := clioptions.LoadSharedCredentialsForProfile(opts.AuthOptions.CredentialsFile, opts.AuthOptions.Profile); err == nil {
			raw = creds.InferenceBaseURL
		}
	}
	if raw == "" {
		raw = defaultInferenceBaseURL
	}
	return normalizeBaseURL(raw)
}

// normalizeBaseURL strips a trailing /v1: agents append their own API paths.
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", cmdutil.NewValidationError("inference-url", fmt.Sprintf("%q is not an http(s) URL", raw))
	}
	s := strings.TrimRight(u.String(), "/")
	s = strings.TrimSuffix(s, "/v1")
	return s, nil
}
