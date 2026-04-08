package images

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda/testutil"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestFilterExcludesClusterImages(t *testing.T) {
	t.Parallel()

	images := []verda.Image{
		{Name: "ubuntu-24.04", Category: "ubuntu", IsCluster: false},
		{Name: "cluster-image", Category: "cluster", IsCluster: true},
		{Name: "pytorch-2.0", Category: "pytorch", IsCluster: false},
	}

	filtered := images[:0:0]
	for i := range images {
		if !images[i].IsCluster {
			filtered = append(filtered, images[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 non-cluster images, got %d", len(filtered))
	}
	if filtered[0].Name != "ubuntu-24.04" || filtered[1].Name != "pytorch-2.0" {
		t.Fatalf("unexpected filtered images: %v", filtered)
	}
}

func TestFilterByCategory(t *testing.T) {
	t.Parallel()

	images := []verda.Image{
		{Name: "ubuntu-24.04", Category: "ubuntu"},
		{Name: "ubuntu-22.04", Category: "ubuntu"},
		{Name: "pytorch-2.0", Category: "pytorch"},
	}

	category := "ubuntu"
	var filtered []verda.Image
	for i := range images {
		if images[i].Category == category {
			filtered = append(filtered, images[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 ubuntu images, got %d", len(filtered))
	}
}

func TestFilterEmptyList(t *testing.T) {
	t.Parallel()

	images := []verda.Image{}
	var filtered []verda.Image
	for _, img := range images {
		if !img.IsCluster {
			filtered = append(filtered, img)
		}
	}

	if len(filtered) != 0 {
		t.Fatalf("expected 0 images, got %d", len(filtered))
	}
}

func TestOutputShowsImageTypeNotID(t *testing.T) {
	t.Parallel()

	mockServer := testutil.NewMockServer()
	defer mockServer.Close()

	client := verda.NewTestClient(mockServer)

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)
	f.ClientOverride = client

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdImages(f, ioStreams))
	root.SetArgs([]string{"images"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Header should say IMAGE TYPE, not ID.
	if !strings.Contains(out, "IMAGE TYPE") {
		t.Errorf("expected header to contain IMAGE TYPE, got:\n%s", out)
	}
	if strings.Contains(out, "  ID  ") {
		t.Errorf("header should not contain ID column, got:\n%s", out)
	}

	// Rows should contain image_type values from mock server, not UUIDs.
	if !strings.Contains(out, "ubuntu-22.04-cuda-12.0") {
		t.Errorf("expected image_type 'ubuntu-22.04-cuda-12.0' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "pytorch-2.0") {
		t.Errorf("expected image_type 'pytorch-2.0' in output, got:\n%s", out)
	}
}
