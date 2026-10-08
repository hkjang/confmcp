// Drive every confmcp screen in a real browser, fail on any console or page
// error, exercise the select boxes, and save the screenshots used by the
// documentation site.
//
// Runs inside the official Playwright image so no browser has to be installed:
//
//   hack/screenshots.sh            # wraps the docker run below
//   node hack/screenshots.mjs --base http://host.docker.internal:18088 --out docs/screenshots

import { chromium } from '../web/node_modules/playwright/index.mjs'
import { mkdir } from 'node:fs/promises'

const args = process.argv.slice(2)
const argOf = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback
}

const BASE = argOf('base', 'http://127.0.0.1:18088')
const OUT = argOf('out', 'docs/screenshots')
const ADMIN = { username: 'admin', password: process.env.CONFMCP_ADMIN_PASSWORD || 'admin-password-1' }
const USER = { username: 'alice', password: 'alice-password-1' }

// Benign console noise that does not indicate a broken screen.
const IGNORED = [/Download the React DevTools/i, /React Router Future Flag/i, /\[vite\]/i, /status of 401/i]

const problems = []

async function capture(page, name, { full = false } = {}) {
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: full })
  console.log(`  저장: ${name}.png`)
}

function watch(page, label) {
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    const text = msg.text()
    if (IGNORED.some((re) => re.test(text))) return
    problems.push(`[${label}] console: ${text}`)
  })
  page.on('pageerror', (err) => problems.push(`[${label}] pageerror: ${err.message}`))
  page.on('requestfailed', (req) => {
    const failure = req.failure()?.errorText ?? ''
    if (/ERR_ABORTED/.test(failure)) return
    // The dev Keycloak listens on the host's loopback, which the browser
    // container cannot reach; silent SSO then quietly falls back to the form.
    if (/openid-connect\/auth/.test(req.url())) return
    problems.push(`[${label}] request failed: ${req.url()} (${failure})`)
  })
  page.on('response', (res) => {
    if (res.status() >= 500) problems.push(`[${label}] HTTP ${res.status()} ${res.url()}`)
  })
}

async function signIn(page, who) {
  await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByLabel('아이디').fill(who.username)
  await page.locator('input[type="password"]').first().fill(who.password)
  await page.getByRole('button', { name: '로그인', exact: true }).click()
  await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 20000 })
  await page.waitForLoadState('networkidle').catch(() => {})
}

/** [file, route, heading text] — the heading proves the right screen rendered. */
const adminScreens = [
  ['admin-dashboard', '/admin', '관리 대시보드'],
  ['admin-auth', '/admin/auth', '인증'],
  ['admin-confluence', '/admin/confluence', 'Confluence 연결'],
  ['admin-permission', '/admin/permission', '권한 판정'],
  ['admin-identity', '/admin/identity', '사용자 매핑'],
  ['admin-policy', '/admin/policy', '접근 정책'],
  ['admin-tools', '/admin/tools', 'MCP 도구'],
  ['admin-approvals', '/admin/approvals', '승인 요청'],
  ['admin-operations', '/admin/operations', '쓰기 실행 기록'],
  ['admin-connections', '/admin/connections', 'MCP 연결'],
  ['admin-keys', '/admin/keys', 'API 키'],
  ['admin-users', '/admin/users', '사용자'],
  ['admin-limits', '/admin/limits', '처리 한도'],
  ['admin-ai', '/admin/ai', 'AI 설정'],
  ['admin-security', '/admin/security', '보안'],
  ['admin-ui', '/admin/ui', '화면 설정'],
  ['admin-audit', '/admin/audit', '감사 로그'],
  ['admin-system', '/admin/system', '시스템'],
]

const userScreens = [
  ['user-overview', '/', '안녕하세요'],
  ['user-connect', '/me/connect', 'MCP 연결'],
  ['user-confluence', '/me/confluence', '내 Confluence'],
  ['user-approvals', '/me/approvals', '변경 승인'],
  ['user-keys', '/me/keys', '내 API 키'],
  ['user-uploads', '/me/uploads', '첨부 업로드'],
  ['user-permissions', '/me/permissions', '내 권한 확인'],
  ['user-tools', '/me/tools', '사용 가능한 도구'],
  ['user-ai', '/me/ai', 'AI 도우미'],
  ['user-audit', '/me/audit', '내 활동 기록'],
  ['user-settings', '/me/settings', '개인 설정'],
]

async function go(page, url) {
  for (let i = 0; ; i++) {
    try {
      return await page.goto(url, { waitUntil: 'domcontentloaded' })
    } catch (err) {
      // Docker-to-host port forwarding occasionally drops a connection.
      if (i >= 2 || !/ERR_EMPTY_RESPONSE|ERR_CONNECTION_RESET/.test(String(err))) throw err
      await page.waitForTimeout(800)
    }
  }
}

async function visit(page, [name, path, heading]) {
  await go(page, `${BASE}${path}`)
  await page.getByRole('heading', { name: heading, exact: false }).first().waitFor({ timeout: 20000 })
  await capture(page, name)
}

/** Open a Mantine Select by its label and pick an option, failing loudly if it misbehaves. */
async function pick(page, label, option) {
  const input = page.getByRole('textbox', { name: label }).first()
  await input.click()
  await page.getByRole('option', { name: option }).first().click()
  await page.waitForTimeout(150)
  const value = await input.inputValue()
  if (!value) problems.push(`[select] ${label}: 선택 후 값이 비어 있습니다`)
  return value
}

async function main() {
  await mkdir(OUT, { recursive: true })
  const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage', '--font-render-hinting=none'] })
  const desktop = { viewport: { width: 1440, height: 1000 }, locale: 'ko-KR', timezoneId: 'Asia/Seoul' }

  // Login screen, which shows the version before sign-in.
  const anon = await browser.newContext(desktop)
  const anonPage = await anon.newPage()
  watch(anonPage, 'login')
  await go(anonPage, `${BASE}/login`)
  await anonPage.getByTestId('login-version').waitFor()
  await capture(anonPage, 'login', { full: false })
  await anon.close()

  // Admin console.
  const adminCtx = await browser.newContext(desktop)
  const adminPage = await adminCtx.newPage()
  watch(adminPage, 'admin')
  await signIn(adminPage, ADMIN)
  console.log('관리자 로그인')
  for (const screen of adminScreens) await visit(adminPage, screen)

  // Refresh on a deep link must come back to the same screen.
  await go(adminPage, `${BASE}/admin/policy`)
  await adminPage.reload({ waitUntil: 'domcontentloaded' })
  await adminPage.getByRole('heading', { name: '접근 정책' }).first().waitFor({ timeout: 20000 })
  if (!adminPage.url().endsWith('/admin/policy')) problems.push(`[refresh] ${adminPage.url()}`)

  // Keycloak connection test with the values to register.
  await go(adminPage, `${BASE}/admin/auth`)
  await adminPage.getByRole('button', { name: '연결 시험' }).first().click()
  await adminPage.waitForTimeout(2500)
  await capture(adminPage, 'admin-auth-test')

  // Policy editor select boxes.
  await go(adminPage, `${BASE}/admin/policy`)
  await adminPage.getByRole('button', { name: '규칙 추가' }).first().click()
  await adminPage.getByRole('dialog').waitFor()
  await pick(adminPage, '종류', '페이지 트리')
  await pick(adminPage, '종류', '콘텐츠 유형')
  await capture(adminPage, 'admin-policy-edit', { full: false })
  await adminPage.keyboard.press('Escape')

  // Profile menu with the version.
  await go(adminPage, `${BASE}/admin`)
  await adminPage.getByTestId('profile-menu').click()
  await adminPage.getByTestId('profile-version').waitFor()
  await capture(adminPage, 'profile-version', { full: false })
  await adminPage.keyboard.press('Escape')

  // Dark theme.
  await adminPage.getByRole('button', { name: '테마 전환' }).click()
  await adminPage.waitForTimeout(400)
  await capture(adminPage, 'admin-dashboard-dark', { full: false })
  await adminPage.getByRole('button', { name: '테마 전환' }).click()
  await adminCtx.close()

  // Personal pages as a writer.
  const userCtx = await browser.newContext(desktop)
  const userPage = await userCtx.newPage()
  watch(userPage, 'user')
  await signIn(userPage, USER)
  // Revoke keys left by earlier runs, so the key list stays tidy and the
  // per-user active key limit never blocks the issue step.
  const oldKeys = await (await userPage.request.get(`${BASE}/api/me/keys`)).json()
  for (const k of oldKeys) {
    if (k.status === 'active' && ['Claude Code', 'e2e'].includes(k.name)) {
      await userPage.request.delete(`${BASE}/api/me/keys/${k.id}`)
    }
  }
  console.log('사용자 로그인')
  for (const screen of userScreens) await visit(userPage, screen)

  // The newest pending change proposal, with its diff.
  await go(userPage, `${BASE}/me/approvals`)
  await userPage.locator('tbody tr').first().click()
  await userPage.getByTestId('diff-view').waitFor({ timeout: 20000 })
  await capture(userPage, 'user-approval-detail')

  // Permission check.
  await go(userPage, `${BASE}/me/permissions`)
  await userPage.getByText('페이지·첨부 ID', { exact: true }).click()
  await userPage.getByRole('textbox', { name: '콘텐츠 ID' }).fill('65545')
  await userPage.getByRole('button', { name: '확인', exact: true }).click()
  await userPage.getByText('작업별 판정').waitFor({ timeout: 20000 })
  await capture(userPage, 'user-permissions-result')

  // Key issue dialog with its role select.
  await go(userPage, `${BASE}/me/keys`)
  await userPage.getByRole('button', { name: '키 발급' }).first().click()
  await userPage.getByLabel('키 이름').fill('Claude Code')
  await pick(userPage, '권한 역할', /writer/)
  await capture(userPage, 'user-keys-create', { full: false })
  await userPage.getByRole('button', { name: '발급', exact: true }).click()
  await userPage.getByText('이 값은 지금 한 번만 표시됩니다').waitFor({ timeout: 20000 })
  await capture(userPage, 'user-keys-issued', { full: false })
  await userPage.keyboard.press('Escape')

  // MCP OAuth consent, as an MCP client would trigger it.
  const reg = await (
    await userPage.request.post(`${BASE}/oauth/register`, {
      data: { client_name: 'Claude Code', redirect_uris: ['http://localhost:33418/callback'] },
    })
  ).json()
  const authorize = `${BASE}/oauth/authorize?response_type=code&client_id=${reg.client_id}` +
    `&redirect_uri=${encodeURIComponent('http://localhost:33418/callback')}` +
    `&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256&state=demo`
  await go(userPage, authorize)
  await userPage.getByTestId('consent-approve').waitFor({ timeout: 20000 })
  await capture(userPage, 'consent', { full: false })
  await userCtx.close()

  // Mobile layout.
  const mobileCtx = await browser.newContext({
    viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true,
    locale: 'ko-KR', timezoneId: 'Asia/Seoul',
  })
  const mobilePage = await mobileCtx.newPage()
  watch(mobilePage, 'mobile')
  await go(mobilePage, `${BASE}/login`)
  await mobilePage.getByRole('button', { name: '로그인', exact: true }).waitFor()
  await capture(mobilePage, 'mobile-login', { full: false })
  await signIn(mobilePage, USER)
  await capture(mobilePage, 'mobile-overview', { full: false })
  const overflow = await mobilePage.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  if (overflow > 1) problems.push(`[mobile] 가로 넘침 ${overflow}px`)
  await mobilePage.getByRole('button', { name: '메뉴 열기' }).click()
  await mobilePage.waitForTimeout(400)
  await capture(mobilePage, 'mobile-nav', { full: false })
  await mobileCtx.close()

  await browser.close()
  if (problems.length > 0) {
    console.error(`\n화면 오류 ${problems.length}건:`)
    for (const p of problems) console.error(`  - ${p}`)
    process.exit(1)
  }
  console.log('\n모든 화면이 오류 없이 렌더링되었습니다.')
}

main().catch((err) => {
  console.error(err)
  for (const p of problems) console.error(`  - ${p}`)
  process.exit(1)
})
