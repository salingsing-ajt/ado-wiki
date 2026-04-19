package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{Organization: "contoso", Project: "Platform", Wiki: "Platform.wiki"}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil || *out != *in {
		t.Fatalf("roundtrip: got %+v err=%v, want %+v", out, err, in)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{Organization: "o", Project: "p", Wiki: "w"}).Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	for _, miss := range []*Config{
		{Project: "p", Wiki: "w"},
		{Organization: "o", Wiki: "w"},
		{Organization: "o", Project: "p"},
	} {
		if err := miss.Validate(); err == nil {
			t.Errorf("missing-field config accepted: %+v", miss)
		}
	}
	placeholder := &Config{Organization: "your-azure-devops-organization", Project: "p", Wiki: "w"}
	if err := placeholder.Validate(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("placeholder not rejected: %v", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("organisation: contoso\nproject: p\nwiki: w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "organisation") {
		t.Fatalf("want unknown-key error naming \"organisation\", got %v", err)
	}
}
