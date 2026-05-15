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

package util

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	_ "github.com/verda-cloud/verdagostack/pkg/tui/bubbletea" // registers bubbletea TUI backend
	"github.com/verda-cloud/verdagostack/pkg/version"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// sensitiveJSONFieldRe matches "field": "value" JSON entries whose values must
// not appear in debug output (OAuth credentials, bearer tokens, etc.).
var sensitiveJSONFieldRe = regexp.MustCompile(
	`("(?:client_secret|access_token|refresh_token|id_token|password|api_key|bearer|authorization)")(\s*:\s*)"[^"]*"`)

func redactSensitiveJSON(s string) string {
	return sensitiveJSONFieldRe.ReplaceAllString(s, `$1$2"<redacted>"`)
}

// Factory provides shared resources that are created once in the root command
// and passed down to every subcommand. This pattern keeps commands testable
// and shared configuration in one place.
type Factory interface {
	// ServerAddr returns the configured API server address.
	ServerAddr() string
	// HTTPClient returns a shared HTTP client with the configured timeout.
	HTTPClient() *http.Client
	// Options returns the underlying Options for advanced use.
	Options() *clioptions.Options
	// VerdaClient returns a configured Verda SDK client.
	VerdaClient() (*verda.Client, error)
	// Login authenticates and returns a bearer token.
	Login() (string, error)
	// Token resolves a bearer token using the best available method.
	Token() string
	// Prompter returns the interactive prompt interface.
	Prompter() tui.Prompter
	// Status returns the status/output display interface.
	Status() tui.Status
	// Debug returns true if --debug is enabled.
	Debug() bool
	// OutputFormat returns the configured output format (table, json, yaml).
	OutputFormat() string
	// AgentMode returns true if --agent mode is enabled.
	AgentMode() bool
}

type factoryImpl struct {
	opts     *clioptions.Options
	client   *http.Client
	prompter tui.Prompter
	status   tui.Status
	verda    *verda.Client
}

// userAgentString returns a User-Agent header value like "verda-cli/v1.2.3".
func userAgentString() string {
	return "verda-cli/" + version.Get().GitVersion
}

// userAgentTransport wraps an http.RoundTripper to inject a User-Agent header.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(req)
}

// debugTransport logs HTTP request and response wire details to out when
// enabled() returns true. The Authorization header value is redacted.
type debugTransport struct {
	base    http.RoundTripper
	out     io.Writer
	enabled func() bool
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.enabled == nil || !t.enabled() || t.out == nil {
		return t.base.RoundTrip(req)
	}

	var reqBody []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err == nil {
			reqBody = b
		}
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	_, _ = fmt.Fprintf(t.out, "DEBUG: HTTP %s %s\n", req.Method, req.URL)
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.EqualFold(k, "Authorization") {
			_, _ = fmt.Fprintf(t.out, "DEBUG:   %s: <redacted>\n", k)
			continue
		}
		_, _ = fmt.Fprintf(t.out, "DEBUG:   %s: %s\n", k, strings.Join(req.Header[k], ", "))
	}
	if len(reqBody) > 0 {
		_, _ = fmt.Fprintf(t.out, "DEBUG: request body: %s\n", redactSensitiveJSON(string(reqBody)))
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		_, _ = fmt.Fprintf(t.out, "DEBUG: HTTP error: %v\n", err)
		return resp, err
	}

	var respBody []byte
	if resp.Body != nil {
		b, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr == nil {
			respBody = b
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}
	_, _ = fmt.Fprintf(t.out, "DEBUG: HTTP response %s\n", resp.Status)
	if len(respBody) > 0 {
		_, _ = fmt.Fprintf(t.out, "DEBUG: response body: %s\n", redactSensitiveJSON(string(respBody)))
	}
	return resp, nil
}

// NewFactory creates a Factory from the given Options. debugOut receives
// HTTP request/response dumps when --debug is enabled.
func NewFactory(opts *clioptions.Options, debugOut io.Writer) Factory {
	f := &factoryImpl{opts: opts}
	var rt http.RoundTripper = &userAgentTransport{base: http.DefaultTransport, userAgent: userAgentString()}
	rt = &debugTransport{base: rt, out: debugOut, enabled: f.Debug}
	f.client = &http.Client{
		Timeout:   opts.Timeout,
		Transport: rt,
	}
	f.prompter = tui.Default()
	f.status = tui.DefaultStatus()
	if opts.Agent {
		f.prompter = &agentPrompter{}
		f.status = nil
	}
	return f
}

func (f *factoryImpl) ServerAddr() string           { return f.opts.Server }
func (f *factoryImpl) HTTPClient() *http.Client     { return f.client }
func (f *factoryImpl) Options() *clioptions.Options { return f.opts }
func (f *factoryImpl) Prompter() tui.Prompter       { return f.prompter }
func (f *factoryImpl) Status() tui.Status {
	if f.opts.Output != "table" {
		return nil
	}
	return f.status
}
func (f *factoryImpl) Debug() bool          { return f.opts.Debug }
func (f *factoryImpl) OutputFormat() string { return f.opts.Output }
func (f *factoryImpl) AgentMode() bool      { return f.opts.Agent }

// VerdaClient creates or reuses the shared Verda SDK client.
func (f *factoryImpl) VerdaClient() (*verda.Client, error) {
	if f.verda != nil {
		return f.verda, nil
	}

	auth := f.opts.AuthOptions
	if auth.ClientID == "" || auth.ClientSecret == "" {
		return nil, errors.New("no credentials configured\n\n" +
			"Run \"verda auth login\" to set up your credentials, or provide them via:\n" +
			"  --auth.client-id / VERDA_CLIENT_ID\n" +
			"  --auth.client-secret / VERDA_CLIENT_SECRET")
	}

	options := []verda.ClientOption{
		verda.WithBaseURL(f.opts.Server),
		verda.WithClientID(auth.ClientID),
		verda.WithClientSecret(auth.ClientSecret),
		verda.WithHTTPClient(f.client),
		verda.WithUserAgent(userAgentString()),
	}
	if auth.BearerToken != "" {
		options = append(options, verda.WithAuthBearerToken(auth.BearerToken))
	}

	client, err := verda.NewClient(options...)
	if err != nil {
		return nil, err
	}

	f.verda = client
	return client, nil
}

// Login performs authentication against the API server.
func (f *factoryImpl) Login() (string, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return "", err
	}

	token, err := client.Auth.GetBearerToken()
	if err != nil {
		return "", err
	}

	return strings.TrimPrefix(token, "Bearer "), nil
}

// Token resolves a bearer token using the best available method.
func (f *factoryImpl) Token() string {
	auth := f.opts.AuthOptions
	if auth.BearerToken != "" {
		return auth.BearerToken
	}
	if auth.ClientID != "" && auth.ClientSecret != "" {
		token, _ := f.Login()
		return token
	}
	return ""
}
