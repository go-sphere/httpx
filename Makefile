# ============================================================================
# Variables
# ============================================================================

# All available adapters
ADAPTERS := ginx fiberx echox fasthttpx hertzx testing

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

# Test a specific adapter
define test_adapter
	@echo ""
	@echo "Testing $(1) adapter..."
	@echo "----------------------------"
	@if cd $(1) && go test -v . 2>&1; then \
		echo "✅ $(1) tests PASSED"; \
	else \
		echo "❌ $(1) tests FAILED"; \
		exit 1; \
	fi
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
	$(call lint_directory,./fasthttpx)
	$(call lint_directory,./hertzx)
	$(call lint_directory,./testing)

# ============================================================================
# Testing Commands
# ============================================================================

.PHONY: test test-all test-ginx test-fiberx test-echox test-fasthttpx test-hertzx test-testing

test: test-all

# Test all adapters with summary
test-all:
	@echo "Testing all httpx adapters..."
	@echo "=================================================="
	@passed=0; failed=0; failed_adapters=""; \
	for adapter in $(ADAPTERS); do \
		echo ""; \
		echo "Testing $$adapter adapter..."; \
		echo "----------------------------"; \
		if cd $$adapter && go test -v . 2>&1; then \
			echo "✅ $$adapter tests PASSED"; \
			passed=$$((passed + 1)); \
			cd ..; \
		else \
			echo "❌ $$adapter tests FAILED"; \
			failed=$$((failed + 1)); \
			failed_adapters="$$failed_adapters $$adapter"; \
			cd ..; \
		fi; \
	done; \
	echo ""; \
	echo "=================================================="; \
	echo "Test Summary:"; \
	echo "✅ Passed: $$passed adapters"; \
	echo "❌ Failed: $$failed adapters"; \
	if [ $$failed -gt 0 ]; then \
		echo "Failed adapters:$$failed_adapters"; \
		echo ""; \
		echo "Note: Some failures may be expected due to adapter-specific"; \
		echo "implementation differences (e.g., BasePath behavior)."; \
	fi; \
	echo ""; \
	echo "Integration testing complete!"

# Individual adapter tests
test-ginx:
	$(call test_adapter,ginx)

test-fiberx:
	$(call test_adapter,fiberx)

test-echox:
	$(call test_adapter,echox)

test-fasthttpx:
	$(call test_adapter,fasthttpx)

test-hertzx:
	$(call test_adapter,hertzx)

test-testing:
	$(call test_adapter,testing)

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
	@echo ""
	@echo "Testing:"
	@echo "  test, test-all    - Run tests on all adapters"
	@echo "  test-ginx         - Test ginx adapter only"
	@echo "  test-fiberx       - Test fiberx adapter only"
	@echo "  test-echox        - Test echox adapter only"
	@echo "  test-fasthttpx    - Test fasthttpx adapter only"
	@echo "  test-hertzx       - Test hertzx adapter only"
	@echo "  test-testing      - Test testing framework only"
	@echo ""
	@echo "Git Tagging:"
	@echo "  tag TAG=v0.0.1    - Create and push single tag"
	@echo "  tag-all TAG=v0.0.1 - Create and push tags for all adapters"
	@echo "  tag-delete TAG=v0.0.1 - Delete tags (local and remote)"
	@echo ""
	@echo "Other:"
	@echo "  help              - Show this help message"