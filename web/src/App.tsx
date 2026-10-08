import { Center, Loader, Stack, Text, useMantineColorScheme } from '@mantine/core'
import { useEffect } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'

import { AppLayout } from './components/AppLayout'
import { useAuth } from './lib/auth'
import { ApprovalDetailPage } from './pages/ApprovalDetail'
import { ConsentPage } from './pages/Consent'
import { LoginPage } from './pages/Login'
import { MyAiPage } from './pages/MyAi'
import { MyApprovalsPage } from './pages/MyApprovals'
import { MyAuditPage } from './pages/MyAudit'
import { MyConfluencePage } from './pages/MyConfluence'
import { MyConnectPage } from './pages/MyConnect'
import { MyKeysPage } from './pages/MyKeys'
import { MyPermissionsPage } from './pages/MyPermissions'
import { MySettingsPage } from './pages/MySettings'
import { MyToolsPage } from './pages/MyTools'
import { MyUploadsPage } from './pages/MyUploads'
import { NotFoundPage } from './pages/NotFound'
import { OverviewPage } from './pages/Overview'
import { AdminAiPage } from './pages/admin/Ai'
import { AdminApprovalsPage } from './pages/admin/Approvals'
import { AdminAuditPage } from './pages/admin/Audit'
import { AdminAuthPage } from './pages/admin/Auth'
import { AdminConfluencePage } from './pages/admin/Confluence'
import { AdminConnectionsPage } from './pages/admin/Connections'
import { AdminDashboardPage } from './pages/admin/Dashboard'
import { AdminIdentityPage } from './pages/admin/Identity'
import { AdminKeysPage } from './pages/admin/Keys'
import { AdminLimitsPage } from './pages/admin/Limits'
import { AdminOperationsPage } from './pages/admin/Operations'
import { AdminPermissionPage } from './pages/admin/Permission'
import { AdminPolicyPage } from './pages/admin/Policy'
import { AdminSecurityPage } from './pages/admin/Security'
import { AdminSystemPage } from './pages/admin/System'
import { AdminToolsPage } from './pages/admin/Tools'
import { AdminUiPage } from './pages/admin/Ui'
import { AdminUsersPage } from './pages/admin/Users'

function FullScreenLoader({ label }: { label: string }) {
  return (
    <Center h="100vh">
      <Stack align="center" gap="sm">
        <Loader size="lg" />
        <Text c="dimmed">{label}</Text>
      </Stack>
    </Center>
  )
}

/**
 * RequireAuth keeps the attempted path so that a refresh or a deep link
 * returns to the same screen after signing in.
 */
function RequireAuth({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  const location = useLocation()
  if (loading) return <FullScreenLoader label="세션을 확인하고 있습니다…" />
  if (!me) {
    const target = `${location.pathname}${location.search}`
    return <Navigate to={`/login?next=${encodeURIComponent(target)}`} replace />
  }
  return <>{children}</>
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { me } = useAuth()
  if (me && !me.isServiceAdmin) return <Navigate to="/" replace />
  return <>{children}</>
}

/** ConsentRoute shows an authorization error without a session; a real request needs one. */
function ConsentRoute() {
  const location = useLocation()
  if (new URLSearchParams(location.search).has('error')) return <ConsentPage />
  return (
    <RequireAuth>
      <ConsentPage />
    </RequireAuth>
  )
}

function Admin({ children }: { children: React.ReactNode }) {
  return <RequireAdmin>{children}</RequireAdmin>
}

export function App() {
  const { me, loading } = useAuth()
  const { setColorScheme } = useMantineColorScheme()

  // The user's font scale multiplies every rem-based size.
  useEffect(() => {
    const scale = me?.prefs.fontScale || me?.ui.fontScale || 1
    document.documentElement.style.setProperty('--confmcp-font-scale', String(scale))
  }, [me?.prefs.fontScale, me?.ui.fontScale])

  // Apply the saved theme once per sign-in.
  useEffect(() => {
    const pref = me?.prefs.theme
    if (pref === 'light' || pref === 'dark') setColorScheme(pref)
    else if (pref === 'system') setColorScheme('auto')
  }, [me?.prefs.theme, setColorScheme])

  useEffect(() => {
    const name = me?.ui.serviceName ?? 'confmcp'
    document.title = `${name} — ${me?.ui.tagline ?? 'Confluence MCP 게이트웨이'}`
  }, [me?.ui.serviceName, me?.ui.tagline])

  if (loading) return <FullScreenLoader label="서비스를 준비하고 있습니다…" />

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/oauth/consent" element={<ConsentRoute />} />

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<OverviewPage />} />
        <Route path="/me/connect" element={<MyConnectPage />} />
        <Route path="/me/confluence" element={<MyConfluencePage />} />
        <Route path="/me/approvals" element={<MyApprovalsPage />} />
        <Route path="/me/approvals/:id" element={<ApprovalDetailPage scope="me" />} />
        <Route path="/me/keys" element={<MyKeysPage />} />
        <Route path="/me/uploads" element={<MyUploadsPage />} />
        <Route path="/me/permissions" element={<MyPermissionsPage />} />
        <Route path="/me/tools" element={<MyToolsPage />} />
        <Route path="/me/ai" element={<MyAiPage />} />
        <Route path="/me/audit" element={<MyAuditPage />} />
        <Route path="/me/settings" element={<MySettingsPage />} />

        <Route path="/admin" element={<Admin><AdminDashboardPage /></Admin>} />
        <Route path="/admin/auth" element={<Admin><AdminAuthPage /></Admin>} />
        <Route path="/admin/confluence" element={<Admin><AdminConfluencePage /></Admin>} />
        <Route path="/admin/permission" element={<Admin><AdminPermissionPage /></Admin>} />
        <Route path="/admin/identity" element={<Admin><AdminIdentityPage /></Admin>} />
        <Route path="/admin/policy" element={<Admin><AdminPolicyPage /></Admin>} />
        <Route path="/admin/tools" element={<Admin><AdminToolsPage /></Admin>} />
        <Route path="/admin/approvals" element={<Admin><AdminApprovalsPage /></Admin>} />
        <Route path="/admin/approvals/:id" element={<Admin><ApprovalDetailPage scope="admin" /></Admin>} />
        <Route path="/admin/operations" element={<Admin><AdminOperationsPage /></Admin>} />
        <Route path="/admin/connections" element={<Admin><AdminConnectionsPage /></Admin>} />
        <Route path="/admin/keys" element={<Admin><AdminKeysPage /></Admin>} />
        <Route path="/admin/users" element={<Admin><AdminUsersPage /></Admin>} />
        <Route path="/admin/limits" element={<Admin><AdminLimitsPage /></Admin>} />
        <Route path="/admin/ai" element={<Admin><AdminAiPage /></Admin>} />
        <Route path="/admin/security" element={<Admin><AdminSecurityPage /></Admin>} />
        <Route path="/admin/ui" element={<Admin><AdminUiPage /></Admin>} />
        <Route path="/admin/audit" element={<Admin><AdminAuditPage /></Admin>} />
        <Route path="/admin/system" element={<Admin><AdminSystemPage /></Admin>} />

        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
