import {
  Anchor,
  Badge,
  Box,
  Button,
  Code,
  Group,
  Paper,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Tooltip,
} from '@mantine/core'
import { IconExternalLink, IconRefresh } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import {
  ErrorBlock,
  HealthBadge,
  LoadingBlock,
  PageHeader,
  Section,
  StatCard,
  StatList,
  TableScroll,
} from '../../components/ui'
import { api, type HealthComponent, type Stats, type VersionInfo } from '../../lib/api'
import {
  formatDateTime,
  formatNumber,
  formatPercent,
  formatRelative,
  healthLabels,
} from '../../lib/format'

interface SystemInfo {
  version: VersionInfo
  startedAt: string
  health: Record<string, HealthComponent>
  database: { version: string; name: string; size: string }
  migrations?: { version: string; appliedAt: string }[]
  pool: { total: number; idle: number; acquired: number }
  environment: string[]
}

const envDescriptions: Record<string, string> = {
  DATABASE_URL: 'PostgreSQL 접속 문자열',
  BOOTSTRAP_ADMIN: '초기 관리자 계정 이름 (기동할 때마다 관리자 권한·사용 상태를 복구)',
  BOOTSTRAP_ADMIN_PASSWORD: '초기 관리자 비밀번호 (기동할 때마다 이 값으로 다시 설정)',
  ENCRYPTION_KEY: '비밀값 암호화 키 (32바이트 hex·base64, 분실 시 저장된 비밀값 복구 불가)',
}

const healthOrder = ['database', 'keycloak', 'confluence', 'permission', 'ai']

type Range = '24h' | '7d' | '30d'

const rangeData = [
  { value: '24h', label: '24시간' },
  { value: '7d', label: '7일' },
  { value: '30d', label: '30일' },
]

interface Bucket {
  at: Date
  calls: number
  failures: number
  p95Ms: number
}

/** fillBuckets lays the sparse server series onto a continuous time axis. */
function fillBuckets(stats: Stats | undefined, range: Range): Bucket[] {
  const hourly = range === '24h'
  const count = range === '24h' ? 24 : range === '7d' ? 7 : 30
  const floor = (d: Date) => {
    const c = new Date(d)
    if (hourly) c.setMinutes(0, 0, 0)
    else c.setHours(0, 0, 0, 0)
    return c
  }
  const now = floor(new Date())
  const buckets: Bucket[] = []
  for (let i = count - 1; i >= 0; i--) {
    const at = new Date(now)
    if (hourly) at.setHours(at.getHours() - i)
    else at.setDate(at.getDate() - i)
    buckets.push({ at, calls: 0, failures: 0, p95Ms: 0 })
  }
  const index = new Map(buckets.map((b, i) => [b.at.getTime(), i]))
  for (const p of stats?.series ?? []) {
    const t = new Date(p.at)
    if (Number.isNaN(t.getTime())) continue
    const i = index.get(floor(t).getTime())
    if (i === undefined) continue
    buckets[i].calls += p.calls
    buckets[i].failures += p.failures
    buckets[i].p95Ms = Math.max(buckets[i].p95Ms, p.p95Ms)
  }
  return buckets
}

function bucketLabel(at: Date, hourly: boolean): string {
  if (hourly) return `${at.getHours()}시`
  return `${at.getMonth() + 1}/${at.getDate()}`
}

function CallsChart({ buckets, hourly }: { buckets: Bucket[]; hourly: boolean }) {
  const max = Math.max(1, ...buckets.map((b) => b.calls))
  const height = 160
  const labelEvery = buckets.length > 12 ? Math.ceil(buckets.length / 6) : 1
  return (
    <Box>
      <Group gap="md" mb="xs" wrap="wrap">
        <Group gap={6} wrap="nowrap">
          <Box w={12} h={12} style={{ borderRadius: 3, background: 'var(--mantine-color-confmcp-5)' }} />
          <Text size="xs" c="dimmed">
            성공 호출
          </Text>
        </Group>
        <Group gap={6} wrap="nowrap">
          <Box w={12} h={12} style={{ borderRadius: 3, background: 'var(--mantine-color-red-6)' }} />
          <Text size="xs" c="dimmed">
            실패
          </Text>
        </Group>
        <Text size="xs" c="dimmed">
          최대 {formatNumber(max)}건/{hourly ? '시간' : '일'}
        </Text>
      </Group>
      <Box
        role="img"
        aria-label="기간별 도구 호출 막대 그래프"
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          gap: 2,
          height,
          borderBottom: '1px solid var(--mantine-color-default-border)',
        }}
      >
        {buckets.map((b) => {
          const ok = Math.max(0, b.calls - b.failures)
          const okH = (ok / max) * height
          const failH = (b.failures / max) * height
          return (
            <Tooltip
              key={b.at.getTime()}
              label={
                <Stack gap={0}>
                  <Text size="xs" fw={700}>
                    {hourly ? formatDateTime(b.at.toISOString()) : b.at.toLocaleDateString('ko-KR')}
                  </Text>
                  <Text size="xs">호출 {formatNumber(b.calls)}건</Text>
                  <Text size="xs">실패 {formatNumber(b.failures)}건</Text>
                  {b.calls > 0 ? <Text size="xs">p95 {formatNumber(b.p95Ms)} ms</Text> : null}
                </Stack>
              }
            >
              <Box
                style={{
                  flex: '1 1 0',
                  minWidth: 0,
                  height: '100%',
                  display: 'flex',
                  flexDirection: 'column',
                  justifyContent: 'flex-end',
                  gap: b.failures > 0 && ok > 0 ? 2 : 0,
                  cursor: 'default',
                }}
              >
                {b.failures > 0 ? (
                  <Box
                    style={{
                      height: Math.max(2, failH),
                      background: 'var(--mantine-color-red-6)',
                      borderRadius: '3px 3px 0 0',
                    }}
                  />
                ) : null}
                {ok > 0 ? (
                  <Box
                    style={{
                      height: Math.max(2, okH),
                      background: 'var(--mantine-color-confmcp-5)',
                      borderRadius: b.failures > 0 ? 0 : '3px 3px 0 0',
                    }}
                  />
                ) : null}
              </Box>
            </Tooltip>
          )
        })}
      </Box>
      <Box style={{ display: 'flex', gap: 2, overflow: 'hidden' }} mt={4}>
        {buckets.map((b, i) => (
          <Box key={b.at.getTime()} style={{ flex: '1 1 0', minWidth: 0, overflow: 'visible' }}>
            {i % labelEvery === 0 ? (
              <Text size="xs" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                {bucketLabel(b.at, hourly)}
              </Text>
            ) : null}
          </Box>
        ))}
      </Box>
    </Box>
  )
}

export function AdminSystemPage() {
  const [range, setRange] = useState<Range>('24h')

  const system = useQuery({
    queryKey: ['admin', 'system'],
    queryFn: () => api.get<SystemInfo>('/api/admin/system'),
  })
  const stats = useQuery({
    queryKey: ['admin', 'stats', range],
    queryFn: () => api.get<Stats>(`/api/admin/stats?range=${range}`),
    placeholderData: (prev) => prev,
  })

  const buckets = useMemo(() => fillBuckets(stats.data, range), [stats.data, range])

  const info = system.data
  const healthEntries = useMemo(() => {
    const entries = Object.entries(info?.health ?? {})
    return entries.sort(
      ([a], [b]) =>
        (healthOrder.indexOf(a) === -1 ? 99 : healthOrder.indexOf(a)) -
        (healthOrder.indexOf(b) === -1 ? 99 : healthOrder.indexOf(b)),
    )
  }, [info])

  const s = stats.data

  return (
    <>
      <PageHeader
        title="시스템"
        description="버전, 의존 서비스 상태, 데이터베이스와 마이그레이션, 호출 통계를 봅니다."
        actions={
          <Button
            variant="default"
            leftSection={<IconRefresh size={18} />}
            loading={(system.isFetching && !system.isLoading) || (stats.isFetching && !stats.isLoading)}
            onClick={() => {
              void system.refetch()
              void stats.refetch()
            }}
          >
            새로 고침
          </Button>
        }
      />

      <Section
        title="호출 통계"
        description="MCP 도구 호출 기준입니다. 실패에는 거부·승인 대기가 포함되지 않습니다."
        actions={
          <SegmentedControl
            data={rangeData}
            value={range}
            onChange={(v) => setRange((v as Range) ?? '24h')}
          />
        }
      >
        {stats.isLoading ? <LoadingBlock /> : null}
        {stats.error ? <ErrorBlock error={stats.error} /> : null}
        {s ? (
          <Stack gap="lg" style={{ opacity: stats.isPlaceholderData ? 0.6 : 1 }}>
            <SimpleGrid cols={{ base: 2, sm: 3, lg: 7 }} spacing="sm">
              <StatCard label="호출" value={formatNumber(s.calls)} />
              <StatCard label="실패" value={formatNumber(s.failures)} color={s.failures > 0 ? 'red' : undefined} />
              <StatCard label="거부" value={formatNumber(s.denied)} color={s.denied > 0 ? 'orange' : undefined} />
              <StatCard label="오류율" value={formatPercent(s.errorRate)} />
              <StatCard label="p50 지연" value={`${formatNumber(s.p50Ms)}`} hint="ms" />
              <StatCard label="p95 지연" value={`${formatNumber(s.p95Ms)}`} hint="ms" />
              <StatCard label="평균 지연" value={`${formatNumber(s.avgMs)}`} hint="ms" />
            </SimpleGrid>
            <CallsChart buckets={buckets} hourly={range === '24h'} />
          </Stack>
        ) : null}
      </Section>

      {system.isLoading ? <LoadingBlock /> : null}
      {system.error ? <ErrorBlock error={system.error} /> : null}

      {info ? (
        <>
          <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
            <Section title="버전">
              <StatList
                items={[
                  { label: '이름', value: info.version?.name || 'confmcp' },
                  { label: '버전', value: info.version?.version || '—' },
                  { label: '커밋', value: <Code>{info.version?.commit || '—'}</Code> },
                  { label: '빌드 일자', value: info.version?.buildDate || '—' },
                  {
                    label: '기동 시각',
                    value: `${formatDateTime(info.startedAt)} (${formatRelative(info.startedAt)})`,
                  },
                ]}
              />
              <Group gap="xs" mt="md" wrap="wrap">
                {[
                  { href: '/healthz', label: '/healthz' },
                  { href: '/readyz', label: '/readyz' },
                  { href: '/metrics', label: '/metrics' },
                ].map((l) => (
                  <Anchor key={l.href} href={l.href} target="_blank" rel="noopener noreferrer" size="sm">
                    <Group gap={4} wrap="nowrap">
                      {l.label}
                      <IconExternalLink size={14} />
                    </Group>
                  </Anchor>
                ))}
              </Group>
            </Section>

            <Section title="상태">
              <Stack gap="sm">
                {healthEntries.map(([name, value]) => (
                  <Group key={name} justify="space-between" wrap="nowrap" gap="sm" align="flex-start">
                    <HealthBadge name={healthLabels[name] ?? name} value={value} />
                    <Text size="sm" c="dimmed" ta="right" style={{ overflowWrap: 'anywhere', minWidth: 0 }}>
                      {value.detail}
                    </Text>
                  </Group>
                ))}
              </Stack>
            </Section>

            <Section title="데이터베이스">
              <StatList
                items={[
                  { label: '데이터베이스', value: info.database?.name || '—' },
                  { label: '크기', value: info.database?.size || '—' },
                  { label: '연결 풀', value: `전체 ${info.pool?.total ?? 0} · 사용 중 ${info.pool?.acquired ?? 0} · 유휴 ${info.pool?.idle ?? 0}` },
                ]}
              />
              <Text size="xs" c="dimmed" mt="sm" style={{ overflowWrap: 'anywhere' }}>
                {info.database?.version}
              </Text>
            </Section>

            <Section
              title="환경 변수"
              description="confmcp 가 읽는 환경 변수는 아래 네 가지뿐입니다. 나머지 설정은 모두 이 콘솔에서 관리합니다."
            >
              <Stack gap="xs">
                {(info.environment ?? []).map((name) => (
                  <Paper key={name} withBorder p="xs">
                    <Code>{name}</Code>
                    {envDescriptions[name] ? (
                      <Text size="xs" c="dimmed" mt={2}>
                        {envDescriptions[name]}
                      </Text>
                    ) : null}
                  </Paper>
                ))}
              </Stack>
            </Section>
          </SimpleGrid>

          <Section title="마이그레이션" description="적용된 데이터베이스 스키마 버전입니다.">
            <TableScroll minWidth={420}>
              <Table striped fz="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>버전</Table.Th>
                    <Table.Th>적용 시각</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {(info.migrations ?? []).map((m) => (
                    <Table.Tr key={m.version}>
                      <Table.Td>
                        <Code>{m.version}</Code>
                      </Table.Td>
                      <Table.Td>{formatDateTime(m.appliedAt)}</Table.Td>
                    </Table.Tr>
                  ))}
                  {(info.migrations ?? []).length === 0 ? (
                    <Table.Tr>
                      <Table.Td colSpan={2}>
                        <Badge variant="light" color="gray">
                          기록 없음
                        </Badge>
                      </Table.Td>
                    </Table.Tr>
                  ) : null}
                </Table.Tbody>
              </Table>
            </TableScroll>
          </Section>
        </>
      ) : null}
    </>
  )
}
