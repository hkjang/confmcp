import {
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Center,
  Divider,
  Group,
  Loader,
  PasswordInput,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core'
import {
  IconAlertTriangle,
  IconChecklist,
  IconKey,
  IconPlugConnected,
  IconShieldCheck,
  IconShieldLock,
} from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { BrandMark } from '../components/BrandMark'
import { useAuth } from '../lib/auth'

/** safeNext keeps only same-origin paths, so ?next= cannot redirect elsewhere. */
function safeNext(raw: string | null): string {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/login')) return '/'
  return raw
}

const highlights = [
  { icon: IconShieldCheck, title: '본인 권한만', text: '요청자 본인의 Confluence 권한으로만 문서를 보여 줍니다' },
  { icon: IconChecklist, title: '승인 후 반영', text: '모든 변경은 diff 를 확인하고 승인해야 적용됩니다' },
  { icon: IconPlugConnected, title: 'MCP OAuth', text: 'URL 하나로 AI 클라이언트를 바로 연결합니다' },
]

export function LoginPage() {
  const { me, config, login, startSso, silentChecking, ssoError } = useAuth()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const version = config?.version
  const ui = config?.ui
  const keycloakEnabled = config?.auth.keycloakEnabled ?? false
  const next = safeNext(params.get('next') ?? params.get('returnTo'))

  // Already signed in (for example by silent SSO): continue where the user was going.
  useEffect(() => {
    if (me) navigate(next, { replace: true })
  }, [me, navigate, next])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(username.trim(), password)
      navigate(next, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '로그인에 실패했습니다.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Box className="confmcp-login-hero" mih="100vh">
      <Center mih="100vh" p="md">
        <Stack w="100%" maw={480} gap="lg">
          <Stack align="center" gap={6}>
            <BrandMark size={68} />
            <Title order={1} ta="center">
              {ui?.serviceName ?? 'confmcp'}
            </Title>
            <Text c="dimmed" ta="center" size="lg">
              {ui?.tagline ?? 'Confluence MCP 게이트웨이'}
            </Text>
          </Stack>

          <Card withBorder p="xl" shadow="sm">
            <Stack gap="md">
              {ui?.loginNotice ? (
                <Alert variant="light" color="confmcp">
                  {ui.loginNotice}
                </Alert>
              ) : null}

              {next.startsWith('/oauth/consent') ? (
                <Alert variant="light" color="blue" icon={<IconPlugConnected size={20} />} title="MCP 클라이언트 연결">
                  AI 클라이언트가 연결을 요청했습니다. 로그인하면 연결 승인 화면으로 이동합니다.
                </Alert>
              ) : null}

              {silentChecking ? (
                <Group gap="xs" justify="center">
                  <Loader size="xs" />
                  <Text size="sm" c="dimmed">
                    기존 SSO 세션을 확인하고 있습니다…
                  </Text>
                </Group>
              ) : null}

              {ssoError ? (
                <Alert variant="light" color="orange" icon={<IconAlertTriangle size={20} />} title="SSO 안내">
                  {ssoError}
                </Alert>
              ) : null}

              {keycloakEnabled ? (
                <>
                  <Button size="lg" fullWidth leftSection={<IconShieldLock size={22} />} onClick={() => startSso(next)}>
                    회사 계정(SSO)으로 로그인
                  </Button>
                  <Divider label="또는 로컬 계정" labelPosition="center" />
                </>
              ) : null}

              <form onSubmit={submit}>
                <Stack gap="md">
                  <TextInput
                    label="아이디"
                    placeholder="관리자 또는 로컬 계정"
                    autoComplete="username"
                    required
                    value={username}
                    onChange={(event) => setUsername(event.currentTarget.value)}
                  />
                  <PasswordInput
                    label="비밀번호"
                    placeholder="비밀번호"
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(event) => setPassword(event.currentTarget.value)}
                  />
                  {error ? (
                    <Alert color="red" variant="light" icon={<IconAlertTriangle size={20} />}>
                      {error}
                    </Alert>
                  ) : null}
                  <Button
                    type="submit"
                    size="md"
                    fullWidth
                    loading={busy}
                    variant={keycloakEnabled ? 'default' : 'filled'}
                    leftSection={<IconKey size={20} />}
                  >
                    로그인
                  </Button>
                </Stack>
              </form>
            </Stack>
          </Card>

          <SimpleGrid cols={{ base: 1, xs: 3 }} spacing="xs">
            {highlights.map((h) => (
              <Group key={h.title} gap="xs" wrap="nowrap" align="flex-start">
                <ThemeIcon variant="light" size="md" radius="md">
                  <h.icon size={16} />
                </ThemeIcon>
                <div>
                  <Text size="sm" fw={700}>
                    {h.title}
                  </Text>
                  <Text size="xs" c="dimmed">
                    {h.text}
                  </Text>
                </div>
              </Group>
            ))}
          </SimpleGrid>

          {/* The running version is visible before sign-in, so an operator can
              confirm which build an environment is on without logging in. */}
          <Group justify="center" gap="xs" data-testid="login-version">
            <Badge variant="light" color="gray" size="lg">
              버전 v{version?.version ?? '—'}
            </Badge>
            <Badge variant="light" color="gray" size="lg">
              빌드 {version?.commit?.slice(0, 7) ?? '—'}
            </Badge>
            <Badge variant="light" color="gray" size="lg">
              {version?.buildDate ?? '—'}
            </Badge>
          </Group>
        </Stack>
      </Center>
    </Box>
  )
}
