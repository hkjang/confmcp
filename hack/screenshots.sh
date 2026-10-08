#!/usr/bin/env bash
# Run the browser tooling inside the official Playwright image, against a
# confmcp reachable from Docker (scripts/dev.sh up). On Docker Desktop with
# WSL, the host's loopback is reached through host.docker.internal.
#
#   hack/screenshots.sh          # all screens → docs/screenshots
#   hack/screenshots.sh icons    # logo PNGs and social card
set -euo pipefail
cd "$(dirname "$0")/.."
BASE="${CONFMCP_URL:-http://host.docker.internal:18088}"
IMAGE="${PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.55.0-noble}"
SCRIPT=hack/screenshots.mjs
ARGS=(--base "$BASE" --out docs/screenshots)
if [ "${1:-}" = "icons" ]; then SCRIPT=hack/icons.mjs; ARGS=(); fi
exec docker run --rm --add-host=host.docker.internal:host-gateway \
  -v "$PWD":/work -w /work "$IMAGE" node "$SCRIPT" "${ARGS[@]}"
