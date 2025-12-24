GO ?= go

.PHONY: build test fmt fmt-check

build:
	$(GO) build

test:
	./scripts/test.sh

fmt:
	$(GO) fmt -w .

fmt-check:
	@if [ -n "$$($(GO) fmt -l .)" ]; then \
		echo "gofmt check failed; run gofmt -w ."; \
		exit 1; \
	fi
