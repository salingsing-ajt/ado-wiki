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

## Configure

There is no default sync folder — pick or create an empty directory
and `cd` into it. That directory is where `wiki.yaml` must live and
where pages will be written.

    mkdir ~/my-wiki
    cd ~/my-wiki

Generate a `wiki.yaml` template by running `./wiki sync` once (it
writes the template and exits), then edit it:

    organization: your-azure-devops-organization
    project: Your Project Name
    wiki: Your Project.wiki

## Run

From inside that same directory:

    ./wiki login
    ./wiki sync
    ./wiki logout

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
