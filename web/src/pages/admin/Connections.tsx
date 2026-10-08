import { Badge, Button, Code, Group, Stack, Switch, Table, Tabs, Text } from '@mantine/core'
import { IconKey, IconPlugConnected, IconRefresh, IconTerminal2 } from '@tabler/icons-react'
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
import { api, qs, type McpSession, type OAuthClient, type OAuthGrant } from '../../lib/api'
import { authModeLabels, formatDateTime, formatRelative } from '../../lib/format'

function RefreshButton({ onClick, loading }: { onClick: () => void; loading: boolean }) {
  return (
    <Button size="sm" variant="default" leftSection={<IconRefresh size={16} />} onClick={onClick} loading={loading}>
      새로 고침
    </Button>
  )
}

function ClientsTab() {
  const queryClient = useQueryClient()
  const clients = useQuery({
    queryKey: ['admin', 'oauth', 'clients'],
    queryFn: () => api.get<OAuthClient[]>('/api/admin/oauth/clients'),
  })
  const disable = useMutation({
    mutationFn: (id: string) => api.del(`/api/admin/oauth/clients/${encodeURIComponent(id)}`),
    onSuccess: () => {
      notifyOk('클라이언트를 차단하고 연결을 모두 철회했습니다.')
      void queryClient.invalidateQueries({ queryKey: ['admin', 'oauth'] })
    },
    onError: (err) => notifyError(err, '차단 실패'),
  })
  const rows = clients.data ?? []

  return (
    <Section
      title="OAuth 클라이언트"
      description="MCP 클라이언트(Claude Code 등)가 동적 등록(DCR)으로 만든 클라이언트입니다. 차단하면 해당 클라이언트로 맺은 모든 사용자 연결이 철회됩니다."
      actions={<RefreshButton onClick={() => void clients.refetch()} loading={clients.isFetching && !clients.isLoading} />}
    >
      {clients.isLoading ? <LoadingBlock /> : null}
      {clients.error ? <ErrorBlock error={clients.error} /> : null}
      {clients.data && rows.length === 0 ? <EmptyState label="등록된 클라이언트가 없습니다" /> : null}
      {rows.length > 0 ? (
        <TableScroll minWidth={960}>
          <Table striped fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>이름</Table.Th>
                <Table.Th>클라이언트 ID</Table.Th>
                <Table.Th>Redirect URI</Table.Th>
                <Table.Th>등록</Table.Th>
                <Table.Th>최근 사용</Table.Th>
                <Table.Th ta="right">활성 연결</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((c) => (
                <Table.Tr key={c.clientId}>
                  <Table.Td>
                    <Text size="sm" fw={600}>
                      {c.clientName || '(이름 없음)'}
                    </Text>
                    {c.softwareId ? (
                      <Text size="xs" c="dimmed">
                        {c.softwareId}
                      </Text>
                    ) : null}
                  </Table.Td>
                  <Table.Td>
                    <Code>{c.clientId}</Code>
                  </Table.Td>
                  <Table.Td maw={300}>
                    <Stack gap={2}>
                      {(c.redirectUris ?? []).map((u) => (
                        <Text key={u} size="xs" ff="monospace" style={{ overflowWrap: 'anywhere' }}>
                          {u}
                        </Text>
                      ))}
                    </Stack>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatDateTime(c.createdAt)}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatRelative(c.lastUsedAt)}</Text>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Badge variant="light" color={c.activeGrants > 0 ? 'teal' : 'gray'}>
                      {c.activeGrants}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs" wrap="nowrap" justify="flex-end">
                      <ConfirmButton
                        size="xs"
                        color="red"
                        title="클라이언트 차단"
                        message={`'${c.clientName || c.clientId}' 클라이언트를 차단합니다. 이 클라이언트로 맺은 사용자 연결 ${c.activeGrants}건이 모두 철회되고, 발급된 토큰은 즉시 쓸 수 없게 됩니다. 사용자가 다시 연결하려면 클라이언트를 새로 등록해야 합니다.`}
                        confirmLabel="차단"
                        loading={disable.isPending && disable.variables === c.clientId}
                        onConfirm={() => disable.mutate(c.clientId)}
                      >
                        차단
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

function GrantsTab() {
  const queryClient = useQueryClient()
  const [includeRevoked, setIncludeRevoked] = useState(false)
  const grants = useQuery({
    queryKey: ['admin', 'oauth', 'grants', includeRevoked],
    queryFn: () => api.get<OAuthGrant[]>(`/api/admin/oauth/grants${qs({ all: includeRevoked ? 1 : undefined })}`),
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.del(`/api/admin/oauth/grants/${encodeURIComponent(id)}`),
    onSuccess: () => {
      notifyOk('연결을 철회했습니다.')
      void queryClient.invalidateQueries({ queryKey: ['admin', 'oauth'] })
    },
    onError: (err) => notifyError(err, '철회 실패'),
  })
  const rows = grants.data ?? []

  return (
    <Section
      title="사용자 연결(grant)"
      description="사용자가 MCP 클라이언트에 허락한 연결입니다. 철회하면 해당 연결의 토큰이 즉시 무효화됩니다."
      actions={
        <>
          <Switch
            label="철회된 연결 포함"
            checked={includeRevoked}
            onChange={(event) => setIncludeRevoked(event.currentTarget.checked)}
            size="sm"
          />
          <RefreshButton onClick={() => void grants.refetch()} loading={grants.isFetching && !grants.isLoading} />
        </>
      }
    >
      {grants.isLoading ? <LoadingBlock /> : null}
      {grants.error ? <ErrorBlock error={grants.error} /> : null}
      {grants.data && rows.length === 0 ? <EmptyState label="연결이 없습니다" /> : null}
      {rows.length > 0 ? (
        <TableScroll minWidth={920}>
          <Table striped fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>사용자</Table.Th>
                <Table.Th>클라이언트</Table.Th>
                <Table.Th>스코프</Table.Th>
                <Table.Th>연결</Table.Th>
                <Table.Th>최근 사용</Table.Th>
                <Table.Th>상태</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((g) => (
                <Table.Tr key={g.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>
                      {g.username || `#${g.userId}`}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{g.clientName || '(이름 없음)'}</Text>
                    <Text size="xs" ff="monospace" c="dimmed">
                      {g.clientId}
                    </Text>
                  </Table.Td>
                  <Table.Td maw={260}>
                    <Group gap={4} wrap="wrap">
                      {(g.scopes ?? []).length === 0 ? <Text size="sm">—</Text> : null}
                      {(g.scopes ?? []).map((s) => (
                        <Badge key={s} size="sm" variant="light" color="gray">
                          {s === '*' ? '역할 전체' : s}
                        </Badge>
                      ))}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatDateTime(g.createdAt)}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatRelative(g.lastUsedAt)}</Text>
                  </Table.Td>
                  <Table.Td>
                    {g.revokedAt ? (
                      <>
                        <Badge variant="light" color="gray">
                          철회됨
                        </Badge>
                        <Text size="xs" c="dimmed" mt={2}>
                          {formatDateTime(g.revokedAt)}
                        </Text>
                      </>
                    ) : (
                      <Badge variant="light" color="teal">
                        사용 중
                      </Badge>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs" wrap="nowrap" justify="flex-end">
                      {!g.revokedAt ? (
                        <ConfirmButton
                          size="xs"
                          color="red"
                          title="연결 철회"
                          message={`${g.username} 사용자의 '${g.clientName || g.clientId}' 연결을 철회합니다. 이 연결로 발급된 토큰은 즉시 쓸 수 없게 됩니다.`}
                          confirmLabel="철회"
                          loading={revoke.isPending && revoke.variables === g.id}
                          onConfirm={() => revoke.mutate(g.id)}
                        >
                          철회
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
  )
}

function isActiveSession(s: McpSession): boolean {
  if (s.closedAt) return false
  const last = new Date(s.lastSeenAt).getTime()
  return Number.isFinite(last) && Date.now() - last < 60 * 60 * 1000
}

function SessionsTab() {
  const sessions = useQuery({
    queryKey: ['admin', 'sessions'],
    queryFn: () => api.get<McpSession[]>('/api/admin/sessions'),
  })
  const rows = sessions.data ?? []
  const active = rows.filter(isActiveSession).length

  return (
    <Section
      title="MCP 세션"
      description={`최근 MCP 세션 기록입니다. 닫히지 않았고 1시간 안에 요청이 있었던 세션을 활성으로 표시합니다. 현재 활성 ${active}개.`}
      actions={<RefreshButton onClick={() => void sessions.refetch()} loading={sessions.isFetching && !sessions.isLoading} />}
    >
      {sessions.isLoading ? <LoadingBlock /> : null}
      {sessions.error ? <ErrorBlock error={sessions.error} /> : null}
      {sessions.data && rows.length === 0 ? <EmptyState label="MCP 세션 기록이 없습니다" /> : null}
      {rows.length > 0 ? (
        <TableScroll minWidth={900}>
          <Table striped fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>사용자</Table.Th>
                <Table.Th>클라이언트</Table.Th>
                <Table.Th>인증 방식</Table.Th>
                <Table.Th>주소</Table.Th>
                <Table.Th>시작</Table.Th>
                <Table.Th>마지막 요청</Table.Th>
                <Table.Th>상태</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rows.map((s) => (
                <Table.Tr key={s.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>
                      {s.username || '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{s.client || '—'}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color="gray">
                      {authModeLabels[s.authMode] ?? (s.authMode || '—')}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace">
                      {s.ip || '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatDateTime(s.createdAt)}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{formatRelative(s.lastSeenAt)}</Text>
                  </Table.Td>
                  <Table.Td>
                    {isActiveSession(s) ? (
                      <Badge variant="light" color="teal">
                        활성
                      </Badge>
                    ) : s.closedAt ? (
                      <>
                        <Badge variant="light" color="gray">
                          종료
                        </Badge>
                        <Text size="xs" c="dimmed" mt={2}>
                          {formatDateTime(s.closedAt)}
                        </Text>
                      </>
                    ) : (
                      <Badge variant="light" color="yellow">
                        유휴
                      </Badge>
                    )}
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

export function AdminConnectionsPage() {
  const [tab, setTab] = useState<string | null>('clients')
  return (
    <>
      <PageHeader
        title="MCP 연결"
        description="MCP OAuth 클라이언트, 사용자별 연결(grant), MCP 세션을 관리합니다."
      />
      <Tabs value={tab} onChange={setTab}>
        <Tabs.List mb="lg">
          <Tabs.Tab value="clients" leftSection={<IconPlugConnected size={18} />}>
            OAuth 클라이언트
          </Tabs.Tab>
          <Tabs.Tab value="grants" leftSection={<IconKey size={18} />}>
            사용자 연결(grant)
          </Tabs.Tab>
          <Tabs.Tab value="sessions" leftSection={<IconTerminal2 size={18} />}>
            MCP 세션
          </Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="clients">
          <ClientsTab />
        </Tabs.Panel>
        <Tabs.Panel value="grants">
          <GrantsTab />
        </Tabs.Panel>
        <Tabs.Panel value="sessions">
          <SessionsTab />
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
