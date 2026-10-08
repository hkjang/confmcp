#!/usr/bin/env bash
# confmcp 빌드 스크립트.
#
#   scripts/build.sh                 # 로컬 바이너리 빌드 (웹 콘솔 포함)
#   scripts/build.sh --image         # 도커 이미지 confmcp:v<버전> 빌드
#   scripts/build.sh --image --save  # 이미지 빌드 후 confmcp-v<버전>.tar.gz 생성
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="$(cat VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
IMAGE="confmcp:v${VERSION}"
TARBALL="confmcp-v${VERSION}.tar.gz"

BUILD_IMAGE=0
SAVE_IMAGE=0
for arg in "$@"; do
  case "$arg" in
    --image) BUILD_IMAGE=1 ;;
    --save) BUILD_IMAGE=1; SAVE_IMAGE=1 ;;
    *) echo "알 수 없는 옵션: $arg" >&2; exit 2 ;;
  esac
done

echo "== confmcp v${VERSION} (${COMMIT}) =="

if [ "$BUILD_IMAGE" -eq 0 ]; then
  echo "-- 웹 콘솔 빌드"
  (cd web && npm ci --no-audit --no-fund && npm run build)

  echo "-- 서버 빌드"
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/hkjang/confmcp/internal/version.Version=${VERSION} \
      -X github.com/hkjang/confmcp/internal/version.Commit=${COMMIT} \
      -X github.com/hkjang/confmcp/internal/version.BuildDate=${BUILD_DATE}" \
    -o bin/confmcp ./cmd/server
  echo "완료: bin/confmcp"
  exit 0
fi

echo "-- 도커 이미지 빌드: ${IMAGE}"
docker build \
  --build-arg "VERSION=${VERSION}" \
  --build-arg "COMMIT=${COMMIT}" \
  --build-arg "BUILD_DATE=${BUILD_DATE}" \
  -t "${IMAGE}" \
  .
echo "완료: ${IMAGE}"

if [ "$SAVE_IMAGE" -eq 1 ]; then
  echo "-- 이미지 저장: ${TARBALL}"
  docker save "${IMAGE}" | gzip -9 > "${TARBALL}"
  sha256sum "${TARBALL}" > "${TARBALL}.sha256"
  ls -lh "${TARBALL}"
  cat "${TARBALL}.sha256"
fi
