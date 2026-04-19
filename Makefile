BINARY := wiki
ifeq ($(OS),Windows_NT)
	BINARY := wiki.exe
endif

.PHONY: build test release-snapshot
build:
	go build -o $(BINARY) ./cmd/wiki
test:
	go test ./...
release-snapshot:
	goreleaser release --snapshot --clean
