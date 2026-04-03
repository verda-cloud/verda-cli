package images

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
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
