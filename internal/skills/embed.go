package skills

import (
	"embed"
	"io/fs"
)

//go:embed manifest.json
var manifestData []byte

//go:embed files/*
var skillFiles embed.FS

// ManifestData returns the raw embedded manifest JSON.
func ManifestData() []byte { return manifestData }

// ReadSkillFile reads a single skill file from the embedded filesystem.
func ReadSkillFile(name string) (string, error) {
	data, err := fs.ReadFile(skillFiles, "files/"+name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
