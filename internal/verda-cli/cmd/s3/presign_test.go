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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type fakePresigner struct {
	url string
	err error
}

func (p *fakePresigner) PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &v4.PresignedHTTPRequest{URL: p.url, Method: http.MethodGet}, nil
}

func withFakePresigner(p Presigner) func() {
	orig := presignerBuilder
	presignerBuilder = func(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (Presigner, error) {
		return p, nil
	}
	return func() { presignerBuilder = orig }
}

func TestPresign_Human(t *testing.T) {
	restore := withFakePresigner(&fakePresigner{url: "https://signed.example.com/file.txt?X-Amz-Signature=abc"})
	defer restore()

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdPresign(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://bucket/file.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "https://signed.example.com/file.txt?X-Amz-Signature=abc" {
		t.Errorf("stdout = %q, want single URL line", out.String())
	}
	if !strings.Contains(errOut.String(), "URL expires at") {
		t.Errorf("stderr should mention expiration; got %q", errOut.String())
	}
}

func TestPresign_JSON(t *testing.T) {
	restore := withFakePresigner(&fakePresigner{url: "https://signed/x"})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	cmd := NewCmdPresign(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://bucket/file.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.URL != "https://signed/x" {
		t.Errorf("URL = %q", payload.URL)
	}
	if payload.ExpiresAt == "" {
		t.Error("ExpiresAt missing")
	}
}

func TestPresign_MissingKey(t *testing.T) {
	restore := withFakePresigner(&fakePresigner{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdPresign(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://bucket"}) // no key
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for bucket-only URI")
	}
}

func TestPresign_NegativeExpires(t *testing.T) {
	restore := withFakePresigner(&fakePresigner{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdPresign(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://bucket/key", "--expires-in", "-5m"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for negative --expires-in")
	}
}
