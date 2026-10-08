import {
  Alert,
  Badge,
  Button,
  Group,
  Modal,
  MultiSelect,
  PasswordInput,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { IconInfoCircle, IconRefresh, IconSearch, IconUserPlus } from '@tabler/icons-react'
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
import { api, qs, type User } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDateTime, formatRelative, roleLabels, roleOptions } from '../../lib/format'

const sourceLabels: Record<string, string> = {
  local: '로컬',
  keycloak: 'Keycloak',
  bootstrap: '초기 관리자',
}

const sourceColors: Record<string, string> = {
  local: 'gray',
  keycloak: 'confmcp',
  bootstrap: 'grape',
}

/** roleData returns the known roles plus any stored role so MultiSelect values always exist. */
function roleData(current: string[]) {
  const extra = current
    .filter((r) => !roleOptions.some((o) => o.value === r))
    .filter((r, i, arr) => arr.indexOf(r) === i)
    .map((r) => ({ value: r, label: `${r} (저장된 값)` }))
  return [...roleOptions, ...extra]
}

interface CreateDraft {
  username: string
  password: string
  displayName: string
  email: string
  roles: string[]
  isServiceAdmin: boolean
}

interface EditDraft {
  user: User
  displayName: string
  email: string
  roles: string[]
  isServiceAdmin: boolean
  active: boolean
}

const emptyCreate: CreateDraft = {
  username: '',
  password: '',
  displayName: '',
  email: '',
  roles: ['confluence-mcp-reader'],
  isServiceAdmin: false,
}

export function AdminUsersPage() {
  const queryClient = useQueryClient()
  const { me } = useAuth()
  const [search, setSearch] = useState('')
  const [q] = useDebouncedValue(search.trim(), 300)
  const [create, setCreate] = useState<CreateDraft | null>(null)
  const [edit, setEdit] = useState<EditDraft | null>(null)
  const [pwTarget, setPwTarget] = useState<User | null>(null)
  const [password, setPassword] = useState('')

  const users = useQuery({
    queryKey: ['admin', 'users', q],
    queryFn: () => api.get<User[]>(`/api/admin/users${qs({ q })}`),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
    void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
  }

  const createMut = useMutation({
    mutationFn: (d: CreateDraft) =>
      api.post<User>('/api/admin/users', {
        username: d.username.trim(),
        password: d.password,
        displayName: d.displayName.trim(),
        email: d.email.trim(),
        roles: d.roles,
        isServiceAdmin: d.isServiceAdmin,
      }),
    onSuccess: (u) => {
      notifyOk(`${u.username} 사용자를 만들었습니다.`)
      setCreate(null)
      invalidate()
    },
    onError: (err) => notifyError(err, '사용자 생성 실패'),
  })

  const updateMut = useMutation({
    mutationFn: (d: EditDraft) =>
      api.patch<User>(`/api/admin/users/${d.user.id}`, {
        displayName: d.displayName.trim(),
        email: d.email.trim(),
        roles: d.roles,
        isServiceAdmin: d.isServiceAdmin,
        active: d.active,
      }),
    onSuccess: (u) => {
      notifyOk(`${u?.username ?? '사용자'} 정보를 저장했습니다.`)
      setEdit(null)
      invalidate()
    },
    onError: (err) => notifyError(err, '사용자 수정 실패'),
  })

  const passwordMut = useMutation({
    mutationFn: (vars: { id: number; password: string }) =>
      api.post(`/api/admin/users/${vars.id}/password`, { password: vars.password }),
    onSuccess: () => {
      notifyOk('비밀번호를 바꾸고 기존 로그인 세션을 종료했습니다.')
      setPwTarget(null)
      setPassword('')
    },
    onError: (err) => notifyError(err, '비밀번호 변경 실패'),
  })

  const deleteMut = useMutation({
    mutationFn: (id: number) => api.del(`/api/admin/users/${id}`),
    onSuccess: () => {
      notifyOk('사용자를 삭제했습니다.')
      invalidate()
    },
    onError: (err) => notifyError(err, '사용자 삭제 실패'),
  })

  const rows = users.data ?? []
  const createValid =
    create !== null && create.username.trim() !== '' && create.password.length >= 10
  const passwordValid = password.length >= 10

  return (
    <>
      <PageHeader
        title="사용자"
        description="콘솔 사용자와 MCP 역할을 관리합니다. Keycloak 사용자의 역할은 로그인할 때마다 토큰의 역할로 다시 갱신되므로, 여기서 바꾼 역할은 다음 로그인 때 덮어써질 수 있습니다."
        actions={
          <>
            <Button
              variant="default"
              leftSection={<IconRefresh size={18} />}
              onClick={() => void users.refetch()}
              loading={users.isFetching && !users.isLoading}
            >
              새로 고침
            </Button>
            <Button leftSection={<IconUserPlus size={18} />} onClick={() => setCreate({ ...emptyCreate })}>
              추가
            </Button>
          </>
        }
      />

      <Section>
        <TextInput
          mb="md"
          aria-label="사용자 검색"
          placeholder="사용자명·이름·이메일 검색"
          leftSection={<IconSearch size={18} />}
          value={search}
          onChange={(event) => setSearch(event.currentTarget.value)}
          maw={420}
        />

        {users.isLoading ? <LoadingBlock /> : null}
        {users.error ? <ErrorBlock error={users.error} /> : null}
        {users.data && rows.length === 0 ? <EmptyState label="해당하는 사용자가 없습니다" /> : null}

        {rows.length > 0 ? (
          <TableScroll minWidth={1080}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>사용자</Table.Th>
                  <Table.Th>이메일</Table.Th>
                  <Table.Th>출처</Table.Th>
                  <Table.Th>역할</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>최근 로그인</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((u) => {
                  const self = me?.user.id === u.id
                  return (
                    <Table.Tr key={u.id}>
                      <Table.Td>
                        <Group gap={6} wrap="nowrap">
                          <Text size="sm" fw={600}>
                            {u.username}
                          </Text>
                          {self ? (
                            <Badge size="sm" variant="outline" color="gray">
                              나
                            </Badge>
                          ) : null}
                        </Group>
                        <Text size="xs" c="dimmed">
                          {u.displayName || '—'}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                          {u.email || '—'}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Badge variant="light" color={sourceColors[u.source] ?? 'gray'}>
                          {sourceLabels[u.source] ?? u.source}
                        </Badge>
                      </Table.Td>
                      <Table.Td maw={280}>
                        <Group gap={4} wrap="wrap">
                          {u.isServiceAdmin ? (
                            <Badge variant="filled" color="grape" size="sm">
                              서비스 관리자
                            </Badge>
                          ) : null}
                          {(u.roles ?? []).map((r) => (
                            <Badge key={r} variant="light" size="sm" color="confmcp">
                              {roleLabels[r] ?? r}
                            </Badge>
                          ))}
                          {!u.isServiceAdmin && (u.roles ?? []).length === 0 ? <Text size="sm">—</Text> : null}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        {u.active ? (
                          <Badge variant="light" color="teal">
                            사용
                          </Badge>
                        ) : (
                          <Badge variant="light" color="gray">
                            중지
                          </Badge>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{formatRelative(u.lastLoginAt)}</Text>
                        <Text size="xs" c="dimmed">
                          {formatDateTime(u.lastLoginAt)}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Group gap="xs" wrap="nowrap" justify="flex-end">
                          <Button
                            size="xs"
                            variant="light"
                            onClick={() =>
                              setEdit({
                                user: u,
                                displayName: u.displayName ?? '',
                                email: u.email ?? '',
                                roles: [...(u.roles ?? [])],
                                isServiceAdmin: u.isServiceAdmin,
                                active: u.active,
                              })
                            }
                          >
                            수정
                          </Button>
                          {u.hasPassword ? (
                            <Button
                              size="xs"
                              variant="default"
                              onClick={() => {
                                setPwTarget(u)
                                setPassword('')
                              }}
                            >
                              비밀번호
                            </Button>
                          ) : null}
                          <ConfirmButton
                            size="xs"
                            color="red"
                            title="사용자 삭제"
                            message={`${u.username} 사용자를 삭제합니다. 이 사용자의 로그인 세션, API 키, MCP 연결도 더 이상 쓸 수 없게 됩니다. 되돌릴 수 없습니다.`}
                            confirmLabel="삭제"
                            disabled={self}
                            loading={deleteMut.isPending && deleteMut.variables === u.id}
                            onConfirm={() => deleteMut.mutate(u.id)}
                          >
                            삭제
                          </ConfirmButton>
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
        opened={create !== null}
        onClose={() => setCreate(null)}
        title={<Text fw={700}>로컬 사용자 추가</Text>}
        size="lg"
      >
        {create ? (
          <Stack>
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
              <TextInput
                label="사용자명"
                required
                autoComplete="off"
                value={create.username}
                onChange={(event) => setCreate({ ...create, username: event.currentTarget.value })}
              />
              <PasswordInput
                label="비밀번호"
                description="10자 이상"
                required
                autoComplete="new-password"
                value={create.password}
                error={create.password.length > 0 && create.password.length < 10 ? '10자 이상 입력하십시오' : null}
                onChange={(event) => setCreate({ ...create, password: event.currentTarget.value })}
              />
              <TextInput
                label="표시 이름"
                value={create.displayName}
                onChange={(event) => setCreate({ ...create, displayName: event.currentTarget.value })}
              />
              <TextInput
                label="이메일"
                type="email"
                value={create.email}
                onChange={(event) => setCreate({ ...create, email: event.currentTarget.value })}
              />
            </SimpleGrid>
            <MultiSelect
              label="역할"
              data={roleData(create.roles)}
              value={create.roles}
              onChange={(next) => setCreate({ ...create, roles: next })}
              clearable
            />
            <Switch
              label="서비스 관리자"
              description="관리 화면 접근 권한입니다. 문서 권한을 우회하지는 않습니다."
              checked={create.isServiceAdmin}
              onChange={(event) => setCreate({ ...create, isServiceAdmin: event.currentTarget.checked })}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setCreate(null)}>
                취소
              </Button>
              <Button disabled={!createValid} loading={createMut.isPending} onClick={() => createMut.mutate(create)}>
                추가
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>

      <Modal
        opened={edit !== null}
        onClose={() => setEdit(null)}
        title={<Text fw={700}>사용자 수정: {edit?.user.username}</Text>}
        size="lg"
      >
        {edit ? (
          <Stack>
            {edit.user.source === 'keycloak' ? (
              <Alert variant="light" color="confmcp" icon={<IconInfoCircle size={20} />}>
                Keycloak 사용자입니다. 역할은 다음 로그인 때 토큰의 역할로 다시 갱신됩니다.
              </Alert>
            ) : null}
            {edit.user.source === 'bootstrap' ? (
              <Alert variant="light" color="orange" icon={<IconInfoCircle size={20} />}>
                초기 관리자 계정입니다. 서비스를 다시 기동하면 서비스 관리자 권한과 사용 상태가 다시 켜집니다.
              </Alert>
            ) : null}
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
              <TextInput
                label="표시 이름"
                value={edit.displayName}
                onChange={(event) => setEdit({ ...edit, displayName: event.currentTarget.value })}
              />
              <TextInput
                label="이메일"
                type="email"
                value={edit.email}
                onChange={(event) => setEdit({ ...edit, email: event.currentTarget.value })}
              />
            </SimpleGrid>
            <MultiSelect
              label="역할"
              data={roleData(edit.roles)}
              value={edit.roles}
              onChange={(next) => setEdit({ ...edit, roles: next })}
              clearable
            />
            <Switch
              label="서비스 관리자"
              checked={edit.isServiceAdmin}
              onChange={(event) => setEdit({ ...edit, isServiceAdmin: event.currentTarget.checked })}
            />
            <Switch
              label="계정 사용"
              description="끄면 로그인 세션과 MCP OAuth 연결이 즉시 종료됩니다."
              checked={edit.active}
              onChange={(event) => setEdit({ ...edit, active: event.currentTarget.checked })}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setEdit(null)}>
                취소
              </Button>
              <Button loading={updateMut.isPending} onClick={() => updateMut.mutate(edit)}>
                저장
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>

      <Modal
        opened={pwTarget !== null}
        onClose={() => setPwTarget(null)}
        title={<Text fw={700}>비밀번호 재설정: {pwTarget?.username}</Text>}
      >
        {pwTarget ? (
          <Stack>
            <Text size="sm" c="dimmed">
              새 비밀번호를 저장하면 이 사용자의 기존 로그인 세션이 모두 종료됩니다.
            </Text>
            {pwTarget.source === 'bootstrap' ? (
              <Alert variant="light" color="orange" icon={<IconInfoCircle size={20} />}>
                초기 관리자 계정입니다. 서비스를 다시 기동하면 비밀번호가 BOOTSTRAP_ADMIN_PASSWORD 값으로 되돌아갑니다.
              </Alert>
            ) : null}
            <PasswordInput
              label="새 비밀번호"
              description="10자 이상"
              autoComplete="new-password"
              value={password}
              error={password.length > 0 && !passwordValid ? '10자 이상 입력하십시오' : null}
              onChange={(event) => setPassword(event.currentTarget.value)}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setPwTarget(null)}>
                취소
              </Button>
              <Button
                disabled={!passwordValid}
                loading={passwordMut.isPending}
                onClick={() => passwordMut.mutate({ id: pwTarget.id, password })}
              >
                저장
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>
    </>
  )
}
