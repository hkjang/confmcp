import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Code,
  Drawer,
  Group,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core'
import { IconInfoCircle, IconRefresh, IconRotateClockwise, IconSearch } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import {
  EmptyState,
  ErrorBlock,
  JsonBlock,
  LoadingBlock,
  PageHeader,
  Section,
  StatList,
  TableScroll,
  notifyError,
  notifyOk,
} from '../../components/ui'
import { api, type ToolRecord } from '../../lib/api'
import {
  formatDateTime,
  opLabels,
  riskColors,
  riskLabels,
  roleLabels,
  roleOptions,
  toolGroupLabels,
} from '../../lib/format'

const groupOrder = ['identity', 'discovery', 'content', 'search', 'context', 'change', 'attachment']
const riskOrder = ['READ', 'WRITE', 'EXECUTE', 'ADMIN']

const priorityColors: Record<string, string> = { P0: 'confmcp', P1: 'cyan', P2: 'gray' }

function permissionLabel(perm: string): string {
  if (!perm || perm === 'NONE') return '없음'
  return opLabels[perm] ?? perm
}

/** roleSelectData keeps the stored role selectable even if it is not a known role. */
function roleSelectData(current: string) {
  if (!current || roleOptions.some((o) => o.value === current)) return roleOptions
  return [...roleOptions, { value: current, label: `${current} (저장된 값)` }]
}

function compareGroups(a: string, b: string) {
  const ia = groupOrder.indexOf(a)
  const ib = groupOrder.indexOf(b)
  return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib) || a.localeCompare(b)
}

export function AdminToolsPage() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [groupFilter, setGroupFilter] = useState<string>('all')
  const [riskFilter, setRiskFilter] = useState<string>('all')
  const [detail, setDetail] = useState<ToolRecord | null>(null)

  const tools = useQuery({
    queryKey: ['admin', 'tools'],
    queryFn: () => api.get<ToolRecord[]>('/api/admin/tools'),
  })

  const patch = useMutation({
    mutationFn: ({ name, body }: { name: string; body: Partial<Pick<ToolRecord, 'enabled' | 'requiresApproval' | 'minRole'>> }) =>
      api.patch<ToolRecord>(`/api/admin/tools/${encodeURIComponent(name)}`, body),
    onSuccess: (updated) => {
      queryClient.setQueryData<ToolRecord[]>(['admin', 'tools'], (prev) =>
        prev?.map((t) => (t.name === updated.name ? updated : t)),
      )
      setDetail((cur) => (cur && cur.name === updated.name ? updated : cur))
      void queryClient.invalidateQueries({ queryKey: ['admin', 'tools'] })
      void queryClient.invalidateQueries({ queryKey: ['me'] })
      notifyOk(`${updated.title || updated.name} 설정을 변경했습니다.`)
    },
    onError: (err) => notifyError(err, '도구 설정 변경 실패'),
  })

  const sync = useMutation({
    mutationFn: () => api.post<ToolRecord[]>('/api/admin/tools/sync'),
    onSuccess: (data) => {
      queryClient.setQueryData(['admin', 'tools'], data)
      notifyOk('빌드에 포함된 도구 목록을 반영했습니다.', '동기화 완료')
    },
    onError: (err) => notifyError(err, '동기화 실패'),
  })

  const groupData = useMemo(() => {
    const set = new Set<string>(groupOrder)
    for (const t of tools.data ?? []) if (t.group) set.add(t.group)
    return [
      { value: 'all', label: '전체 그룹' },
      ...[...set].sort(compareGroups).map((g) => ({ value: g, label: toolGroupLabels[g] ?? g })),
    ]
  }, [tools.data])

  const riskData = useMemo(() => {
    const set = new Set<string>(riskOrder)
    for (const t of tools.data ?? []) if (t.risk) set.add(t.risk)
    return [
      { value: 'all', label: '전체 위험도' },
      ...[...set].map((r) => ({ value: r, label: riskLabels[r] ?? r })),
    ]
  }, [tools.data])

  const grouped = useMemo(() => {
    const needle = search.trim().toLowerCase()
    const filtered = (tools.data ?? []).filter((tool) => {
      if (groupFilter !== 'all' && tool.group !== groupFilter) return false
      if (riskFilter !== 'all' && tool.risk !== riskFilter) return false
      if (!needle) return true
      return (
        tool.name.toLowerCase().includes(needle) ||
        (tool.title ?? '').toLowerCase().includes(needle) ||
        (tool.description ?? '').toLowerCase().includes(needle)
      )
    })
    const map = new Map<string, ToolRecord[]>()
    for (const t of filtered) {
      const list = map.get(t.group) ?? []
      list.push(t)
      map.set(t.group, list)
    }
    return [...map.entries()].sort(([a], [b]) => compareGroups(a, b))
  }, [tools.data, search, groupFilter, riskFilter])

  const total = tools.data?.length ?? 0
  const enabledCount = (tools.data ?? []).filter((t) => t.enabled).length

  return (
    <>
      <PageHeader
        title="MCP 도구"
        description="도구별 사용 여부, 승인 필요 여부, 최소 역할을 조정합니다. 고위험(이동·휴지통) 도구는 승인을 끌 수 없습니다."
        actions={
          <>
            <Button
              variant="default"
              leftSection={<IconRefresh size={18} />}
              onClick={() => void tools.refetch()}
              loading={tools.isFetching && !tools.isLoading}
            >
              새로 고침
            </Button>
            <Button
              variant="light"
              leftSection={<IconRotateClockwise size={18} />}
              loading={sync.isPending}
              onClick={() => sync.mutate()}
            >
              목록 동기화
            </Button>
          </>
        }
      />

      <Alert variant="light" color="confmcp" mb="lg" icon={<IconInfoCircle size={20} />}>
        도구를 켜도 요청자 본인의 Confluence 권한과 접근 정책을 통과하지 못하면 호출은 거부됩니다. 현재{' '}
        {total}개 중 {enabledCount}개가 활성 상태입니다.
      </Alert>

      <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm" mb="lg">
        <TextInput
          placeholder="이름·설명 검색"
          aria-label="도구 검색"
          leftSection={<IconSearch size={18} />}
          value={search}
          onChange={(event) => setSearch(event.currentTarget.value)}
        />
        <Select
          aria-label="그룹"
          data={groupData}
          value={groupFilter}
          onChange={(value) => setGroupFilter(value ?? 'all')}
          allowDeselect={false}
        />
        <Select
          aria-label="위험도"
          data={riskData}
          value={riskFilter}
          onChange={(value) => setRiskFilter(value ?? 'all')}
          allowDeselect={false}
        />
      </SimpleGrid>

      {tools.isLoading ? <LoadingBlock /> : null}
      {tools.error ? <ErrorBlock error={tools.error} /> : null}

      {tools.data && grouped.length === 0 ? (
        <Section>
          <EmptyState label="조건에 맞는 도구가 없습니다" />
        </Section>
      ) : null}

      {grouped.map(([group, list]) => (
        <Section key={group} title={`${toolGroupLabels[group] ?? group} (${list.length})`}>
          <TableScroll minWidth={1080}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>도구</Table.Th>
                  <Table.Th>위험도</Table.Th>
                  <Table.Th>필요 권한</Table.Th>
                  <Table.Th>스코프</Table.Th>
                  <Table.Th>최소 역할</Table.Th>
                  <Table.Th ta="center">활성</Table.Th>
                  <Table.Th ta="center">승인 필요</Table.Th>
                  <Table.Th ta="center">상세</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {list.map((tool) => {
                  const execute = tool.risk === 'EXECUTE'
                  const busy = patch.isPending && patch.variables?.name === tool.name
                  return (
                    <Table.Tr key={tool.name}>
                      <Table.Td maw={420}>
                        <Group gap={6} wrap="wrap">
                          <Text fw={600} size="sm">
                            {tool.title || tool.name}
                          </Text>
                          <Badge size="sm" variant="light" color={priorityColors[tool.priority] ?? 'gray'}>
                            {tool.priority || '—'}
                          </Badge>
                          {tool.write ? (
                            <Badge size="sm" variant="outline" color="orange">
                              쓰기
                            </Badge>
                          ) : null}
                          {tool.highLevel ? (
                            <Badge size="sm" variant="light" color="grape">
                              고수준
                            </Badge>
                          ) : null}
                        </Group>
                        <Text size="xs" ff="monospace" c="dimmed">
                          {tool.name}
                        </Text>
                        <Text size="xs" c="dimmed" lineClamp={2}>
                          {tool.description}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Badge variant="light" color={riskColors[tool.risk] ?? 'gray'}>
                          {riskLabels[tool.risk] ?? tool.risk}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{permissionLabel(tool.requiredPermission)}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Code>{tool.scope || '—'}</Code>
                      </Table.Td>
                      <Table.Td>
                        <Select
                          aria-label={`${tool.name} 최소 역할`}
                          data={roleSelectData(tool.minRole)}
                          value={tool.minRole || null}
                          onChange={(value) => {
                            if (value && value !== tool.minRole) {
                              patch.mutate({ name: tool.name, body: { minRole: value } })
                            }
                          }}
                          allowDeselect={false}
                          size="sm"
                          w={230}
                          disabled={busy}
                        />
                      </Table.Td>
                      <Table.Td ta="center">
                        <Group justify="center">
                          <Switch
                            checked={tool.enabled}
                            disabled={busy}
                            onChange={(event) =>
                              patch.mutate({ name: tool.name, body: { enabled: event.currentTarget.checked } })
                            }
                            aria-label={`${tool.name} 활성`}
                          />
                        </Group>
                      </Table.Td>
                      <Table.Td ta="center">
                        <Group justify="center">
                          <Tooltip
                            label="고위험 도구는 항상 별도 승인자의 승인이 필요합니다"
                            disabled={!execute}
                          >
                            <div>
                              <Switch
                                checked={execute ? true : tool.requiresApproval}
                                disabled={execute || busy}
                                onChange={(event) =>
                                  patch.mutate({
                                    name: tool.name,
                                    body: { requiresApproval: event.currentTarget.checked },
                                  })
                                }
                                aria-label={`${tool.name} 승인 필요`}
                              />
                            </div>
                          </Tooltip>
                        </Group>
                      </Table.Td>
                      <Table.Td ta="center">
                        <ActionIcon
                          variant="subtle"
                          onClick={() => setDetail(tool)}
                          aria-label={`${tool.name} 상세`}
                        >
                          <IconInfoCircle size={20} />
                        </ActionIcon>
                      </Table.Td>
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          </TableScroll>
        </Section>
      ))}

      <Drawer
        opened={detail !== null}
        onClose={() => setDetail(null)}
        position="right"
        size="lg"
        title={<Text fw={700}>{detail?.title || detail?.name}</Text>}
      >
        {detail ? (
          <Stack gap="md">
            <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
              {detail.description || '설명이 없습니다.'}
            </Text>
            <StatList
              items={[
                { label: '이름', value: <Code>{detail.name}</Code> },
                { label: '그룹', value: toolGroupLabels[detail.group] ?? detail.group },
                { label: '우선순위', value: detail.priority || '—' },
                { label: '위험도', value: riskLabels[detail.risk] ?? detail.risk },
                { label: '쓰기 도구', value: detail.write ? '예' : '아니요' },
                { label: '고수준 도구', value: detail.highLevel ? '예' : '아니요' },
                { label: '필요 권한', value: permissionLabel(detail.requiredPermission) },
                { label: '스코프', value: detail.scope || '—' },
                { label: '최소 역할', value: roleLabels[detail.minRole] ?? detail.minRole },
                { label: '활성', value: detail.enabled ? '예' : '아니요' },
                { label: '승인 필요', value: detail.requiresApproval || detail.risk === 'EXECUTE' ? '예' : '아니요' },
                { label: '변경 시각', value: formatDateTime(detail.updatedAt) },
              ]}
            />
            <div>
              <Title order={5} mb="xs">
                입력 스키마
              </Title>
              {detail.inputSchema ? (
                <JsonBlock value={detail.inputSchema} maxHeight={480} />
              ) : (
                <EmptyState label="입력 스키마가 없습니다" />
              )}
            </div>
          </Stack>
        ) : null}
      </Drawer>
    </>
  )
}
