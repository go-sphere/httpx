# ============================================================================
# Variables
# ============================================================================

# All available adapters (for building/linting)
ADAPTERS := ginx fiberx echox hertzx

# Tag variable for versioning
TAG ?=

# ============================================================================
# Helper Functions
# ============================================================================

# Lint a specific directory
define lint_directory
	@echo "Linting $(1)..."
	cd $(1) && \
		go fmt ./... && \
		go vet ./... && \
		go get ./... && \
		go test ./... && \
		go mod tidy && \
		golangci-lint fmt --no-config --enable gofmt,goimports && \
		golangci-lint run --no-config --fix && \
		nilaway -include-pkgs="$(MODULE)" ./...
endef

# ============================================================================
# Development Commands
# ============================================================================

.PHONY: lint lint-all
lint: lint-all

# Lint all directories
lint-all:
	@echo "Running lint on all modules..."
	$(call lint_directory,./)
	$(call lint_directory,./ginx)
	$(call lint_directory,./fiberx)
	$(call lint_directory,./echox)
	$(call lint_directory,./hertzx)
	$(call lint_directory,./testing)
	$(call lint_directory,./integration)

# ============================================================================
# Testing Commands
# ============================================================================

.PHONY: test test-all test-ginx test-fiberx test-echox test-hertzx test-testing test-integration

test: test-all

# Test all integration tests
test-all:
	@echo "Running all integration tests..."
	cd integration && go test -v ./...

# Test specific adapter integration tests
test-ginx:
	@echo "Running ginx integration tests..."
	cd integration && go test -v -run TestGinx

test-fiberx:
	@echo "Running fiberx integration tests..."
	cd integration && go test -v -run TestFiberx

test-echox:
	@echo "Running echox integration tests..."
	cd integration && go test -v -run TestEchox

test-hertzx:
	@echo "Running hertzx integration tests..."
	cd integration && go test -v -run TestHertz

# Test testing framework
test-testing:
	@echo "Running testing framework tests..."
	cd testing && go test -v ./...

# Alias for test-all
test-integration: test-all

# ============================================================================
# Build Commands
# ============================================================================

.PHONY: build build-all

build: build-all

# Build all adapter modules
build-all:
	@echo "Building all httpx adapter modules..."
	@echo "=================================================="
	@for adapter in $(ADAPTERS); do \
		echo ""; \
		echo "Building $$adapter adapter..."; \
		echo "----------------------------"; \
		if cd $$adapter && go build ./... 2>&1; then \
			echo "✅ $$adapter build PASSED"; \
			cd ..; \
		else \
			echo "❌ $$adapter build FAILED"; \
			cd ..; \
			exit 1; \
		fi; \
	done; \
	echo ""; \
	echo "All adapters built successfully!"

# ============================================================================
# Git Tagging Commands
# ============================================================================

.PHONY: tag tag-all tag-delete help

# Create and push a single tag
tag:
	@if [ -z "$(TAG)" ]; then \
		echo "❌ TAG not set. Usage: make tag TAG=v0.0.1"; \
		exit 1; \
	fi
	@echo "Creating tag $(TAG)..."
	git tag -s $(TAG) -m "$(TAG)"
	git push origin --tags
	@echo "✅ Tag $(TAG) created and pushed"
	@echo "Install with: GOPROXY=direct GONOSUMDB=github.com/go-sphere/httpx go get github.com/go-sphere/httpx@$(TAG)"

# Create and push tags for all adapters
tag-all:
	@if [ -z "$(TAG)" ]; then \
		echo "❌ TAG not set. Usage: make tag-all TAG=v0.0.1"; \
		exit 1; \
	fi
	@echo "Creating tags for all adapters with version $(TAG)..."
	git tag -s ginx/$(TAG) -m "ginx/$(TAG)"
	git tag -s fiberx/$(TAG) -m "fiberx/$(TAG)"
	git tag -s hertzx/$(TAG) -m "hertzx/$(TAG)"
	git push origin --tags
	@echo "✅ All adapter tags created and pushed"
	@echo ""
	@echo "Install commands:"
	@echo "  go get github.com/go-sphere/httpx/ginx@$(TAG)"
	@echo "  go get github.com/go-sphere/httpx/fiberx@$(TAG)"
	@echo "  go get github.com/go-sphere/httpx/hertzx@$(TAG)"

# Delete tags (local and remote)
tag-delete:
	@if [ -z "$(TAG)" ]; then \
		echo "❌ TAG not set. Usage: make tag-delete TAG=v0.0.1"; \
		exit 1; \
	fi
	@echo "Deleting tags for version $(TAG)..."
	git tag -d $(TAG) || true
	git tag -d ginx/$(TAG) || true
	git tag -d fiberx/$(TAG) || true
	git tag -d hertzx/$(TAG) || true
	git push origin --delete $(TAG) || true
	git push origin --delete ginx/$(TAG) || true
	git push origin --delete fiberx/$(TAG) || true
	git push origin --delete hertzx/$(TAG) || true
	@echo "✅ Tags deleted"

# ============================================================================
# Help
# ============================================================================

.PHONY: help
help:
	@echo "HTTPx Makefile Commands"
	@echo "======================="
	@echo ""
	@echo "Development:"
	@echo "  lint, lint-all    - Run linting on all modules"
	@echo "  build, build-all  - Build all adapter modules"
	@echo ""
	@echo "Testing:"
	@echo "  test, test-all    - Run all integration tests"
	@echo "  test-ginx         - Run ginx integration tests only"
	@echo "  test-fiberx       - Run fiberx integration tests only"
	@echo "  test-echox        - Run echox integration tests only"
	@echo "  test-hertzx       - Run hertzx integration tests only"
	@echo "  test-testing      - Run testing framework tests only"
	@echo "  test-integration  - Alias for test-all"
	@echo ""
	@echo "Git Tagging:"
	@echo "  tag TAG=v0.0.1    - Create and push single tag"
	@echo "  tag-all TAG=v0.0.1 - Create and push tags for all adapters"
	@echo "  tag-delete TAG=v0.0.1 - Delete tags (local and remote)"
	@echo ""
	@echo "Other:"
	@echo "  help              - Show this help message"
	@echo ""
	@echo "Note: Individual adapter modules (ginx, fiberx, etc.) no longer"
	@echo "      have tests. All integration tests are in the integration module."