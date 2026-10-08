import { Alert, Anchor, Badge, Box, Button, Group, SimpleGrid, Stack, Table, Text, Tooltip } from '@mantine/core'
import {
  IconAlertTriangle,
  IconArrowRight,
  IconInfoCircle,
  IconRefresh,
  IconShieldX,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'

import {
  EmptyState,
  ErrorBlock,
  HealthBadge,
  LoadingBlock,
  PageHeader,
  Section,
  StatCard,
  TableScroll,
} from '../../components/ui'
import { api, type Dashboard, type SetupWarning, type StatsPoint } from '../../lib/api'
import {
  auditCategoryLabels,
  formatDateTime,
  formatNumber,
  formatPercent,
  formatRelative,
  healthLabels,
  resultLabel,
} from '../../lib/format'

const warningColors: Record<SetupWarning['level'], string> = { error: 'red', warning: 'yellow', info: 'blue' }
const warningTitles: Record<SetupWarning['level'], string> = { error: '조치 필요', warning: '주의', info: '안내' }

const healthOrder = ['database', 'keycloak', 'confluence', 'permission', 'ai']

/** Count tiles shown on the dashboard, in reading order. */
const countTiles: { key: string; label: string; hint: string; link: string; alert?: boolean }[] = [
  { key: 'users', label: '활성 사용자', hint: '콘솔 계정', link: '/admin/users' },
  { key: 'mappings', label: 'Confluence 매핑', hint: '활성 식별 매핑', link: '/admin/identity' },
  { key: 'allowedSpaces', label: '허용 공간 규칙', hint: 'MCP 접근 허용 공간', link: '/admin/policy' },
  { key: 'pendingApprovals', label: '승인 대기', hint: '만료 전 요청', link: '/admin/approvals', alert: true },
  { key: 'activeKeys', label: '활성 API 키', hint: '만료·폐기 제외', link: '/admin/keys' },
  { key: 'oauthGrants', label: 'MCP OAuth 연결', hint: '철회되지 않은 연결', link: '/admin/connections' },
  { key: 'mcpSessions', label: 'MCP 세션', hint: '최근 1시간', link: '/admin/connections' },
  { key: 'outcomeUnknown', label: '결과 불명확', hint: '확인이 필요한 쓰기', link: '/admin/operations', alert: true },
  { key: 'mappingErrors', label: '매핑 실패', hint: '식별 실패 기록', link: '/admin/identity', alert: true },
  { key: 'rotationDue', label: '회전 필요 키', hint: '회전 기한 경과', link: '/admin/keys', alert: true },
]

export function AdminDashboardPage() {
  const navigate = useNavigate()
  const dashboard = useQuery({
    queryKey: ['dashboard'],
    queryFn: () => api.get<Dashboard>('/api/admin/dashboard'),
    refetchInterval: 30_000,
  })

  const data = dashboard.data
  const counts = data?.counts ?? {}
  const stats = data?.stats24h
  const health = data?.health ?? {}
  const healthKeys = [
    ...healthOrder.filter((k) => k in health),
    ...Object.keys(health).filter((k) => !healthOrder.includes(k)),
  ]

  return (
    <>
      <PageHeader
        title="관리 대시보드"
        description="연결 상태, 설정 경고, 최근 24시간 사용량을 확인합니다. 30초마다 자동으로 갱신됩니다."
        actions={
          <Button
            variant="default"
            leftSection={<IconRefresh size={18} />}
            loading={dashboard.isFetching}
            onClick={() => void dashboard.refetch()}
          >
            새로고침
          </Button>
        }
      />

      {dashboard.isLoading ? <LoadingBlock /> : null}
      {dashboard.error ? <ErrorBlock error={dashboard.error} title="대시보드를 불러오지 못했습니다" /> : null}

      {data ? (
        <>
          {data.warnings.length > 0 ? (
            <Stack gap="sm" mb="lg">
              {data.warnings.map((w, i) => (
                <Alert
                  key={`${w.level}-${i}`}
                  color={warningColors[w.level] ?? 'blue'}
                  variant="light"
                  title={warningTitles[w.level] ?? '안내'}
                  icon={
                    w.level === 'error' ? (
                      <IconShieldX size={20} />
                    ) : w.level === 'warning' ? (
                      <IconAlertTriangle size={20} />
                    ) : (
                      <IconInfoCircle size={20} />
                    )
                  }
                >
                  <Group justify="space-between" gap="sm" wrap="wrap">
                    <Text size="sm" style={{ flex: '1 1 240px', minWidth: 0 }}>
                      {w.message}
                    </Text>
                    {w.link ? (
                      <Button
                        component={Link}
                        to={w.link}
                        size="xs"
                        variant="light"
                        color={warningColors[w.level] ?? 'blue'}
                        rightSection={<IconArrowRight size={14} />}
                      >
                        설정으로 이동
                      </Button>
                    ) : null}
                  </Group>
                </Alert>
              ))}
            </Stack>
          ) : (
            <Alert color="teal" variant="light" mb="lg" title="설정 경고 없음">
              필수 설정이 모두 갖춰져 있습니다.
            </Alert>
          )}

          <Section title="연결 상태" description="배지에 마우스를 올리면 세부 내용을 볼 수 있습니다.">
            <Group gap="sm" wrap="wrap">
              {healthKeys.map((k) => (
                <HealthBadge key={k} name={healthLabels[k] ?? k} value={health[k]} />
              ))}
            </Group>
            <Text size="xs" c="dimmed" mt="sm">
              {data.version.name} {data.version.version}
              {data.version.commit ? ` (${data.version.commit.slice(0, 8)})` : ''}
            </Text>
          </Section>

          <SimpleGrid cols={{ base: 1, xs: 2, md: 4 }} spacing="md" mb="lg">
            <StatCard label="24시간 호출" value={formatNumber(stats?.calls ?? 0)} hint="도구 호출 수" />
            <StatCard
              label="오류율"
              value={formatPercent(stats?.errorRate ?? 0)}
              color={(stats?.errorRate ?? 0) > 0.1 ? 'red' : undefined}
              hint={`실패 ${formatNumber(stats?.failures ?? 0)}건`}
            />
            <StatCard
              label="권한·정책 거부"
              value={formatNumber(stats?.denied ?? 0)}
              color={(stats?.denied ?? 0) > 0 ? 'orange' : undefined}
              hint="최근 24시간"
            />
            <StatCard
              label="응답 시간 p95"
              value={`${formatNumber(stats?.p95Ms ?? 0)} ms`}
              hint={`중앙값 ${formatNumber(stats?.p50Ms ?? 0)} ms · 평균 ${formatNumber(stats?.avgMs ?? 0)} ms`}
            />
          </SimpleGrid>

          <SimpleGrid cols={{ base: 1, xs: 2, md: 4 }} spacing="md" mb="lg">
            {countTiles.map((t) => {
              const n = counts[t.key] ?? 0
              return (
                <StatCard
                  key={t.key}
                  label={t.label}
                  value={formatNumber(n)}
                  hint={t.hint}
                  color={t.alert && n > 0 ? 'orange' : undefined}
                  onClick={() => navigate(t.link)}
                />
              )
            })}
          </SimpleGrid>

          <Section title="최근 24시간 호출 추이" description="시간대별 도구 호출 수와 실패 수입니다.">
            <TrafficChart series={stats?.series ?? []} />
          </Section>

          <Section title="많이 쓰인 도구" description="최근 7일 기준 상위 도구입니다.">
            {data.topTools && data.topTools.length > 0 ? (
              <TableScroll minWidth={480}>
                <Table striped highlightOnHover verticalSpacing="xs" fz="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>도구</Table.Th>
                      <Table.Th ta="right">호출</Table.Th>
                      <Table.Th ta="right">실패</Table.Th>
                      <Table.Th ta="right">실패율</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {data.topTools.map((t) => (
                      <Table.Tr key={t.tool}>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {t.tool}
                          </Text>
                        </Table.Td>
                        <Table.Td ta="right">{formatNumber(t.calls)}</Table.Td>
                        <Table.Td ta="right">
                          <Text size="sm" c={t.failures > 0 ? 'red' : undefined}>
                            {formatNumber(t.failures)}
                          </Text>
                        </Table.Td>
                        <Table.Td ta="right">{formatPercent(t.calls ? t.failures / t.calls : 0)}</Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </TableScroll>
            ) : (
              <EmptyState label="최근 7일간 도구 호출이 없습니다" />
            )}
          </Section>

          <Section
            title="최근 감사 기록"
            actions={
              <Anchor component={Link} to="/admin/audit" size="sm">
                전체 감사 로그 보기
              </Anchor>
            }
          >
            {data.recentAudit && data.recentAudit.length > 0 ? (
              <TableScroll minWidth={760}>
                <Table striped highlightOnHover verticalSpacing="xs" fz="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>시각</Table.Th>
                      <Table.Th>사용자</Table.Th>
                      <Table.Th>분류 · 동작</Table.Th>
                      <Table.Th>도구</Table.Th>
                      <Table.Th>공간</Table.Th>
                      <Table.Th>결과</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {data.recentAudit.map((e) => (
                      <Table.Tr key={e.id}>
                        <Table.Td style={{ whiteSpace: 'nowrap' }}>
                          <Text size="sm">{formatDateTime(e.occurredAt)}</Text>
                          <Text size="xs" c="dimmed">
                            {formatRelative(e.occurredAt)}
                          </Text>
                        </Table.Td>
                        <Table.Td>{e.keycloakUsername || e.confluenceUsername || '—'}</Table.Td>
                        <Table.Td>
                          <Text size="sm">{auditCategoryLabels[e.category] ?? e.category}</Text>
                          <Text size="xs" c="dimmed" ff="monospace">
                            {e.action}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          {e.toolName ? (
                            <Text size="sm" ff="monospace">
                              {e.toolName}
                            </Text>
                          ) : (
                            '—'
                          )}
                        </Table.Td>
                        <Table.Td>{e.spaceKey || '—'}</Table.Td>
                        <Table.Td>
                          <Badge color={e.success ? 'teal' : 'red'} variant="light" title={e.message}>
                            {resultLabel(e.success, e.errorCode)}
                          </Badge>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </TableScroll>
            ) : (
              <EmptyState label="감사 기록이 없습니다" />
            )}
          </Section>
        </>
      ) : null}
    </>
  )
}

interface Bucket {
  at: Date
  calls: number
  failures: number
  p95Ms: number
}

/** fillHours lays the sparse hourly series onto the last 24 hours. */
function fillHours(series: StatsPoint[]): Bucket[] {
  const hour = 60 * 60 * 1000
  const byHour = new Map<number, StatsPoint>()
  for (const p of series) {
    const t = new Date(p.at).getTime()
    if (!Number.isNaN(t)) byHour.set(Math.floor(t / hour), p)
  }
  const nowHour = Math.floor(Date.now() / hour)
  const out: Bucket[] = []
  for (let h = nowHour - 23; h <= nowHour; h++) {
    const p = byHour.get(h)
    out.push({ at: new Date(h * hour), calls: p?.calls ?? 0, failures: p?.failures ?? 0, p95Ms: p?.p95Ms ?? 0 })
  }
  return out
}

/**
 * TrafficChart draws hourly calls and failures as HTML bars, so labels keep a
 * readable size at every width.
 */
function TrafficChart({ series }: { series: StatsPoint[] }) {
  const buckets = fillHours(series)
  const total = buckets.reduce((s, b) => s + b.calls, 0)
  if (total === 0) return <EmptyState label="최근 24시간 동안 도구 호출이 없습니다" />

  const max = Math.max(1, ...buckets.map((b) => b.calls))
  const pct = (v: number) => `${Math.max(v > 0 ? 2 : 0, (v / max) * 100)}%`
  const plotHeight = 160

  return (
    <Box>
      <Group gap={8} align="stretch" wrap="nowrap">
        <Stack justify="space-between" gap={0} h={plotHeight} style={{ flexShrink: 0 }}>
          <Text size="xs" c="dimmed" ta="right">
            {formatNumber(max)}
          </Text>
          <Text size="xs" c="dimmed" ta="right">
            0
          </Text>
        </Stack>
        <Box style={{ flex: 1, minWidth: 0 }}>
          <Box
            h={plotHeight}
            style={{
              display: 'flex',
              alignItems: 'flex-end',
              gap: 2,
              borderBottom: '1px solid var(--mantine-color-default-border)',
              borderTop: '1px dashed var(--mantine-color-default-border)',
            }}
          >
            {buckets.map((b) => (
              <Tooltip
                key={b.at.getTime()}
                label={`${b.at.getHours()}시 · 호출 ${b.calls} · 실패 ${b.failures}${b.p95Ms ? ` · p95 ${b.p95Ms}ms` : ''}`}
              >
                <Box style={{ flex: 1, minWidth: 0, height: '100%', position: 'relative', cursor: 'default' }}>
                  <Box
                    style={{
                      position: 'absolute',
                      bottom: 0,
                      left: 0,
                      right: 0,
                      height: pct(b.calls),
                      background: 'var(--mantine-color-confmcp-filled)',
                      opacity: 0.85,
                      borderRadius: '2px 2px 0 0',
                    }}
                  />
                  <Box
                    style={{
                      position: 'absolute',
                      bottom: 0,
                      left: 0,
                      right: 0,
                      height: pct(b.failures),
                      background: 'var(--mantine-color-red-filled)',
                      borderRadius: '2px 2px 0 0',
                    }}
                  />
                </Box>
              </Tooltip>
            ))}
          </Box>
          <Group justify="space-between" mt={4} wrap="nowrap" gap={0}>
            {[0, 6, 12, 18, 23].map((i) => (
              <Text key={i} size="xs" c="dimmed">
                {`${buckets[i].at.getHours()}시`}
              </Text>
            ))}
          </Group>
        </Box>
      </Group>
      <Group gap="lg" mt="sm" wrap="wrap">
        <Legend color="var(--mantine-color-confmcp-filled)" label={`호출 (합계 ${formatNumber(total)})`} />
        <Legend
          color="var(--mantine-color-red-filled)"
          label={`실패 (합계 ${formatNumber(buckets.reduce((s, b) => s + b.failures, 0))})`}
        />
      </Group>
    </Box>
  )
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <Group gap={6} wrap="nowrap">
      <Box w={12} h={12} style={{ background: color, borderRadius: 3 }} />
      <Text size="sm" c="dimmed">
        {label}
      </Text>
    </Group>
  )
}
