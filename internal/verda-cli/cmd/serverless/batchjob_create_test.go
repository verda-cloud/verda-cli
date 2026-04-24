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

package serverless

import (
	"strings"
	"testing"
	"time"
)

func validJobOpts() *batchjobCreateOptions {
	return &batchjobCreateOptions{
		Name:               "nightly-embed",
		Image:              "ghcr.io/org/embedder:v1",
		Compute:            "RTX4500Ada",
		ComputeSize:        1,
		Port:               80,
		MaxReplicas:        3,
		Deadline:           30 * time.Minute,
		RequestTTL:         300 * time.Second,
		GeneralStorageSize: defaultGeneralStorageGiB,
		SHMSize:            defaultSHMMiB,
	}
}

func TestBatchjobRequest_HappyPath(t *testing.T) {
	opts := validJobOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Name != "nightly-embed" {
		t.Errorf("name: got %q", req.Name)
	}
	if req.Compute == nil || req.Compute.Name != "RTX4500Ada" {
		t.Errorf("compute: got %+v", req.Compute)
	}
	if req.Scaling == nil || req.Scaling.DeadlineSeconds != int((30*time.Minute).Seconds()) {
		t.Errorf("deadline: got %+v, want %d", req.Scaling, int((30 * time.Minute).Seconds()))
	}
	if req.Scaling.MaxReplicaCount != 3 {
		t.Errorf("max replicas: got %d", req.Scaling.MaxReplicaCount)
	}
}

func TestBatchjobRequest_RejectsLatest(t *testing.T) {
	opts := validJobOpts()
	opts.Image = "nginx:latest"
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestBatchjobRequest_RequiresDeadline(t *testing.T) {
	opts := validJobOpts()
	opts.Deadline = 0
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestBatchjobMissingFlags(t *testing.T) {
	opts := &batchjobCreateOptions{}
	missing := missingBatchjobCreateFlags(opts)
	want := []string{"--name", "--image", "--compute", "--deadline"}
	if len(missing) != len(want) {
		t.Fatalf("missing: got %v, want %v", missing, want)
	}
	for i, w := range want {
		if missing[i] != w {
			t.Errorf("missing[%d]: got %q, want %q", i, missing[i], w)
		}
	}
}
