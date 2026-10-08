#!/usr/bin/env bash
# confmcp 릴리스 스크립트.
#
# 오프라인망에서 운영할 수 있는 서비스 도커 이미지만 tar.gz 로 묶어 GitHub
# 릴리스에 올립니다. 이미지 이름은 confmcp:v<버전>, 산출물은 confmcp-v<버전>.tar.gz
# 하나입니다.
#
#   scripts/release.sh            # VERSION 파일의 버전으로 릴리스
#   scripts/release.sh --dry-run  # 빌드와 패키징까지만 수행
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="$(cat VERSION)"
TAG="v${VERSION}"
IMAGE="confmcp:${TAG}"
TARBALL="confmcp-${TAG}.tar.gz"
DRY_RUN=0
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

echo "== confmcp ${TAG} 릴리스 =="

if [ -n "$(git status --porcelain)" ]; then
  echo "작업 트리가 깨끗하지 않습니다. 커밋 후 다시 실행하십시오." >&2
  git status --short >&2
  exit 1
fi

scripts/build.sh --save
SHA="$(cut -d' ' -f1 "${TARBALL}.sha256")"
SIZE="$(du -h "${TARBALL}" | cut -f1)"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "건식 실행이므로 태그와 릴리스를 생성하지 않습니다. (${TARBALL}, ${SIZE}, ${SHA})"
  exit 0
fi

if ! git rev-parse "${TAG}" >/dev/null 2>&1; then
  git tag -a "${TAG}" -m "confmcp ${TAG}"
fi
git push origin "${TAG}"

NOTES_FILE="$(mktemp)"
{
  echo "오프라인망에서 운영할 수 있는 confmcp 서비스 도커 이미지입니다. 웹 콘솔·폰트·마이그레이션이 모두 이미지에 들어 있어 실행 중 외부 다운로드가 없습니다."
  echo
  echo "## 설치"
  echo
  echo '```bash'
  echo "# 1) 이미지 적재"
  echo "docker load -i ${TARBALL}"
  echo
  echo "# 2) 실행 (환경변수는 네 개뿐입니다)"
  echo "docker run -d --name confmcp -p 8080:8080 \\"
  echo "  -e DATABASE_URL='postgres://confmcp:<비밀번호>@<DB 호스트>:5432/confmcp?sslmode=disable' \\"
  echo "  -e BOOTSTRAP_ADMIN=admin -e BOOTSTRAP_ADMIN_PASSWORD='<관리자 비밀번호>' \\"
  echo "  -e ENCRYPTION_KEY=\"\$(openssl rand -base64 32)\" \\"
  echo "  ${IMAGE}"
  echo '```'
  echo
  echo "그다음 관리 콘솔(http://<호스트>:8080)에서 Keycloak, Confluence 연결, 권한 플러그인, 접근 정책을 설정합니다."
  echo "docker compose 구성과 업그레이드 절차는 [관리자 가이드](https://hkjang.github.io/confmcp/guide-admin.html)를 참고하십시오."
  echo
  echo "| 항목 | 값 |"
  echo "|---|---|"
  echo "| 이미지 | \`${IMAGE}\` |"
  echo "| 파일 | \`${TARBALL}\` (${SIZE}) |"
  echo "| SHA-256 | \`${SHA}\` |"
  echo
  echo "확인: \`curl -s http://<호스트>:8080/api/version\` 또는 로그인 화면·프로필 메뉴의 버전 표시"
  echo
  echo "문서: https://hkjang.github.io/confmcp/"
} > "${NOTES_FILE}"

if gh release view "${TAG}" >/dev/null 2>&1; then
  gh release upload "${TAG}" "${TARBALL}" --clobber
else
  gh release create "${TAG}" "${TARBALL}" --title "confmcp ${TAG}" --notes-file "${NOTES_FILE}"
fi
rm -f "${NOTES_FILE}"
echo "릴리스 완료: ${TAG}"
