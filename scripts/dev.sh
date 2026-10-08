#!/usr/bin/env bash
# 개발용 기동 스크립트: PostgreSQL 컨테이너 + 모의 Confluence 7.2 + confmcp 서버.
# 로컬에 Go 가 없으면 scripts/go.sh 가 도커로 빌드합니다.
#
#   scripts/dev.sh up     # 전부 기동하고 샘플 데이터를 넣습니다
#   scripts/dev.sh down   # 정리합니다
set -euo pipefail
cd "$(dirname "$0")/.."

PG_CONTAINER=confmcp-dev-pg
PG_PORT=${PG_PORT:-55488}
CONFMCP_PORT=${CONFMCP_PORT:-18088}
MOCK_PORT=${MOCK_PORT:-18090}
RUN_DIR=${RUN_DIR:-/tmp/confmcp-dev}
GO=go
command -v go >/dev/null 2>&1 || GO=scripts/go.sh

case "${1:-up}" in
  up)
    mkdir -p "$RUN_DIR"
    if ! docker ps --format '{{.Names}}' | grep -q "^${PG_CONTAINER}$"; then
      docker run -d --name "${PG_CONTAINER}" \
        -e POSTGRES_USER=confmcp -e POSTGRES_PASSWORD=confmcp -e POSTGRES_DB=confmcp \
        -p "127.0.0.1:${PG_PORT}:5432" postgres:17-alpine >/dev/null
      sleep 5
    fi
    (cd web && npm run build)
    $GO build -o bin/confmcp ./cmd/server
    $GO build -o bin/mockconfluence ./hack/mockconfluence
    cp bin/confmcp bin/mockconfluence "$RUN_DIR/"

    MOCK_ADDR="127.0.0.1:${MOCK_PORT}" nohup "$RUN_DIR/mockconfluence" > "$RUN_DIR/mock.log" 2>&1 &
    echo $! > "$RUN_DIR/mock.pid"
    DATABASE_URL="postgres://confmcp:confmcp@127.0.0.1:${PG_PORT}/confmcp?sslmode=disable" \
    BOOTSTRAP_ADMIN=admin BOOTSTRAP_ADMIN_PASSWORD=admin-password-1 \
    ENCRYPTION_KEY=0123456789abcdef0123456789abcdef CONFMCP_ADDR="127.0.0.1:${CONFMCP_PORT}" \
      nohup "$RUN_DIR/confmcp" > "$RUN_DIR/confmcp.log" 2>&1 &
    echo $! > "$RUN_DIR/confmcp.pid"
    for _ in $(seq 1 40); do curl -sf "127.0.0.1:${CONFMCP_PORT}/healthz" >/dev/null && break; sleep 0.5; done
    CONFMCP_URL="http://127.0.0.1:${CONFMCP_PORT}" MOCK_CONFLUENCE="http://127.0.0.1:${MOCK_PORT}/confluence" hack/seed.sh
    echo "콘솔: http://127.0.0.1:${CONFMCP_PORT}  (admin / admin-password-1, alice / alice-password-1)"
    ;;
  down)
    for p in confmcp mock; do
      [ -f "$RUN_DIR/$p.pid" ] && kill "$(cat "$RUN_DIR/$p.pid")" 2>/dev/null || true
      rm -f "$RUN_DIR/$p.pid"
    done
    docker rm -f "${PG_CONTAINER}" >/dev/null 2>&1 || true
    ;;
  *)
    echo "사용법: scripts/dev.sh up|down" >&2
    exit 2
    ;;
esac
