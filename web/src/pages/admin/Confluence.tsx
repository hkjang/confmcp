import { Alert, Badge, Button, Group, List, Paper, SimpleGrid, Stack, Text } from '@mantine/core'
import { IconAlertTriangle, IconCircleCheck, IconCircleX, IconPlugConnected } from '@tabler/icons-react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import { ErrorBlock, LoadingBlock, PageHeader, SaveBar, Section, StatList } from '../../components/ui'
import { executionModeLabels, formatDateTime } from '../../lib/format'
import { useConnectivityTest, useSettingsGroup } from '../../lib/useSettingsGroup'

interface ConfluenceSettings {
  instanceId: string
  baseUrl: string
  serviceUsername: string
  timeoutSec: number
  toolTimeoutSec: number
  insecureSkipTls: boolean
  executionMode: string
  allowUserCredential: boolean
  trustSameDirectory: boolean
  detectedVersion?: string
  detectedBuild?: string
  detectedAt?: string
}

interface ConfluenceTestResult {
  ok: boolean
  error?: string
  note?: string
  warnings?: string[]
  sampleSpaces?: string[]
  info?: {
    version?: string
    buildNumber?: string
    baseUrl?: string
    user?: { username: string; userKey: string; displayName: string; email?: string }
    features?: Record<string, boolean>
  }
}

const featureLabels: Record<string, string> = {
  spaces: '공간 목록 (spaces)',
  cql: 'CQL 검색 (cql)',
  content: '콘텐츠 조회 (content)',
}

function connectionFields(hasPassword: boolean): Field<ConfluenceSettings>[] {
  return [
    {
      kind: 'text',
      key: 'baseUrl',
      label: 'Confluence 기본 URL',
      description: '컨텍스트 경로까지 포함합니다. 예: https://wiki.example.internal/confluence',
      placeholder: 'https://wiki.example.internal/confluence',
      span: 8,
    },
    {
      kind: 'text',
      key: 'instanceId',
      label: '인스턴스 ID',
      description: '매핑·감사 기록을 구분하는 이름입니다. 보통 default 그대로 둡니다.',
      placeholder: 'default',
      span: 4,
    },
    {
      kind: 'text',
      key: 'serviceUsername',
      label: '서비스 계정 사용자명',
      description: '게이트웨이가 Confluence REST 를 호출할 때 쓰는 전용 계정입니다.',
      placeholder: 'confmcp-svc',
    },
    {
      kind: 'secret',
      secretKey: 'servicePassword',
      label: '서비스 계정 비밀번호',
      description: `Confluence 7.2.0 은 PAT 가 없어 HTTPS Basic 인증을 사용합니다. ${
        hasPassword ? '저장된 값이 있으며, 비워 두면 유지합니다.' : '아직 저장된 값이 없습니다.'
      }`,
    },
    {
      kind: 'number',
      key: 'timeoutSec',
      label: 'REST 요청 제한 시간 (초)',
      description: 'Confluence 요청 하나의 최대 대기 시간입니다 (1–120).',
      min: 1,
      max: 120,
    },
    {
      kind: 'number',
      key: 'toolTimeoutSec',
      label: '도구 실행 제한 시간 (초)',
      description: 'MCP 도구 호출 전체의 최대 시간입니다 (1–600).',
      min: 1,
      max: 600,
    },
    {
      kind: 'switch',
      key: 'insecureSkipTls',
      label: 'TLS 인증서 검증 생략',
      description: '시험 환경에서만 사용하십시오. 운영에서는 내부 CA 를 신뢰 저장소에 추가하십시오.',
      span: 12,
    },
  ]
}

const executionFields: Field<ConfluenceSettings>[] = [
  {
    kind: 'select',
    key: 'executionMode',
    label: '실행 방식',
    description: '서비스 계정 실행 시 요청자 권한은 권한 플러그인으로 확인합니다.',
    options: Object.entries(executionModeLabels).map(([value, label]) => ({ value, label })),
    span: 12,
  },
  {
    kind: 'switch',
    key: 'allowUserCredential',
    label: '사용자 본인 자격증명 연결 허용',
    description: '사용자가 내 설정에서 자신의 Confluence 비밀번호를 등록할 수 있습니다. 위임 실행·위임 판정에 필요합니다.',
  },
  {
    kind: 'switch',
    key: 'trustSameDirectory',
    label: '같은 사용자 디렉터리 신뢰',
    description:
      'Keycloak 과 Confluence 가 같은 사용자 디렉터리를 쓸 때만 켜십시오. 켜면 첫 로그인 시 사용자명이 정확히 같은 계정을 자동 매핑합니다.',
  },
]

export function AdminConfluencePage() {
  const { query, draft, setDraft, secrets, setSecrets, secretPresence, save } =
    useSettingsGroup<ConfluenceSettings>('confluence')
  const test = useConnectivityTest('confluence')
  const result = test.data as ConfluenceTestResult | undefined

  const runTest = () =>
    test.mutate(undefined, {
      // The probe records the detected version; reload it.
      onSuccess: () => void query.refetch(),
    })

  const hasPassword = Boolean(secretPresence.servicePassword)

  return (
    <>
      <PageHeader
        title="Confluence 연결"
        description="Confluence Server 7.2.0 연결과 도구 실행 방식을 설정합니다."
      />

      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}

      {draft ? (
        <>
          <Section title="연결" description="저장한 뒤 연결 시험을 실행하면 버전과 기능을 확인합니다.">
            <SettingsForm
              fields={connectionFields(hasPassword)}
              value={draft}
              onChange={setDraft}
              secrets={secrets}
              onSecretChange={setSecrets}
              secretPresence={secretPresence}
            />
            <SaveBar
              onSave={() => save.mutate()}
              saving={save.isPending}
              extra={
                <Button
                  variant="default"
                  leftSection={<IconPlugConnected size={18} />}
                  loading={test.isPending}
                  onClick={runTest}
                >
                  연결 시험
                </Button>
              }
            />
            {test.error ? (
              <Stack mt="md">
                <ErrorBlock error={test.error} title="연결 시험 실패" />
              </Stack>
            ) : null}
            {result ? <TestResult result={result} /> : null}
          </Section>

          <Section title="실행 방식과 식별">
            <SettingsForm
              fields={executionFields}
              value={draft}
              onChange={setDraft}
              secrets={secrets}
              onSecretChange={setSecrets}
              secretPresence={secretPresence}
            />
            <SaveBar onSave={() => save.mutate()} saving={save.isPending} />
          </Section>

          <Section title="감지된 서버 정보" description="연결 시험에서 마지막으로 확인한 값입니다. 직접 수정할 수 없습니다.">
            <StatList
              items={[
                { label: '버전', value: query.data?.value.detectedVersion || '—' },
                { label: '빌드 번호', value: query.data?.value.detectedBuild || '—' },
                { label: '확인 시각', value: formatDateTime(query.data?.value.detectedAt) },
              ]}
            />
          </Section>
        </>
      ) : null}
    </>
  )
}

function TestResult({ result }: { result: ConfluenceTestResult }) {
  const info = result.info
  const features = Object.entries(info?.features ?? {}).sort(([a], [b]) => a.localeCompare(b))
  return (
    <Stack gap="md" mt="md">
      <Alert
        variant="light"
        color={result.ok ? 'teal' : 'red'}
        icon={result.ok ? <IconCircleCheck size={20} /> : <IconCircleX size={20} />}
        title={result.ok ? 'Confluence 연결 정상' : 'Confluence 연결 실패'}
      >
        {result.error ? (
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
            {result.error}
          </Text>
        ) : (
          <Text size="sm">{result.note ?? '서비스 계정으로 확인한 결과입니다. 요청자 본인의 권한이 아닙니다.'}</Text>
        )}
      </Alert>

      {info ? (
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <Paper withBorder p="md" radius="md">
            <Text fw={700} mb="sm">
              서버
            </Text>
            <StatList
              items={[
                { label: '버전', value: info.version || '확인 불가' },
                { label: '빌드 번호', value: info.buildNumber || '—' },
                { label: '기본 URL', value: info.baseUrl || '—' },
              ]}
            />
          </Paper>
          <Paper withBorder p="md" radius="md">
            <Text fw={700} mb={4}>
              서비스 계정
            </Text>
            <Text size="xs" c="dimmed" mb="sm">
              요청자가 아닌 게이트웨이 서비스 계정입니다.
            </Text>
            <StatList
              items={[
                { label: '사용자명', value: info.user?.username || '—' },
                { label: '표시 이름', value: info.user?.displayName || '—' },
                { label: 'userKey', value: info.user?.userKey || '—' },
              ]}
            />
          </Paper>
        </SimpleGrid>
      ) : null}

      {features.length > 0 ? (
        <div>
          <Text fw={600} size="sm" mb="xs">
            기능 확인
          </Text>
          <Group gap="xs" wrap="wrap">
            {features.map(([name, ok]) => (
              <Badge
                key={name}
                color={ok ? 'teal' : 'red'}
                variant="light"
                leftSection={ok ? <IconCircleCheck size={14} /> : <IconCircleX size={14} />}
                style={{ textTransform: 'none' }}
              >
                {featureLabels[name] ?? name}
              </Badge>
            ))}
          </Group>
        </div>
      ) : null}

      {result.sampleSpaces ? (
        <div>
          <Text fw={600} size="sm" mb="xs">
            서비스 계정이 볼 수 있는 공간 (일부)
          </Text>
          {result.sampleSpaces.length > 0 ? (
            <Group gap="xs" wrap="wrap">
              {result.sampleSpaces.map((k) => (
                <Badge key={k} variant="outline" color="gray" style={{ textTransform: 'none' }}>
                  {k}
                </Badge>
              ))}
            </Group>
          ) : (
            <Text size="sm" c="dimmed">
              조회된 공간이 없습니다.
            </Text>
          )}
        </div>
      ) : null}

      {result.warnings && result.warnings.length > 0 ? (
        <Alert variant="light" color="yellow" icon={<IconAlertTriangle size={20} />} title="확인할 점">
          <List size="sm" spacing={4}>
            {result.warnings.map((w) => (
              <List.Item key={w}>{w}</List.Item>
            ))}
          </List>
        </Alert>
      ) : null}
    </Stack>
  )
}
