package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = "wiki.yaml"

// Placeholders is every string the Sync template writes. Validate rejects
// configs that still contain any of them so we don't hit ADO with the
// literal placeholder values.
var Placeholders = map[string]bool{
	"your-azure-devops-organization": true,
	"Your Project Name":              true,
	"Your Project.wiki":              true,
}

type Config struct {
	Organization string `yaml:"organization"`
	Project      string `yaml:"project"`
	Wiki         string `yaml:"wiki"`
}

func (c *Config) Validate() error {
	fields := []struct {
		name, value string
	}{
		{"organization", c.Organization},
		{"project", c.Project},
		{"wiki", c.Wiki},
	}
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.name)
		}
		if Placeholders[f.value] {
			return fmt.Errorf("%s still has the template placeholder %q — edit wiki.yaml", f.name, f.value)
		}
	}
	return nil
}

var ErrNotFound = errors.New("wiki.yaml not found")

func Path(dir string) string { return filepath.Join(dir, FileName) }

func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w at %s", ErrNotFound, Path(dir))
		}
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("wiki.yaml: %w", err)
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir), data, 0o644)
}
