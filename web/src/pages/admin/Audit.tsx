import {
  Badge,
  Button,
  Code,
  Drawer,
  Group,
  Pagination,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
} from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { IconEraser, IconFilterOff, IconRefresh } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

import {
  ConfirmButton,
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
import { api, qs, type AuditEntry, type AuditPage } from '../../lib/api'
import {
  auditCategoryLabels,
  authModeLabels,
  errorCodeOptions,
  formatDateTime,
  formatNumber,
  resultLabel,
} from '../../lib/format'

const categoryData = [
  { value: 'all', label: '전체 분류' },
  ...Object.entries(auditCategoryLabels).map(([value, label]) => ({ value, label })),
]

const codeData = [{ value: 'all', label: '전체 결과 코드' }, ...errorCodeOptions]

const successData = [
  { value: 'all', label: '성공·실패 전체' },
  { value: 'true', label: '성공만' },
  { value: 'false', label: '실패만' },
]

const pageSizeData = [
  { value: '25', label: '25개씩' },
  { value: '50', label: '50개씩' },
  { value: '100', label: '100개씩' },
  { value: '200', label: '200개씩' },
]

const decisionLabels: Record<string, string> = {
  allow: '허용',
  deny: '거부',
  approve: '승인',
  reject: '거절',
  pending: '승인 대기',
}

const decisionColors: Record<string, string> = {
  allow: 'teal',
  deny: 'red',
  approve: 'teal',
  reject: 'red',
  pending: 'yellow',
}

/** dayStart converts a yyyy-mm-dd input into the local midnight as RFC 3339. */
function dayStart(value: string, addDays = 0): string | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!m) return undefined
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + addDays)
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString()
}

interface Filters {
  category: string
  username: string
  tool: string
  space: string
  contentId: string
  code: string
  success: string
  since: string
  until: string
}

const emptyFilters: Filters = {
  category: 'all',
  username: '',
  tool: '',
  space: '',
  contentId: '',
  code: 'all',
  success: 'all',
  since: '',
  until: '',
}

export function AdminAuditPage() {
  const queryClient = useQueryClient()
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [debounced] = useDebouncedValue(filters, 350)
  const [pageSize, setPageSize] = useState<string>('50')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<AuditEntry | null>(null)

  const set = <K extends keyof Filters>(key: K, value: Filters[K]) => setFilters((f) => ({ ...f, [key]: value }))

  useEffect(() => setPage(1), [debounced, pageSize])

  const limit = Number(pageSize)
  const params = useMemo(
    () => ({
      category: debounced.category === 'all' ? undefined : debounced.category,
      username: debounced.username.trim(),
      tool: debounced.tool.trim(),
      space: debounced.space.trim(),
      contentId: debounced.contentId.trim(),
      code: debounced.code === 'all' ? undefined : debounced.code,
      success: debounced.success === 'all' ? undefined : debounced.success,
      since: dayStart(debounced.since),
      until: dayStart(debounced.until, 1),
      limit,
      offset: (page - 1) * limit,
    }),
    [debounced, limit, page],
  )

  const audit = useQuery({
    queryKey: ['admin', 'audit', params],
    queryFn: () => api.get<AuditPage>(`/api/admin/audit${qs(params)}`),
    placeholderData: (prev) => prev,
  })

  const purge = useMutation({
    mutationFn: () => api.post<{ ok: boolean; retainDays: number }>('/api/admin/audit/purge'),
    onSuccess: (res) => {
      notifyOk(
        res?.retainDays && res.retainDays > 0
          ? `${res.retainDays}일보다 오래된 감사 로그를 정리했습니다.`
          : '보존 기간이 0(무제한)이라 정리할 항목이 없습니다.',
      )
      void queryClient.invalidateQueries({ queryKey: ['admin', 'audit'] })
    },
    onError: (err) => notifyError(err, '정리 실패'),
  })

  const total = audit.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / limit))
  const rows = audit.data?.values ?? []
  const filtered = JSON.stringify(filters) !== JSON.stringify(emptyFilters)

  return (
    <>
      <PageHeader
        title="감사 로그"
        description="로그인, 도구 호출, 쓰기, 거부, 승인, 관리 변경이 모두 기록됩니다. 행을 누르면 상세 내용을 봅니다."
        actions={
          <>
            <Button
              variant="default"
              leftSection={<IconRefresh size={18} />}
              onClick={() => void audit.refetch()}
              loading={audit.isFetching && !audit.isLoading}
            >
              새로 고침
            </Button>
            <ConfirmButton
              color="orange"
              leftSection={<IconEraser size={18} />}
              title="보존 기간 정리"
              message="보안 설정의 '감사 로그 보존 기간'보다 오래된 감사 로그를 삭제합니다. 삭제한 기록은 되돌릴 수 없습니다."
              confirmLabel="정리"
              loading={purge.isPending}
              onConfirm={() => purge.mutate()}
            >
              보존 기간 정리
            </ConfirmButton>
          </>
        }
      />

      <Section
        title="검색 조건"
        actions={
          <Button
            size="xs"
            variant="subtle"
            leftSection={<IconFilterOff size={16} />}
            disabled={!filtered}
            onClick={() => setFilters(emptyFilters)}
          >
            조건 초기화
          </Button>
        }
      >
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 5 }} spacing="sm">
          <Select
            label="분류"
            data={categoryData}
            value={filters.category}
            onChange={(v) => set('category', v ?? 'all')}
            allowDeselect={false}
          />
          <TextInput
            label="사용자"
            placeholder="Keycloak·Confluence 사용자명"
            value={filters.username}
            onChange={(e) => set('username', e.currentTarget.value)}
          />
          <TextInput
            label="도구"
            placeholder="confluence_get_page"
            value={filters.tool}
            onChange={(e) => set('tool', e.currentTarget.value)}
          />
          <TextInput
            label="공간"
            placeholder="공간 키"
            value={filters.space}
            onChange={(e) => set('space', e.currentTarget.value)}
          />
          <TextInput
            label="콘텐츠 ID"
            value={filters.contentId}
            onChange={(e) => set('contentId', e.currentTarget.value)}
          />
          <Select
            label="결과 코드"
            data={codeData}
            value={filters.code}
            onChange={(v) => set('code', v ?? 'all')}
            allowDeselect={false}
            searchable
          />
          <Select
            label="성공 여부"
            data={successData}
            value={filters.success}
            onChange={(v) => set('success', v ?? 'all')}
            allowDeselect={false}
          />
          <TextInput
            label="시작일"
            type="date"
            value={filters.since}
            onChange={(e) => set('since', e.currentTarget.value)}
          />
          <TextInput
            label="종료일"
            type="date"
            value={filters.until}
            onChange={(e) => set('until', e.currentTarget.value)}
          />
          <Select
            label="페이지 크기"
            data={pageSizeData}
            value={pageSize}
            onChange={(v) => setPageSize(v ?? '50')}
            allowDeselect={false}
          />
        </SimpleGrid>
      </Section>

      <Section title={`기록 ${formatNumber(total)}건`}>
        {audit.isLoading ? <LoadingBlock /> : null}
        {audit.error ? <ErrorBlock error={audit.error} /> : null}
        {audit.data && rows.length === 0 ? <EmptyState label="조건에 맞는 기록이 없습니다" /> : null}

        {rows.length > 0 ? (
          <TableScroll minWidth={1180}>
            <Table striped fz="sm" style={{ opacity: audit.isPlaceholderData ? 0.6 : 1 }}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>시각</Table.Th>
                  <Table.Th>분류</Table.Th>
                  <Table.Th>요청자</Table.Th>
                  <Table.Th>실행 계정</Table.Th>
                  <Table.Th>도구 / 동작</Table.Th>
                  <Table.Th>공간 / 콘텐츠</Table.Th>
                  <Table.Th>판정</Table.Th>
                  <Table.Th>결과</Table.Th>
                  <Table.Th ta="right">지연(ms)</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((e) => (
                  <Table.Tr
                    key={e.id}
                    style={{ cursor: 'pointer' }}
                    tabIndex={0}
                    onClick={() => setSelected(e)}
                    onKeyDown={(ev) => {
                      if (ev.key === 'Enter') setSelected(e)
                    }}
                  >
                    <Table.Td>
                      <Text size="sm" style={{ whiteSpace: 'nowrap' }}>
                        {formatDateTime(e.occurredAt)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" color={e.category === 'denied' || e.category === 'error' ? 'red' : 'gray'}>
                        {auditCategoryLabels[e.category] ?? e.category}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{e.keycloakUsername || '—'}</Text>
                      {e.confluenceUsername && e.confluenceUsername !== e.keycloakUsername ? (
                        <Text size="xs" c="dimmed">
                          Confluence: {e.confluenceUsername}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {e.executedAs || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td maw={260}>
                      <Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                        {e.toolName || '—'}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {e.action}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{e.spaceKey || '—'}</Text>
                      {e.contentId ? (
                        <Text size="xs" c="dimmed" ff="monospace">
                          {e.contentId}
                          {e.contentVersion ? ` v${e.contentVersion}` : ''}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      {e.decision ? (
                        <Badge variant="light" color={decisionColors[e.decision] ?? 'gray'}>
                          {decisionLabels[e.decision] ?? e.decision}
                        </Badge>
                      ) : (
                        <Text size="sm">—</Text>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c={e.success ? undefined : 'red'} fw={e.success ? undefined : 600}>
                        {resultLabel(e.success, e.errorCode)}
                      </Text>
                    </Table.Td>
                    <Table.Td ta="right">
                      <Text size="sm">{formatNumber(e.latencyMs)}</Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}

        {total > limit ? (
          <Group justify="center" mt="md">
            <Pagination size="sm" total={pages} value={Math.min(page, pages)} onChange={setPage} siblings={1} boundaries={1} />
          </Group>
        ) : null}
      </Section>

      <Drawer
        opened={selected !== null}
        onClose={() => setSelected(null)}
        position="right"
        size="lg"
        title={<Text fw={700}>감사 기록 #{selected?.id}</Text>}
      >
        {selected ? (
          <Stack gap="md">
            <StatList
              items={[
                { label: '시각', value: formatDateTime(selected.occurredAt) },
                { label: '분류', value: auditCategoryLabels[selected.category] ?? selected.category },
                { label: '동작', value: selected.action || '—' },
                {
                  label: '결과',
                  value: (
                    <Text span c={selected.success ? 'teal' : 'red'} fw={700} size="sm">
                      {resultLabel(selected.success, selected.errorCode)}
                      {selected.errorCode ? ` (${selected.errorCode})` : ''}
                    </Text>
                  ),
                },
                {
                  label: '판정',
                  value: selected.decision ? (decisionLabels[selected.decision] ?? selected.decision) : '—',
                },
                { label: 'Keycloak 사용자', value: selected.keycloakUsername || '—' },
                { label: 'Keycloak sub', value: selected.keycloakSub || '—' },
                { label: 'Confluence 사용자', value: selected.confluenceUsername || '—' },
                { label: 'Confluence userKey', value: selected.confluenceUserKey || '—' },
                { label: '실행 계정', value: selected.executedAs || '—' },
                {
                  label: '인증 방식',
                  value: selected.authMode ? (authModeLabels[selected.authMode] ?? selected.authMode) : '—',
                },
                { label: 'MCP 클라이언트', value: selected.mcpClient || '—' },
                { label: '도구', value: selected.toolName || '—' },
                { label: '공간', value: selected.spaceKey || '—' },
                {
                  label: '콘텐츠',
                  value: selected.contentId
                    ? `${selected.contentId}${selected.contentVersion ? ` (v${selected.contentVersion})` : ''}`
                    : '—',
                },
                { label: '승인 요청', value: selected.approvalId || '—' },
                { label: '요청 ID', value: selected.requestId || '—' },
                { label: '접속 주소', value: selected.ip || '—' },
                { label: '지연', value: `${formatNumber(selected.latencyMs)} ms` },
              ]}
            />
            {selected.message ? (
              <div>
                <Title order={5} mb="xs">
                  메시지
                </Title>
                <Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                  {selected.message}
                </Code>
              </div>
            ) : null}
            <div>
              <Title order={5} mb="xs">
                상세
              </Title>
              {selected.detail && Object.keys(selected.detail).length > 0 ? (
                <JsonBlock value={selected.detail} maxHeight={420} />
              ) : (
                <Text size="sm" c="dimmed">
                  추가 상세 정보가 없습니다.
                </Text>
              )}
            </div>
          </Stack>
        ) : null}
      </Drawer>
    </>
  )
}
