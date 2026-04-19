# wiki-vault

Developer setup for the `wiki` CLI.

## Prerequisites

- Go 1.25+
- `make` (optional; the Makefile is a thin wrapper around `go` commands)
- On Linux: a running Secret Service (GNOME Keyring, KWallet) if you plan
  to exercise `wiki login` locally.

## Clone and build

    git clone https://github.com/rj-ajt/wiki-vault.git
    cd wiki-vault
    make build           # produces ./wiki (or wiki.exe on Windows)

Or without make:

    go build -o wiki ./cmd/wiki

## Run

    ./wiki login
    ./wiki sync
    ./wiki logout

Run from a directory where you want the wiki to be synced; `wiki sync`
writes a `wiki.yaml` template on first run and fills in the tree on the
second.

## Test

    make test
    # or
    go test ./...

## Release snapshot

    make release-snapshot   # requires goreleaser installed locally

Release config lives in `.goreleaser.yaml`.

## Layout

    cmd/wiki              # CLI entry point
    internal/azuredevops  # ADO API client
    internal/cli          # cobra commands
    internal/config       # wiki.yaml load/save
    internal/credentials  # keyring wrapper
    internal/sync         # tree diff + filesystem writer
