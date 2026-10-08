import { Alert, Badge, Button, Divider, Group, List, Paper, Stack, Table, Text } from '@mantine/core'
import {
  IconCircleCheck,
  IconCircleX,
  IconKey,
  IconPlugConnected,
  IconRefresh,
  IconRoute,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import {
  CopyField,
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  SaveBar,
  Section,
  TableScroll,
} from '../../components/ui'
import { api } from '../../lib/api'
import { formatDateTime, formatRelative, roleOptions } from '../../lib/format'
import { useConnectivityTest, useSettingsGroup } from '../../lib/useSettingsGroup'

interface KeycloakSettings {
  enabled: boolean
  issuer: string
  clientId: string
  redirectUrl: string
  postLogoutUrl: string
  scopes: string[]
  usernameClaim: string
  roleClaimPath: string
  adminRole: string
  silentSso: boolean
  silentSsoMaxAgeSec: number
  autoProvision: boolean
  insecureSkipTls: boolean
  requireRole: string
  defaultRole: string
  mcpOauthEnabled: boolean
  mcpAutoConsent: boolean
  acceptKeycloakTokens: boolean
  mcpAudiences: string[]
}

interface KeycloakTestResult {
  ok: boolean
  error?: string
  warnings?: string[]
  redirectUri?: string
  postLogoutUri?: string
  authUrl?: string
  tokenUrl?: string
  register?: {
    clientType: string
    validRedirectUris: string[]
    webOrigins: string[]
    validPostLogoutRedirectUris: string[]
    roles: string[]
  }
}

interface McpOAuthTestResult {
  ok: boolean
  error?: string
  issuer?: string
  mcpUrl?: string
  protectedResourceMetadata?: string
  authorizationServerMetadata?: string
  claudeCode?: string
  registeredClients?: number
  checks?: { name: string; ok: boolean; detail: string }[]
}

interface TraceEvent {
  at: string
  kind: string
  method: string
  path: string
  status: number
  ip: string
  userAgent: string
  detail?: string
}

interface TraceResponse {
  since: string
  trustProxyHeaders: boolean
  events: TraceEvent[]
}

const traceKindLabels: Record<string, string> = {
  challenge: '인증 요구 (401)',
  'resource-metadata': '리소스 메타데이터',
  'server-metadata': '인가 서버 메타데이터',
  register: '클라이언트 등록',
  'unknown-well-known': '알 수 없는 well-known',
}

const ssoFields: Field<KeycloakSettings>[] = [
  {
    kind: 'switch',
    key: 'enabled',
    label: 'Keycloak SSO 사용',
    description: '끄면 로컬 계정으로만 로그인합니다.',
    span: 12,
  },
  {
    kind: 'text',
    key: 'issuer',
    label: 'Issuer URL',
    description: 'realm 의 issuer 주소입니다.',
    placeholder: 'https://sso.example.internal/realms/company',
  },
  { kind: 'text', key: 'clientId', label: 'Client ID', placeholder: 'confmcp' },
  {
    kind: 'secret',
    secretKey: 'clientSecret',
    label: 'Client Secret',
  },
  {
    kind: 'text',
    key: 'redirectUrl',
    label: 'Redirect URI',
    description: '비우면 접속 주소로 자동 계산합니다. 입력하면 /auth/oidc/callback 으로 끝나야 합니다.',
    placeholder: 'https://confmcp.example.internal/auth/oidc/callback',
  },
  {
    kind: 'text',
    key: 'postLogoutUrl',
    label: '로그아웃 후 이동 URL',
    description: '로그아웃 뒤 돌아올 주소입니다. Keycloak 의 Valid post logout redirect URIs 에 등록하십시오.',
    placeholder: 'https://confmcp.example.internal/login',
  },
  {
    kind: 'tags',
    key: 'scopes',
    label: '요청 스코프',
    description: '입력 후 Enter. 비우면 openid, profile, email 을 사용합니다.',
  },
  {
    kind: 'text',
    key: 'usernameClaim',
    label: '사용자명 클레임',
    description: '기본값 preferred_username',
    placeholder: 'preferred_username',
  },
  {
    kind: 'text',
    key: 'roleClaimPath',
    label: '역할 클레임 경로',
    description: '점으로 구분합니다. 예: realm_access.roles, resource_access.confmcp.roles',
    placeholder: 'realm_access.roles',
  },
  {
    kind: 'text',
    key: 'adminRole',
    label: '서비스 관리자 역할',
    description: '이 역할을 가진 사용자는 관리 콘솔에 접근합니다.',
    placeholder: 'confluence-mcp-admin',
  },
  {
    kind: 'select',
    key: 'defaultRole',
    label: '기본 역할',
    description: 'confluence-mcp-* 역할이 하나도 없는 사용자에게 부여합니다.',
    options: roleOptions,
  },
  {
    kind: 'text',
    key: 'requireRole',
    label: '필수 역할',
    description: '입력하면 이 역할이 있는 사용자만 로그인할 수 있습니다. 비우면 제한하지 않습니다.',
  },
  {
    kind: 'switch',
    key: 'silentSso',
    label: '사일런트 SSO',
    description: 'Keycloak 세션이 있으면 로그인 화면 없이 바로 들어옵니다.',
  },
  {
    kind: 'number',
    key: 'silentSsoMaxAgeSec',
    label: '사일런트 SSO 최대 인증 경과 (초)',
    description: '0 이면 제한하지 않습니다.',
    min: 0,
    max: 86400 * 30,
  },
  {
    kind: 'switch',
    key: 'autoProvision',
    label: '첫 로그인 시 계정 자동 생성',
    description: '끄면 관리자가 미리 만든 사용자만 로그인할 수 있습니다.',
  },
  {
    kind: 'switch',
    key: 'insecureSkipTls',
    label: 'TLS 인증서 검증 생략',
    description: '시험 환경에서만 사용하십시오. 운영에서는 내부 CA 를 신뢰 저장소에 추가하십시오.',
  },
]

const mcpFields: Field<KeycloakSettings>[] = [
  {
    kind: 'switch',
    key: 'mcpOauthEnabled',
    label: 'MCP OAuth 사용',
    description: 'MCP 클라이언트가 브라우저 로그인으로 연결할 수 있게 합니다. 끄면 API 키로만 연결합니다.',
  },
  {
    kind: 'switch',
    key: 'mcpAutoConsent',
    label: '동의 화면 생략',
    description: '로그인한 사용자에게 연결 동의 화면을 보이지 않습니다. 끄는 것을 권장합니다.',
  },
  {
    kind: 'switch',
    key: 'acceptKeycloakTokens',
    label: 'Keycloak 발급 토큰도 허용',
    description: 'Keycloak 에 직접 설정한 클라이언트를 위한 옵션입니다. audience 가 아래 목록에 있어야 합니다.',
  },
  {
    kind: 'tags',
    key: 'mcpAudiences',
    label: '허용 audience',
    description: 'Keycloak 발급 토큰을 허용할 때만 사용합니다. azp 만으로는 허용하지 않습니다.',
    placeholder: '예: confmcp-mcp',
  },
]

export function AdminAuthPage() {
  const { query, draft, setDraft, secrets, setSecrets, secretPresence, save } =
    useSettingsGroup<KeycloakSettings>('keycloak')
  const kcTest = useConnectivityTest('keycloak')
  const oauthTest = useConnectivityTest('mcp-oauth')
  const trace = useQuery({
    queryKey: ['admin', 'mcp-oauth-trace'],
    queryFn: () => api.get<TraceResponse>('/api/admin/mcp-oauth/trace'),
  })

  const kc = kcTest.data as KeycloakTestResult | undefined
  const oauth = oauthTest.data as McpOAuthTestResult | undefined
  const mcpFieldsShown = draft?.acceptKeycloakTokens ? mcpFields : mcpFields.filter((f) => f.kind !== 'tags')

  return (
    <>
      <PageHeader
        title="인증"
        description="콘솔 로그인(Keycloak SSO)과 MCP 클라이언트 연결용 OAuth 를 설정합니다."
      />

      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}

      {draft ? (
        <>
          <Section
            title="Keycloak SSO"
            description="Keycloak 에 confidential 클라이언트 하나를 등록하고 값을 입력하십시오. 저장한 뒤 연결 시험으로 등록 값을 확인할 수 있습니다."
          >
            <SettingsForm
              fields={ssoFields}
              value={draft}
              onChange={setDraft}
              secrets={secrets}
              onSecretChange={setSecrets}
              secretPresence={secretPresence}
            />
            <SaveBar
              onSave={() => save.mutate()}
              saving={save.isPending}
              extra={
                <Button
                  variant="default"
                  leftSection={<IconPlugConnected size={18} />}
                  loading={kcTest.isPending}
                  onClick={() => kcTest.mutate(undefined)}
                >
                  연결 시험
                </Button>
              }
            />
            {kcTest.error ? (
              <Stack mt="md">
                <ErrorBlock error={kcTest.error} title="연결 시험 실패" />
              </Stack>
            ) : null}
            {kc ? <KeycloakResult result={kc} /> : null}
          </Section>

          <Section
            title="MCP OAuth"
            description="confmcp 자체가 MCP 클라이언트(Claude Code 등)의 OAuth 인가 서버입니다. 사용자는 위의 같은 Keycloak 클라이언트로 로그인하므로 Keycloak 에 클라이언트를 추가로 만들 필요가 없습니다."
          >
            <SettingsForm
              fields={mcpFieldsShown}
              value={draft}
              onChange={setDraft}
              secrets={secrets}
              onSecretChange={setSecrets}
              secretPresence={secretPresence}
            />
            <SaveBar
              onSave={() => save.mutate()}
              saving={save.isPending}
              extra={
                <Button
                  variant="default"
                  leftSection={<IconRoute size={18} />}
                  loading={oauthTest.isPending}
                  onClick={() => oauthTest.mutate(undefined)}
                >
                  MCP OAuth 점검
                </Button>
              }
            />
            {oauthTest.error ? (
              <Stack mt="md">
                <ErrorBlock error={oauthTest.error} title="점검 실패" />
              </Stack>
            ) : null}
            {oauth ? <McpOAuthResult result={oauth} /> : null}
          </Section>
        </>
      ) : null}

      <Section
        title="최근 OAuth 탐색 기록"
        description="MCP 클라이언트가 이 서버에 보낸 OAuth 탐색·등록 요청입니다. 연결이 안 될 때 어느 단계에서 멈췄는지 확인하십시오."
        actions={
          <Button
            size="xs"
            variant="default"
            leftSection={<IconRefresh size={16} />}
            loading={trace.isFetching}
            onClick={() => void trace.refetch()}
          >
            새로고침
          </Button>
        }
      >
        {trace.isLoading ? <LoadingBlock /> : null}
        {trace.error ? <ErrorBlock error={trace.error} /> : null}
        {trace.data ? (
          <>
            <Text size="sm" c="dimmed" mb="sm">
              {formatDateTime(trace.data.since)} 이후 기록 · 프록시 헤더 신뢰{' '}
              {trace.data.trustProxyHeaders ? '켜짐' : '꺼짐'}
            </Text>
            {trace.data.events.length === 0 ? (
              <EmptyState label="기록된 OAuth 탐색 요청이 없습니다" />
            ) : (
              <TableScroll minWidth={880}>
                <Table striped highlightOnHover verticalSpacing="xs" fz="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>시각</Table.Th>
                      <Table.Th>단계</Table.Th>
                      <Table.Th>요청</Table.Th>
                      <Table.Th>상태</Table.Th>
                      <Table.Th>IP</Table.Th>
                      <Table.Th>클라이언트</Table.Th>
                      <Table.Th>내용</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {trace.data.events.map((e, i) => (
                      <Table.Tr key={`${e.at}-${i}`}>
                        <Table.Td style={{ whiteSpace: 'nowrap' }}>
                          <Text size="sm">{formatDateTime(e.at)}</Text>
                          <Text size="xs" c="dimmed">
                            {formatRelative(e.at)}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="light" color="gray">
                            {traceKindLabels[e.kind] ?? e.kind}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                            {e.method} {e.path}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="light" color={e.status >= 400 ? (e.status === 401 ? 'yellow' : 'red') : 'teal'}>
                            {e.status}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {e.ip}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="xs" c="dimmed" lineClamp={2} maw={200}>
                            {e.userAgent || '—'}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                            {e.detail || '—'}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </TableScroll>
            )}
          </>
        ) : null}
      </Section>
    </>
  )
}

function ResultHeader({ ok, okTitle, failTitle, children }: { ok: boolean; okTitle: string; failTitle: string; children?: ReactNode }) {
  return (
    <Alert
      mt="md"
      variant="light"
      color={ok ? 'teal' : 'red'}
      icon={ok ? <IconCircleCheck size={20} /> : <IconCircleX size={20} />}
      title={ok ? okTitle : failTitle}
    >
      {children}
    </Alert>
  )
}

function KeycloakResult({ result }: { result: KeycloakTestResult }) {
  const reg = result.register
  return (
    <Stack gap="md">
      <ResultHeader ok={result.ok} okTitle="Keycloak 연결 정상" failTitle="Keycloak 연결 실패">
        {result.error ? (
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
            {result.error}
          </Text>
        ) : (
          <Text size="sm">디스커버리와 Redirect URI 등록을 확인했습니다.</Text>
        )}
        {result.warnings && result.warnings.length > 0 ? (
          <List size="sm" mt="xs" spacing={4}>
            {result.warnings.map((w) => (
              <List.Item key={w}>{w}</List.Item>
            ))}
          </List>
        ) : null}
      </ResultHeader>

      {reg ? (
        <Paper withBorder p="md" radius="md">
          <Group gap="xs" mb="sm">
            <IconKey size={18} />
            <Text fw={700}>Keycloak 에 등록할 값</Text>
          </Group>
          <Stack gap="sm">
            <RegisterRow label="클라이언트 유형">
              <Text size="sm">{reg.clientType}</Text>
            </RegisterRow>
            <RegisterRow label="Valid redirect URIs">
              <CopyList values={reg.validRedirectUris} />
            </RegisterRow>
            <RegisterRow label="Web origins">
              <CopyList values={reg.webOrigins} />
            </RegisterRow>
            <RegisterRow label="Valid post logout redirect URIs">
              <CopyList values={reg.validPostLogoutRedirectUris} empty="로그아웃 후 이동 URL 을 입력하면 표시됩니다" />
            </RegisterRow>
            <RegisterRow label="Realm 역할">
              <CopyList values={reg.roles} />
            </RegisterRow>
          </Stack>
          {result.authUrl || result.tokenUrl ? (
            <>
              <Divider my="sm" />
              <Stack gap={4}>
                {result.authUrl ? (
                  <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
                    인가 엔드포인트: {result.authUrl}
                  </Text>
                ) : null}
                {result.tokenUrl ? (
                  <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
                    토큰 엔드포인트: {result.tokenUrl}
                  </Text>
                ) : null}
              </Stack>
            </>
          ) : null}
        </Paper>
      ) : null}
    </Stack>
  )
}

function RegisterRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <Text size="sm" c="dimmed" fw={600} mb={2}>
        {label}
      </Text>
      {children}
    </div>
  )
}

function CopyList({ values, empty = '없음' }: { values?: string[]; empty?: string }) {
  if (!values || values.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        {empty}
      </Text>
    )
  }
  return (
    <Stack gap={4}>
      {values.map((v) => (
        <CopyField key={v} value={v} />
      ))}
    </Stack>
  )
}

function McpOAuthResult({ result }: { result: McpOAuthTestResult }) {
  return (
    <Stack gap="md">
      <ResultHeader ok={result.ok} okTitle="MCP OAuth 점검 결과" failTitle="MCP OAuth 점검 실패">
        {result.error ? <Text size="sm">{result.error}</Text> : null}
        {result.checks && result.checks.length > 0 ? (
          <Stack gap={6} mt={result.error ? 'xs' : 0}>
            {result.checks.map((c) => (
              <Group key={c.name} gap="xs" wrap="nowrap" align="flex-start">
                {c.ok ? (
                  <IconCircleCheck size={18} color="var(--mantine-color-teal-6)" style={{ flexShrink: 0, marginTop: 2 }} />
                ) : (
                  <IconCircleX size={18} color="var(--mantine-color-red-6)" style={{ flexShrink: 0, marginTop: 2 }} />
                )}
                <Text size="sm" style={{ overflowWrap: 'anywhere', minWidth: 0 }}>
                  <b>{c.name}</b> — {c.detail}
                </Text>
              </Group>
            ))}
          </Stack>
        ) : null}
        {typeof result.registeredClients === 'number' ? (
          <Text size="sm" mt="xs">
            등록된 MCP 클라이언트: {result.registeredClients}개
          </Text>
        ) : null}
      </ResultHeader>

      {result.mcpUrl ? (
        <Paper withBorder p="md" radius="md">
          <Stack gap="sm">
            {result.issuer ? (
              <RegisterRow label="Issuer">
                <CopyField value={result.issuer} />
              </RegisterRow>
            ) : null}
            <RegisterRow label="MCP 주소">
              <CopyField value={result.mcpUrl} />
            </RegisterRow>
            {result.protectedResourceMetadata ? (
              <RegisterRow label="보호 리소스 메타데이터">
                <CopyField value={result.protectedResourceMetadata} />
              </RegisterRow>
            ) : null}
            {result.authorizationServerMetadata ? (
              <RegisterRow label="인가 서버 메타데이터">
                <CopyField value={result.authorizationServerMetadata} />
              </RegisterRow>
            ) : null}
            {result.claudeCode ? (
              <RegisterRow label="Claude Code 연결 명령">
                <CopyField value={result.claudeCode} />
              </RegisterRow>
            ) : null}
          </Stack>
        </Paper>
      ) : null}
    </Stack>
  )
}
