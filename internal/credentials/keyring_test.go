package credentials

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveGet(t *testing.T) {
	keyring.MockInit()
	if err := Save("secret"); err != nil {
		t.Fatal(err)
	}
	got, err := Get()
	if err != nil || got != "secret" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestGetMissing(t *testing.T) {
	keyring.MockInit()
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	keyring.MockInit()
	_ = Save("secret")
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete, Get = %v, want ErrNotFound", err)
	}
	if err := Delete(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}
