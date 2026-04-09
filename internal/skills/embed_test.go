package skills

import "testing"

func TestManifestData(t *testing.T) {
	t.Parallel()
	data := ManifestData()
	if len(data) == 0 {
		t.Fatal("expected non-empty manifest data")
	}
}

func TestReadSkillFile(t *testing.T) {
	t.Parallel()
	content, err := ReadSkillFile("verda-cloud.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestReadSkillFile_NotFound(t *testing.T) {
	t.Parallel()
	_, err := ReadSkillFile("nonexistent.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
