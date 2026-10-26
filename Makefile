BINARY_NAME := matrix-kchat
MODULE := github.com/rosch100/kchat
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-s -w -X main.Tag=$(VERSION) -X main.Commit=$(COMMIT) -X 'main.BuildTime=$(shell date -Iseconds)'"

# Platform-aware libolm CGO flags (needed for mautrix olm bindings).
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
    BREW_PREFIX := $(shell brew --prefix 2>/dev/null || echo /usr/local)
    OLM_PREFIX  := $(shell brew --prefix libolm 2>/dev/null || echo $(BREW_PREFIX))
    CGO_FLAGS   := CGO_CFLAGS="-I$(OLM_PREFIX)/include" CGO_LDFLAGS="-L$(OLM_PREFIX)/lib"
else
    CGO_FLAGS :=
endif

.PHONY: build test test-race lint fmt vet docker-build docker-push clean help

## build: Build the bridge binary
build:
	$(CGO_FLAGS) go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/matrix-kchat

## test: Run all tests
test:
	$(CGO_FLAGS) go test ./... -v

## test-race: Run tests with race detector
test-race:
	$(CGO_FLAGS) go test -race ./... -v

## lint: Run golangci-lint
lint:
	$(CGO_FLAGS) golangci-lint run ./...

## fmt: Format Go source files
fmt:
	gofmt -s -w .

## vet: Run go vet
vet:
	$(CGO_FLAGS) go vet ./...

## docker-build: Build Docker image
docker-build:
	docker build -t ghcr.io/rosch/$(BINARY_NAME):$(VERSION) .
	docker tag ghcr.io/rosch/$(BINARY_NAME):$(VERSION) ghcr.io/rosch/$(BINARY_NAME):latest
	docker tag ghcr.io/rosch/$(BINARY_NAME):$(VERSION) $(BINARY_NAME):$(VERSION)
	docker tag ghcr.io/rosch/$(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

## docker-push: Push Docker image to ghcr.io and Docker Hub
docker-push: docker-build
	docker push ghcr.io/rosch/$(BINARY_NAME):$(VERSION)
	docker push ghcr.io/rosch/$(BINARY_NAME):latest
	docker push $(BINARY_NAME):$(VERSION)
	docker push $(BINARY_NAME):latest

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
