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
	"maps"
	"net"
	"net/http"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	reasonFeatureNotEnabled = "feature_not_enabled"

	notEnabledMessage = "Managed Kubernetes is not enabled for this project."
)

// What a create makes, for the "may have been created" warning.
const (
	kindCluster    = "cluster"
	kindNodePool   = "node pool"
	kindKubeconfig = "kubeconfig"
)

// SDK error texts that tell where a failed request stopped. The SDK wraps
// these errors as plain strings, so there is no type to match.
const (
	sdkAuthFailed       = "failed to get authentication token"
	sdkResponseUnread   = "failed to read response body"
	sdkResponseUnparsed = "failed to unmarshal response"
)

// mapErr turns a 403 whose reason is feature_not_enabled into the "not enabled"
// message, still AUTH_ERROR (exit 3) through ClassifyError. It returns a plain
// APIError, not an AgentError: main prints AgentErrors as JSON even for humans.
// Every other error is shown as the API sent it.
func mapErr(err error) error {
	if apiErr, ok := errors.AsType[*verda.APIError](err); ok && apiErr.StatusCode == http.StatusForbidden &&
		(strings.Contains(apiErr.Code, reasonFeatureNotEnabled) || strings.Contains(apiErr.Details, reasonFeatureNotEnabled)) {
		return &verda.APIError{StatusCode: http.StatusForbidden, Code: reasonFeatureNotEnabled, Message: notEnabledMessage}
	}
	return err
}

// createMayHaveSucceeded reports whether the API may have saved the object
// although the call failed: the request was sent and the answer is a 5xx, a
// 408, a timeout, a dropped or unreadable response. A 4xx (429 too) means
// nothing was created, and so does an error before the request left (a failed
// token fetch, no connection, a validation error).
func createMayHaveSucceeded(err error) bool {
	if neverSent(err) {
		return false
	}
	if apiErr, ok := errors.AsType[*verda.APIError](err); ok {
		return apiErr.StatusCode >= http.StatusInternalServerError || apiErr.StatusCode == http.StatusRequestTimeout
	}
	var netErr net.Error
	msg := err.Error()
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		errors.As(err, &netErr) ||
		strings.Contains(msg, sdkResponseUnread) ||
		strings.Contains(msg, sdkResponseUnparsed)
}

// neverSent: the failure came before the create request reached the server.
func neverSent(err error) bool {
	// A 5xx from the token endpoint is not a 5xx from the create.
	if strings.Contains(err.Error(), sdkAuthFailed) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// createWarning is the "may have been created anyway" text.
func createWarning(kind, listCmd string) string {
	w := fmt.Sprintf("The %s may have been created anyway. Before running create again, check `%s`.", kind, listCmd)
	if kind == kindKubeconfig {
		w += " Delete any kubeconfig you do not need with `verda mk8s kubeconfig delete`."
	}
	return w
}

// createFailed returns the error of a failed create. When the create may have
// been saved anyway it adds the warning: in the agent error details, or on
// stderr for humans. Creates are never retried: the API can save an object and
// still fail the answer, and a retry would make a second one.
func createFailed(f cmdutil.Factory, ioStreams cmdutil.IOStreams, kind, listCmd string, err error) error {
	err = mapErr(err)
	if !createMayHaveSucceeded(err) {
		return err
	}
	warning := createWarning(kind, listCmd)
	if !f.AgentMode() {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, warning)
		return err
	}
	ae := *cmdutil.ClassifyError(err)
	details := make(map[string]any, len(ae.Details)+1)
	maps.Copy(details, ae.Details)
	details["warning"] = warning
	ae.Details = details
	return &ae
}
