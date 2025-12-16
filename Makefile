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

tags-root:
	@if [ -z "$(TAG)" ]; then echo "TAG not set. Use TAG=v0.0.1 make tags"; exit 1; fi
	git tag -s ${TAG} -m "$(TAG)"
	git push origin --tags
	echo "GOPROXY=direct GONOSUMDB=github.com/go-sphere/httpx go get github.com/go-sphere/httpx@$(TAG)"

tags-subs:
	@if [ -z "$(TAG)" ]; then echo "TAG not set. Use TAG=v0.0.1 make tags"; exit 1; fi
	git tag -s ginx/$(TAG) -m "ginx/$(TAG)"
	git tag -s fiberx/$(TAG) -m "fiberx/$(TAG)"
	git tag -s hertzx/$(TAG) -m "hertzx/$(TAG)"
	git push origin --tags
	echo "GOPROXY=direct GONOSUMDB=github.com/go-sphere/httpx go get github.com/go-sphere/httpx/ginx@$(TAG)"
	echo "GOPROXY=direct GONOSUMDB=github.com/go-sphere/httpx go get github.com/go-sphere/httpx/fiberx@$(TAG)"
	echo "GOPROXY=direct GONOSUMDB=github.com/go-sphere/httpx go get github.com/go-sphere/httpx/hertzx@$(TAG)"

del-tags:
	@if [ -z "$(TAG)" ]; then echo "TAG not set. Use TAG=v0.0.1 make del-tags"; exit 1; fi
	git tag -d ${TAG} || true
	git tag -d ginx/$(TAG) || true
	git tag -d fiberx/$(TAG) || true
	git tag -d hertzx/$(TAG) || true
	git push origin --delete ${TAG} || true
	git push origin --delete ginx/$(TAG) || true
	git push origin --delete fiberx/$(TAG) || true
	git push origin --delete hertzx/$(TAG) || true