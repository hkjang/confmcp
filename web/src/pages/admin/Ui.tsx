import { Alert, Badge, Box, Button, Grid, Group, Paper, Stack, Text, Title } from '@mantine/core'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import { ErrorBlock, LoadingBlock, PageHeader, SaveBar, Section } from '../../components/ui'
import type { UISettings } from '../../lib/api'
import { useSettingsGroup } from '../../lib/useSettingsGroup'

const colorOptions = [
  { value: 'confmcp', label: 'confmcp (기본 남색)' },
  { value: 'blue', label: '파랑 (blue)' },
  { value: 'teal', label: '청록 (teal)' },
  { value: 'violet', label: '보라 (violet)' },
  { value: 'grape', label: '자주 (grape)' },
  { value: 'cyan', label: '하늘 (cyan)' },
  { value: 'green', label: '초록 (green)' },
  { value: 'orange', label: '주황 (orange)' },
]

const themeLabels: Record<string, string> = { light: '밝게', dark: '어둡게', system: '시스템 설정 따름' }

const fields: Field<UISettings>[] = [
  { kind: 'text', key: 'serviceName', label: '서비스 이름', description: '머리글과 브라우저 제목에 표시됩니다.' },
  { kind: 'text', key: 'tagline', label: '부제', description: '로그인 화면과 머리글에 표시됩니다.' },
  { kind: 'select', key: 'primaryColor', label: '대표 색상', options: colorOptions },
  {
    kind: 'number',
    key: 'fontScale',
    label: '글자 크기 배율',
    description: '0.85 – 1.4. 사용자가 개인 설정에서 따로 바꿀 수 있습니다.',
    min: 0.85,
    max: 1.4,
    step: 0.05,
    decimal: true,
  },
  {
    kind: 'select',
    key: 'defaultTheme',
    label: '기본 테마',
    options: [
      { value: 'light', label: '밝게' },
      { value: 'dark', label: '어둡게' },
      { value: 'system', label: '시스템 설정 따름' },
    ],
  },
  { kind: 'select', key: 'locale', label: '언어', options: [{ value: 'ko', label: '한국어' }] },
  {
    kind: 'textarea',
    key: 'loginNotice',
    label: '로그인 안내문',
    description: '로그인 화면 아래에 표시할 안내(예: 사용 목적, 문의처)입니다. 텍스트로만 표시됩니다.',
    minRows: 3,
    span: 12,
  },
]

function Preview({ value }: { value: UISettings }) {
  const color = colorOptions.some((o) => o.value === value.primaryColor) ? value.primaryColor : 'confmcp'
  const scale = Number.isFinite(value.fontScale) && value.fontScale >= 0.85 && value.fontScale <= 1.4 ? value.fontScale : 1
  const dark = value.defaultTheme === 'dark'
  return (
    <Paper
      withBorder
      p="lg"
      style={{
        fontSize: `calc(1rem * ${scale})`,
        background: dark ? 'var(--mantine-color-dark-7)' : 'var(--mantine-color-white)',
        color: dark ? 'var(--mantine-color-dark-0)' : 'var(--mantine-color-dark-9)',
      }}
    >
      <Group gap="sm" wrap="nowrap" mb="sm">
        <Box
          w={36}
          h={36}
          style={{
            borderRadius: 8,
            flexShrink: 0,
            background: `var(--mantine-color-${color}-filled)`,
          }}
        />
        <Box style={{ minWidth: 0 }}>
          <Title order={4} style={{ fontSize: '1.25em', overflowWrap: 'anywhere' }}>
            {value.serviceName || 'confmcp'}
          </Title>
          <Text style={{ fontSize: '0.875em', opacity: 0.7, overflowWrap: 'anywhere' }}>{value.tagline || ' '}</Text>
        </Box>
      </Group>
      <Text style={{ fontSize: '1em' }} mb="sm">
        본문 글자는 이 크기로 표시됩니다. 표와 설명 문구도 같은 배율을 따릅니다.
      </Text>
      <Group gap="xs" mb={value.loginNotice ? 'sm' : 0} wrap="wrap">
        <Button color={color} size="xs" style={{ fontSize: '0.875em' }}>
          기본 버튼
        </Button>
        <Button color={color} variant="light" size="xs" style={{ fontSize: '0.875em' }}>
          보조 버튼
        </Button>
        <Badge color={color} variant="light">
          {themeLabels[value.defaultTheme] ?? value.defaultTheme}
        </Badge>
      </Group>
      {value.loginNotice ? (
        <Alert color={color} variant="light" p="xs">
          <Text style={{ fontSize: '0.875em', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
            {value.loginNotice}
          </Text>
        </Alert>
      ) : null}
    </Paper>
  )
}

export function AdminUiPage() {
  const { query, draft, setDraft, secrets, setSecrets, save } = useSettingsGroup<UISettings>('ui')

  return (
    <>
      <PageHeader
        title="화면 설정"
        description="서비스 이름, 대표 색상, 글자 크기, 기본 테마처럼 모든 사용자에게 적용되는 표시 설정입니다."
      />
      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}
      {draft ? (
        <Grid gutter="lg">
          <Grid.Col span={{ base: 12, lg: 8 }}>
            <Section title="표시 설정">
              <SettingsForm
                fields={fields}
                value={draft}
                onChange={setDraft}
                secrets={secrets}
                onSecretChange={setSecrets}
              />
              <SaveBar onSave={() => save.mutate()} saving={save.isPending} />
            </Section>
          </Grid.Col>
          <Grid.Col span={{ base: 12, lg: 4 }}>
            <Section title="미리 보기" description="저장 전 값으로 그린 예시입니다.">
              <Stack>
                <Preview value={draft} />
              </Stack>
            </Section>
          </Grid.Col>
        </Grid>
      ) : null}
    </>
  )
}
