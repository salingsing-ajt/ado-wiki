package credentials

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	service = "wikivault"
	account = "azure-devops-pat"
)

var ErrNotFound = errors.New("pat not found in keyring")

func Save(pat string) error {
	if pat == "" {
		return errors.New("pat is empty")
	}
	return keyring.Set(service, account, pat)
}

func Get() (string, error) {
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func Delete() error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
