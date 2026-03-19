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

# requires ImageMagick: winget install ImageMagick
assets/jot.ico:
	magick assets/jot-logo.png -define icon:auto-resize="256,128,64,48,32,16" assets/jot.ico

# requires goversioninfo: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
resource.syso: assets/jot.ico
	goversioninfo -icon=assets/jot.ico -o resource.syso

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o jot.exe .