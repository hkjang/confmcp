import {
  Alert,
  Autocomplete,
  Badge,
  Button,
  Group,
  Modal,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import {
  IconAlertTriangle,
  IconHistory,
  IconLink,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconUsers,
} from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import {
  ConfirmButton,
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  Section,
  TableScroll,
  notifyError,
  notifyOk,
} from '../../components/ui'
import { api, qs, type Mapping, type MappingError, type MappingHistory, type User } from '../../lib/api'
import { evidenceLabels, formatDateTime, formatRelative } from '../../lib/format'

const stateOptions = [
  { value: 'all', label: '전체' },
  { value: 'active', label: '활성' },
  { value: 'inactive', label: '비활성' },
  { value: 'unverified', label: '미확인' },
  { value: 'error', label: '오류 있음' },
]

const historyActionLabels: Record<string, string> = {
  'map:same_directory': '자동 매핑 (같은 디렉터리)',
  'map:admin_confirmed': '관리자 매핑',
  'map:delegated_auth': '본인 로그인으로 매핑',
  verified: '재확인 성공',
  'verified:delegated': '본인 로그인으로 확인',
  verify_failed: '재확인 실패',
  activate: '활성화',
  deactivate: '비활성화',
  delete: '삭제',
}

/**
 * subPath encodes a Keycloak subject for a URL path segment. Characters Go
 * keeps literal in paths (such as the ":" in "local:alice") stay literal so the
 * router sees the decoded value.
 */
function subPath(sub: string): string {
  return encodeURIComponent(sub).replace(/%(3A|40|24|26|2B|2C|3B|3D)/gi, (m) => decodeURIComponent(m))
}

export function AdminIdentityPage() {
  const [tab, setTab] = useState<string>('mappings')
  const [createOpen, setCreateOpen] = useState(false)

  return (
    <>
      <PageHeader
        title="사용자 매핑"
        description="콘솔 사용자(Keycloak 또는 로컬 계정)와 Confluence 계정의 연결을 관리합니다. 매핑이 없는 사용자는 MCP 도구를 쓸 수 없습니다."
        actions={
          <Button leftSection={<IconPlus size={18} />} onClick={() => setCreateOpen(true)}>
            수동 매핑
          </Button>
        }
      />

      <Tabs value={tab} onChange={(v) => setTab(v ?? 'mappings')} keepMounted={false}>
        <Tabs.List mb="md">
          <Tabs.Tab value="mappings" leftSection={<IconUsers size={16} />}>
            매핑
          </Tabs.Tab>
          <Tabs.Tab value="errors" leftSection={<IconAlertTriangle size={16} />}>
            실패 기록
          </Tabs.Tab>
          <Tabs.Tab value="history" leftSection={<IconHistory size={16} />}>
            변경 이력
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="mappings">
          <MappingsTab />
        </Tabs.Panel>
        <Tabs.Panel value="errors">
          <ErrorsTab />
        </Tabs.Panel>
        <Tabs.Panel value="history">
          <HistoryTab />
        </Tabs.Panel>
      </Tabs>

      <CreateMappingModal opened={createOpen} onClose={() => setCreateOpen(false)} />
    </>
  )
}

function MappingsTab() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [state, setState] = useState<string>('all')
  const [q] = useDebouncedValue(search.trim(), 300)

  const mappings = useQuery({
    queryKey: ['admin', 'mappings', q, state],
    queryFn: () =>
      api.get<Mapping[]>(`/api/admin/identity/mappings${qs({ q, state: state === 'all' ? '' : state })}`),
  })

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'mappings'] })
    void queryClient.invalidateQueries({ queryKey: ['admin', 'mapping-history'] })
    void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
  }

  const verify = useMutation({
    mutationFn: (sub: string) =>
      api.post<{ ok: boolean; error?: string; mapping?: Mapping }>(`/api/admin/identity/mappings/${subPath(sub)}/verify`),
    onSuccess: (res) => {
      if (res.ok) notifyOk('Confluence 계정을 다시 확인했습니다.', '재확인 성공')
      else notifyError(new Error(res.error ?? '확인하지 못했습니다'), '재확인 실패')
      refresh()
    },
    onError: (err) => notifyError(err, '재확인 실패'),
  })

  const setActive = useMutation({
    mutationFn: ({ sub, active }: { sub: string; active: boolean }) =>
      api.post(`/api/admin/identity/mappings/${subPath(sub)}/active`, { active }),
    onSuccess: (_, v) => {
      notifyOk(v.active ? '매핑을 활성화했습니다.' : '매핑을 비활성화했습니다.')
      refresh()
    },
    onError: (err) => notifyError(err),
  })

  const remove = useMutation({
    mutationFn: (sub: string) => api.del(`/api/admin/identity/mappings/${subPath(sub)}`),
    onSuccess: () => {
      notifyOk('매핑을 삭제했습니다.')
      refresh()
    },
    onError: (err) => notifyError(err, '삭제 실패'),
  })

  return (
    <Section>
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md" mb="md">
        <TextInput
          label="검색"
          placeholder="콘솔 또는 Confluence 사용자명"
          leftSection={<IconSearch size={18} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
        />
        <Select
          label="상태"
          data={stateOptions}
          value={state}
          onChange={(v) => setState(v ?? 'all')}
        />
      </SimpleGrid>

      {mappings.isLoading ? <LoadingBlock /> : null}
      {mappings.error ? <ErrorBlock error={mappings.error} /> : null}
      {mappings.data && mappings.data.length === 0 ? <EmptyState label="조건에 맞는 매핑이 없습니다" /> : null}
      {mappings.data && mappings.data.length > 0 ? (
        <TableScroll minWidth={1080}>
          <Table striped highlightOnHover verticalSpacing="sm" fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>콘솔 사용자</Table.Th>
                <Table.Th>Confluence 사용자</Table.Th>
                <Table.Th>userKey</Table.Th>
                <Table.Th>근거</Table.Th>
                <Table.Th>상태</Table.Th>
                <Table.Th>매핑 시각</Table.Th>
                <Table.Th>마지막 확인</Table.Th>
                <Table.Th>작업</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {mappings.data.map((m) => (
                <Table.Tr key={m.keycloakSub}>
                  <Table.Td>
                    <Text size="sm" fw={600}>
                      {m.keycloakUsername || '—'}
                    </Text>
                    <Text size="xs" c="dimmed" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                      {m.keycloakSub}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{m.confluenceDisplay || m.confluenceUsername}</Text>
                    <Text size="xs" c="dimmed">
                      {m.confluenceUsername}
                      {m.confluenceEmail ? ` · ${m.confluenceEmail}` : ''}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="xs" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                      {m.confluenceUserKey}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color="gray" style={{ textTransform: 'none' }}>
                      {evidenceLabels[m.evidence] ?? m.evidence}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Stack gap={4} align="flex-start">
                      <Badge color={m.active ? 'teal' : 'gray'} variant="light">
                        {m.active ? '활성' : '비활성'}
                      </Badge>
                      {!m.verifiedAt ? (
                        <Badge color="yellow" variant="light">
                          미확인
                        </Badge>
                      ) : null}
                      {m.lastError ? (
                        <Tooltip label={m.lastError} multiline maw={360}>
                          <Badge color="red" variant="light" style={{ cursor: 'help' }}>
                            오류
                          </Badge>
                        </Tooltip>
                      ) : null}
                    </Stack>
                  </Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(m.mappedAt)}</Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>
                    <Text size="sm">{formatRelative(m.verifiedAt)}</Text>
                    {m.verifiedAt ? (
                      <Text size="xs" c="dimmed">
                        {formatDateTime(m.verifiedAt)}
                      </Text>
                    ) : null}
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs" wrap="nowrap">
                      <Button
                        size="xs"
                        variant="light"
                        leftSection={<IconRefresh size={14} />}
                        loading={verify.isPending && verify.variables === m.keycloakSub}
                        onClick={() => verify.mutate(m.keycloakSub)}
                      >
                        재확인
                      </Button>
                      <Button
                        size="xs"
                        variant="default"
                        loading={setActive.isPending && setActive.variables?.sub === m.keycloakSub}
                        onClick={() => setActive.mutate({ sub: m.keycloakSub, active: !m.active })}
                      >
                        {m.active ? '비활성' : '활성'}
                      </Button>
                      <ConfirmButton
                        size="xs"
                        color="red"
                        title="매핑 삭제"
                        message={`${m.keycloakUsername} ↔ ${m.confluenceUsername} 매핑을 삭제합니다. 이 사용자는 다시 매핑될 때까지 MCP 도구를 쓸 수 없습니다.`}
                        confirmLabel="삭제"
                        loading={remove.isPending && remove.variables === m.keycloakSub}
                        onConfirm={() => remove.mutate(m.keycloakSub)}
                      >
                        삭제
                      </ConfirmButton>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </TableScroll>
      ) : null}
    </Section>
  )
}

function ErrorsTab() {
  const queryClient = useQueryClient()
  const errors = useQuery({
    queryKey: ['admin', 'mapping-errors'],
    queryFn: () => api.get<MappingError[]>('/api/admin/identity/errors'),
  })
  const clear = useMutation({
    mutationFn: () => api.del('/api/admin/identity/errors'),
    onSuccess: () => {
      notifyOk('실패 기록을 비웠습니다.')
      void queryClient.invalidateQueries({ queryKey: ['admin', 'mapping-errors'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
    onError: (err) => notifyError(err),
  })

  return (
    <Section
      title="매핑 실패 기록"
      description="로그인이나 도구 호출 중 Confluence 계정을 찾지 못한 기록입니다. 원인을 해결한 뒤 비우십시오."
      actions={
        <ConfirmButton
          size="xs"
          color="red"
          title="실패 기록 비우기"
          message="매핑 실패 기록을 모두 삭제합니다."
          confirmLabel="비우기"
          loading={clear.isPending}
          disabled={!errors.data || errors.data.length === 0}
          onConfirm={() => clear.mutate()}
        >
          기록 비우기
        </ConfirmButton>
      }
    >
      {errors.isLoading ? <LoadingBlock /> : null}
      {errors.error ? <ErrorBlock error={errors.error} /> : null}
      {errors.data && errors.data.length === 0 ? <EmptyState label="매핑 실패 기록이 없습니다" /> : null}
      {errors.data && errors.data.length > 0 ? (
        <TableScroll minWidth={760}>
          <Table striped verticalSpacing="sm" fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>사용자</Table.Th>
                <Table.Th>사유</Table.Th>
                <Table.Th ta="right">횟수</Table.Th>
                <Table.Th>처음</Table.Th>
                <Table.Th>최근</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {errors.data.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>
                      {e.keycloakUsername || '—'}
                    </Text>
                    <Text size="xs" c="dimmed" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                      {e.keycloakSub}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                      {e.reason}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right">{e.occurrences}</Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(e.firstSeenAt)}</Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(e.occurredAt)}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </TableScroll>
      ) : null}
    </Section>
  )
}

function HistoryTab() {
  const history = useQuery({
    queryKey: ['admin', 'mapping-history'],
    queryFn: () => api.get<MappingHistory[]>('/api/admin/identity/history'),
  })

  return (
    <Section title="변경 이력" description="매핑 생성·재확인·활성 변경·삭제 기록입니다 (최근 100건).">
      {history.isLoading ? <LoadingBlock /> : null}
      {history.error ? <ErrorBlock error={history.error} /> : null}
      {history.data && history.data.length === 0 ? <EmptyState label="변경 이력이 없습니다" /> : null}
      {history.data && history.data.length > 0 ? (
        <TableScroll minWidth={820}>
          <Table striped verticalSpacing="sm" fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>시각</Table.Th>
                <Table.Th>대상</Table.Th>
                <Table.Th>변경</Table.Th>
                <Table.Th>Confluence 계정</Table.Th>
                <Table.Th>처리자</Table.Th>
                <Table.Th>메모</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {history.data.map((h) => (
                <Table.Tr key={h.id}>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>{formatDateTime(h.occurredAt)}</Table.Td>
                  <Table.Td>
                    <Text size="xs" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                      {h.keycloakSub}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color="gray" style={{ textTransform: 'none' }}>
                      {historyActionLabels[h.action] ?? h.action}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{h.confluenceUsername || '—'}</Text>
                    {h.confluenceUserKey ? (
                      <Text size="xs" c="dimmed" ff="monospace">
                        {h.confluenceUserKey}
                      </Text>
                    ) : null}
                  </Table.Td>
                  <Table.Td>{h.actor || '—'}</Table.Td>
                  <Table.Td>
                    <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                      {h.note || '—'}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </TableScroll>
      ) : null}
    </Section>
  )
}

function CreateMappingModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [confluenceUsername, setConfluenceUsername] = useState('')

  const users = useQuery({
    queryKey: ['admin', 'users'],
    queryFn: () => api.get<User[]>('/api/admin/users'),
    enabled: opened,
  })
  const userOptions = Array.from(new Set((users.data ?? []).map((u) => u.username)))

  const create = useMutation({
    mutationFn: () =>
      api.post<Mapping>('/api/admin/identity/mappings', {
        username: username.trim(),
        confluenceUsername: confluenceUsername.trim(),
      }),
    onSuccess: (m) => {
      notifyOk(`${m.keycloakUsername} ↔ ${m.confluenceUsername} 매핑을 만들었습니다.`)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'mappings'] })
      void queryClient.invalidateQueries({ queryKey: ['admin', 'mapping-history'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
      setUsername('')
      setConfluenceUsername('')
      onClose()
    },
    onError: (err) => notifyError(err, '매핑 실패'),
  })

  const canSubmit = username.trim() !== '' && confluenceUsername.trim() !== ''

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={700}>수동 매핑</Text>} size="lg">
      <Stack>
        <Alert variant="light" color="yellow" icon={<IconAlertTriangle size={20} />}>
          두 계정이 같은 사람의 것인지 관리자가 직접 확인했을 때만 매핑하십시오. 매핑된 사용자는 해당 Confluence 계정의
          권한으로 문서를 읽고 씁니다. 근거는 &ldquo;관리자 확인&rdquo; 으로 기록됩니다.
        </Alert>
        <Autocomplete
          label="콘솔 사용자명"
          description="한 번 로그인했거나 로컬로 만든 사용자여야 합니다."
          placeholder="alice"
          data={userOptions}
          value={username}
          onChange={setUsername}
          limit={20}
          comboboxProps={{ withinPortal: true }}
        />
        <TextInput
          label="Confluence 사용자명"
          description="Confluence 에 실제로 있는 계정이어야 합니다. 저장 시 존재 여부와 userKey 를 확인합니다."
          placeholder="alice"
          value={confluenceUsername}
          onChange={(e) => setConfluenceUsername(e.currentTarget.value)}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            취소
          </Button>
          <Button
            leftSection={<IconLink size={18} />}
            loading={create.isPending}
            disabled={!canSubmit}
            onClick={() => create.mutate()}
          >
            매핑
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}
