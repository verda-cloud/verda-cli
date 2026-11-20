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

package mk8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestMapErr_FeatureNotEnabled(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]*verda.APIError{
		"details": {StatusCode: 403, Code: "forbidden", Message: "forbidden", Details: "feature_not_enabled"},
		"code":    {StatusCode: 403, Code: "feature_not_enabled", Message: "forbidden"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ae := cmdutil.ClassifyError(mapErr(fmt.Errorf("wrapped: %w", e)))
			if ae.Code != "AUTH_ERROR" || ae.ExitCode != cmdutil.ExitAuth {
				t.Errorf("got %s exit %d", ae.Code, ae.ExitCode)
			}
			if ae.Message != notEnabledMessage {
				t.Errorf("message = %q", ae.Message)
			}
		})
	}
}

func TestMapErr_OtherErrorsUnchanged(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"other 403": &verda.APIError{StatusCode: 403, Message: "no access", Details: "something_else"},
		// Only the code and the details carry the reason, not free text.
		"403 text": &verda.APIError{StatusCode: 403, Message: "reason: feature_not_enabled"},
		"404":      &verda.APIError{StatusCode: 404, Message: "cluster not found"},
		"400":      &verda.APIError{StatusCode: 400, Message: "bad", Details: "cluster_quota_exceeded"},
		"plain":    errors.New("boom"),
	} {
		if got := mapErr(err); !errors.Is(got, err) && got != err { //nolint:errorlint // identity check on purpose
			t.Errorf("%s: mapErr changed the error: %v", name, got)
		}
	}
	// A 403 with the reason on a non-403 status is not the feature flag.
	if got := mapErr(&verda.APIError{StatusCode: 400, Details: "feature_not_enabled"}); strings.Contains(got.Error(), "not enabled for this project") {
		t.Error("only a 403 maps to the not-enabled message")
	}
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

var _ net.Error = timeoutNetError{}

func TestCreateMayHaveSucceeded(t *testing.T) {
	t.Parallel()
	yes := map[string]error{
		"500":      &verda.APIError{StatusCode: 500, Message: "x"},
		"502":      &verda.APIError{StatusCode: 502, Message: "x"},
		"503":      &verda.APIError{StatusCode: 503, Message: "x"},
		"408":      &verda.APIError{StatusCode: 408, Message: "x"},
		"deadline": fmt.Errorf("create: %w", context.DeadlineExceeded),
		"network":  fmt.Errorf("create: %w", timeoutNetError{}),
	}
	no := map[string]error{
		"400":            &verda.APIError{StatusCode: 400, Message: "x"},
		"403":            &verda.APIError{StatusCode: 403, Message: "x"},
		"409":            &verda.APIError{StatusCode: 409, Message: "x"},
		"429":            &verda.APIError{StatusCode: 429, Message: "x"},
		"sdk validation": &verda.ValidationError{Field: "name", Message: "bad"},
		"plain":          errors.New("boom"),
	}
	for name, err := range yes {
		if !createMayHaveSucceeded(err) {
			t.Errorf("%s: want warning", name)
		}
	}
	for name, err := range no {
		if createMayHaveSucceeded(err) {
			t.Errorf("%s: want no warning", name)
		}
	}
}

// realClientError runs a request the way the SDK does (net/http) and returns
// the error it wraps, so these cases use errors the standard library produces,
// not hand-made ones.
func realClientError(t *testing.T, handler http.HandlerFunc) error {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{}")) //nolint:noctx // test against a local server
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	t.Fatal("the request was expected to fail")
	return nil
}

func TestCreateMayHaveSucceeded_RealNetworkErrors(t *testing.T) {
	t.Parallel()

	t.Run("connection refused: nothing was sent", func(t *testing.T) {
		t.Parallel()
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		resp, err := http.Post("http://"+addr, "application/json", strings.NewReader("{}")) //nolint:noctx // test against a closed port
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("expected a dial error")
		}
		if createMayHaveSucceeded(fmt.Errorf("request failed: %w", err)) {
			t.Errorf("no request was sent, but the warning would show: %v", err)
		}
	})

	t.Run("response cut short after the headers", func(t *testing.T) {
		t.Parallel()
		err := realClientError(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"id":`))
		})
		if !createMayHaveSucceeded(err) {
			t.Errorf("the server may have saved the object, but no warning: %v", err)
		}
		if !createMayHaveSucceeded(io.ErrUnexpectedEOF) || !createMayHaveSucceeded(io.EOF) {
			t.Error("a bare EOF after the request was sent must warn")
		}
	})

	t.Run("connection dropped before any response", func(t *testing.T) {
		t.Parallel()
		err := realClientError(t, func(w http.ResponseWriter, _ *http.Request) {
			conn, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr == nil {
				_ = conn.Close()
			}
		})
		if !createMayHaveSucceeded(err) {
			t.Errorf("the request reached the server, but no warning: %v", err)
		}
	})
}

func TestCreateMayHaveSucceeded_WhereTheRequestStopped(t *testing.T) {
	t.Parallel()
	yes := map[string]error{
		"canceled after sending": fmt.Errorf("request failed: %w", &url.Error{Op: "Post", URL: "https://x", Err: context.Canceled}),
		"2xx body not readable":  errors.New("failed to read response body: connection reset by peer"),
		"2xx body not json":      errors.New("failed to unmarshal response: invalid character '<'"),
	}
	no := map[string]error{
		"token endpoint 502":  fmt.Errorf("failed to get authentication token: %w", &verda.APIError{StatusCode: 502, Message: "bad gateway"}),
		"token endpoint dial": fmt.Errorf("failed to get authentication token: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}),
		"validation":          errors.New("pod_cidr: must be a valid IPv4 CIDR"),
	}
	for name, err := range yes {
		if !createMayHaveSucceeded(err) {
			t.Errorf("%s: want warning", name)
		}
	}
	for name, err := range no {
		if createMayHaveSucceeded(err) {
			t.Errorf("%s: want no warning", name)
		}
	}
}

func TestCreateFailed_HumanWarnsOnStderr(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	io := cmdutil.IOStreams{Out: &out, ErrOut: &errOut}
	apiErr := &verda.APIError{StatusCode: 502, Message: "bad gateway"}

	err := createFailed(&cmdutil.TestFactory{}, io, kindCluster, "verda mk8s cluster list", apiErr)
	if !errors.Is(err, apiErr) {
		t.Fatalf("the API error must be returned: %v", err)
	}
	if got := errOut.String(); !strings.Contains(got, "The cluster may have been created anyway.") ||
		!strings.Contains(got, "verda mk8s cluster list") {
		t.Errorf("stderr = %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty: %q", out.String())
	}
}

func TestCreateFailed_AgentWarnsInDetails(t *testing.T) {
	t.Parallel()
	var errOut bytes.Buffer
	io := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errOut}
	apiErr := &verda.APIError{StatusCode: 500, Message: "boom"}

	err := createFailed(&cmdutil.TestFactory{AgentModeOverride: true}, io, kindKubeconfig, "verda mk8s kubeconfig list --cluster c", apiErr)
	ae := cmdutil.ClassifyError(err)
	if ae.Code != "API_ERROR" || ae.ExitCode != cmdutil.ExitAPI {
		t.Errorf("got %s exit %d", ae.Code, ae.ExitCode)
	}
	w, _ := ae.Details["warning"].(string)
	if !strings.Contains(w, "The kubeconfig may have been created anyway.") || !strings.Contains(w, "kubeconfig delete") {
		t.Errorf("warning = %q", w)
	}
	if ae.Details["status"] != 500 {
		t.Errorf("status detail lost: %v", ae.Details)
	}
	if errOut.Len() != 0 {
		t.Errorf("agent mode reports through the error envelope only: %q", errOut.String())
	}
}

func TestCreateFailed_4xxNoWarning(t *testing.T) {
	t.Parallel()
	for _, agent := range []bool{false, true} {
		var errOut bytes.Buffer
		io := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &errOut}
		apiErr := &verda.APIError{StatusCode: 400, Message: "bad", Details: "cluster_quota_exceeded"}

		err := createFailed(&cmdutil.TestFactory{AgentModeOverride: agent}, io, kindNodePool, "verda mk8s node-pool list", apiErr)
		if !errors.Is(err, apiErr) {
			t.Errorf("agent=%v: the 4xx must come back as is: %v", agent, err)
		}
		if errOut.Len() != 0 {
			t.Errorf("agent=%v: unexpected warning %q", agent, errOut.String())
		}
	}
}

func TestCreateFailed_FeatureNotEnabledIsMapped(t *testing.T) {
	t.Parallel()
	io := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	err := createFailed(&cmdutil.TestFactory{}, io, kindCluster, "x", &verda.APIError{StatusCode: 403, Details: "feature_not_enabled"})
	if ae := cmdutil.ClassifyError(err); ae.Code != "AUTH_ERROR" || !strings.Contains(ae.Message, "not enabled") {
		t.Errorf("got %+v", ae)
	}
}
