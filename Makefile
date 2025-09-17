BINARY := gb
PKG := ./cmd/gb
DIST := bin/release

.PHONY: all build clean test lint release snapshot dist

all: build

## Build for the current OS/Arch
build:
	go build -o bin/$(BINARY) $(PKG)

## Clean build artifacts
clean:
	rm -rf bin/ $(DIST) dist/

## Run tests
test:
	go test ./...

## Run linter
lint:
	golangci-lint run ./...

## Create a snapshot release (no GitHub upload)
snapshot:
	goreleaser release --snapshot --clean --skip-publish

## Full release (uploads to GitHub)
release:
	goreleaser release --clean

## Build binaries for all OS/ARCH
dist:
	goreleaser build --clean --snapshot --skip-publish
