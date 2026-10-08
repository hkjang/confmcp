import {
  Alert,
  Badge,
  Button,
  Code,
  Group,
  List,
  Modal,
  Paper,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import {
  IconCircleCheck,
  IconCircleX,
  IconHeartbeat,
  IconKey,
  IconShieldCheck,
  IconShieldLock,
} from '@tabler/icons-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import {
  ConfirmButton,
  CopyField,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  SaveBar,
  Section,
  StatList,
  TableScroll,
  notifyError,
} from '../../components/ui'
import { api, qs, type PermissionDecision, type SettingsEnvelope } from '../../lib/api'
import {
  formatDateTime,
  opLabels,
  opOrder,
  permissionModeLabels,
  verdictColors,
  verdictLabels,
} from '../../lib/format'
import { useConnectivityTest, useSettingsGroup } from '../../lib/useSettingsGroup'

interface PermissionSettings {
  mode: string
  pluginBaseUrl: string
  cacheTtlSec: number
  timeoutSec: number
}

interface PermissionHealth {
  mode?: string
  detail?: string
  plugin?: { status?: string; pluginVersion?: string; confluenceVersion?: string; buildNumber?: string } | null
}

interface PermissionTestResult {
  ok: boolean
  error?: string
  health?: PermissionHealth | null
  decision?: PermissionDecision
}

const reasonLabels: Record<string, string> = {
  NOT_VISIBLE: '볼 수 없는 콘텐츠',
  NOT_PERMITTED: '권한 없음',
  NO_SPACE_PERMISSION: '공간 권한 없음',
  NO_SPACE_CREATE: '공간 작성 권한 없음',
  NO_SPACE_EDIT: '공간 편집 권한 없음',
  NO_SPACE_COMMENT: '공간 댓글 권한 없음',
  NO_SPACE_DELETE: '공간 삭제 권한 없음',
  NO_EDIT_PERMISSION: '편집 권한 없음',
  NO_COMMENT_PERMISSION: '댓글 권한 없음',
  NO_DELETE_PERMISSION: '삭제 권한 없음',
  NO_ATTACH: '첨부 권한 없음',
  PAGE_EDIT_RESTRICTED: '페이지 편집 제한',
  CONTENT_NOT_CURRENT: '현재 버전이 아님',
  USER_DEACTIVATED: '비활성 사용자',
  INVALID_TARGET: '대상 오류',
  NOT_SUPPORTED: '지원하지 않음',
  CHECK_FAILED: '판정 실패',
  PERMISSION_UNKNOWN: '확인 불가',
  OPERATION_NOT_REPORTED: '플러그인 응답에 없음',
  OPERATIONS_UNAVAILABLE: '판정 불가',
  UNKNOWN_OPERATION: '알 수 없는 작업',
}

const fields: Field<PermissionSettings>[] = [
  {
    kind: 'select',
    key: 'mode',
    label: '판정 방식',
    description: '플러그인은 Confluence 내부 권한 서비스로 요청자 본인의 유효 권한을 판정합니다.',
    options: Object.entries(permissionModeLabels).map(([value, label]) => ({ value, label })),
    span: 12,
  },
  {
    kind: 'text',
    key: 'pluginBaseUrl',
    label: '권한 플러그인 URL',
    description: '비우면 Confluence 기본 URL 을 사용합니다.',
    placeholder: 'https://wiki.example.internal/confluence',
    span: 12,
  },
  {
    kind: 'secret',
    secretKey: 'pluginSecret',
    label: '플러그인 공유 비밀',
    description: '요청 HMAC 서명에 사용합니다. 직접 입력하거나 아래 "새 비밀 생성" 을 사용하십시오.',
    span: 12,
  },
  {
    kind: 'number',
    key: 'cacheTtlSec',
    label: '판정 캐시 (초)',
    description: '0 = 매번 확인(권장). 최대 300초.',
    min: 0,
    max: 300,
  },
  {
    kind: 'number',
    key: 'timeoutSec',
    label: '요청 제한 시간 (초)',
    description: '플러그인 응답 대기 시간입니다 (1–60).',
    min: 1,
    max: 60,
  },
]

export function AdminPermissionPage() {
  const queryClient = useQueryClient()
  const { query, draft, setDraft, secrets, setSecrets, secretPresence, save } =
    useSettingsGroup<PermissionSettings>('permission_plugin')
  const health = useConnectivityTest('permission')
  const check = useConnectivityTest('permission')

  const [generated, setGenerated] = useState<string | null>(null)
  const [username, setUsername] = useState('')
  const [targetKind, setTargetKind] = useState<'space' | 'content'>('space')
  const [spaceKey, setSpaceKey] = useState('')
  const [contentId, setContentId] = useState('')

  const generate = useMutation({
    mutationFn: () =>
      api.put<{ settings: SettingsEnvelope<PermissionSettings>; generatedSecret: string }>(
        '/api/admin/settings/permission_plugin',
        { value: draft, secrets: { pluginSecret: 'generate' } },
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(['settings', 'permission_plugin'], data.settings)
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
      setGenerated(data.generatedSecret)
    },
    onError: (err) => notifyError(err, '비밀 생성 실패'),
  })

  const healthResult = health.data as PermissionTestResult | undefined
  const checkResult = check.data as PermissionTestResult | undefined
  const target = targetKind === 'space' ? spaceKey.trim() : contentId.trim()
  const canCheck = username.trim() !== '' && target !== ''

  const runCheck = () =>
    check.mutate(
      qs({
        username: username.trim(),
        spaceKey: targetKind === 'space' ? spaceKey.trim().toUpperCase() : undefined,
        contentId: targetKind === 'content' ? contentId.trim() : undefined,
      }),
    )

  return (
    <>
      <PageHeader
        title="권한 판정"
        description="요청자 본인의 Confluence 유효 권한을 확인하는 방법을 설정합니다. 모든 도구 호출은 이 판정을 통과해야 합니다."
      />

      <Alert variant="light" color="blue" icon={<IconShieldLock size={20} />} mb="lg" title="판정할 수 없으면 거부합니다 (fail-closed)">
        <Text size="sm">
          권한을 확인할 수 없는 경우(확인 불가)는 항상 거부로 처리합니다. 서비스 계정이 보는 REST 제한 정보로 요청자 권한을
          추정하는 방식은 의도적으로 제공하지 않습니다. 서비스 계정의 시야는 요청자의 권한이 아니기 때문입니다.
        </Text>
      </Alert>

      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}

      {draft ? (
        <Section title="판정 설정">
          <SettingsForm
            fields={fields}
            value={draft}
            onChange={setDraft}
            secrets={secrets}
            onSecretChange={setSecrets}
            secretPresence={secretPresence}
          />
          {draft.mode === 'delegated' ? (
            <Alert variant="light" color="yellow" mt="md">
              사용자 위임 모드는 각 사용자가 내 설정에서 본인 Confluence 자격증명을 연결해야 하며, Confluence 연결 설정에서
              &ldquo;사용자 본인 자격증명 연결 허용&rdquo; 을 켜야 합니다.
            </Alert>
          ) : null}
          <SaveBar
            onSave={() => save.mutate()}
            saving={save.isPending}
            extra={
              <>
                <ConfirmButton
                  title="새 공유 비밀 생성"
                  message="새 비밀을 생성해 바로 저장합니다. 지금 입력 중인 다른 설정도 함께 저장됩니다. Confluence 플러그인에 새 값을 입력하기 전까지는 권한 판정이 실패하고 모든 도구가 차단됩니다."
                  confirmLabel="생성"
                  color="orange"
                  variant="default"
                  size="md"
                  leftSection={<IconKey size={18} />}
                  loading={generate.isPending}
                  onConfirm={() => generate.mutate()}
                >
                  새 비밀 생성
                </ConfirmButton>
                <Button
                  variant="default"
                  leftSection={<IconHeartbeat size={18} />}
                  loading={health.isPending}
                  onClick={() => health.mutate(undefined)}
                >
                  상태 확인
                </Button>
              </>
            }
          />
          {health.error ? (
            <Stack mt="md">
              <ErrorBlock error={health.error} title="상태 확인 실패" />
            </Stack>
          ) : null}
          {healthResult ? <HealthResult result={healthResult} /> : null}
        </Section>
      ) : null}

      <Section
        title="판정 시험"
        description="매핑된 사용자가 공간이나 콘텐츠에 대해 어떤 작업을 할 수 있는지 실제로 판정합니다. 정책(공간 허용 등)은 반영하지 않은 Confluence 권한 결과입니다."
      >
        <Stack gap="md">
          <SimpleGrid cols={{ base: 1, sm: 2, md: 3 }} spacing="md">
            <TextInput
              label="사용자명"
              description="콘솔 사용자명 또는 Confluence 사용자명"
              placeholder="alice"
              value={username}
              onChange={(e) => setUsername(e.currentTarget.value)}
            />
            <Stack gap={4}>
              <Text size="sm" fw={500}>
                대상 유형
              </Text>
              <SegmentedControl
                fullWidth
                value={targetKind}
                onChange={(v) => setTargetKind(v === 'content' ? 'content' : 'space')}
                data={[
                  { value: 'space', label: '공간' },
                  { value: 'content', label: '콘텐츠' },
                ]}
              />
            </Stack>
            {targetKind === 'space' ? (
              <TextInput
                label="공간 키"
                placeholder="DEV"
                value={spaceKey}
                onChange={(e) => setSpaceKey(e.currentTarget.value)}
              />
            ) : (
              <TextInput
                label="콘텐츠 ID"
                placeholder="123456"
                inputMode="numeric"
                value={contentId}
                onChange={(e) => setContentId(e.currentTarget.value)}
              />
            )}
          </SimpleGrid>
          <Group justify="flex-end">
            <Button
              leftSection={<IconShieldCheck size={18} />}
              loading={check.isPending}
              disabled={!canCheck}
              onClick={runCheck}
            >
              판정 시험
            </Button>
          </Group>
          {check.error ? <ErrorBlock error={check.error} title="판정 실패" /> : null}
          {checkResult ? (
            checkResult.ok && checkResult.decision ? (
              <DecisionView decision={checkResult.decision} />
            ) : (
              <Alert variant="light" color="red" icon={<IconCircleX size={20} />} title="판정하지 못했습니다">
                <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                  {checkResult.error ?? '판정 결과가 없습니다.'}
                </Text>
              </Alert>
            )
          ) : null}
        </Stack>
      </Section>

      <Section title="Confluence 권한 플러그인">
        <Text size="sm" mb="xs">
          플러그인은 저장소의 <Code>plugin/</Code> 디렉터리에 있습니다. Confluence 관리 → 앱 관리에서 jar 를 업로드한 뒤 공유
          비밀을 입력하십시오.
        </Text>
        <List size="sm" spacing={4}>
          <List.Item>
            공유 비밀 설정: <Code>PUT /rest/confmcp/1.0/config</Code> (Confluence 시스템 관리자만) 또는 JVM 옵션{' '}
            <Code>-Dconfmcp.secret=…</Code>
          </List.Item>
          <List.Item>
            상태 확인: <Code>GET /rest/confmcp/1.0/health</Code>
          </List.Item>
          <List.Item>
            권한 판정: <Code>POST /rest/confmcp/1.0/permissions/check</Code>, <Code>/permissions/batch</Code>
          </List.Item>
        </List>
        <Text size="sm" c="dimmed" mt="sm">
          모든 요청은 공유 비밀로 서명되며, 비밀이 설정되지 않은 플러그인은 판정 요청에 응답하지 않습니다.
        </Text>
      </Section>

      <Modal
        opened={generated !== null}
        onClose={() => setGenerated(null)}
        title={<Text fw={700}>새 공유 비밀이 생성되었습니다</Text>}
        size="lg"
        closeOnClickOutside={false}
      >
        <Stack>
          <Alert variant="light" color="orange">
            이 값은 지금 한 번만 표시됩니다. 복사해서 Confluence 권한 플러그인에 입력하십시오.
          </Alert>
          {generated ? <CopyField value={generated} label="비밀 복사" /> : null}
          <Text size="sm">
            Confluence 시스템 관리자로 <Code>PUT /rest/confmcp/1.0/config</Code> 를 호출해 이 값을 저장하십시오. 플러그인에 같은
            값이 들어가기 전까지는 권한 판정이 실패하므로 모든 도구 호출이 차단됩니다. 입력 후 &ldquo;상태 확인&rdquo; 으로
            확인하십시오.
          </Text>
          <Group justify="flex-end">
            <Button onClick={() => setGenerated(null)}>복사했습니다</Button>
          </Group>
        </Stack>
      </Modal>
    </>
  )
}

function HealthResult({ result }: { result: PermissionTestResult }) {
  const h = result.health ?? undefined
  return (
    <Stack gap="md" mt="md">
      <Alert
        variant="light"
        color={result.ok ? 'teal' : 'red'}
        icon={result.ok ? <IconCircleCheck size={20} /> : <IconCircleX size={20} />}
        title={result.ok ? '권한 판정 사용 가능' : '권한 판정 사용 불가 — 모든 도구가 차단됩니다'}
      >
        {result.error ? (
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
            {result.error}
          </Text>
        ) : (
          <Text size="sm">{h?.detail ?? '플러그인이 서명된 요청에 정상 응답했습니다.'}</Text>
        )}
      </Alert>
      {h ? (
        <Paper withBorder p="md" radius="md">
          <StatList
            items={[
              { label: '판정 방식', value: h.mode ? (permissionModeLabels[h.mode] ?? h.mode) : '—' },
              ...(h.plugin
                ? [
                    { label: '플러그인 상태', value: h.plugin.status || '—' },
                    { label: '플러그인 버전', value: h.plugin.pluginVersion || '—' },
                    {
                      label: 'Confluence 버전',
                      value: h.plugin.confluenceVersion
                        ? `${h.plugin.confluenceVersion}${h.plugin.buildNumber ? ` (빌드 ${h.plugin.buildNumber})` : ''}`
                        : '—',
                    },
                  ]
                : []),
            ]}
          />
        </Paper>
      ) : null}
    </Stack>
  )
}

function DecisionView({ decision }: { decision: PermissionDecision }) {
  const ops = [
    ...opOrder.filter((op) => op in decision.results),
    ...Object.keys(decision.results).filter((op) => !opOrder.includes(op)),
  ]
  const targetLabel =
    decision.target.kind === 'content' ? `콘텐츠 ${decision.target.id ?? ''}` : `공간 ${decision.target.spaceKey ?? ''}`
  return (
    <Paper withBorder p="md" radius="md">
      <Group gap="xs" mb="sm" wrap="wrap">
        <Badge variant="light" color="gray" style={{ textTransform: 'none' }}>
          {targetLabel}
        </Badge>
        {decision.spaceKey && decision.target.kind === 'content' ? (
          <Badge variant="outline" color="gray" style={{ textTransform: 'none' }}>
            공간 {decision.spaceKey}
          </Badge>
        ) : null}
        <Badge variant="outline" style={{ textTransform: 'none' }}>
          출처: {decision.source === 'plugin' ? '권한 플러그인' : decision.source === 'delegated' ? '사용자 위임' : decision.source}
        </Badge>
        <Text size="sm" c="dimmed">
          {formatDateTime(decision.evaluatedAt)} 판정
        </Text>
      </Group>
      <TableScroll minWidth={420}>
        <Table verticalSpacing="xs" fz="sm" striped>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>작업</Table.Th>
              <Table.Th>판정</Table.Th>
              <Table.Th>사유</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {ops.map((op) => {
              const verdict = decision.results[op]
              const reason = decision.reasons?.[op]
              return (
                <Table.Tr key={op}>
                  <Table.Td>{opLabels[op] ?? op}</Table.Td>
                  <Table.Td>
                    <Badge color={verdictColors[verdict] ?? 'gray'} variant="light">
                      {verdictLabels[verdict] ?? verdict}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    {reason ? (
                      <Text size="sm">
                        {reasonLabels[reason] ?? reason}{' '}
                        {reasonLabels[reason] ? (
                          <Text span size="xs" c="dimmed" ff="monospace">
                            {reason}
                          </Text>
                        ) : null}
                      </Text>
                    ) : (
                      <Text size="sm" c="dimmed">
                        —
                      </Text>
                    )}
                  </Table.Td>
                </Table.Tr>
              )
            })}
          </Table.Tbody>
        </Table>
      </TableScroll>
    </Paper>
  )
}
