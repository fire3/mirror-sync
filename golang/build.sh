#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

BINARY="mirror-sync"
GOFLAGS="${GOFLAGS:-}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

echo "==> Building ${BINARY}..."

GOPROXY="${GOPROXY}" go build ${GOFLAGS} -o "${BINARY}" ./cmd/mirror-sync/

echo "==> Done: ./${BINARY}"
ls -lh "${BINARY}"
