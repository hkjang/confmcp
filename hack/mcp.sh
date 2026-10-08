#!/usr/bin/env bash
# Call an MCP tool on a running confmcp:  hack/mcp.sh <api-key> <tool> '<json args>'
set -euo pipefail
B="${CONFMCP_URL:-http://127.0.0.1:18088}"
KEY="$1"; TOOL="$2"; ARGS="${3:-{\}}"
curl -s "$B/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"$TOOL\",\"arguments\":$ARGS}}" \
  | (command -v jq >/dev/null && jq -r '.result.content[0].text // .' || cat)
