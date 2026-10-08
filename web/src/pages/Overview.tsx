import { Alert, Badge, Button, Grid, Group, SimpleGrid, Stack, Table, Text, ThemeIcon, Timeline } from '@mantine/core'
import {
  IconAlertTriangle,
  IconArrowRight,
  IconChecklist,
  IconCircleCheck,
  IconKey,
  IconLink,
  IconPlugConnected,
  IconRobot,
  IconShieldCheck,
  IconTool,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'

import { CopyField, EmptyState, PageHeader, Section, StatCard, StatList, TableScroll } from '../components/ui'
import { api, type ApiKey, type ApprovalRequest, type AuditPage, type McpConfig, type OAuthGrant, type ToolRecord } from '../lib/api'
import { useAuth } from '../lib/auth'
import {
  evidenceLabels,
  executionModeLabels,
  formatDateTime,
  formatRelative,
  permissionModeLabels,
  resultLabel,
  roleLabels,
} from '../lib/format'

export function OverviewPage() {
  const { me } = useAuth()
  const keys = useQuery({ queryKey: ['me', 'keys'], queryFn: () => api.get<ApiKey[]>('/api/me/keys') })
  const tools = useQuery({ queryKey: ['me', 'tools'], queryFn: () => api.get<ToolRecord[]>('/api/me/tools') })
  const approvals = useQuery({
    queryKey: ['my-approvals', 'pending'],
    queryFn: () => api.get<ApprovalRequest[]>('/api/me/approvals?status=pending'),
  })
  const grants = useQuery({ queryKey: ['me', 'connections'], queryFn: () => api.get<OAuthGrant[]>('/api/me/connections') })
  const audit = useQuery({ queryKey: ['me', 'audit', 'recent'], queryFn: () => api.get<AuditPage>('/api/me/audit?limit=8') })
  const cfg = useQuery({ queryKey: ['me', 'mcp-config'], queryFn: () => api.get<McpConfig>('/api/me/mcp-config') })

  const activeKeys = (keys.data ?? []).filter((k) => k.status === 'active' || k.status === 'rotation_due' || k.status === 'grace')
  const rotationDue = (keys.data ?? []).filter((k) => k.status === 'rotation_due')
  const mapped = Boolean(me?.confluence)
  const conn = me?.connection

  const steps = [
    { done: mapped, title: 'Confluence 계정 확인', text: mapped ? `${me?.confluence?.confluenceUsername} 계정과 연결됨` : '내 Confluence 화면에서 계정을 확인하십시오', to: '/me/confluence' },
    { done: (grants.data?.length ?? 0) > 0 || activeKeys.length > 0, title: 'AI 클라이언트 연결', text: 'MCP URL 하나로 OAuth 로그인하거나 API 키를 씁니다', to: '/me/connect' },
    { done: false, title: '변경은 승인 후 적용', text: 'AI 가 만든 변경안은 변경 승인 화면에서 diff 를 보고 승인합니다', to: '/me/approvals' },
  ]

  return (
    <>
      <PageHeader
        title={`${me?.user.displayName || me?.user.username}님, 안녕하세요`}
        description="confmcp 는 AI 가 Confluence 를 읽거나 바꿀 때, 회원님이 Confluence 에서 볼 수 있는 문서만 쓰도록 매 호출을 검증합니다."
      />

      {!mapped ? (
        <Alert color="orange" variant="light" icon={<IconAlertTriangle size={20} />} title="Confluence 계정이 확인되지 않았습니다" mb="lg">
          <Text size="sm">{me?.mappingError || '매핑된 Confluence 사용자가 없어 문서 도구를 쓸 수 없습니다.'}</Text>
          <Button component={Link} to="/me/confluence" variant="light" size="compact-md" mt="xs" rightSection={<IconArrowRight size={16} />}>
            내 Confluence 확인
          </Button>
        </Alert>
      ) : null}
      {rotationDue.length > 0 ? (
        <Alert color="yellow" variant="light" icon={<IconKey size={20} />} title="키 회전이 필요합니다" mb="lg">
          회전 주기가 지난 키가 {rotationDue.length}개 있습니다.{' '}
          <Button component={Link} to="/me/keys" variant="subtle" size="compact-sm">
            키 관리로 이동
          </Button>
        </Alert>
      ) : null}

      <SimpleGrid cols={{ base: 1, xs: 2, lg: 4 }} mb="lg">
        <StatCard icon={<IconChecklist size={22} />} label="승인 대기 변경안" value={approvals.data?.length ?? '—'} hint="내가 요청한 변경" color={approvals.data?.length ? 'yellow' : undefined} />
        <StatCard icon={<IconPlugConnected size={22} />} label="연결된 AI 클라이언트" value={grants.data?.length ?? '—'} hint="MCP OAuth 연결" />
        <StatCard icon={<IconKey size={22} />} label="활성 API 키" value={activeKeys.length} hint={rotationDue.length ? `회전 필요 ${rotationDue.length}개` : '정상'} />
        <StatCard icon={<IconTool size={22} />} label="사용 가능한 도구" value={tools.data?.length ?? '—'} hint="역할·스코프 기준" />
      </SimpleGrid>

      <Grid gutter="lg">
        <Grid.Col span={{ base: 12, lg: 7 }}>
          <Section title="AI 클라이언트 연결" description="Claude Code, Cursor 등 MCP 클라이언트에 아래 주소만 넣으면 브라우저 로그인으로 연결됩니다.">
            <Stack gap="sm">
              <Text size="sm" fw={600}>
                MCP 주소
              </Text>
              <CopyField value={cfg.data?.mcpUrl ?? `${window.location.origin}/mcp`} />
              <Text size="sm" fw={600} mt="xs">
                Claude Code
              </Text>
              <CopyField value={cfg.data?.oauth.claudeCode ?? `claude mcp add --transport http confmcp ${window.location.origin}/mcp`} />
              <Group>
                <Button component={Link} to="/me/connect" variant="light" rightSection={<IconArrowRight size={16} />}>
                  다른 클라이언트·API 키 설정
                </Button>
              </Group>
            </Stack>
          </Section>

          <Section title="최근 활동" actions={<Button component={Link} to="/me/audit" variant="subtle" size="compact-sm">전체 보기</Button>}>
            {(audit.data?.values.length ?? 0) === 0 ? (
              <EmptyState label="아직 활동이 없습니다" />
            ) : (
              <TableScroll minWidth={560}>
                <Table>
                  <Table.Tbody>
                    {audit.data!.values.map((e) => (
                      <Table.Tr key={e.id}>
                        <Table.Td w={150}>
                          <Text size="sm" c="dimmed">
                            {formatRelative(e.occurredAt)}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" ff={e.toolName ? 'monospace' : undefined}>
                            {e.toolName || e.action}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{[e.spaceKey, e.contentId].filter(Boolean).join(' / ') || '—'}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge color={e.success ? 'teal' : 'red'} variant="light">
                            {resultLabel(e.success, e.errorCode)}
                          </Badge>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </TableScroll>
            )}
          </Section>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 5 }}>
          <Section title="시작하기">
            <Timeline active={steps.filter((s) => s.done).length - 1} bulletSize={26} lineWidth={2}>
              {steps.map((s) => (
                <Timeline.Item
                  key={s.title}
                  bullet={s.done ? <IconCircleCheck size={16} /> : null}
                  title={
                    <Text fw={700} component={Link} to={s.to} c="inherit" td="none">
                      {s.title}
                    </Text>
                  }
                >
                  <Text size="sm" c="dimmed">
                    {s.text}
                  </Text>
                </Timeline.Item>
              ))}
            </Timeline>
          </Section>

          <Section title="내 연결 상태">
            <StatList
              items={[
                { label: 'Confluence 사용자', value: me?.confluence ? `${me.confluence.confluenceDisplay} (${me.confluence.confluenceUsername})` : '매핑 없음' },
                { label: '확인 근거', value: me?.confluence ? (evidenceLabels[me.confluence.evidence] ?? me.confluence.evidence) : '—' },
                { label: '역할', value: (me?.roles ?? []).map((r) => roleLabels[r] ?? r).join(', ') || '—' },
                { label: '권한 판정', value: permissionModeLabels[conn?.permissionMode ?? ''] ?? '—' },
                { label: '실행 방식', value: executionModeLabels[conn?.executionMode ?? ''] ?? '—' },
                { label: '마지막 확인', value: formatDateTime(me?.confluence?.verifiedAt) },
              ]}
            />
            <Group mt="md" gap="xs">
              <ThemeIcon variant="light" color="teal" size="sm">
                <IconShieldCheck size={14} />
              </ThemeIcon>
              <Text size="xs" c="dimmed">
                관리자 역할도 Confluence 문서 권한을 넘어서지 못합니다.
              </Text>
            </Group>
          </Section>

          <Section title="AI 도우미">
            <Text size="sm" c="dimmed" mb="sm">
              콘솔에서 바로 AI 와 대화합니다. 응답은 스트리밍으로 표시됩니다.
            </Text>
            <Button component={Link} to="/me/ai" variant="light" leftSection={<IconRobot size={18} />}>
              AI 도우미 열기
            </Button>
            <Button component={Link} to="/me/confluence" variant="subtle" leftSection={<IconLink size={18} />} ml="xs">
              내 Confluence
            </Button>
          </Section>
        </Grid.Col>
      </Grid>
    </>
  )
}
