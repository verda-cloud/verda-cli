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
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestMapErr(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err        error
		notEnabled bool
	}{
		"403 details": {&verda.APIError{StatusCode: 403, Code: "forbidden", Details: "feature_not_enabled"}, true},
		"403 code":    {&verda.APIError{StatusCode: 403, Code: "feature_not_enabled"}, true},
		"other 403":   {&verda.APIError{StatusCode: 403, Message: "no access", Details: "something_else"}, false},
		"403 text":    {&verda.APIError{StatusCode: 403, Message: "reason: feature_not_enabled"}, false},
		"400 reason":  {&verda.APIError{StatusCode: 400, Details: "feature_not_enabled"}, false},
		"plain":       {errors.New("boom"), false},
	} {
		err := fmt.Errorf("wrapped: %w", tc.err)
		got := mapErr(err)
		if ae := cmdutil.ClassifyError(got); (ae.Message == notEnabledMessage) != tc.notEnabled || (tc.notEnabled && (ae.Code != "AUTH_ERROR" || ae.ExitCode != cmdutil.ExitAuth)) {
			t.Errorf("%s: got %s %q", name, ae.Code, ae.Message)
		}
		if !tc.notEnabled && got != err { //nolint:errorlint // identity check on purpose
			t.Errorf("%s: mapErr changed the error: %v", name, got)
		}
	}
}

// TestCreateMayHaveSucceeded guards the SDK error texts that createMayHaveSucceeded matches.
func TestCreateMayHaveSucceeded(t *testing.T) {
	t.Parallel()
	sent := func(err error) error {
		return fmt.Errorf("request failed: %w", &url.Error{Op: "Post", URL: "https://x", Err: err})
	}
	for name, want := range map[error]bool{
		&verda.APIError{StatusCode: 500}:                      true,
		&verda.APIError{StatusCode: 503}:                      true,
		&verda.APIError{StatusCode: 408}:                      true,
		fmt.Errorf("create: %w", context.DeadlineExceeded):    true,
		sent(context.Canceled):                                true,
		sent(io.EOF):                                          true,
		io.EOF:                                                true,
		io.ErrUnexpectedEOF:                                   true,
		&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}: true,
		errors.New("failed to read response body: connection reset by peer"):                   true,
		errors.New("failed to unmarshal response: invalid character '<'"):                      true,
		&verda.APIError{StatusCode: 400}:                                                       false,
		&verda.APIError{StatusCode: 403}:                                                       false,
		&verda.APIError{StatusCode: 409}:                                                       false,
		&verda.APIError{StatusCode: 429}:                                                       false,
		&verda.ValidationError{Field: "name", Message: "bad"}:                                  false,
		errors.New("boom"):                                                                     false,
		fmt.Errorf("failed to get authentication token: %w", &verda.APIError{StatusCode: 502}): false,
		sent(&net.OpError{Op: "dial", Err: errors.New("refused")}):                             false,
	} {
		if got := createMayHaveSucceeded(name); got != want {
			t.Errorf("%v: got %v, want %v", name, got, want)
		}
	}

	// A real refused connection must look like "never sent": the dial check relies on its shape.
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	resp, err := http.Post("http://"+addr, "application/json", strings.NewReader("{}")) //nolint:noctx // closed local port
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a dial error")
	}
	if createMayHaveSucceeded(fmt.Errorf("request failed: %w", err)) {
		t.Errorf("no request was sent, but the warning would show: %v", err)
	}
}
