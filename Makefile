GO ?= go
GOLANGCI_LINT ?= golangci-lint
NILAWAY ?= nilaway

# Sampling for the three statistical benchmark targets. 12 is the smallest
# count that kept every row under +/-15% on the reference machine; 6 produced
# occasional rows above +/-50%, which benchstat cannot tell from a real change.
# Override for a quick look (BENCH_COUNT=4) or to match a report (18).
BENCH_COUNT ?= 12
BENCH_TIME ?= 200ms

GO_MOD_DIRS := . ginx fiberx echox hertzx stdx conformance
TAG_ADAPTERS := ginx fiberx echox hertzx stdx
DIRECT_DEPS_TEMPLATE := {{if and (not .Main) (not .Indirect) (not .Replace)}}{{.Path}}{{end}}

.DEFAULT_GOAL := check

# Local builds use the workspace to test all adapters against the root module.
ifneq ($(wildcard $(CURDIR)/go.work),)
export GOWORK := $(CURDIR)/go.work
endif

.PHONY: work deps-update tidy fmt test test-race lint lint-all check verify api-compat
.PHONY: bench bench-5x bench-adapter bench-suite bench-native bench-network golden tag tag-all tag-delete help prepare-release release-check

# The workspace is the only way the repo builds before a release: every adapter
# requires the published github.com/go-sphere/httpx, but they use symbols that
# are only in the local root module until it is tagged. go.work is gitignored,
# so CI regenerates it here — from GO_MOD_DIRS, so adding a module cannot leave
# CI resolving it from the proxy.
work:
	@test -f go.work || GOWORK= $(GO) work init
	@GOWORK=$(CURDIR)/go.work $(GO) work use $(GO_MOD_DIRS)
	@echo "==> workspace: $$(GOWORK=$(CURDIR)/go.work $(GO) work edit -json | grep -c '"DiskPath"' || true) modules"

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

# Rewrites the shared response contracts from the adapters, then verifies that
# every adapter still matches what was written: a change only one adapter
# agrees with fails the second pass instead of being frozen into the contract.
golden:
	@set -eu; \
	for adapter in $(TAG_ADAPTERS); do \
		echo "==> recording contracts from $$adapter"; \
		HTTPX_UPDATE_GOLDEN=1 $(GO) test ./conformance -run "TestHTTPXTestSuite/$$adapter" -count=1; \
	done; \
	echo "==> verifying every adapter against the recorded contracts"; \
	$(GO) test ./conformance -run TestHTTPXTestSuite -count=1

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
	$(MAKE) test
	$(MAKE) test-race

verify: check api-compat

api-compat:
	./scripts/check-api-compat.sh

bench:
	$(GO) test -run '^$$' -bench BenchmarkFramework -benchmem ./conformance/...

bench-5x:
	$(GO) test -run '^$$' -bench BenchmarkFramework -benchmem -count=5 ./conformance/...

# Native versus httpx, without sockets/client allocations. Compare saved runs
# with golang.org/x/perf/cmd/benchstat; benchmarks/README.md documents the setup.
bench-adapter:
	$(GO) test -run '^$$' -bench '^BenchmarkAdapter$$' -benchmem -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT) -cpu=4 ./conformance

# The shared scenario table (httpxtest) per adapter, driven through each
# framework's own dispatcher via Suite.Dispatch. Same sampling as bench-adapter
# (BENCH_COUNT/BENCH_TIME) so the two are comparable.
bench-suite:
	$(GO) test -run '^$$' -bench '^BenchmarkHTTPXTestSuite$$' -benchmem -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT) -cpu=4 ./conformance

# The shared scenario table again, but each scenario paired with a hand-written
# implementation on the raw framework (no httpx at all), so the delta is the
# price of the abstraction on identical behavior.
bench-native:
	$(GO) test -run '^$$' -bench '^BenchmarkNativeVsHTTPX$$' -benchmem -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT) -cpu=4 ./conformance

# Requires Vegeta on PATH or in ~/go/bin. No load-test dependencies enter go.mod.
bench-network:
	python3 benchmarks/network.py

tag:
	@test -n "$(TAG)" || { echo "TAG is required: make tag TAG=v0.0.1"; exit 1; }
	git tag -s $(TAG) -m "$(TAG)"
	git push origin --tags

# Publication is deliberately staged: root tag, dependency preparation,
# commit, consumer checks, and only then adapter tags. Dependency preparation
# bypasses GOPROXY and the module cache, reading the just-pushed root tag
# straight from GitHub; see the head of scripts/prepare-release.sh for why, and
# RELEASE_GOPROXY to override it.
prepare-release:
	@test -n "$(TAG)" || { echo "TAG is required"; exit 1; }
	GO="$(GO)" bash scripts/prepare-release.sh "$(TAG)"

release-check:
	@test -n "$(TAG)" || { echo "TAG is required"; exit 1; }
	GO="$(GO)" bash scripts/check-release.sh "$(TAG)"

tag-all: release-check
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
	  '  work                        create or refresh go.work from the module list' \
	  '  deps-update                 update direct dependencies in all modules' \
	  '  tidy                        tidy all modules' \
	  '  fmt                         format all modules' \
	  '  test | test-race            test all modules' \
	  '  lint | lint-all             lint all modules' \
	  '  check                       run dependency, lint, test, and race checks' \
	  '  verify                      run check plus API compatibility validation' \
	  '  api-compat                  compare public APIs with the baseline tag' \
	  '  golden                      rewrite and verify the shared response contracts' \
	  '  bench | bench-5x            run conformance benchmarks' \
	  '  bench-adapter               compare native and httpx allocations/latency' \
	  '  bench-suite                 measure the shared scenario table per adapter' \
	  '  bench-native                pair each scenario against a no-httpx implementation' \
	  '  (bench-adapter/suite/native take BENCH_COUNT=$(BENCH_COUNT) BENCH_TIME=$(BENCH_TIME))' \
	  '  bench-network               run fixed-rate Vegeta network comparison' \
	  '  prepare-release TAG=v0.0.5    update adapter dependencies after the root tag is published' \
	  '  release-check TAG=v0.0.5      test published dependencies without go.work' \
	  '  tag TAG=v0.0.1              create and push the root tag' \
	  '  tag-all TAG=v0.0.1          create and push adapter tags' \
	  '  tag-delete TAG=v0.0.1       delete local and remote tags'
