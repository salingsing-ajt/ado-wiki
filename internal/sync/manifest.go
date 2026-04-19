package sync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Manifest struct {
	Files []string `json:"files"`
}

func manifestPath(dir string) string { return filepath.Join(dir, ".wikisync.json") }

func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{}, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func SaveManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(dir), data, 0o600)
}
