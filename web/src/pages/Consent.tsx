import { Alert, Badge, Box, Button, Card, Center, Group, List, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { IconAlertTriangle, IconCheck, IconPlugConnected, IconShieldCheck, IconX } from '@tabler/icons-react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { BrandMark } from '../components/BrandMark'
import { LoadingBlock } from '../components/ui'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'

interface ConsentInfo {
  clientId: string
  clientName: string
  redirectHost: string
  scopes: string[]
  effectiveScopes: string[]
  resource: string
  user: string
  mapped: boolean
}

const scopeText: Record<string, string> = {
  'confluence:read': '문서 조회와 검색',
  'confluence:attachment:read': '첨부 목록과 다운로드 링크',
  'confluence:write': '변경안 작성 (적용은 승인 후)',
  'confluence:attachment:write': '첨부 올리기 (승인 후)',
  'confluence:execute': '페이지 이동·휴지통 (별도 승인자 필요)',
  'ai:invoke': 'AI 호출',
  'admin:read': '관리 조회',
  'admin:write': '관리 변경',
}

/**
 * ConsentPage is where a signed-in user approves an MCP client once. The
 * client then receives confmcp tokens that act only with this user's own
 * Confluence permissions.
 */
export function ConsentPage() {
  const [params] = useSearchParams()
  const { me } = useAuth()
  const req = params.get('req') ?? ''
  const pageError = params.get('error')
  const [done, setDone] = useState<string | null>(null)

  const info = useQuery({
    queryKey: ['consent', req],
    queryFn: () => api.get<ConsentInfo>(`/api/oauth/consent?req=${encodeURIComponent(req)}`),
    enabled: req !== '' && !pageError,
    retry: false,
  })

  const decide = useMutation({
    mutationFn: (approve: boolean) => api.post<{ redirect: string }>('/api/oauth/consent', { req, approve }),
    onSuccess: (res, approve) => {
      setDone(approve ? '연결을 승인했습니다. 클라이언트로 돌아갑니다…' : '연결을 거부했습니다.')
      window.location.href = res.redirect
    },
  })

  const scopes = info.data
    ? info.data.scopes.length === 1 && info.data.scopes[0] === '*'
      ? info.data.effectiveScopes
      : info.data.scopes.filter((s) => info.data!.effectiveScopes.includes(s))
    : []

  return (
    <Box className="confmcp-login-hero" mih="100vh">
      <Center mih="100vh" p="md">
        <Stack w="100%" maw={520} gap="lg">
          <Group justify="center" gap="sm">
            <BrandMark size={44} />
            <Title order={2}>MCP 클라이언트 연결</Title>
          </Group>

          <Card withBorder p="xl" shadow="sm">
            {pageError ? (
              <Alert color="red" icon={<IconAlertTriangle size={20} />} title="연결할 수 없습니다">
                {pageError}
              </Alert>
            ) : !req ? (
              <Alert color="orange" icon={<IconAlertTriangle size={20} />}>
                연결 요청 정보가 없습니다. AI 클라이언트에서 다시 연결하십시오.
              </Alert>
            ) : info.isLoading ? (
              <LoadingBlock label="연결 요청을 확인하고 있습니다…" />
            ) : info.isError ? (
              <Alert color="red" icon={<IconAlertTriangle size={20} />} title="요청이 유효하지 않습니다">
                {(info.error as Error).message}
              </Alert>
            ) : info.data ? (
              <Stack gap="md">
                <Group gap="sm" wrap="nowrap" align="flex-start">
                  <ThemeIcon size={44} radius="md" variant="light">
                    <IconPlugConnected size={26} />
                  </ThemeIcon>
                  <div>
                    <Text fw={800} size="lg">
                      {info.data.clientName}
                    </Text>
                    <Text size="sm" c="dimmed">
                      돌아갈 주소: {info.data.redirectHost}
                    </Text>
                  </div>
                </Group>

                <Text>
                  <b>{me?.user.displayName || info.data.user}</b> 님의 계정으로 이 클라이언트가 confmcp 에 접근하려고 합니다.
                </Text>

                <Box>
                  <Text fw={700} size="sm" mb={6}>
                    허용되는 작업
                  </Text>
                  <List spacing={6} size="sm" icon={<IconCheck size={16} color="var(--mantine-color-teal-6)" />}>
                    {scopes.map((s) => (
                      <List.Item key={s}>
                        {scopeText[s] ?? s}{' '}
                        <Badge size="xs" variant="light" color="gray">
                          {s}
                        </Badge>
                      </List.Item>
                    ))}
                  </List>
                </Box>

                <Alert variant="light" color="teal" icon={<IconShieldCheck size={20} />}>
                  클라이언트는 회원님이 Confluence 에서 볼 수 있는 문서만 볼 수 있고, 문서 변경은 회원님이 콘솔에서 승인해야 적용됩니다.
                  연결은 [MCP 연결] 화면에서 언제든 끊을 수 있습니다.
                </Alert>

                {!info.data.mapped ? (
                  <Alert variant="light" color="orange" icon={<IconAlertTriangle size={20} />}>
                    아직 Confluence 계정과 연결되지 않았습니다. 연결 후 [내 Confluence] 화면에서 계정을 확인해야 도구를 쓸 수 있습니다.
                  </Alert>
                ) : null}

                {decide.isError ? (
                  <Alert color="red">{(decide.error as Error).message}</Alert>
                ) : null}
                {done ? <Alert color="teal">{done}</Alert> : null}

                <Group grow>
                  <Button
                    variant="default"
                    leftSection={<IconX size={18} />}
                    onClick={() => decide.mutate(false)}
                    disabled={decide.isPending || Boolean(done)}
                  >
                    거부
                  </Button>
                  <Button
                    leftSection={<IconCheck size={18} />}
                    onClick={() => decide.mutate(true)}
                    loading={decide.isPending}
                    disabled={Boolean(done)}
                    data-testid="consent-approve"
                  >
                    연결 허용
                  </Button>
                </Group>
              </Stack>
            ) : null}
          </Card>
        </Stack>
      </Center>
    </Box>
  )
}
