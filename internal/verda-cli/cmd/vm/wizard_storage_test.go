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

package vm

import (
	"context"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Regression: with volumes queued, "None (skip)" used to stay on the menu
// (row 0); selecting it silently discarded the queued volumes.
func TestBuildStorageChoicesSkipOnlyOnFreshMenu(t *testing.T) {
	t.Parallel()

	fresh := buildStorageChoices(nil, nil)
	if fresh[0].Value != "" || fresh[0].Label != "None (skip)" {
		t.Fatalf("fresh menu should offer skip at row 0, got %+v", fresh[0])
	}

	for name, tc := range map[string]struct {
		volumes     []verda.VolumeCreateRequest
		existingIDs []string
	}{
		"new volume queued":      {volumes: []verda.VolumeCreateRequest{{Name: "data", Size: 50, Type: verda.VolumeTypeNVMe}}},
		"existing volume queued": {existingIDs: []string{"vol-123"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			choices := buildStorageChoices(tc.volumes, tc.existingIDs)
			for _, c := range choices {
				if c.Value == "" || c.Label == "None (skip)" {
					t.Fatalf("skip must not be offered once storage is queued: %+v", choices)
				}
			}
			if done := choices[len(choices)-1]; done.Value != "__done__" {
				t.Fatalf("queued menu should end with Done, got %+v", done)
			}
		})
	}
}

// Add a volume, then choose Done: the spec must be flushed to opts.
func TestStorageLoaderAddThenDoneKeepsVolume(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, baseMux())
	getClient := func() (*verda.Client, error) { return h.Factory.ClientOverride, nil }
	opts := &createOptions{}

	p := tuitest.New().
		AddSelect(1).         // "+ Add new block volume"
		AddTextInput("data"). // volume name
		AddTextInput("50").   // size in GiB
		AddSelect(3)          // "Done — continue with above storage"

	step := stepStorage(getClient, &apiCache{}, opts)
	if _, err := step.Loader(context.Background(), p, nil, wizard.NewStore()); err != nil {
		t.Fatalf("loader: %v", err)
	}

	if len(opts.VolumeSpecs) != 1 || opts.VolumeSpecs[0] != "data:50:NVMe" {
		t.Fatalf("expected flushed volume spec, got %v", opts.VolumeSpecs)
	}
}

// Skip on the fresh menu must leave storage untouched.
func TestStorageLoaderSkipOnFreshMenu(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, baseMux())
	getClient := func() (*verda.Client, error) { return h.Factory.ClientOverride, nil }
	opts := &createOptions{}

	p := tuitest.New().AddSelect(0) // "None (skip)"

	step := stepStorage(getClient, &apiCache{}, opts)
	if _, err := step.Loader(context.Background(), p, nil, wizard.NewStore()); err != nil {
		t.Fatalf("loader: %v", err)
	}

	if opts.VolumeSpecs != nil || opts.ExistingVolumes != nil || opts.StorageSize != 0 {
		t.Fatalf("skip should leave storage empty, got specs=%v existing=%v size=%d",
			opts.VolumeSpecs, opts.ExistingVolumes, opts.StorageSize)
	}
}
