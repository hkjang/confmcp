import { Alert, Badge, Button, Code, Group, List, SegmentedControl, Stack, Table, Tabs, Text } from '@mantine/core'
import { IconInfoCircle, IconKey, IconPlugConnected, IconTerminal2 } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import {
  ConfirmButton,
  CopyField,
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  Section,
  TableScroll,
  TextBlock,
  notifyError,
  notifyOk,
} from '../components/ui'
import { api, type McpConfig, type OAuthGrant } from '../lib/api'
import { formatDateTime, formatRelative } from '../lib/format'

const clients = [
  { value: 'claude', label: 'Claude Code' },
  { value: 'json', label: 'JSON 설정 (Cursor·VS Code 등)' },
]

export function MyConnectPage() {
  const qc = useQueryClient()
  const [client, setClient] = useState('claude')
  const cfg = useQuery({ queryKey: ['me', 'mcp-config'], queryFn: () => api.get<McpConfig>('/api/me/mcp-config') })
  const grants = useQuery({ queryKey: ['me', 'connections'], queryFn: () => api.get<OAuthGrant[]>('/api/me/connections') })
  const revoke = useMutation({
    mutationFn: (id: string) => api.del(`/api/me/connections/${id}`),
    onSuccess: () => {
      notifyOk('연결을 끊었습니다. 그 클라이언트는 다시 로그인해야 합니다.')
      void qc.invalidateQueries({ queryKey: ['me', 'connections'] })
    },
    onError: (e) => notifyError(e),
  })

  if (cfg.isLoading) return <LoadingBlock />
  if (cfg.isError) return <ErrorBlock error={cfg.error} />
  const c = cfg.data!

  return (
    <>
      <PageHeader
        title="MCP 연결"
        description="AI 클라이언트를 confmcp 에 연결합니다. OAuth 로그인을 권장하며, OAuth 를 지원하지 않는 도구나 자동화에는 API 키를 씁니다."
      />

      <Tabs defaultValue="oauth" variant="pills" radius="md">
        <Tabs.List mb="md">
          <Tabs.Tab value="oauth" leftSection={<IconPlugConnected size={18} />}>
            OAuth (권장)
          </Tabs.Tab>
          <Tabs.Tab value="apikey" leftSection={<IconKey size={18} />}>
            API 키
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="oauth">
          <Section
            title="URL 하나로 연결"
            description="클라이언트가 처음 연결할 때 브라우저가 열리고, 회사 계정으로 로그인(이미 로그인되어 있으면 자동)한 뒤 [연결 허용]을 누르면 끝납니다. 별도 클라이언트 등록이나 키 복사가 필요 없습니다."
          >
            <Stack gap="md">
              <div>
                <Text size="sm" fw={600} mb={4}>
                  MCP 주소
                </Text>
                <CopyField value={c.mcpUrl} />
              </div>
              <SegmentedControl data={clients} value={client} onChange={setClient} />
              {client === 'claude' ? (
                <>
                  <Group gap="xs">
                    <IconTerminal2 size={18} />
                    <Text size="sm" fw={600}>
                      터미널에서 실행
                    </Text>
                  </Group>
                  <CopyField value={c.oauth.claudeCode} />
                  <Text size="sm" c="dimmed">
                    추가한 뒤 Claude Code 에서 <Code>/mcp</Code> 를 열고 confmcp 의 인증을 선택하면 브라우저 로그인이 시작됩니다.
                  </Text>
                </>
              ) : (
                <>
                  <Text size="sm" fw={600}>
                    클라이언트 설정 파일 (mcp.json 등)
                  </Text>
                  <TextBlock text={JSON.stringify(c.oauth.json, null, 2)} />
                </>
              )}
              <Alert variant="light" color="blue" icon={<IconInfoCircle size={20} />} title="연결 후에는">
                <List size="sm" spacing={4}>
                  <List.Item>클라이언트는 회원님이 Confluence 에서 볼 수 있는 문서만 봅니다.</List.Item>
                  <List.Item>문서를 바꾸는 요청은 변경안으로 저장되고, [변경 승인]에서 승인해야 적용됩니다.</List.Item>
                  <List.Item>토큰은 1시간마다 자동 갱신되며, 아래에서 언제든 연결을 끊을 수 있습니다.</List.Item>
                </List>
              </Alert>
            </Stack>
          </Section>

          <Section title="연결된 클라이언트">
            {grants.isLoading ? (
              <LoadingBlock />
            ) : (grants.data?.length ?? 0) === 0 ? (
              <EmptyState label="연결된 클라이언트가 없습니다" />
            ) : (
              <TableScroll minWidth={640}>
                <Table>
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>클라이언트</Table.Th>
                      <Table.Th>권한 범위</Table.Th>
                      <Table.Th>연결 시각</Table.Th>
                      <Table.Th>마지막 사용</Table.Th>
                      <Table.Th />
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {grants.data!.map((g) => (
                      <Table.Tr key={g.id}>
                        <Table.Td>
                          <Text fw={600} size="sm">
                            {g.clientName}
                          </Text>
                          <Text size="xs" c="dimmed" ff="monospace">
                            {g.clientId}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          {g.scopes.length === 1 && g.scopes[0] === '*' ? (
                            <Badge variant="light">역할에 따름</Badge>
                          ) : (
                            <Group gap={4}>
                              {g.scopes.map((s) => (
                                <Badge key={s} size="sm" variant="light" color="gray">
                                  {s}
                                </Badge>
                              ))}
                            </Group>
                          )}
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{formatDateTime(g.createdAt)}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{formatRelative(g.lastUsedAt)}</Text>
                        </Table.Td>
                        <Table.Td>
                          <ConfirmButton
                            color="red"
                            title="연결 끊기"
                            message={`${g.clientName} 의 연결을 끊습니다. 발급된 토큰이 모두 무효가 됩니다.`}
                            confirmLabel="연결 끊기"
                            onConfirm={() => revoke.mutate(g.id)}
                          >
                            연결 끊기
                          </ConfirmButton>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </TableScroll>
            )}
          </Section>
        </Tabs.Panel>

        <Tabs.Panel value="apikey">
          <Section
            title="API 키로 연결"
            description="OAuth 를 지원하지 않는 클라이언트나 CI 에서는 개인 API 키를 Authorization 헤더로 보냅니다. 키 권한은 발급 후에도 매 호출마다 현재 역할로 다시 확인됩니다."
          >
            <Stack gap="md">
              <Text size="sm" fw={600}>
                Claude Code
              </Text>
              <CopyField value={c.apiKey.claudeCode} />
              <Text size="sm" fw={600}>
                JSON 설정
              </Text>
              <TextBlock text={JSON.stringify(c.apiKey.json, null, 2)} />
              <Group>
                <Button component={Link} to="/me/keys" leftSection={<IconKey size={18} />}>
                  API 키 발급·회전
                </Button>
              </Group>
              <Alert variant="light" color="gray" icon={<IconKey size={20} />}>
                키는 비밀번호처럼 다루십시오. 저장소나 공유 문서에 넣지 말고, 주기적으로 회전하십시오.
              </Alert>
            </Stack>
          </Section>
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
