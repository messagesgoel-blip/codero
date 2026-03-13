.PHONY: fmtcheck lint unit contract ci

fmtcheck:
	@unformatted=$$(gofmt -l $$(find . -type f -name '*.go' -not -path './vendor/*')); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

lint: fmtcheck
	go vet ./...

unit:
	go test ./cmd/... ./internal/...

contract:
	go test ./tests/contract/...

ci: lint unit contract
