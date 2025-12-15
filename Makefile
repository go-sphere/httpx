define lint_mode
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

.PHONY: lint
lint:
	$(call lint_mode,./)
	$(call lint_mode,./fiberx)
	$(call lint_mode,./ginx)
	$(call lint_mode,./hertzx)

TAG ?=

tags:
	@if [ -z "$(TAG)" ]; then echo "TAG not set. Use TAG=v0.0.1 make tags"; exit 1; fi
	git tag -a ${TAG} -m "$(TAG)"
	git tag -a ginx/$(TAG) -m "ginx $(TAG)"
	git tag -a fiberx/$(TAG) -m "fiberx $(TAG)"
	git tag -a hertzx/$(TAG) -m "hertzx $(TAG)"
	git push origin --tags
