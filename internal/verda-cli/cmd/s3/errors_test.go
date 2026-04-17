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

package s3

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type fakeSmithyError struct {
	code    string
	message string
}

func (f *fakeSmithyError) Error() string                 { return f.message }
func (f *fakeSmithyError) ErrorCode() string             { return f.code }
func (f *fakeSmithyError) ErrorMessage() string          { return f.message }
func (f *fakeSmithyError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestTranslateError_Nil(t *testing.T) {
	t.Parallel()
	if translateError(nil) != nil {
		t.Fatal("translateError(nil) should be nil")
	}
}

func TestTranslateError_NotFound(t *testing.T) {
	t.Parallel()
	err := translateError(&fakeSmithyError{code: "NoSuchBucket", message: "bucket missing"})
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T", err)
	}
	if ae.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", ae.Code)
	}
	if ae.ExitCode != cmdutil.ExitNotFound {
		t.Errorf("ExitCode = %d, want %d", ae.ExitCode, cmdutil.ExitNotFound)
	}
}

func TestTranslateError_Auth(t *testing.T) {
	t.Parallel()
	cases := []string{"AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch"}
	for _, code := range cases {
		err := translateError(&fakeSmithyError{code: code, message: "denied"})
		var ae *cmdutil.AgentError
		if !errors.As(err, &ae) {
			t.Fatalf("%s: expected *AgentError, got %T", code, err)
		}
		if ae.Code != "AUTH_ERROR" {
			t.Errorf("%s: Code = %q, want AUTH_ERROR", code, ae.Code)
		}
	}
}

func TestTranslateError_BucketAlreadyExists(t *testing.T) {
	t.Parallel()
	err := translateError(&fakeSmithyError{code: "BucketAlreadyOwnedByYou", message: "yours"})
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T", err)
	}
	if ae.Code != "VALIDATION_ERROR" {
		t.Errorf("Code = %q, want VALIDATION_ERROR", ae.Code)
	}
}

func TestTranslateError_Passthrough(t *testing.T) {
	t.Parallel()
	base := errors.New("unknown failure")
	err := translateError(base)
	if !errors.Is(err, base) {
		t.Fatal("unknown errors should pass through unchanged")
	}
}
