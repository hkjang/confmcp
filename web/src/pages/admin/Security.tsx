import { SettingsForm, type Field } from '../../components/SettingsForm'
import { ErrorBlock, LoadingBlock, PageHeader, SaveBar, Section } from '../../components/ui'
import { useSettingsGroup } from '../../lib/useSettingsGroup'

interface SecuritySettings {
  ipAllowlist: string[] | null
  rateLimitPerMin: number
  rateLimitBurst: number
  sessionTtlMinutes: number
  approvalTtlMinutes: number
  allowSelfApproval: boolean
  auditRetainDays: number
  trustProxyHeaders: boolean
}

interface MCPSettings {
  serverName: string
  resourceUrl: string
  maxResponseKb: number
  exposeHighLevelTools: boolean
  allowRawCql: boolean
}

const securityFields: Field<SecuritySettings>[] = [
  {
    kind: 'tags',
    key: 'ipAllowlist',
    label: '허용 IP 목록',
    description: 'IP 또는 CIDR(예: 10.0.0.0/8). 비워 두면 모든 주소를 허용합니다. 목록에 없는 주소는 콘솔과 MCP 모두 차단되므로 관리자 주소를 반드시 포함하십시오.',
    placeholder: '10.0.0.0/8',
    span: 12,
  },
  { kind: 'number', key: 'rateLimitPerMin', label: 'MCP 분당 요청 한도', description: '접속 주소(IP)별 기준입니다.', min: 1, max: 100000 },
  { kind: 'number', key: 'rateLimitBurst', label: '순간 허용량(burst)', min: 1, max: 100000 },
  { kind: 'number', key: 'sessionTtlMinutes', label: '로그인 세션 유지 (분)', min: 5, max: 60 * 24 * 30 },
  {
    kind: 'number',
    key: 'approvalTtlMinutes',
    label: '승인 요청 유효 시간 (분)',
    description: '이 시간 안에 승인·실행하지 않으면 만료됩니다.',
    min: 1,
    max: 60 * 24 * 7,
  },
  {
    kind: 'switch',
    key: 'allowSelfApproval',
    label: '본인 승인 허용',
    description: '작성 등급 변경을 요청자 본인이 승인할 수 있음. 이동·휴지통은 항상 별도 승인자',
  },
  {
    kind: 'switch',
    key: 'trustProxyHeaders',
    label: '프록시 헤더 신뢰',
    description: '리버스 프록시 뒤에 있을 때 X-Forwarded-For/X-Real-IP 로 실제 접속 주소를 판단합니다. 끄면 IP 허용 목록과 요청 한도가 프록시 주소 기준으로 동작합니다.',
  },
  {
    kind: 'number',
    key: 'auditRetainDays',
    label: '감사 로그 보존 기간 (일)',
    description: '0 이면 정리하지 않습니다. 감사 로그 화면의 "보존 기간 정리"가 이 값을 씁니다.',
    min: 0,
    max: 3650,
  },
]

const mcpFields: Field<MCPSettings>[] = [
  { kind: 'text', key: 'serverName', label: '서버 이름', description: 'MCP initialize 응답에 표시되는 이름입니다.' },
  {
    kind: 'text',
    key: 'resourceUrl',
    label: '공개 URL',
    description: '사용자가 접속하는 외부 주소입니다. OAuth 발급자(issuer)와 링크 생성에 쓰입니다.',
    placeholder: 'https://confmcp.example.internal',
  },
  {
    kind: 'number',
    key: 'maxResponseKb',
    label: '최대 응답 크기 (KB)',
    description: '도구 응답 하나의 크기 상한입니다.',
    min: 16,
    max: 65536,
  },
  {
    kind: 'switch',
    key: 'exposeHighLevelTools',
    label: '고수준 도구 노출',
    description: '여러 단계를 묶은 고수준 도구(맥락 수집 등)를 MCP 클라이언트에 보여 줍니다.',
  },
  {
    kind: 'switch',
    key: 'allowRawCql',
    label: 'CQL 직접 입력 허용',
    description: '고급 기능입니다. 켜도 허용된 공간 조건은 항상 함께 적용됩니다.',
  },
]

export function AdminSecurityPage() {
  const sec = useSettingsGroup<SecuritySettings>('security')
  const mcp = useSettingsGroup<MCPSettings>('mcp')

  return (
    <>
      <PageHeader
        title="보안 · MCP"
        description="접근 주소 제한, 요청 한도, 세션·승인 유효 시간, 감사 로그 보존과 MCP 게이트웨이 동작을 설정합니다."
      />

      <Section title="보안">
        {sec.query.isLoading ? <LoadingBlock /> : null}
        {sec.query.error ? <ErrorBlock error={sec.query.error} /> : null}
        {sec.draft ? (
          <>
            <SettingsForm
              fields={securityFields}
              value={sec.draft}
              onChange={sec.setDraft}
              secrets={sec.secrets}
              onSecretChange={sec.setSecrets}
            />
            <SaveBar onSave={() => sec.save.mutate()} saving={sec.save.isPending} />
          </>
        ) : null}
      </Section>

      <Section title="MCP 게이트웨이">
        {mcp.query.isLoading ? <LoadingBlock /> : null}
        {mcp.query.error ? <ErrorBlock error={mcp.query.error} /> : null}
        {mcp.draft ? (
          <>
            <SettingsForm
              fields={mcpFields}
              value={mcp.draft}
              onChange={mcp.setDraft}
              secrets={mcp.secrets}
              onSecretChange={mcp.setSecrets}
            />
            <SaveBar onSave={() => mcp.save.mutate()} saving={mcp.save.isPending} />
          </>
        ) : null}
      </Section>
    </>
  )
}
