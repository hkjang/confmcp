// Thin typed wrapper over the confmcp HTTP API.

export class ApiError extends Error {
  status: number
  code?: string

  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const isForm = typeof FormData !== 'undefined' && init?.body instanceof FormData
  const res = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body && !isForm ? { 'Content-Type': 'application/json' } : {}),
      ...(init?.headers ?? {}),
    },
  })

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let body: unknown = undefined
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = { error: text }
    }
  }

  if (!res.ok) {
    const envelope = body as { error?: string; code?: string } | undefined
    throw new ApiError(res.status, envelope?.error ?? `요청이 실패했습니다 (${res.status})`, envelope?.code)
  }
  return body as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: body === undefined ? undefined : JSON.stringify(body) }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PATCH', body: body === undefined ? undefined : JSON.stringify(body) }),
  del: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  upload: <T>(path: string, form: FormData) => request<T>(path, { method: 'POST', body: form }),
}

/** qs builds a query string from defined, non-empty values. */
export function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

// ---------- shared types ----------

export interface VersionInfo {
  name: string
  version: string
  commit: string
  buildDate: string
}

export interface UISettings {
  serviceName: string
  tagline: string
  primaryColor: string
  fontScale: number
  defaultTheme: string
  locale: string
  loginNotice: string
}

export interface PublicConfig {
  ui: UISettings
  version: VersionInfo
  auth: {
    keycloakEnabled: boolean
    silentSso: boolean
    startUrl: string
    silentUrl: string
    localLogin: boolean
  }
}

export interface User {
  id: number
  username: string
  email: string
  displayName: string
  keycloakSub?: string
  keycloakIssuer?: string
  isServiceAdmin: boolean
  roles: string[]
  active: boolean
  source: string
  createdAt: string
  lastLoginAt?: string
  hasPassword: boolean
}

export interface Mapping {
  id: number
  keycloakIssuer: string
  keycloakSub: string
  keycloakUsername: string
  instanceId: string
  confluenceUserKey: string
  confluenceUsername: string
  confluenceDisplay: string
  confluenceEmail: string
  mappingType: string
  evidence: string
  active: boolean
  lastError?: string
  mappedAt: string
  verifiedAt?: string
}

export interface MappingError {
  id: number
  keycloakSub: string
  keycloakUsername: string
  reason: string
  occurrences: number
  firstSeenAt: string
  occurredAt: string
}

export interface MappingHistory {
  id: number
  keycloakSub: string
  action: string
  confluenceUserKey?: string
  confluenceUsername?: string
  actor?: string
  note?: string
  occurredAt: string
}

export interface Prefs {
  theme: string
  fontScale: number
  locale: string
}

export interface ConnectionInfo {
  permissionMode: string
  executionMode: string
  allowUserCredential: boolean
  needsUserCredential: boolean
  configured: boolean
}

export interface Me {
  user: User
  confluence: Mapping | null
  mappingError?: string
  roles: string[]
  scopes: string[]
  authMode: string
  isServiceAdmin: boolean
  canApprove: boolean
  prefs: Prefs
  ui: UISettings
  version: VersionInfo
  connection: ConnectionInfo
}

export interface ApiKey {
  id: string
  userId: number
  username?: string
  name: string
  prefix: string
  role: string
  scopes: string[]
  status: string
  rotatedFrom?: string
  rotationDueAt?: string
  expiresAt?: string
  lastUsedAt?: string
  createdAt: string
  revokedAt?: string
  revokedReason?: string
}

export interface IssuedKey {
  key: ApiKey
  secret: string
  graceUntil?: string
}

export interface KeyRole {
  name: string
  description: string
  scopes: string[]
  builtin: boolean
  createdAt?: string
  updatedAt?: string
}

export interface ScopeInfo {
  name: string
  label: string
  description: string
  risk: string
}

export interface ToolRecord {
  name: string
  title: string
  description: string
  group: string
  risk: string
  requiredPermission: string
  scope: string
  priority: string
  highLevel: boolean
  write: boolean
  enabled: boolean
  requiresApproval: boolean
  minRole: string
  inputSchema?: Record<string, unknown>
  updatedAt?: string
}

export interface ApprovalPreview {
  action: string
  summary: string
  spaceKey?: string
  title?: string
  oldTitle?: string
  targetUrl?: string
  parentId?: string
  parentTitle?: string
  newParentId?: string
  baseVersion?: number
  diff?: string
  newBody?: string
  bodyFormat?: string
  labels?: string[]
  attachment?: Record<string, unknown>
  warnings?: string[]
  visibility?: string
  stats?: Record<string, number>
}

export interface ApprovalRequest {
  id: string
  keycloakSub: string
  userId?: number
  username: string
  instanceId: string
  toolName: string
  risk: string
  argumentsHash: string
  arguments?: Record<string, unknown>
  resource: string
  spaceKey?: string
  targetId?: string
  targetVersion?: number
  targetHash?: string
  policyGeneration: number
  preview?: ApprovalPreview
  requiresApprover: boolean
  status: string
  decidedBy?: string
  decisionNote?: string
  createdAt: string
  expiresAt: string
  approvedAt?: string
  consumedAt?: string
}

export interface ApprovalDetail {
  request: ApprovalRequest
  own: boolean
  canApprove: boolean
  canReject: boolean
}

export interface OperationRecord {
  id: string
  idempotencyKey: string
  keycloakSub: string
  username: string
  toolName: string
  argumentsHash: string
  approvalId?: string
  targetId?: string
  status: string
  upstreamId?: string
  resultVersion?: number
  errorCode?: string
  message?: string
  executedBy?: string
  createdAt: string
  updatedAt: string
  resolvedBy?: string
  resolvedAt?: string
}

export interface PolicyRule {
  id: number
  kind: string
  pattern: string
  spaceKey?: string
  effect: string
  riskCap?: string
  priority: number
  note: string
  createdAt: string
  updatedAt?: string
}

export interface PolicyVerdict {
  allowed: boolean
  riskCap?: string
  matchedBy?: string
  reason?: string
  generation: number
}

export interface PermissionDecision {
  target: { kind: string; spaceKey?: string; id?: string }
  results: Record<string, string>
  reasons?: Record<string, string>
  spaceKey?: string
  contentType?: string
  status?: string
  source: string
  evaluatedAt: string
}

export interface PermissionCheck {
  ok: boolean
  data: {
    target: { kind: string; spaceKey?: string; id?: string }
    operations: Record<string, { verdict: string; label: string; reason?: string }>
    source: string
    evaluatedAt: string
    note?: string
    title?: string
    spaceKey?: string
    type?: string
    policy?: PolicyVerdict
  }
}

export interface AuditEntry {
  id: number
  occurredAt: string
  category: string
  action: string
  requestId?: string
  keycloakSub?: string
  keycloakUsername?: string
  confluenceUserKey?: string
  confluenceUsername?: string
  executedAs?: string
  mcpClient?: string
  authMode?: string
  toolName?: string
  spaceKey?: string
  contentId?: string
  contentVersion?: number
  decision?: string
  approvalId?: string
  success: boolean
  errorCode?: string
  message?: string
  latencyMs: number
  ip?: string
  detail?: Record<string, unknown>
}

export interface AuditPage {
  values: AuditEntry[]
  total: number
}

export interface HealthComponent {
  ok: boolean
  detail: string
  skipped?: boolean
}

export interface StatsPoint {
  at: string
  calls: number
  failures: number
  p95Ms: number
}

export interface Stats {
  calls: number
  failures: number
  denied: number
  errorRate: number
  p50Ms: number
  p95Ms: number
  avgMs: number
  series: StatsPoint[]
}

export interface SetupWarning {
  level: 'error' | 'warning' | 'info'
  message: string
  link: string
}

export interface Dashboard {
  version: VersionInfo
  counts: Record<string, number>
  stats24h?: Stats
  topTools?: { tool: string; calls: number; failures: number }[]
  recentAudit?: AuditEntry[]
  health: Record<string, HealthComponent>
  warnings: SetupWarning[]
}

export interface SettingsEnvelope<T> {
  value: T
  secrets?: Record<string, boolean>
  limits?: Record<string, number>
  defaults?: T
}

export interface Upload {
  uploadId: string
  filename: string
  mediaType: string
  size: number
  sha256: string
  createdAt: string
  expiresAt: string
  consumedAt?: string
}

export interface OAuthGrant {
  id: string
  clientId: string
  clientName: string
  userId: number
  username: string
  scopes: string[]
  resource: string
  createdAt: string
  lastUsedAt?: string
  revokedAt?: string
}

export interface OAuthClient {
  clientId: string
  clientName: string
  redirectUris: string[]
  softwareId?: string
  createdAt: string
  lastUsedAt?: string
  activeGrants: number
}

export interface McpSession {
  id: string
  username: string
  client: string
  authMode: string
  ip: string
  createdAt: string
  lastSeenAt: string
  closedAt?: string
}

export interface McpConfig {
  mcpUrl: string
  oauth: { claudeCode: string; json: unknown }
  apiKey: { claudeCode: string; json: unknown }
}

export interface MyConfluence {
  mapping: Mapping | null
  mappingError?: string
  credential?: {
    userId: number
    confluenceUsername: string
    confluenceUserKey?: string
    hasSecret: boolean
    verifiedAt?: string
    updatedAt?: string
  }
  permissionMode: string
  executionMode: string
  baseUrl: string
  serviceAccount: string
  allowUserCredential: boolean
  trustSameDirectory: boolean
  detectedVersion?: string
}

export interface RoleInfo {
  name: string
  label: string
  description: string
}
