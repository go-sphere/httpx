GO ?= go
GOLANGCI_LINT ?= golangci-lint
NILAWAY ?= nilaway

GO_MOD_DIRS := . ginx fiberx echox hertzx conformance
TAG_ADAPTERS := ginx fiberx echox hertzx
DIRECT_DEPS_TEMPLATE := {{if and (not .Main) (not .Indirect) (not .Replace)}}{{.Path}}{{end}}

.DEFAULT_GOAL := check

# Local builds use the workspace to test all adapters against the root module.
ifneq ($(wildcard $(CURDIR)/go.work),)
export GOWORK := $(CURDIR)/go.work
endif

.PHONY: deps-update tidy fmt test test-race lint lint-all check verify api-compat
.PHONY: bench bench-5x tag tag-all tag-delete help

deps-update:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> updating $$dir"; \
		( cd "$$dir"; \
		  deps="$$(GOWORK=off $(GO) list -m -f '$(DIRECT_DEPS_TEMPLATE)' all)"; \
		  if [ -n "$$deps" ]; then GOWORK=off $(GO) get -u $$deps; fi; \
		  GOWORK=off $(GO) mod tidy ); \
	done

tidy:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> tidying $$dir"; \
		( cd "$$dir" && GOWORK=off $(GO) mod tidy ); \
	done

fmt:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> formatting $$dir"; \
		( cd "$$dir" && $(GO) fmt ./... && \
		  $(GOLANGCI_LINT) fmt --no-config --enable gofmt --enable goimports ); \
	done

test:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> testing $$dir"; \
		( cd "$$dir" && $(GO) test ./... ); \
	done

test-race:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> race testing $$dir"; \
		( cd "$$dir" && $(GO) test -race ./... ); \
	done

lint:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> linting $$dir"; \
		( cd "$$dir"; \
		  $(GOLANGCI_LINT) fmt --no-config --enable gofmt --enable goimports --diff; \
		  $(GO) vet ./...; \
		  $(GOLANGCI_LINT) run --no-config; \
		  $(NILAWAY) ./... ); \
	done

# Backward-compatible alias.
lint-all: lint

check:
	@set -eu; \
	for dir in $(GO_MOD_DIRS); do \
		echo "==> checking dependencies in $$dir"; \
		( cd "$$dir" && GOWORK=off $(GO) mod tidy -diff ); \
	done
	$(MAKE) lint
	$(MAKE) test-race

verify: check api-compat

api-compat:
	./scripts/check-api-compat.sh

bench:
	$(GO) test -run '^$$' -bench BenchmarkFramework -benchmem ./conformance/...

bench-5x:
	$(GO) test -run '^$$' -bench BenchmarkFramework -benchmem -count=5 ./conformance/...

tag:
	@test -n "$(TAG)" || { echo "TAG is required: make tag TAG=v0.0.1"; exit 1; }
	git tag -s $(TAG) -m "$(TAG)"
	git push origin --tags

tag-all:
	@test -n "$(TAG)" || { echo "TAG is required: make tag-all TAG=v0.0.1"; exit 1; }
	@set -eu; \
	for adapter in $(TAG_ADAPTERS); do \
		git tag -s "$$adapter/$(TAG)" -m "$$adapter/$(TAG)"; \
	done
	git push origin --tags

tag-delete:
	@test -n "$(TAG)" || { echo "TAG is required: make tag-delete TAG=v0.0.1"; exit 1; }
	-git tag -d $(TAG)
	@for adapter in $(TAG_ADAPTERS); do git tag -d "$$adapter/$(TAG)" || true; done
	-git push origin --delete $(TAG)
	@for adapter in $(TAG_ADAPTERS); do git push origin --delete "$$adapter/$(TAG)" || true; done

help:
	@printf '%s\n' \
	  'Targets:' \
	  '  deps-update                 update direct dependencies in all modules' \
	  '  tidy                        tidy all modules' \
	  '  fmt                         format all modules' \
	  '  test | test-race            test all modules' \
	  '  lint | lint-all             lint all modules' \
	  '  check                       run dependency, lint, and race checks' \
	  '  verify                      run check plus API compatibility validation' \
	  '  api-compat                  compare public APIs with the baseline tag' \
	  '  bench | bench-5x            run conformance benchmarks' \
	  '  tag TAG=v0.0.1              create and push the root tag' \
	  '  tag-all TAG=v0.0.1          create and push adapter tags' \
	  '  tag-delete TAG=v0.0.1       delete local and remote tags'
