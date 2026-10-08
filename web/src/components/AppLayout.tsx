import {
  ActionIcon,
  AppShell,
  Avatar,
  Badge,
  Box,
  Burger,
  Divider,
  Group,
  Indicator,
  Menu,
  NavLink,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
  UnstyledButton,
  useComputedColorScheme,
  useMantineColorScheme,
} from '@mantine/core'
import { useDisclosure, useMediaQuery } from '@mantine/hooks'
import {
  IconActivity,
  IconAdjustmentsAlt,
  IconApi,
  IconBook2,
  IconChecklist,
  IconChevronDown,
  IconCloudUpload,
  IconDashboard,
  IconFileText,
  IconFingerprint,
  IconGauge,
  IconHistory,
  IconKey,
  IconLayoutDashboard,
  IconLink,
  IconLogout,
  IconMoon,
  IconPalette,
  IconPlugConnected,
  IconRobot,
  IconServer2,
  IconShieldCheck,
  IconShieldLock,
  IconSun,
  IconTool,
  IconUserCog,
  IconUsers,
  IconUsersGroup,
  IconVersions,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { api, type ApprovalRequest } from '../lib/api'
import { useAuth } from '../lib/auth'
import { authModeLabels, roleLabels } from '../lib/format'
import { BrandMark } from './BrandMark'

interface NavItem {
  label: string
  to: string
  icon: typeof IconDashboard
  badgeKey?: 'approvals'
}

interface NavGroup {
  label: string
  items: NavItem[]
  admin?: boolean
}

const navGroups: NavGroup[] = [
  {
    label: '내 작업 공간',
    items: [
      { label: '개요', to: '/', icon: IconLayoutDashboard },
      { label: 'MCP 연결', to: '/me/connect', icon: IconPlugConnected },
      { label: '내 Confluence', to: '/me/confluence', icon: IconLink },
      { label: '변경 승인', to: '/me/approvals', icon: IconChecklist, badgeKey: 'approvals' },
      { label: 'API 키', to: '/me/keys', icon: IconKey },
      { label: '첨부 업로드', to: '/me/uploads', icon: IconCloudUpload },
      { label: '내 권한 확인', to: '/me/permissions', icon: IconFingerprint },
      { label: '사용 가능한 도구', to: '/me/tools', icon: IconTool },
      { label: 'AI 도우미', to: '/me/ai', icon: IconRobot },
      { label: '내 활동 기록', to: '/me/audit', icon: IconActivity },
      { label: '개인 설정', to: '/me/settings', icon: IconUserCog },
    ],
  },
  {
    label: '서비스 관리',
    admin: true,
    items: [
      { label: '관리 대시보드', to: '/admin', icon: IconDashboard },
      { label: '인증 · SSO', to: '/admin/auth', icon: IconShieldLock },
      { label: 'Confluence 연결', to: '/admin/confluence', icon: IconPlugConnected },
      { label: '권한 판정', to: '/admin/permission', icon: IconShieldCheck },
      { label: '사용자 매핑', to: '/admin/identity', icon: IconUsersGroup },
      { label: '접근 정책', to: '/admin/policy', icon: IconAdjustmentsAlt },
      { label: 'MCP 도구', to: '/admin/tools', icon: IconTool },
      { label: '변경 승인', to: '/admin/approvals', icon: IconChecklist },
      { label: '쓰기 작업 기록', to: '/admin/operations', icon: IconHistory },
      { label: 'MCP 연결 현황', to: '/admin/connections', icon: IconApi },
      { label: '키 · 권한 체계', to: '/admin/keys', icon: IconKey },
      { label: '사용자', to: '/admin/users', icon: IconUsers },
      { label: '운영 한도', to: '/admin/limits', icon: IconGauge },
      { label: 'AI 설정', to: '/admin/ai', icon: IconRobot },
      { label: '보안', to: '/admin/security', icon: IconShieldLock },
      { label: '화면 설정', to: '/admin/ui', icon: IconPalette },
      { label: '감사 로그', to: '/admin/audit', icon: IconFileText },
      { label: '시스템', to: '/admin/system', icon: IconServer2 },
    ],
  },
]

export function AppLayout() {
  const [mobileOpened, { toggle: toggleMobile, close: closeMobile }] = useDisclosure(false)
  const [desktopOpened, { toggle: toggleDesktop }] = useDisclosure(true)
  const { me, logout } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const { setColorScheme } = useMantineColorScheme()
  const computed = useComputedColorScheme('light', { getInitialValueInEffect: true })
  const narrow = useMediaQuery('(max-width: 36em)')

  const isAdmin = Boolean(me?.isServiceAdmin)
  const serviceName = me?.ui.serviceName || 'confmcp'
  const version = me?.version

  const pending = useQuery({
    queryKey: ['my-approvals', 'pending-count'],
    queryFn: () => api.get<ApprovalRequest[]>('/api/me/approvals?status=pending'),
    refetchInterval: 30_000,
  })
  const queue = useQuery({
    queryKey: ['approval-queue'],
    queryFn: () => api.get<ApprovalRequest[]>('/api/me/approval-queue'),
    enabled: Boolean(me?.canApprove),
    refetchInterval: 30_000,
  })
  const approvalsBadge = (pending.data?.length ?? 0) + (queue.data?.length ?? 0)

  const isActive = (to: string) => {
    if (to === '/') return location.pathname === '/'
    if (to === '/admin') return location.pathname === '/admin'
    return location.pathname === to || location.pathname.startsWith(`${to}/`)
  }
  const initials = (me?.user.displayName || me?.user.username || '?').slice(0, 2).toUpperCase()

  return (
    <AppShell
      header={{ height: 64 }}
      navbar={{ width: 280, breakpoint: 'sm', collapsed: { mobile: !mobileOpened, desktop: !desktopOpened } }}
      padding={{ base: 'sm', sm: 'lg' }}
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger opened={mobileOpened} onClick={toggleMobile} hiddenFrom="sm" size="sm" aria-label="메뉴 열기" />
            <Burger opened={desktopOpened} onClick={toggleDesktop} visibleFrom="sm" size="sm" aria-label="메뉴 접기" />
            <UnstyledButton component={Link} to="/" aria-label="홈으로">
              <Group gap="xs" wrap="nowrap">
                <BrandMark size={32} />
                <Box>
                  <Text fw={800} size="lg" lh={1.1}>
                    {serviceName}
                  </Text>
                  <Text size="xs" c="dimmed" lh={1.2} visibleFrom="xs">
                    {me?.ui.tagline || 'Confluence MCP 게이트웨이'}
                  </Text>
                </Box>
              </Group>
            </UnstyledButton>
          </Group>

          <Group gap="xs" wrap="nowrap">
            <Tooltip label={computed === 'dark' ? '밝은 테마로' : '어두운 테마로'}>
              <ActionIcon
                variant="default"
                size="lg"
                aria-label="테마 전환"
                onClick={() => setColorScheme(computed === 'dark' ? 'light' : 'dark')}
              >
                {computed === 'dark' ? <IconSun size={20} /> : <IconMoon size={20} />}
              </ActionIcon>
            </Tooltip>

            <Menu shadow="md" width={300} position="bottom-end" withinPortal>
              <Menu.Target>
                <UnstyledButton aria-label="프로필 메뉴" data-testid="profile-menu">
                  <Group gap="xs" wrap="nowrap">
                    <Avatar color="confmcp" radius="xl" size={36} variant="filled">
                      {initials}
                    </Avatar>
                    {!narrow ? (
                      <Box>
                        <Text size="sm" fw={600} lh={1.15}>
                          {me?.user.displayName || me?.user.username}
                        </Text>
                        <Text size="xs" c="dimmed" lh={1.15}>
                          {isAdmin ? '서비스 관리자' : (roleLabels[me?.roles?.[0] ?? ''] ?? '사용자')}
                        </Text>
                      </Box>
                    ) : null}
                    <IconChevronDown size={16} />
                  </Group>
                </UnstyledButton>
              </Menu.Target>

              <Menu.Dropdown>
                <Menu.Label>계정</Menu.Label>
                <Box px="sm" py={6}>
                  <Text size="sm" fw={700}>
                    {me?.user.displayName || me?.user.username}
                  </Text>
                  <Text size="xs" c="dimmed">
                    {me?.user.username}
                    {me?.user.email ? ` · ${me.user.email}` : ''}
                  </Text>
                  <Group gap={6} mt={6}>
                    {(me?.roles ?? []).map((r) => (
                      <Badge key={r} size="sm" variant="light" color={r.endsWith('admin') ? 'grape' : 'confmcp'}>
                        {roleLabels[r] ?? r}
                      </Badge>
                    ))}
                    <Badge size="sm" variant="light" color="gray">
                      {authModeLabels[me?.authMode ?? ''] ?? me?.authMode}
                    </Badge>
                  </Group>
                  <Text size="xs" c="dimmed" mt={6}>
                    Confluence: {me?.confluence ? `${me.confluence.confluenceUsername} (${me.confluence.confluenceDisplay})` : '매핑 없음'}
                  </Text>
                </Box>

                <Menu.Divider />
                <Menu.Label>서비스 버전</Menu.Label>
                <Box px="sm" py={6} data-testid="profile-version">
                  <Group gap="xs" wrap="nowrap">
                    <IconVersions size={18} />
                    <div>
                      <Text size="sm" fw={700}>
                        {serviceName} v{version?.version ?? '—'}
                      </Text>
                      <Text size="xs" c="dimmed">
                        빌드 {version?.commit?.slice(0, 7) ?? '—'} · {version?.buildDate ?? '—'}
                      </Text>
                    </div>
                  </Group>
                </Box>

                <Menu.Divider />
                <Menu.Item leftSection={<IconUserCog size={18} />} onClick={() => navigate('/me/settings')}>
                  개인 설정
                </Menu.Item>
                <Menu.Item leftSection={<IconKey size={18} />} onClick={() => navigate('/me/keys')}>
                  내 API 키
                </Menu.Item>
                <Menu.Item
                  leftSection={<IconBook2 size={18} />}
                  component="a"
                  href="https://hkjang.github.io/confmcp/guide-user.html"
                  target="_blank"
                  rel="noreferrer"
                >
                  사용자 가이드
                </Menu.Item>
                <Menu.Divider />
                <Menu.Item color="red" leftSection={<IconLogout size={18} />} onClick={() => void logout()}>
                  로그아웃
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="xs">
        <AppShell.Section
          grow
          component={ScrollArea}
          type="hover"
          scrollbarSize={10}
          scrollHideDelay={500}
          // ScrollArea's own classNames; AppShell.Section's typing does not see them.
          {...({ classNames: { scrollbar: 'confmcp-nav-scrollbar', thumb: 'confmcp-nav-thumb' } } as Record<string, unknown>)}
        >
          <Stack gap="lg" py="xs" pr={4}>
            {navGroups
              .filter((group) => !group.admin || isAdmin)
              .map((group) => (
                <Box key={group.label}>
                  <Text size="xs" fw={700} c="dimmed" px="sm" mb={6} lts={0.4}>
                    {group.label}
                  </Text>
                  <Stack gap={2}>
                    {group.items.map((item) => (
                      <NavLink
                        key={item.to}
                        component={Link}
                        to={item.to}
                        label={item.label}
                        className="confmcp-nav-link"
                        leftSection={<item.icon size={20} stroke={1.7} />}
                        rightSection={
                          item.badgeKey === 'approvals' && approvalsBadge > 0 ? (
                            <Badge size="sm" color="yellow" variant="filled" circle>
                              {approvalsBadge}
                            </Badge>
                          ) : null
                        }
                        active={isActive(item.to)}
                        onClick={closeMobile}
                        fz="sm"
                        py={9}
                        style={{ borderRadius: 'var(--mantine-radius-md)' }}
                      />
                    ))}
                  </Stack>
                </Box>
              ))}
          </Stack>
        </AppShell.Section>

        <AppShell.Section>
          <Divider mb="xs" />
          <Group justify="space-between" px="sm" pb="xs" wrap="nowrap">
            <Indicator color="teal" size={8} offset={-2} position="middle-start" processing>
              <Text size="xs" c="dimmed" pl="sm">
                {serviceName} v{version?.version ?? '—'}
              </Text>
            </Indicator>
            <Badge size="xs" variant="light" color="gray">
              {version?.commit?.slice(0, 7) ?? 'dev'}
            </Badge>
          </Group>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main className="confmcp-main">
        <Box maw={1440} mx="auto">
          <Outlet />
        </Box>
      </AppShell.Main>
    </AppShell>
  )
}
