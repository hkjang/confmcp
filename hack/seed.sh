#!/usr/bin/env bash
# Seed a running confmcp with the mock Confluence, sample users, mappings and
# policy so the console and MCP can be exercised end to end. Development only.
#
#   CONFMCP_URL=http://127.0.0.1:18088 MOCK_CONFLUENCE=http://127.0.0.1:18090/confluence hack/seed.sh
set -euo pipefail
B="${CONFMCP_URL:-http://127.0.0.1:18088}"
ADMIN="${CONFMCP_ADMIN:-admin}"
PASS="${CONFMCP_ADMIN_PASSWORD:-admin-password-1}"
MOCK="${MOCK_CONFLUENCE:-http://127.0.0.1:18090/confluence}"
SECRET="${MOCK_PLUGIN_SECRET:-mock-plugin-secret}"
J=$(mktemp)
trap 'rm -f "$J"' EXIT

curl -sf -c "$J" -X POST "$B/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN\",\"password\":\"$PASS\"}" > /dev/null
echo "관리자 로그인"

api() { curl -sf -b "$J" -X "$1" "$B$2" -H 'Content-Type: application/json' ${3:+-d "$3"}; }

api PUT /api/admin/settings/confluence "{\"value\":{\"instanceId\":\"default\",\"baseUrl\":\"$MOCK\",
 \"serviceUsername\":\"confmcp-svc\",\"timeoutSec\":15,\"toolTimeoutSec\":30,\"executionMode\":\"service\",
 \"allowUserCredential\":true,\"trustSameDirectory\":true},\"secrets\":{\"servicePassword\":\"svc-password\"}}" > /dev/null
echo "Confluence 연결 설정"

api PUT /api/admin/settings/permission_plugin \
  "{\"value\":{\"mode\":\"plugin\",\"pluginBaseUrl\":\"\",\"cacheTtlSec\":0,\"timeoutSec\":10},\"secrets\":{\"pluginSecret\":\"$SECRET\"}}" > /dev/null
echo "권한 플러그인 설정"

api POST /api/admin/test/confluence > /dev/null && echo "연결 시험 (버전 감지)"

api PUT /api/admin/settings/mcp "{\"value\":{\"serverName\":\"confmcp\",\"resourceUrl\":\"$B\",\"maxResponseKb\":512,\"exposeHighLevelTools\":true,\"allowRawCql\":false}}" > /dev/null

for rule in \
  '{"kind":"space","pattern":"DEV","effect":"allow","priority":10,"note":"개발팀 공간"}' \
  '{"kind":"space","pattern":"OPS","effect":"allow","priority":10,"note":"운영 공간"}' \
  '{"kind":"space","pattern":"ARCH","effect":"allow","priority":10,"riskCap":"WRITE","note":"아키텍처 (이동·휴지통 불가)"}' \
  '{"kind":"space","pattern":"HR","effect":"deny","priority":1,"note":"인사 문서는 AI 접근 금지"}' \
  '{"kind":"content_type","pattern":"attachment","effect":"allow","priority":50,"note":"첨부 허용"}' \
  '{"kind":"content_type","pattern":"page","effect":"allow","priority":50,"note":""}' \
  '{"kind":"content_type","pattern":"blogpost","effect":"allow","priority":50,"note":""}' \
  '{"kind":"content_type","pattern":"comment","effect":"allow","priority":50,"note":""}'; do
  api POST /api/admin/policy/rules "$rule" > /dev/null
done
echo "접근 정책"

for u in \
  '{"username":"alice","password":"alice-password-1","displayName":"김앨리스","email":"alice@example.com","roles":["confluence-mcp-writer"]}' \
  '{"username":"bob","password":"bob-password-12","displayName":"박밥","email":"bob@example.com","roles":["confluence-mcp-approver"]}' \
  '{"username":"carol","password":"carol-password1","displayName":"이캐롤","email":"carol@example.com","roles":["confluence-mcp-reader"]}' \
  '{"username":"dave","password":"dave-password-1","displayName":"정데이브","email":"dave@example.com","roles":["confluence-mcp-reader"]}'; do
  api POST /api/admin/users "$u" > /dev/null || true
done
echo "사용자"

for pair in alice:alice bob:bob carol:carol admin:admin; do
  api POST /api/admin/identity/mappings "{\"username\":\"${pair%%:*}\",\"confluenceUsername\":\"${pair##*:}\"}" > /dev/null || true
done
# dave is deactivated in Confluence: the mapping attempt is refused and shows up as an error.
api POST /api/admin/identity/mappings '{"username":"dave","confluenceUsername":"dave"}' > /dev/null 2>&1 || true
echo "사용자 매핑"

api PATCH /api/admin/tools/confluence_move_page '{"enabled":true}' > /dev/null
api PATCH /api/admin/tools/confluence_remove_labels '{"enabled":true}' > /dev/null
echo "도구 활성화"
echo "완료: $B"
