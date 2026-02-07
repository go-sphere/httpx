.PHONY: test bench bench-5x lint lint-all tag tag-all tag-delete help

TAG ?=
LINT_DIRS := . ginx fiberx echox hertzx conformance
TAG_ADAPTERS := ginx fiberx echox hertzx

test:
	go test ./conformance/... -v

bench:
	go test -run '^$$' -bench BenchmarkFramework -benchmem ./conformance/...

bench-5x:
	go test -run '^$$' -bench BenchmarkFramework -benchmem -count=5 ./conformance/...

lint: lint-all

lint-all:
	@set -e; \
	for dir in $(LINT_DIRS); do \
		cd $$dir; \
		go fmt ./...; \
		go vet ./...; \
		go get ./...; \
		go test ./...; \
		go mod tidy; \
		golangci-lint fmt --no-config --enable gofmt,goimports; \
		golangci-lint run --no-config --fix; \
		nilaway ./...; \
		cd - >/dev/null; \
	done

tag:
	@test -n "$(TAG)" || (echo "TAG is required: make tag TAG=v0.0.1" && exit 1)
	git tag -s $(TAG) -m "$(TAG)"
	git push origin --tags

tag-all:
	@test -n "$(TAG)" || (echo "TAG is required: make tag-all TAG=v0.0.1" && exit 1)
	@set -e; \
	for adapter in $(TAG_ADAPTERS); do \
		git tag -s $$adapter/$(TAG) -m "$$adapter/$(TAG)"; \
	done
	git push origin --tags

tag-delete:
	@test -n "$(TAG)" || (echo "TAG is required: make tag-delete TAG=v0.0.1" && exit 1)
	-git tag -d $(TAG)
	@for adapter in $(TAG_ADAPTERS); do \
		git tag -d $$adapter/$(TAG) || true; \
	done
	-git push origin --delete $(TAG)
	@for adapter in $(TAG_ADAPTERS); do \
		git push origin --delete $$adapter/$(TAG) || true; \
	done

help:
	@printf '%s\n' \
	  'Targets:' \
	  '  test                         run conformance tests' \
	  '  bench                        run framework benchmarks' \
	  '  bench-5x                     run framework benchmarks 5 times' \
	  '  lint | lint-all              run checks for all modules' \
	  '  tag TAG=v0.0.1               create and push root tag' \
	  '  tag-all TAG=v0.0.1           create and push adapter tags' \
	  '  tag-delete TAG=v0.0.1        delete local and remote tags'
