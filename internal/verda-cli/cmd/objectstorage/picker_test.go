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

package objectstorage

import (
	"bytes"
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestSelectBucket_PicksChosen(t *testing.T) {
	// no t.Parallel — clientBuilder/prompter state
	fake := &fakeS3API{buckets: []s3types.Bucket{
		{Name: aws.String("alpha")},
		{Name: aws.String("beta")},
	}}
	f := cmdutil.NewTestFactory(tuitest.New().AddSelect(1)) // choose 2nd
	got, err := selectBucket(context.Background(), f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}, fake)
	if err != nil {
		t.Fatalf("selectBucket: %v", err)
	}
	if got != "beta" {
		t.Errorf("chosen bucket = %q, want beta", got)
	}
}

func TestSelectBucket_EmptyReturnsBlank(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{}
	f := cmdutil.NewTestFactory(tuitest.New())
	errOut := &bytes.Buffer{}
	got, err := selectBucket(context.Background(), f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: errOut}, fake)
	if err != nil {
		t.Fatalf("selectBucket: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty (no buckets)", got)
	}
}

func TestResolveBucketArg_ExplicitPassthrough(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	got, err := resolveBucketArg(cmd, f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}, []string{"s3://my-bucket/key"})
	if err != nil {
		t.Fatalf("resolveBucketArg: %v", err)
	}
	if got != "s3://my-bucket/key" {
		t.Errorf("got %q, want passthrough of explicit arg", got)
	}
}

func TestResolveBucketArg_AgentModeMissing(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	_, err := resolveBucketArg(cmd, f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}, nil)
	if err == nil {
		t.Fatal("expected a missing-arg error in agent mode with no bucket")
	}
}
