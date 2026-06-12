BINARY_NAME := cg
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/mnemcik/consigliere/cmd.Version=$(VERSION)"

.PHONY: all build install test test-short lint fmt tidy clean check snapshot e2e-autoupdate help

all: lint test build ## Run lint, test, and build

build: ## Build the binary
	go build $(LDFLAGS) -o $(BINARY_NAME) .

install: ## Install to $GOPATH/bin
	go install $(LDFLAGS) .

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...

test-short: ## Run tests (short mode, no race detector)
	go test -short ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	gofmt -s -w .
	goimports -w .

tidy: ## Tidy and verify dependencies
	go mod tidy
	go mod verify

clean: ## Remove build artifacts
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* coverage.out

check: fmt tidy lint test ## Run all checks (format, tidy, lint, test)

snapshot: ## Build a release snapshot (no publish)
	goreleaser release --snapshot --clean

e2e-autoupdate: ## End-to-end test of the auto-update subsystem (needs python3)
	./scripts/e2e-autoupdate.sh

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
