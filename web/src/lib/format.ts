// Shared Korean-first formatting helpers and labels.

const dateTime = new Intl.DateTimeFormat('ko-KR', { dateStyle: 'medium', timeStyle: 'short' })
const dateOnly = new Intl.DateTimeFormat('ko-KR', { dateStyle: 'medium' })

export function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return '—'
  return dateTime.format(d)
}

export function formatDate(value?: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return '—'
  return dateOnly.format(d)
}

export function formatRelative(value?: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return '—'
  const diffMs = Date.now() - d.getTime()
  const abs = Math.abs(diffMs)
  const units: [number, string][] = [
    [1000 * 60 * 60 * 24 * 365, '년'],
    [1000 * 60 * 60 * 24 * 30, '개월'],
    [1000 * 60 * 60 * 24, '일'],
    [1000 * 60 * 60, '시간'],
    [1000 * 60, '분'],
  ]
  for (const [ms, label] of units) {
    if (abs >= ms) {
      const n = Math.floor(abs / ms)
      return diffMs >= 0 ? `${n}${label} 전` : `${n}${label} 후`
    }
  }
  return diffMs >= 0 ? '방금' : '곧'
}

export function formatNumber(value?: number | null): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  return value.toLocaleString('ko-KR')
}

export function formatBytes(n?: number | null): string {
  if (n === undefined || n === null) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function formatPercent(v?: number | null): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—'
  return `${(v * 100).toFixed(1)}%`
}

export const riskLabels: Record<string, string> = {
  READ: '조회',
  WRITE: '작성',
  EXECUTE: '고위험',
  ADMIN: '관리',
}

export const riskColors: Record<string, string> = {
  READ: 'teal',
  WRITE: 'yellow',
  EXECUTE: 'red',
  ADMIN: 'grape',
}

export const roleLabels: Record<string, string> = {
  'confluence-mcp-reader': '조회자',
  'confluence-mcp-writer': '작성자',
  'confluence-mcp-approver': '승인자',
  'confluence-mcp-admin': '관리자',
}

export const roleOptions = Object.entries(roleLabels).map(([value, label]) => ({
  value,
  label: `${label} (${value})`,
}))

export const opLabels: Record<string, string> = {
  read: '열람',
  create: '작성',
  edit: '편집',
  comment: '댓글',
  attach: '첨부',
  move: '이동',
  delete: '삭제',
}

export const opOrder = ['read', 'create', 'edit', 'comment', 'attach', 'move', 'delete']

export const verdictLabels: Record<string, string> = { allow: '허용', deny: '거부', unknown: '확인 불가' }
export const verdictColors: Record<string, string> = { allow: 'teal', deny: 'red', unknown: 'gray' }

export const keyStatusLabels: Record<string, string> = {
  active: '정상',
  revoked: '폐기',
  expired: '만료',
  rotation_due: '회전 필요',
  grace: '유예 중',
}

export const keyStatusColors: Record<string, string> = {
  active: 'teal',
  revoked: 'gray',
  expired: 'orange',
  rotation_due: 'yellow',
  grace: 'blue',
}

export const approvalStatusLabels: Record<string, string> = {
  pending: '승인 대기',
  approved: '승인됨',
  rejected: '거절됨',
  expired: '만료',
  consumed: '적용됨',
}

export const approvalStatusColors: Record<string, string> = {
  pending: 'yellow',
  approved: 'teal',
  rejected: 'red',
  expired: 'gray',
  consumed: 'blue',
}

export const operationStatusLabels: Record<string, string> = {
  pending: '대기',
  executing: '실행 중',
  succeeded: '성공',
  failed: '실패',
  outcome_unknown: '결과 불명확',
}

export const operationStatusColors: Record<string, string> = {
  pending: 'gray',
  executing: 'blue',
  succeeded: 'teal',
  failed: 'red',
  outcome_unknown: 'orange',
}

export const actionLabels: Record<string, string> = {
  create_page: '페이지 생성',
  create_blogpost: '블로그 작성',
  update_page: '페이지 수정',
  add_comment: '댓글 추가',
  add_labels: '라벨 추가',
  remove_labels: '라벨 제거',
  upload_attachment: '첨부 올리기',
  move_page: '페이지 이동',
  trash_page: '휴지통 이동',
}

export const toolGroupLabels: Record<string, string> = {
  identity: '식별',
  discovery: '탐색',
  content: '본문',
  search: '검색',
  context: '컨텍스트',
  change: '변경',
  attachment: '첨부',
}

export const auditCategoryLabels: Record<string, string> = {
  auth: '인증',
  tool: '도구 호출',
  write: '쓰기 작업',
  denied: '거부',
  approval: '승인',
  admin: '관리',
  key: '키',
  error: '오류',
  ai: 'AI',
  file: '파일',
}

export const errorCodeLabels: Record<string, string> = {
  AUTH_REQUIRED: '인증 필요',
  IDENTITY_UNMAPPED: '매핑 없음',
  POLICY_DENIED: '정책 차단',
  PERMISSION_DENIED: '권한 거부',
  PERMISSION_UNKNOWN: '권한 확인 불가',
  APPROVAL_REQUIRED: '승인 필요',
  APPROVAL_STALE: '승인 무효',
  APPROVAL_DENIED: '승인 거부',
  VERSION_CONFLICT: '버전 충돌',
  OUTCOME_UNKNOWN: '결과 불명확',
  UPSTREAM_AUTH_FAILED: 'Confluence 인증 실패',
  UPSTREAM_UNAVAILABLE: 'Confluence 응답 없음',
  UNSUPPORTED_CONTENT: '보존 불가 내용',
  LIMIT_EXCEEDED: '한도 초과',
  TOOL_DISABLED: '도구 비활성',
  ROLE_DENIED: '역할 부족',
  SCOPE_DENIED: '스코프 부족',
  UNKNOWN_TOOL: '알 수 없는 도구',
  BAD_ARGUMENTS: '인자 오류',
  INTERNAL_ERROR: '내부 오류',
  BAD_CREDENTIALS: '로그인 실패',
  RATE_LIMITED: '요청 제한',
  IP_DENIED: '주소 차단',
}

export const errorCodeOptions = Object.entries(errorCodeLabels).map(([value, label]) => ({
  value,
  label: `${label} (${value})`,
}))

/** resultLabel renders an audit outcome in Korean, falling back to the code. */
export function resultLabel(success: boolean, errorCode?: string): string {
  if (success) return '성공'
  if (!errorCode) return '실패'
  return errorCodeLabels[errorCode] ?? errorCode
}

export const healthLabels: Record<string, string> = {
  database: '데이터베이스',
  keycloak: 'Keycloak',
  confluence: 'Confluence',
  permission: '권한 판정',
  ai: 'AI',
}

export const evidenceLabels: Record<string, string> = {
  same_directory: '같은 디렉터리 (자동)',
  admin_confirmed: '관리자 확인',
  delegated_auth: '본인 로그인 확인',
}

export const permissionModeLabels: Record<string, string> = {
  plugin: '서비스 계정 + 권한 플러그인',
  delegated: '사용자 위임 (본인 자격증명)',
}

export const executionModeLabels: Record<string, string> = {
  service: '서비스 계정으로 실행',
  delegated: '요청자 본인 자격증명으로 실행',
}

export const policyKindLabels: Record<string, string> = {
  space: '공간',
  page_tree: '페이지 트리',
  content_type: '콘텐츠 유형',
}

export const contentTypeLabels: Record<string, string> = {
  page: '페이지',
  blogpost: '블로그',
  comment: '댓글',
  attachment: '첨부',
}

export const authModeLabels: Record<string, string> = {
  session: '브라우저 세션',
  oauth: 'OAuth',
  apikey: 'API 키',
}
