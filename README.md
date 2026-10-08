<div align="center">

<img src="docs/assets/logo-192.png" alt="confmcp" width="96" height="96">

# confmcp

**Confluence MCP 게이트웨이**: AI 가 Confluence 를 읽고 쓰더라도, 요청한 사람이 볼 수 있는 문서만 다룹니다.

[문서](https://hkjang.github.io/confmcp/) ·
[사용자 가이드](https://hkjang.github.io/confmcp/guide-user.html) ·
[관리자 가이드](https://hkjang.github.io/confmcp/guide-admin.html) ·
[릴리스](https://github.com/hkjang/confmcp/releases)

</div>

---

confmcp 는 **Confluence Server 7.2.0** 앞에 두는 MCP(Model Context Protocol) 게이트웨이입니다.
Keycloak 으로 사용자를 인증하고, 매 호출마다 **요청자 본인의 Confluence 권한**을 작업별(열람·작성·편집·댓글·첨부·이동·삭제)로
확인한 뒤에만 문서에 접근합니다. 문서 변경은 사람이 콘솔에서 diff 를 보고 승인해야 적용됩니다.

```
최종 허용 = 요청자의 Confluence 유효 권한
          ∩ MCP 접근 정책(공간 · 페이지 트리 · 콘텐츠 유형)
          ∩ 호출자 역할 · 스코프
          ∩ 필요한 승인
          ∩ 실행 자격증명의 실제 권한
```

## 주요 기능

| 영역 | 내용 |
|---|---|
| 인증 | Keycloak OIDC (Authorization Code + PKCE, state·nonce), **사일런트 SSO**(`prompt=none`), 로컬 비상 계정 |
| MCP OAuth | confmcp 가 **직접 OAuth 2.1 인가 서버** 역할: 동적 등록(RFC 7591), PKCE, 리소스 바인딩, refresh 회전·재사용 탐지. Keycloak 에는 웹 클라이언트 하나만 등록 |
| 식별 | `issuer + sub` ↔ `instance + userKey`. 첫 매핑은 같은 디렉터리 선언 · 관리자 확인 · 본인 Confluence 로그인 중 하나로만 성립 |
| 권한 | **권한 플러그인**(서비스 계정 + Confluence 내부 판정) 또는 **사용자 위임**. 판정 불가는 차단(fail-closed), REST ACL 추정 없음 |
| 정책 | 공간 명시적 허용 · 차단 우선, 페이지 트리, 콘텐츠 유형, 위험도 상한, 정책 세대 |
| 본문 | `body.storage` 원본 보존, 매크로 미실행(`body.view` 금지), Markdown/텍스트 변환과 손실 보고 |
| 변경 | 변경안(diff) → 승인 → 버전·원문 해시·정책 세대 바인딩 → 원자적 소비 → 멱등 실행 → 재조회 검증 |
| 오류 | `VERSION_CONFLICT`, `APPROVAL_STALE`, `OUTCOME_UNKNOWN`, `UNSUPPORTED_CONTENT` 등 명확한 코드 |
| 도구 | 28종 (P0 조회 17 · P1 작성·첨부 7 · P2 고위험 4) |
| 키 | 개인 API 키 발급 · 회전(유예) · 폐기, 변경 가능한 역할 → 스코프 체계, 매 호출 재평가 |
| AI | Anthropic · OpenAI 호환, 기본 스트리밍(SSE), 최대 **512k** 토큰, 허용 모델 목록 |
| 감사 | 요청자와 실제 실행 계정 분리 기록, 비밀값·본문 제외, 호출량·오류율·지연 통계 |
| 배포 | 환경변수 4개, 단일 도커 이미지, 외부 CDN·폰트 없이 **오프라인망** 운영 |

## 빠른 시작 (오프라인)

```bash
# 1) 릴리스 이미지 적재
docker load -i confmcp-v0.1.1.tar.gz

# 2) 환경변수 (네 개뿐입니다)
cp deploy/.env.example deploy/.env
$EDITOR deploy/.env          # ENCRYPTION_KEY=$(openssl rand -base64 32)

# 3) 기동 (PostgreSQL 이미지는 따로 반입하거나 기존 DB 를 쓰십시오)
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
curl -fsS http://localhost:8080/healthz
```

| 환경변수 | 설명 |
|---|---|
| `DATABASE_URL` (또는 `POSTGRES_DSN`) | PostgreSQL DSN |
| `BOOTSTRAP_ADMIN` | 최초 관리자 아이디 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 최초 관리자 비밀번호 |
| `ENCRYPTION_KEY` | 저장 비밀값 암호화 키 (32바이트: hex 64자, base64 44자 또는 평문 32자) |

나머지 설정(Keycloak, Confluence 연결, 권한 플러그인, 정책, 도구, AI, 한도, 화면)은 **모두 관리자 콘솔**에서 입력합니다.
비밀값은 AES-256-GCM 으로 암호화해 PostgreSQL 에 저장합니다.

> `ENCRYPTION_KEY` 를 잃으면 저장된 Client Secret · 서비스 계정 비밀번호 · 플러그인 비밀 · 사용자 위임 자격증명을 복호화할 수 없습니다.
> 데이터베이스 백업과 **분리해서** 보관하십시오.

## Keycloak 설정 (한 번)

`deploy/keycloak/` 의 JSON 을 가져오면 됩니다.

1. 클라이언트 `confmcp`: OpenID Connect, Client authentication **ON**, Standard flow, PKCE S256,
   Valid redirect URI `https://<confmcp 주소>/auth/oidc/callback`
2. 렐름 역할 `confluence-mcp-reader | writer | approver | admin` 을 사용자·그룹에 부여
3. 콘솔 → 인증 · SSO 에 Issuer, Client ID, Client Secret 입력 → **연결 시험**이 등록할 값을 그대로 보여 줍니다

MCP 클라이언트를 위한 Keycloak 추가 설정은 **없습니다**. confmcp 가 MCP 클라이언트의 인가 서버이고, 사용자는 위 클라이언트로 로그인합니다.

## MCP 클라이언트 연결

### OAuth (권장): URL 하나

```bash
claude mcp add --transport http confmcp https://confmcp.example.internal/mcp
```

```json
{ "mcpServers": { "confmcp": { "type": "http", "url": "https://confmcp.example.internal/mcp" } } }
```

| 단계 | 요청 | confmcp 응답 |
|---|---|---|
| 1 | `POST /mcp` (자격증명 없음) | `401` + `WWW-Authenticate: Bearer resource_metadata="…/.well-known/oauth-protected-resource/mcp"` |
| 2 | 보호 리소스 메타데이터 | `authorization_servers: [confmcp]` |
| 3 | `/.well-known/oauth-authorization-server` | issuer = confmcp, authorize·token·register·revoke 엔드포인트 |
| 4 | `POST /oauth/register` | 공개 클라이언트 등록 (루프백·https·전용 스킴 redirect) |
| 5 | `/oauth/authorize` (PKCE S256) | 로그인(Keycloak SSO, 세션 있으면 자동) → 동의 화면 한 번 |
| 6 | `POST /oauth/token` | confmcp 토큰 (1시간, 리소스 바인딩), refresh 30일·회전 |

### API 키

```json
{
  "mcpServers": {
    "confmcp": {
      "type": "http",
      "url": "https://confmcp.example.internal/mcp",
      "headers": { "Authorization": "Bearer confmcp_xxx_yyy" }
    }
  }
}
```

## Confluence 권한 판정

| 모드 | 동작 | 준비 |
|---|---|---|
| **권한 플러그인** (기본) | REST 는 서비스 계정, 요청자 판정은 Confluence 내부 권한 엔진 | `plugin/` 의 JAR 설치, 공유 비밀 설정 (HMAC · nonce 서명) |
| 사용자 위임 | 요청자 본인 자격증명으로 호출 → Confluence 가 직접 적용 | 사용자가 [내 Confluence]에서 계정 연결 |

플러그인 경로: `POST /rest/confmcp/1.0/permissions/check`, `/permissions/batch`, `/users`, `GET /health`.
열람 제한은 하위로 상속되고 편집 제한은 상속되지 않는 Confluence 의미 그대로 판정합니다.

## MCP 도구

<details>
<summary><strong>P0 조회 (17)</strong></summary>

`confluence_me` `confluence_my_permissions` `confluence_spaces` `confluence_get_space` `confluence_pages`
`confluence_get_page` `confluence_children` `confluence_ancestors` `confluence_search` `confluence_comments`
`confluence_attachments` `confluence_labels` `confluence_blogposts` `confluence_get_blogpost`
`confluence_recent_changes` `confluence_page_context` `confluence_space_context`
</details>

<details>
<summary><strong>P1 작성 · 첨부 (7)</strong>: 승인 필요</summary>

`confluence_prepare_change` `confluence_create_page` `confluence_update_page` `confluence_add_comment`
`confluence_add_labels` `confluence_upload_attachment` `confluence_download_attachment`
</details>

<details>
<summary><strong>P2 (4)</strong>: 기본 비활성, 이동·휴지통은 별도 승인자</summary>

`confluence_create_blogpost` `confluence_remove_labels` `confluence_move_page` `confluence_trash_page`
</details>

기존 문서 수정 방식: `append` · `prepend` · `replace_text`(텍스트만, 매크로·코드 보존) · `replace_section` ·
`replace_storage`(제거 요소 감지) · `replace_markdown`(구조 없는 문서만). 문서를 Markdown 으로 바꿔 통째로 덮어쓰지 않습니다.

## 개발

```bash
scripts/dev.sh up        # PostgreSQL + 모의 Confluence 7.2 + confmcp + 샘플 데이터
scripts/go.sh test ./... # 로컬에 Go 가 없으면 도커로 실행
cd web && npm run dev    # 콘솔 개발 서버 (프록시 → 127.0.0.1:18088)
hack/e2e.sh              # 인수 시나리오 자동 점검 (모의 환경)
scripts/release.sh       # confmcp:v<버전> 이미지 → confmcp-v<버전>.tar.gz → GitHub 릴리스
```

| 경로 | 책임 |
|---|---|
| `cmd/server` | 시작과 구성 |
| `internal/auth` · `apikey` · `identity` · `oauthserver` | 인증, 키, Confluence 사용자 매핑, MCP OAuth 인가 서버 |
| `internal/confluence` | Adapter 인터페이스, 7.2.0 REST 클라이언트, 기능 탐지 |
| `internal/permission` | 플러그인 · 사용자 위임 판정 |
| `internal/policy` | 공간 · 페이지 트리 · 콘텐츠 유형 정책 |
| `internal/content` | storage 파싱(XXE 안전), 변환, 원문 보존 패치, diff |
| `internal/approval` · `operation` | 변경안 · 승인, 멱등 실행 기록 |
| `internal/tools` · `mcp` | 도구 정의, 인가 파이프라인, Streamable HTTP |
| `internal/attachment` | 업로드 보관 · 검증 |
| `web` | React + Mantine 사용자 · 관리자 콘솔 |
| `plugin` | Confluence 7.2.0 권한 플러그인 |
| `hack/mockconfluence` | 모의 Confluence (제한 페이지 · 충돌 시나리오) |

모의 서버로 검증한 범위와 실제 Confluence 7.2.0 에서 확인할 범위는 [관리자 가이드의 검증 기록](https://hkjang.github.io/confmcp/guide-admin.html#verification)에 구분해 두었습니다.

## 라이선스

Apache-2.0
