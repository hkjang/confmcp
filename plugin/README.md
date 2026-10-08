# confmcp Permission Resolver (Confluence Server 7.2.0 플러그인)

confmcp 게이트웨이가 **Confluence 자체 권한 엔진의 판정**을 그대로 사용할 수 있도록,
서명된 요청에 한해 유효 권한을 알려 주는 읽기 전용 플러그인입니다.

- 판정은 전부 Confluence 의 `PermissionManager` / `SpacePermissionManager` 가 합니다.
  플러그인은 규칙을 다시 구현하지 않습니다. 보기 제한의 상속(상위 페이지의 보기 제한은
  하위로 상속, 편집 제한은 상속되지 않음), 그룹 권한, 익명/관리자 예외가 모두 Confluence
  화면에서와 똑같이 적용됩니다.
- 판정 대상은 **요청 본문의 `userKey` 사용자**입니다. HTTP 호출자(게이트웨이)의 권한이
  아닙니다.
- 권한을 바꾸는 기능은 없습니다.

이 플러그인이 없어도 confmcp 는 위임(delegated) 모드로 동작할 수 있지만, 사용자별
자격증명 없이 페이지 제한·상속까지 Confluence 와 100% 동일하게 판정하려면 플러그인
모드를 권장합니다.

## 제공 API

모든 경로는 `<Confluence 기본 URL>/rest/confmcp/1.0/` 아래에 있습니다
(예: `https://wiki.example.com/confluence/rest/confmcp/1.0/health`).

| 메서드 | 경로 | 인증 | 설명 |
|---|---|---|---|
| GET | `/health` | 게이트웨이 서명 | 플러그인·Confluence 버전 |
| POST | `/permissions/check` | 게이트웨이 서명 | 대상 1건 판정 |
| POST | `/permissions/batch` | 게이트웨이 서명 | 대상 최대 100건 판정 (요청 순서 그대로 응답) |
| POST | `/users` | 게이트웨이 서명 | 사용자 조회 (활성/비활성, 디렉터리) |
| GET / PUT / DELETE | `/config` | Confluence **시스템 관리자** 로그인 | 공유 비밀값 설정 |

### 판정 요청과 응답

```http
POST /confluence/rest/confmcp/1.0/permissions/batch
Content-Type: application/json
X-Confmcp-Timestamp: 1760000000
X-Confmcp-Nonce: 3f2a...
X-Confmcp-Signature: 9c1e...

{"userKey":"8a7f808a6f1e2b3c016f1e2b5a0a0001",
 "checks":[{"target":{"kind":"content","id":"65538"},"operations":["read","edit","comment"]},
           {"target":{"kind":"space","spaceKey":"DOC"},"operations":["create"]}],
 "correlationId":"req-123"}
```

```json
{"userKey":"8a7f808a6f1e2b3c016f1e2b5a0a0001","userStatus":"active","evaluatedAt":"2026-10-08T07:00:00Z",
 "results":[
  {"target":{"kind":"content","id":"65538"},
   "content":{"id":"65538","type":"page","status":"current","spaceKey":"DOC"},
   "results":{"read":"allow","edit":"deny","comment":"allow"},
   "reasons":{"edit":"PAGE_EDIT_RESTRICTED"}},
  {"target":{"kind":"space","spaceKey":"DOC"},"space":{"key":"DOC"},
   "results":{"create":"allow"},"reasons":{}}]}
```

`/permissions/check` 는 `checks` 대신 `target`, `operations` 를 받고, 위 결과 객체 하나에
`userKey`·`userStatus`·`evaluatedAt` 을 더해 돌려줍니다.

### 연산별 판정 기준

| 연산 | 콘텐츠 대상 (`kind: content`) | 스페이스 대상 (`kind: space`) |
|---|---|---|
| `read` | `hasPermission(VIEW, content)` | `hasPermission(VIEW, space)` |
| `edit` | `hasPermission(EDIT, content)` | `hasCreatePermission(space, Page)` |
| `move` | `hasPermission(EDIT, content)` (댓글은 이동 불가) | `hasCreatePermission(space, Page)` |
| `delete` | `hasPermission(REMOVE, content)` | 스페이스 권한 `REMOVEPAGE` |
| `create` | `hasCreatePermission(space, Page 또는 BlogPost)` | `hasCreatePermission(space, Page)` |
| `comment` | `hasCreatePermission(content, Comment)` | `hasCreatePermission(space, Comment)` |
| `attach` | `hasCreatePermission(page, Attachment)` (첨부파일이면 그 첨부파일이 속한 페이지) | 스페이스 권한 `CREATEATTACHMENT` |

- 콘텐츠 ID 는 페이지·블로그·댓글은 숫자, 첨부파일은 `att12345` 형식도 받습니다.
- 결과 값은 `allow` / `deny` / `unknown` 입니다. 알 수 없는 연산은 `unknown`
  (`UNKNOWN_OPERATION`), 판정 중 예외가 나면 해당 연산만 `unknown` (`CHECK_FAILED`) 입니다.
- 휴지통·초안·이전 버전 콘텐츠는 `read` 만 판정하고, 나머지 연산은 `deny`
  (`CONTENT_NOT_CURRENT`) 입니다.
- 비활성 사용자는 `userStatus: "deactivated"` 와 함께 모든 연산이 `deny`
  (`USER_DEACTIVATED`) 입니다. 존재하지 않는 `userKey` 는 404 입니다.
- 사유 코드: `NOT_VISIBLE`, `PAGE_EDIT_RESTRICTED`, `NO_SPACE_EDIT`, `NO_EDIT_PERMISSION`,
  `NO_DELETE_PERMISSION`, `NO_SPACE_CREATE`, `NO_COMMENT_PERMISSION`, `NO_SPACE_COMMENT`,
  `NO_ATTACH`, `NO_SPACE_DELETE`, `NOT_SUPPORTED`, `CONTENT_NOT_CURRENT`, `USER_DEACTIVATED`,
  `INVALID_TARGET`, `UNKNOWN_OPERATION`, `CHECK_FAILED`.

## 빌드

JDK 8 과 Maven 3.6+ 가 필요합니다. Atlassian 공개 저장소
(`https://packages.atlassian.com/mvn/maven-external/`)에 접근할 수 있어야 합니다.

```bash
cd plugin
mvn package            # 또는 Atlassian SDK: atlas-package
# 결과물: target/confmcp-permission-plugin-0.1.0.jar
```

로컬에 Maven 이 없다면 Docker 로 빌드할 수 있습니다.

```bash
docker run --rm -v "$PWD/plugin":/src -v confmcp-m2:/root/.m2 -w /src \
  maven:3.9-eclipse-temurin-8 mvn -B package
```

## 설치 (UPM)

1. Confluence 에 시스템 관리자로 로그인합니다.
2. **설정(톱니바퀴) → 앱 관리(Manage apps)** 로 이동합니다.
3. **앱 업로드(Upload app)** 를 눌러 `confmcp-permission-plugin-0.1.0.jar` 를 올립니다.
4. 목록에 **confmcp Permission Resolver** 가 "사용" 상태로 보이면 설치 완료입니다.

Confluence Data Center 라면 UPM 이 모든 노드에 설치합니다. 재시작은 필요 없습니다.

## 공유 비밀값 설정

게이트웨이와 플러그인은 같은 비밀값(16~512자)을 공유합니다. 둘 중 한 쪽에서 정한 값을
다른 쪽에 똑같이 넣으면 됩니다.

1. **confmcp 관리 콘솔 → 권한 판정** 에서 **플러그인 공유 비밀값**을 입력하거나 `생성`으로
   만들고 저장합니다. 생성한 값은 저장 직후 한 번만 보여 주므로 바로 복사해 둡니다.
2. 같은 값을 Confluence 시스템 관리자 계정으로 플러그인에 저장합니다.

```bash
curl -u admin:관리자비밀번호 \
  -X PUT 'https://wiki.example.com/confluence/rest/confmcp/1.0/config' \
  -H 'Content-Type: application/json' \
  -H 'X-Atlassian-Token: no-check' \
  -d '{"secret":"<콘솔에 넣은 것과 같은 값>"}'
# → {"configured":true,"source":"pluginSettings"}

# 설정 여부 확인 (값 자체는 어떤 API 로도 돌려주지 않습니다)
curl -u admin:관리자비밀번호 'https://wiki.example.com/confluence/rest/confmcp/1.0/config'
```

- 값은 Confluence 전역 플러그인 설정(`com.hkjang.confmcp.secret`)에 저장되므로
  Data Center 의 모든 노드가 같은 값을 씁니다.
- 플러그인 설정에 값이 없으면 JVM 시스템 속성 `-Dconfmcp.secret=...`
  (예: `bin/setenv.sh` 의 `CATALINA_OPTS`)을 대신 사용합니다.
- 둘 다 없으면 서명 엔드포인트는 모두 `503 {"message":"secret not configured"}` 로 닫혀
  있습니다.
- 비밀값을 바꿀 때는 플러그인과 콘솔을 함께 바꿉니다. 바꾸는 사이의 요청은 401 이 됩니다.

## 동작 확인

가장 간단한 방법은 **confmcp 관리 콘솔 → 권한 판정 → 연결 시험** 입니다. 콘솔이
서명된 `GET /rest/confmcp/1.0/health` 를 보내고 다음과 같은 응답을 보여 줍니다.

```json
{"status":"ok","pluginVersion":"0.1.0","confluenceVersion":"7.2.0","buildNumber":"8402"}
```

직접 확인하려면 서명을 만들어 호출합니다. 서명 경로에는 **컨텍스트 경로가 포함**됩니다.

```bash
BASE='https://wiki.example.com'; CTX='/confluence'; SECRET='<공유 비밀값>'
P="$CTX/rest/confmcp/1.0/health"
TS=$(date +%s); NONCE=$(openssl rand -hex 16)
BODY_SHA=$(printf '' | openssl dgst -sha256 -hex | awk '{print $NF}')
SIG=$(printf 'GET\n%s\n%s\n%s\n%s' "$P" "$BODY_SHA" "$TS" "$NONCE" \
      | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $NF}')
curl -s "$BASE$P" -H "X-Confmcp-Timestamp: $TS" -H "X-Confmcp-Nonce: $NONCE" \
     -H "X-Confmcp-Signature: $SIG"
```

서명 없이 호출하면 `401 {"message":"missing signature headers"}` 가 나와야 정상입니다.

| 증상 | 원인 |
|---|---|
| 404 | 플러그인 미설치·비활성, 또는 콘솔의 플러그인 URL 에 컨텍스트 경로 누락 |
| 503 `secret not configured` | 플러그인 쪽 비밀값 미설정 |
| 401 `invalid gateway signature` | 비밀값 불일치, 또는 프록시가 경로를 바꿈 |
| 401 `timestamp outside the allowed window` | 게이트웨이와 Confluence 서버 시계 차이 5분 초과 (NTP 확인) |
| HTML 응답 | 리버스 프록시/SSO 가 `/rest/confmcp/` 를 로그인 화면으로 돌림 → 해당 경로 예외 처리 |

## 제거와 롤백

- **롤백(플러그인 없이 운영)**: confmcp 관리 콘솔 → 권한 판정에서 해석 모드를 플러그인이
  아닌 모드로 바꾸고 저장합니다. 그 다음 플러그인을 비활성화하거나 제거해도 됩니다.
  (모드를 먼저 바꾸지 않으면 fail-closed 설정에 따라 도구 호출이 거부될 수 있습니다.)
- **비활성화**: 앱 관리 → confmcp Permission Resolver → **사용 안 함(Disable)**.
  API 가 즉시 404 가 됩니다.
- **제거**: 앱 관리 → confmcp Permission Resolver → **제거(Uninstall)**.
- **이전 버전으로 되돌리기**: 이전 JAR 를 다시 **앱 업로드**하면 덮어씁니다.
  공유 비밀값은 플러그인 설정에 남아 있으므로 다시 넣을 필요가 없습니다.
- **비밀값 삭제**: 제거 전에 저장된 값까지 지우려면
  `curl -u admin:... -X DELETE -H 'X-Atlassian-Token: no-check' .../rest/confmcp/1.0/config`.

## 보안

- **HMAC 서명**: 판정·사용자·상태 API 는 모두
  `HMAC-SHA256(secret, METHOD \n 요청 경로(컨텍스트 경로 포함, 이스케이프된 그대로) \n
  sha256(본문) \n timestamp \n nonce)` 서명이 맞아야 응답합니다. 본문 해시까지 서명하므로
  본문을 바꾸면 거부됩니다. 비교는 상수 시간으로 합니다.
- **재전송 방지**: 타임스탬프가 서버 시각과 ±5분을 넘으면 거부하고, 한 번 쓴 nonce 는
  10분 동안 기억해 같은 요청의 재전송을 거부합니다. nonce 캐시는 크기 제한이 있으며,
  가득 차면 오래된 값을 버리는 대신 요청을 거부(503)합니다. 서명이 맞는 요청만 nonce 를
  기록하므로 서명 없는 트래픽으로 캐시를 채울 수 없습니다. 캐시는 노드별이므로
  Data Center 에서는 같은 요청을 다른 노드로 10분 안에 재전송하는 것을 막지 못합니다 —
  게이트웨이와 Confluence 사이 구간은 TLS 로 보호하십시오.
- **다른 사용자 조회 차단**: 판정 API 는 Atlassian 인증 계층에서는 익명 허용이지만
  서명이 없으면 아무 것도 답하지 않습니다. 따라서 익명 사용자나 일반 Confluence 사용자는
  다른 사람의 권한·계정 정보를 물어볼 수 없습니다. 비밀값 설정 API(`/config`)는 로그인한
  시스템 관리자만 쓸 수 있고, 비밀값을 되돌려 주는 API 는 없습니다.
- **존재 여부 비노출**: 존재하지 않는 콘텐츠와 사용자가 볼 수 없는 콘텐츠는 똑같이
  모든 연산 `deny` / `NOT_VISIBLE` 로 답하고 `content` 정보를 넣지 않습니다. 스페이스도
  마찬가지입니다.
- **오류 정보 최소화**: 응답에는 스택 트레이스나 예외 메시지가 들어가지 않습니다.
  판정 실패는 `unknown` + 사유 코드, 그 밖의 오류는 `{"message":"internal error"}` 이며
  자세한 내용은 Confluence 로그(`com.hkjang.confmcp`)에만 남습니다.
- **읽기 전용**: 권한·콘텐츠를 바꾸는 코드가 없습니다.
- 비밀값은 Confluence 전역 플러그인 설정(DB)에 평문으로 저장됩니다. DB 백업 접근 권한을
  관리하고, 노출이 의심되면 콘솔과 플러그인에서 함께 교체하십시오.
