#!/usr/bin/env bash
set -euo pipefail

if [ -n "$(gofmt -l .)" ]; then
  echo "gofmt check failed; run gofmt -w ." >&2
  exit 1
fi

go test ./...
