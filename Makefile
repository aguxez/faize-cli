.PHONY: build build-unsigned test install clean lint sign

BINARY_NAME=faize
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"
ENTITLEMENTS=faize.entitlements
UNAME_S=$(shell uname -s)

# Build and sign (macOS gets entitlements for Virtualization.framework)
build: build-unsigned sign

# Build without signing (for CI or cross-compilation)
build-unsigned:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/faize

# Sign with entitlements (macOS only, no-op on other platforms)
sign:
ifeq ($(UNAME_S),Darwin)
	@echo "Signing $(BINARY_NAME) with virtualization entitlements..."
	codesign --entitlements $(ENTITLEMENTS) --force -s - $(BINARY_NAME)
else
	@echo "Skipping code signing (not on macOS)"
endif

test:
	go test -v ./...

install: build
	cp $(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME) 2>/dev/null || cp $(BINARY_NAME) ~/go/bin/$(BINARY_NAME)
ifeq ($(UNAME_S),Darwin)
	@echo "Note: Installed binary may need re-signing. Run 'make sign' in install location if needed."
endif

clean:
	rm -f $(BINARY_NAME)
	go clean

lint:
	golangci-lint run ./...

# Development helpers
dev: build
	./$(BINARY_NAME) --help

run: build
	./$(BINARY_NAME) --debug --minimal-test --project /tmp

fmt:
	go fmt ./...

vet:
	go vet ./...
