#!/usr/bin/env bash
# 로컬에 Go 가 없어도 도커로 go 명령을 실행합니다.  scripts/go.sh build ./...
set -euo pipefail
cd "$(dirname "$0")/.."
exec docker run --rm -i --network host \
  -v "$PWD":/src -w /src \
  -v confmcp-gomod:/go/pkg/mod -v confmcp-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  ${GO_DOCKER_ARGS:-} golang:1.26-alpine go "$@"
