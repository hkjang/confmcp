import { Alert, Button, Stack, Text } from '@mantine/core'
import { IconPlugConnected } from '@tabler/icons-react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import { ErrorBlock, LoadingBlock, PageHeader, SaveBar, Section, TextBlock } from '../../components/ui'
import { formatNumber } from '../../lib/format'
import { useConnectivityTest, useSettingsGroup } from '../../lib/useSettingsGroup'

interface AISettings {
  enabled: boolean
  provider: string
  baseUrl: string
  model: string
  models: string[] | null
  maxTokens: number
  temperature: number
  topP: number
  streaming: boolean
  systemPrompt: string
  timeoutSec: number
  contextLimit: number
}

const DEFAULT_CEILING = 524288

export function AdminAiPage() {
  const { query, draft, setDraft, secrets, setSecrets, secretPresence, limits, save } =
    useSettingsGroup<AISettings>('ai')
  const test = useConnectivityTest('ai')
  const ceiling = limits.maxTokenCeiling ?? DEFAULT_CEILING
  const ceilingText = `최대 ${formatNumber(ceiling)}(${Math.round(ceiling / 1024)}k) 토큰`

  const fields: Field<AISettings>[] = [
    {
      kind: 'switch',
      key: 'enabled',
      label: 'AI 기능 사용',
      description: '끄면 콘솔의 AI 도우미와 AI 호출 스코프가 모두 비활성화됩니다.',
    },
    {
      kind: 'switch',
      key: 'streaming',
      label: '스트리밍',
      description: '기본으로 스트리밍합니다. 응답은 SSE 로 중계합니다.',
    },
    {
      kind: 'select',
      key: 'provider',
      label: '제공자',
      options: [
        { value: 'anthropic', label: 'Anthropic Messages API' },
        { value: 'openai-compatible', label: 'OpenAI 호환 (vLLM, Ollama, 사내 게이트웨이 등)' },
      ],
    },
    {
      kind: 'text',
      key: 'baseUrl',
      label: '기본 URL',
      description: '오프라인망에서는 사내 추론 게이트웨이 주소를 입력하십시오.',
      placeholder: 'https://llm-gateway.example.internal',
    },
    { kind: 'secret', secretKey: 'apiKey', label: 'API 키' },
    {
      kind: 'text',
      key: 'model',
      label: '기본 모델',
      description: '사용자가 모델을 고르지 않을 때 쓰는 모델입니다.',
      placeholder: 'claude-sonnet-5-5',
    },
    {
      kind: 'tags',
      key: 'models',
      label: '모델 목록',
      description: '사용자에게 보여 줄 모델 목록입니다. 입력 후 Enter 로 추가합니다.',
      placeholder: '모델 이름 입력',
      span: 12,
    },
    {
      kind: 'number',
      key: 'maxTokens',
      label: '기본 최대 출력 토큰',
      description: ceilingText,
      min: 1,
      max: ceiling,
      step: 1024,
    },
    {
      kind: 'number',
      key: 'contextLimit',
      label: '사용자 요청 토큰 상한',
      description: `사용자가 요청할 수 있는 최대 출력 토큰입니다. ${ceilingText}`,
      min: 1,
      max: ceiling,
      step: 1024,
    },
    {
      kind: 'number',
      key: 'temperature',
      label: 'temperature',
      description: '0–2',
      min: 0,
      max: 2,
      step: 0.1,
      decimal: true,
    },
    {
      kind: 'number',
      key: 'topP',
      label: 'top_p',
      description: '0–1',
      min: 0,
      max: 1,
      step: 0.05,
      decimal: true,
    },
    {
      kind: 'number',
      key: 'timeoutSec',
      label: '응답 제한 시간 (초)',
      description: '긴 출력을 고려해 넉넉하게 두십시오.',
      min: 10,
      max: 3600,
    },
    {
      kind: 'textarea',
      key: 'systemPrompt',
      label: '시스템 프롬프트',
      description: '문서 내용을 신뢰할 수 없는 데이터로 다루도록 지시하는 문장을 유지하십시오.',
      minRows: 4,
      span: 12,
    },
  ]

  const result = test.data
  const ok = result?.ok === true
  const reply = typeof result?.reply === 'string' ? result.reply : ''
  const error = typeof result?.error === 'string' ? result.error : ''

  return (
    <>
      <PageHeader
        title="AI 설정"
        description="콘솔 AI 도우미가 쓰는 모델과 파라미터입니다. 최대 출력 토큰은 512k 까지 설정할 수 있습니다."
        actions={
          <Button
            variant="default"
            leftSection={<IconPlugConnected size={18} />}
            loading={test.isPending}
            onClick={() => test.mutate(undefined)}
          >
            연결 시험
          </Button>
        }
      />

      {test.error ? <ErrorBlock error={test.error} title="연결 시험 실패" /> : null}
      {result ? (
        <Alert color={ok ? 'teal' : 'red'} variant="light" mb="lg" title={ok ? '연결 성공' : '연결 실패'}>
          {ok ? (
            <Stack gap="xs">
              <Text size="sm">모델 응답:</Text>
              <TextBlock text={reply || '(빈 응답)'} maxHeight={200} />
            </Stack>
          ) : (
            <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
              {error || '알 수 없는 오류'}
            </Text>
          )}
        </Alert>
      ) : null}

      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}

      {draft ? (
        <Section title="모델 · 파라미터" description="저장한 뒤 연결 시험을 누르면 저장된 설정으로 짧은 요청을 보냅니다.">
          <SettingsForm
            fields={fields}
            value={draft}
            onChange={setDraft}
            secrets={secrets}
            onSecretChange={setSecrets}
            secretPresence={secretPresence}
          />
          <SaveBar onSave={() => save.mutate()} saving={save.isPending} />
        </Section>
      ) : null}

      <Section title="오프라인망 참고">
        <Text size="sm">
          외부 인터넷이 없는 환경에서는 사내 추론 게이트웨이를 OpenAI 호환 모드로 연결하십시오. Anthropic Messages
          형식(/v1/messages)과 OpenAI 호환 형식(/v1/chat/completions)을 모두 지원하며, 어느 쪽이든 응답은 서버-전송
          이벤트(SSE)로 브라우저에 중계합니다.
        </Text>
      </Section>
    </>
  )
}
