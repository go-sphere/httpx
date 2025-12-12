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
