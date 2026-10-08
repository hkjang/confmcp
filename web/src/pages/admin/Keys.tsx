import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Modal,
  Paper,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
  Textarea,
} from '@mantine/core'
import {
  IconAlertTriangle,
  IconKey,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSettings,
  IconShieldLock,
} from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import {
  ConfirmButton,
  CopyField,
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  SaveBar,
  Section,
  StatusBadge,
  TableScroll,
  notifyError,
  notifyOk,
} from '../../components/ui'
import { api, type ApiKey, type IssuedKey, type KeyRole, type ScopeInfo } from '../../lib/api'
import {
  formatDateTime,
  formatRelative,
  keyStatusColors,
  keyStatusLabels,
  riskColors,
  riskLabels,
} from '../../lib/format'
import { useSettingsGroup } from '../../lib/useSettingsGroup'

interface KeyPolicy {
  defaultRole: string
  rotationDays: number
  keyTtlDays: number
  maxKeysPerUser: number
  allowSelfCreate: boolean
  graceHours: number
}

const riskOrder = ['READ', 'WRITE', 'EXECUTE', 'ADMIN']

const statusFilterData = [
  { value: 'all', label: '전체 상태' },
  { value: 'active', label: '정상' },
  { value: 'rotation_due', label: '회전 필요' },
  { value: 'expired', label: '만료' },
  { value: 'revoked', label: '폐기' },
]

function useKeyRoles() {
  return useQuery({ queryKey: ['admin', 'key-roles'], queryFn: () => api.get<KeyRole[]>('/api/admin/key-roles') })
}

function useKeyScopes() {
  return useQuery({ queryKey: ['admin', 'key-scopes'], queryFn: () => api.get<ScopeInfo[]>('/api/admin/key-scopes') })
}

// ---------------------------------------------------------------- keys tab

function KeysTab() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<string>('all')
  const [issued, setIssued] = useState<IssuedKey | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<ApiKey | null>(null)
  const [reason, setReason] = useState('')

  const keys = useQuery({ queryKey: ['admin', 'keys'], queryFn: () => api.get<ApiKey[]>('/api/admin/keys') })
  const scopes = useKeyScopes()
  const scopeLabel = useMemo(() => {
    const m: Record<string, ScopeInfo> = {}
    for (const s of scopes.data ?? []) m[s.name] = s
    return m
  }, [scopes.data])

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'keys'] })
    void queryClient.invalidateQueries({ queryKey: ['me', 'keys'] })
    void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
  }

  const rotate = useMutation({
    mutationFn: (id: string) => api.post<IssuedKey>(`/api/admin/keys/${id}/rotate`),
    onSuccess: (data) => {
      setIssued(data)
      invalidate()
    },
    onError: (err) => notifyError(err, '회전 실패'),
  })

  const revoke = useMutation({
    mutationFn: (vars: { id: string; reason: string }) =>
      api.post(`/api/admin/keys/${vars.id}/revoke`, { reason: vars.reason }),
    onSuccess: () => {
      notifyOk('키를 폐기했습니다.')
      setRevokeTarget(null)
      invalidate()
    },
    onError: (err) => notifyError(err, '폐기 실패'),
  })

  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return (keys.data ?? []).filter((k) => {
      if (status !== 'all' && k.status !== status) return false
      if (!needle) return true
      return (
        (k.username ?? '').toLowerCase().includes(needle) ||
        k.name.toLowerCase().includes(needle) ||
        k.prefix.toLowerCase().includes(needle) ||
        k.role.toLowerCase().includes(needle)
      )
    })
  }, [keys.data, search, status])

  return (
    <>
      <Section
        title="발급된 키"
        description="모든 사용자의 개인 API 키입니다. 키 비밀값은 저장하지 않으므로 회전하면 새 비밀값을 한 번만 보여 줍니다."
        actions={
          <Button
            size="sm"
            variant="default"
            leftSection={<IconRefresh size={16} />}
            onClick={() => void keys.refetch()}
            loading={keys.isFetching && !keys.isLoading}
          >
            새로 고침
          </Button>
        }
      >
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm" mb="md">
          <TextInput
            aria-label="키 검색"
            placeholder="소유자·이름·접두사·역할 검색"
            leftSection={<IconSearch size={18} />}
            value={search}
            onChange={(event) => setSearch(event.currentTarget.value)}
          />
          <Select
            aria-label="상태"
            data={statusFilterData}
            value={status}
            onChange={(value) => setStatus(value ?? 'all')}
            allowDeselect={false}
          />
        </SimpleGrid>

        {keys.isLoading ? <LoadingBlock /> : null}
        {keys.error ? <ErrorBlock error={keys.error} /> : null}
        {keys.data && rows.length === 0 ? <EmptyState label="해당하는 키가 없습니다" /> : null}

        {rows.length > 0 ? (
          <TableScroll minWidth={1320}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>소유자</Table.Th>
                  <Table.Th>이름 / 접두사</Table.Th>
                  <Table.Th>역할</Table.Th>
                  <Table.Th>스코프</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>회전 기한</Table.Th>
                  <Table.Th>만료</Table.Th>
                  <Table.Th>최근 사용</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((k) => {
                  const revoked = k.status === 'revoked'
                  return (
                    <Table.Tr key={k.id}>
                      <Table.Td>
                        <Text size="sm" fw={600}>
                          {k.username || `#${k.userId}`}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{k.name || '—'}</Text>
                        <Text size="xs" ff="monospace" c="dimmed">
                          {k.prefix}…
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Badge variant="light" color="gray">
                          {k.role || '—'}
                        </Badge>
                      </Table.Td>
                      <Table.Td maw={300}>
                        <Group gap={4} wrap="wrap">
                          {(k.scopes ?? []).map((s) => (
                            <Badge
                              key={s}
                              size="sm"
                              variant="light"
                              color={riskColors[scopeLabel[s]?.risk ?? ''] ?? 'gray'}
                            >
                              {scopeLabel[s]?.label ?? s}
                            </Badge>
                          ))}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <StatusBadge value={k.status} labels={keyStatusLabels} colors={keyStatusColors} />
                        {k.revokedReason ? (
                          <Text size="xs" c="dimmed" mt={2} maw={180} style={{ overflowWrap: 'anywhere' }}>
                            {k.revokedReason}
                          </Text>
                        ) : null}
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" style={{ whiteSpace: 'nowrap' }}>{formatDateTime(k.rotationDueAt)}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" style={{ whiteSpace: 'nowrap' }}>{formatDateTime(k.expiresAt)}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{formatRelative(k.lastUsedAt)}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Group gap="xs" wrap="nowrap" justify="flex-end">
                          <ConfirmButton
                            size="xs"
                            title="키 회전"
                            message={`${k.username ?? ''} 사용자의 '${k.name}' 키를 회전합니다. 새 키를 발급하고 기존 키는 유예 시간이 지나면 만료됩니다(유예 0 이면 즉시 폐기). 새 비밀값은 한 번만 표시되므로 사용자에게 안전하게 전달하십시오.`}
                            confirmLabel="회전"
                            disabled={revoked || k.status === 'expired'}
                            loading={rotate.isPending && rotate.variables === k.id}
                            onConfirm={() => rotate.mutate(k.id)}
                          >
                            회전
                          </ConfirmButton>
                          <Button
                            size="xs"
                            color="red"
                            variant="light"
                            disabled={revoked}
                            onClick={() => {
                              setRevokeTarget(k)
                              setReason('')
                            }}
                          >
                            폐기
                          </Button>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}
      </Section>

      <Modal
        opened={issued !== null}
        onClose={() => setIssued(null)}
        title={<Text fw={700}>새 키 비밀값</Text>}
        size="lg"
        closeOnClickOutside={false}
      >
        {issued ? (
          <Stack>
            <Alert color="orange" variant="light" icon={<IconAlertTriangle size={20} />} title="지금 한 번만 표시됩니다">
              이 창을 닫으면 비밀값을 다시 볼 수 없습니다. 복사해서 키 소유자({issued.key.username ?? `#${issued.key.userId}`})
              에게 안전한 경로로 전달하십시오.
            </Alert>
            <CopyField value={issued.secret} label="비밀값 복사" />
            <Text size="sm" c="dimmed">
              새 키 접두사 <b>{issued.key.prefix}</b> · 역할 {issued.key.role}
              {issued.graceUntil ? ` · 기존 키 유예 ${formatDateTime(issued.graceUntil)} 까지` : ''}
            </Text>
            <Group justify="flex-end">
              <Button onClick={() => setIssued(null)}>복사했습니다</Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>

      <Modal
        opened={revokeTarget !== null}
        onClose={() => setRevokeTarget(null)}
        title={<Text fw={700}>키 폐기</Text>}
      >
        {revokeTarget ? (
          <Stack>
            <Text size="sm">
              {revokeTarget.username} 사용자의 '{revokeTarget.name}' ({revokeTarget.prefix}…) 키를 즉시 폐기합니다. 이 키를
              쓰는 MCP 클라이언트는 바로 거부됩니다.
            </Text>
            <Textarea
              label="폐기 사유"
              description="감사 로그와 키 목록에 남습니다. 비워 두면 '관리자 폐기'로 기록됩니다."
              autosize
              minRows={2}
              value={reason}
              onChange={(event) => setReason(event.currentTarget.value)}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRevokeTarget(null)}>
                취소
              </Button>
              <Button
                color="red"
                loading={revoke.isPending}
                onClick={() => revoke.mutate({ id: revokeTarget.id, reason: reason.trim() })}
              >
                폐기
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>
    </>
  )
}

// ---------------------------------------------------------------- roles tab

interface RoleDraft {
  isNew: boolean
  name: string
  description: string
  scopes: string[]
}

function RolesTab() {
  const queryClient = useQueryClient()
  const roles = useKeyRoles()
  const scopes = useKeyScopes()
  const [draft, setDraft] = useState<RoleDraft | null>(null)

  const scopeMap = useMemo(() => {
    const m: Record<string, ScopeInfo> = {}
    for (const s of scopes.data ?? []) m[s.name] = s
    return m
  }, [scopes.data])

  const scopesByRisk = useMemo(() => {
    const groups = new Map<string, ScopeInfo[]>()
    for (const s of scopes.data ?? []) {
      const list = groups.get(s.risk) ?? []
      list.push(s)
      groups.set(s.risk, list)
    }
    return [...groups.entries()].sort(
      ([a], [b]) => (riskOrder.indexOf(a) === -1 ? 99 : riskOrder.indexOf(a)) - (riskOrder.indexOf(b) === -1 ? 99 : riskOrder.indexOf(b)),
    )
  }, [scopes.data])

  const save = useMutation({
    mutationFn: (d: RoleDraft) =>
      api.put<KeyRole>(`/api/admin/key-roles/${encodeURIComponent(d.name.trim())}`, {
        description: d.description.trim(),
        scopes: d.scopes,
      }),
    onSuccess: (role) => {
      notifyOk(`'${role.name}' 역할을 저장했습니다.`)
      setDraft(null)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'key-roles'] })
    },
    onError: (err) => notifyError(err, '역할 저장 실패'),
  })

  const remove = useMutation({
    mutationFn: (name: string) => api.del(`/api/admin/key-roles/${encodeURIComponent(name)}`),
    onSuccess: () => {
      notifyOk('역할을 삭제했습니다.')
      void queryClient.invalidateQueries({ queryKey: ['admin', 'key-roles'] })
    },
    onError: (err) => notifyError(err, '역할 삭제 실패'),
  })

  const existingNames = new Set((roles.data ?? []).map((r) => r.name))
  const nameError = (() => {
    if (!draft?.isNew) return null
    const n = draft.name.trim()
    if (!n) return null
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(n)) return '영문, 숫자, 점(.), 밑줄(_), 하이픈(-)만 쓸 수 있습니다'
    if (existingNames.has(n)) return '이미 있는 역할 이름입니다'
    return null
  })()
  const canSave = Boolean(draft && draft.name.trim() && !nameError)

  return (
    <>
      <Section
        title="권한 체계(역할)"
        description="키 역할은 스코프 묶음입니다. 실제 허용 범위는 키 스코프와 소유자의 현재 역할이 허용하는 범위의 교집합이며, 그 위에 본인의 Confluence 권한이 다시 적용됩니다. 기본 역할도 스코프를 바꿀 수 있지만 삭제할 수는 없습니다."
        actions={
          <Button
            size="sm"
            leftSection={<IconPlus size={16} />}
            onClick={() => setDraft({ isNew: true, name: '', description: '', scopes: [] })}
          >
            추가
          </Button>
        }
      >
        {roles.isLoading || scopes.isLoading ? <LoadingBlock /> : null}
        {roles.error ? <ErrorBlock error={roles.error} /> : null}
        {scopes.error ? <ErrorBlock error={scopes.error} /> : null}

        {roles.data ? (
          <TableScroll minWidth={860}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>역할</Table.Th>
                  <Table.Th>설명</Table.Th>
                  <Table.Th>스코프</Table.Th>
                  <Table.Th>변경</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {roles.data.map((role) => (
                  <Table.Tr key={role.name}>
                    <Table.Td>
                      <Group gap={6} wrap="nowrap">
                        <Text size="sm" fw={600} ff="monospace">
                          {role.name}
                        </Text>
                        {role.builtin ? (
                          <Badge size="sm" variant="light" color="gray">
                            기본
                          </Badge>
                        ) : null}
                      </Group>
                    </Table.Td>
                    <Table.Td maw={240}>
                      <Text size="sm">{role.description || '—'}</Text>
                    </Table.Td>
                    <Table.Td maw={380}>
                      <Group gap={4} wrap="wrap">
                        {(role.scopes ?? []).length === 0 ? <Text size="sm">—</Text> : null}
                        {(role.scopes ?? []).map((s) => (
                          <Badge key={s} size="sm" variant="light" color={riskColors[scopeMap[s]?.risk ?? ''] ?? 'gray'}>
                            {scopeMap[s]?.label ?? s}
                          </Badge>
                        ))}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{formatDateTime(role.updatedAt)}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap" justify="flex-end">
                        <Button
                          size="xs"
                          variant="light"
                          onClick={() =>
                            setDraft({
                              isNew: false,
                              name: role.name,
                              description: role.description ?? '',
                              scopes: [...(role.scopes ?? [])],
                            })
                          }
                        >
                          수정
                        </Button>
                        {!role.builtin ? (
                          <ConfirmButton
                            size="xs"
                            color="red"
                            title="역할 삭제"
                            message={`'${role.name}' 역할을 삭제합니다. 이 역할로 발급된 키가 있으면 삭제가 거부될 수 있습니다.`}
                            confirmLabel="삭제"
                            loading={remove.isPending && remove.variables === role.name}
                            onConfirm={() => remove.mutate(role.name)}
                          >
                            삭제
                          </ConfirmButton>
                        ) : null}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}
      </Section>

      <Section title="스코프 목록" description="키와 역할에 넣을 수 있는 스코프입니다.">
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
          {(scopes.data ?? []).map((s) => (
            <Paper key={s.name} withBorder p="sm">
              <Group justify="space-between" wrap="nowrap" gap="xs">
                <Text size="sm" fw={600}>
                  {s.label}
                </Text>
                <Badge size="sm" variant="light" color={riskColors[s.risk] ?? 'gray'}>
                  {riskLabels[s.risk] ?? s.risk}
                </Badge>
              </Group>
              <Text size="xs" ff="monospace" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
                {s.name}
              </Text>
              <Text size="xs" c="dimmed" mt={4}>
                {s.description}
              </Text>
            </Paper>
          ))}
        </SimpleGrid>
      </Section>

      <Modal
        opened={draft !== null}
        onClose={() => setDraft(null)}
        title={<Text fw={700}>{draft?.isNew ? '역할 추가' : `역할 수정: ${draft?.name ?? ''}`}</Text>}
        size="lg"
      >
        {draft ? (
          <Stack>
            {draft.isNew ? (
              <TextInput
                label="역할 이름"
                description="키에 기록되는 식별자입니다. 저장 후에는 바꿀 수 없습니다."
                placeholder="예: docs-writer"
                required
                value={draft.name}
                error={nameError}
                onChange={(event) => setDraft({ ...draft, name: event.currentTarget.value })}
              />
            ) : null}
            <TextInput
              label="설명"
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.currentTarget.value })}
            />
            <Checkbox.Group
              label="스코프"
              value={draft.scopes}
              onChange={(next) => setDraft({ ...draft, scopes: next })}
            >
              <Stack gap="md" mt="xs">
                {scopesByRisk.map(([risk, list]) => (
                  <div key={risk}>
                    <Group gap="xs" mb={6}>
                      <Badge variant="light" color={riskColors[risk] ?? 'gray'}>
                        {riskLabels[risk] ?? risk}
                      </Badge>
                    </Group>
                    <Stack gap="xs">
                      {list.map((s) => (
                        <Checkbox
                          key={s.name}
                          value={s.name}
                          label={`${s.label} (${s.name})`}
                          description={s.description}
                        />
                      ))}
                    </Stack>
                  </div>
                ))}
              </Stack>
            </Checkbox.Group>
            {draft.scopes.some((s) => scopeMap[s]?.risk === 'ADMIN') ? (
              <Alert color="grape" variant="light" icon={<IconAlertTriangle size={20} />}>
                관리 스코프가 포함되어 있습니다. 소유자가 관리자 역할(confluence-mcp-admin)을 가진 경우에만 효력이 있습니다.
              </Alert>
            ) : null}
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setDraft(null)}>
                취소
              </Button>
              <Button disabled={!canSave} loading={save.isPending} onClick={() => save.mutate(draft)}>
                저장
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>
    </>
  )
}

// ---------------------------------------------------------------- policy tab

function PolicyTab() {
  const { query, draft, setDraft, secrets, setSecrets, save } = useSettingsGroup<KeyPolicy>('key_policy')
  const roles = useKeyRoles()

  const roleOptions = useMemo(
    () =>
      (roles.data ?? []).map((r) => ({
        value: r.name,
        label: r.description ? `${r.name} — ${r.description}` : r.name,
      })),
    [roles.data],
  )

  const fields: Field<KeyPolicy>[] = [
    {
      kind: 'select',
      key: 'defaultRole',
      label: '기본 역할',
      description: '사용자가 역할을 고르지 않고 키를 만들 때 적용됩니다.',
      options: roleOptions,
    },
    {
      kind: 'switch',
      key: 'allowSelfCreate',
      label: '사용자 직접 발급 허용',
      description: '끄면 사용자는 내 키 화면에서 새 키를 만들 수 없습니다.',
    },
    { kind: 'number', key: 'rotationDays', label: '회전 주기 (일)', description: '이 기간이 지나면 회전 필요로 표시합니다.', min: 0, max: 3650 },
    { kind: 'number', key: 'keyTtlDays', label: '키 유효 기간 (일)', description: '0 이면 만료하지 않습니다.', min: 0, max: 3650 },
    { kind: 'number', key: 'maxKeysPerUser', label: '사용자당 최대 키 수', min: 1, max: 100 },
    {
      kind: 'number',
      key: 'graceHours',
      label: '회전 유예 (시간)',
      description: '회전 뒤 기존 키를 계속 쓸 수 있는 시간입니다. 0 이면 즉시 폐기합니다.',
      min: 0,
      max: 720,
    },
  ]

  return (
    <Section title="발급 정책" description="개인 API 키의 기본 역할, 회전 주기, 유효 기간을 정합니다.">
      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}
      {roles.error ? <ErrorBlock error={roles.error} /> : null}
      {draft && !roles.isLoading ? (
        <>
          <SettingsForm
            fields={fields}
            value={draft}
            onChange={setDraft}
            secrets={secrets}
            onSecretChange={setSecrets}
          />
          <SaveBar onSave={() => save.mutate()} saving={save.isPending} />
        </>
      ) : null}
    </Section>
  )
}

export function AdminKeysPage() {
  const [tab, setTab] = useState<string | null>('keys')
  return (
    <>
      <PageHeader
        title="API 키"
        description="개인 API 키 현황과 키 권한 체계(역할 → 스코프), 발급 정책을 관리합니다."
      />
      <Tabs value={tab} onChange={setTab}>
        <Tabs.List mb="lg">
          <Tabs.Tab value="keys" leftSection={<IconKey size={18} />}>
            발급된 키
          </Tabs.Tab>
          <Tabs.Tab value="roles" leftSection={<IconShieldLock size={18} />}>
            권한 체계(역할)
          </Tabs.Tab>
          <Tabs.Tab value="policy" leftSection={<IconSettings size={18} />}>
            발급 정책
          </Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="keys">
          <KeysTab />
        </Tabs.Panel>
        <Tabs.Panel value="roles">
          <RolesTab />
        </Tabs.Panel>
        <Tabs.Panel value="policy">
          <PolicyTab />
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
