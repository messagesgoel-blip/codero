# VERSION can be overridden at build time: make build VERSION=v1.2.4
VERSION ?= dev

.PHONY: fmtcheck lint unit contract ci help build run format integration

fmtcheck:
	@unformatted=$$(gofmt -l $$(find . -type f -name '*.go' -not -path './vendor/*')); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

format:
	gofmt -w .
	if command -v goimports >/dev/null 2>&1; then \
		goimports -w . ; \
	fi

lint: fmtcheck
	go vet ./...

unit:
	go test ./cmd/... ./internal/...

contract:
	go test ./tests/contract/...

integration:
	@echo "Running integration tests..."
	@if [ -f "tests/integration/sprint5_integration_test.go" ]; then \
		go test ./tests/integration/... ; \
	else \
		echo "ERROR: No integration tests found." ; \
		echo "Expected: tests/integration/sprint5_integration_test.go" ; \
		exit 1 ; \
	fi

build:
	@if [ "$(VERSION)" != "dev" ] && ! printf '%s' "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$$'; then \
		echo "ERROR: VERSION must match vX.Y.Z (optionally with pre-release/build metadata)"; \
		exit 1; \
	fi
	go build -buildvcs=false -trimpath -ldflags "-X main.version=$(VERSION)" -o codero ./cmd/codero
	@if [ -x "./scripts/sync-live-bin.sh" ]; then \
		./scripts/sync-live-bin.sh ; \
	fi

run:
	@echo "Starting codero daemon in development mode..."
	@if [ -f "codero" ]; then \
		./codero daemon start ; \
	else \
		echo "ERROR: codero binary not found. Run 'make build' first." ; \
		exit 1 ; \
	fi

help:
	@echo "Available targets:"
	@echo "  setup          - Install dependencies (not implemented - use 'go mod tidy')"
	@echo "  fmtcheck       - Check Go formatting"
	@echo "  format         - Format Go code"
	@echo "  lint           - Run linters"
	@echo "  unit           - Run unit tests"
	@echo "  contract       - Run contract tests"
	@echo "  integration    - Run integration tests"
	@echo "  build          - Build codero binary (VERSION=vX.Y.Z to stamp release version)"
	@echo "  run            - Run codero daemon"
	@echo "  ci             - Run full CI pipeline"
	@echo "  help           - Show this help message"

ci: lint unit contract
