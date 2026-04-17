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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// mbFakeAPI is a minimal API implementation for the mb command tests.
type mbFakeAPI struct {
	API
	createBucketInput *s3.CreateBucketInput
	createBucketErr   error
}

func (m *mbFakeAPI) CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	m.createBucketInput = in
	if m.createBucketErr != nil {
		return nil, m.createBucketErr
	}
	return &s3.CreateBucketOutput{}, nil
}

// mbFakeSmithyError is a local test helper — peer tests in this package may
// define their own fakeSmithyError; this name avoids any collision.
type mbFakeSmithyError struct {
	code    string
	message string
}

func (f *mbFakeSmithyError) Error() string                 { return f.message }
func (f *mbFakeSmithyError) ErrorCode() string             { return f.code }
func (f *mbFakeSmithyError) ErrorMessage() string          { return f.message }
func (f *mbFakeSmithyError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestMb_Success(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &mbFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMb(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-new-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.createBucketInput == nil {
		t.Fatal("CreateBucket was not called")
	}
	if aws.ToString(fake.createBucketInput.Bucket) != "my-new-bucket" {
		t.Errorf("Bucket = %q, want my-new-bucket", aws.ToString(fake.createBucketInput.Bucket))
	}
	got := out.String()
	if !strings.Contains(got, "my-new-bucket") {
		t.Errorf("stdout missing bucket name:\n%s", got)
	}
	if !strings.Contains(got, "created") {
		t.Errorf("stdout missing 'created' confirmation:\n%s", got)
	}
}

func TestMb_InvalidURI(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	restore := withFakeClient(&mbFakeAPI{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMb(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://bucket/with/key"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when URI has a key component")
	}
	if !strings.Contains(err.Error(), "bucket URI") {
		t.Errorf("error should mention bucket URI, got: %v", err)
	}
}

func TestMb_AlreadyExists(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &mbFakeAPI{
		createBucketErr: &mbFakeSmithyError{code: "BucketAlreadyExists", message: "exists"},
	}
	restore := withFakeClient(fake)
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMb(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://existing-bucket"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when bucket already exists")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if ae.Code != "VALIDATION_ERROR" {
		t.Errorf("Code = %q, want VALIDATION_ERROR", ae.Code)
	}
}
