BINARY := gb
PKG := ./cmd/gb
DIST := bin/release

.PHONY: all build clean test lint snapshot release dist

# Default target
all: build

## Build for the current OS/Arch
build:
	@echo "Building $(BINARY) for current OS/ARCH..."
	go build -o bin/$(BINARY) $(PKG)

## Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/ $(DIST) dist/

## Run tests
test:
	@echo "Running tests..."
	go test ./...

## Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

## Create a snapshot release (no GitHub upload)
snapshot:
	@echo "Creating snapshot release..."
	goreleaser release --snapshot --clean

## Full release (uploads to GitHub)
release:
	@echo "Creating full release..."
	goreleaser release --clean

## Build binaries for all OS/ARCH as defined in .goreleaser.yml
dist:
	@echo "Building all binaries (cross-platform)..."
	goreleaser build --clean --snapshot

tag-release:
	@read -p "Enter version (e.g., v1.0.0): " version; \
	git tag $$version && \
	git push origin $$version && \
	$(MAKE) release
