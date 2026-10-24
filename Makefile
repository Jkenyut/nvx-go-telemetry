.PHONY: test test-race test-cover lint clean help

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

test: ## Run all unit tests
	go test -v ./...

test-race: ## Run all unit tests with race detector
	go test -v -race ./...

test-cover: ## Run tests with coverage
	go test -v -cover ./...

lint: ## Run code linter and static analysis
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; fi

clean: ## Clean build artifacts
	go clean
	rm -f coverage.out
