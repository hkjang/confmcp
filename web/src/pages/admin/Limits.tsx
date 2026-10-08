import { Alert, Button } from '@mantine/core'
import { IconInfoCircle, IconRestore } from '@tabler/icons-react'

import { SettingsForm, type Field } from '../../components/SettingsForm'
import { ErrorBlock, LoadingBlock, PageHeader, SaveBar, Section } from '../../components/ui'
import { formatNumber } from '../../lib/format'
import { useSettingsGroup } from '../../lib/useSettingsGroup'

interface LimitsSettings {
  listDefault: number
  listMax: number
  bodyChars: number
  contextDocs: number
  contextChars: number
  treeDepth: number
  treeNodes: number
  searchExtraPages: number
  uploadMaxMb: number
  uploadTtlMinutes: number
  proposalTtlHours: number
}

type LimitKey = keyof LimitsSettings

interface LimitSpec {
  key: LimitKey
  label: string
  description: string
  min: number
  max: number
  unit?: string
}

const groups: { title: string; description: string; items: LimitSpec[] }[] = [
  {
    title: '목록과 검색',
    description: '도구가 한 번에 돌려주는 항목 수입니다.',
    items: [
      { key: 'listDefault', label: '기본 목록 크기', description: '요청에 개수가 없을 때 돌려주는 항목 수', min: 1, max: 500, unit: '개' },
      { key: 'listMax', label: '최대 목록 크기', description: '요청이 더 많이 원해도 이 수를 넘지 않습니다', min: 1, max: 500, unit: '개' },
      {
        key: 'searchExtraPages',
        label: '검색 추가 조회 페이지',
        description: '정책·권한으로 걸러진 만큼 더 읽어 올 최대 페이지 수 (0–10)',
        min: 0,
        max: 10,
        unit: '페이지',
      },
    ],
  },
  {
    title: '본문과 컨텍스트',
    description: 'AI 에 전달하는 문서 분량을 제한합니다.',
    items: [
      { key: 'bodyChars', label: '본문 최대 글자 수', description: '페이지 본문 하나를 읽을 때 잘라내는 길이', min: 500, max: 2_000_000, unit: '자' },
      { key: 'contextDocs', label: '컨텍스트 최대 문서 수', description: '컨텍스트 묶음 도구가 모으는 문서 수 (1–50)', min: 1, max: 50, unit: '개' },
      { key: 'contextChars', label: '컨텍스트 최대 글자 수', description: '컨텍스트 묶음 전체 길이', min: 1000, max: 2_000_000, unit: '자' },
    ],
  },
  {
    title: '페이지 트리',
    description: '하위 페이지 트리 조회 범위입니다.',
    items: [
      { key: 'treeDepth', label: '트리 최대 깊이', description: '내려가는 단계 수 (1–20)', min: 1, max: 20, unit: '단계' },
      { key: 'treeNodes', label: '트리 최대 노드 수', description: '한 번에 돌려주는 페이지 수 (1–2000)', min: 1, max: 2000, unit: '개' },
    ],
  },
  {
    title: '첨부와 변경 제안',
    description: '업로드 파일과 쓰기 제안의 크기·보존 기간입니다.',
    items: [
      { key: 'uploadMaxMb', label: '업로드 최대 크기', description: '첨부 파일 하나의 최대 크기 (MB, 1–512)', min: 1, max: 512, unit: 'MB' },
      {
        key: 'uploadTtlMinutes',
        label: '업로드 보관 시간',
        description: '올린 파일을 첨부로 쓰기 전까지 보관하는 시간 (분, 5–1440)',
        min: 5,
        max: 1440,
        unit: '분',
      },
      {
        key: 'proposalTtlHours',
        label: '변경 제안 보관 시간',
        description: '준비한 변경 제안이 유효한 시간 (시간, 1–720)',
        min: 1,
        max: 720,
        unit: '시간',
      },
    ],
  },
]

function fieldsFor(items: LimitSpec[], defaults?: LimitsSettings): Field<LimitsSettings>[] {
  return items.map((it) => ({
    kind: 'number',
    key: it.key,
    label: it.label,
    description: defaults
      ? `${it.description} · 기본값 ${formatNumber(defaults[it.key])}${it.unit ? ` ${it.unit}` : ''}`
      : it.description,
    min: it.min,
    max: it.max,
    span: 4,
  }))
}

export function AdminLimitsPage() {
  const { query, draft, setDraft, secrets, setSecrets, save } = useSettingsGroup<LimitsSettings>('limits')
  const defaults = query.data?.defaults

  return (
    <>
      <PageHeader
        title="처리 한도"
        description="도구 호출 하나가 읽거나 돌려줄 수 있는 양을 제한합니다. 범위를 벗어난 값은 저장할 때 안전한 범위로 조정됩니다."
      />

      {query.isLoading ? <LoadingBlock /> : null}
      {query.error ? <ErrorBlock error={query.error} /> : null}

      {draft ? (
        <>
          <Alert variant="light" color="blue" icon={<IconInfoCircle size={20} />} mb="lg">
            한도를 크게 올리면 AI 클라이언트로 전달되는 응답이 커지고 Confluence 부하가 늘어납니다. 기본값에서 시작해 필요한
            만큼만 조정하십시오.
          </Alert>

          {groups.map((g) => (
            <Section key={g.title} title={g.title} description={g.description}>
              <SettingsForm
                fields={fieldsFor(g.items, defaults)}
                value={draft}
                onChange={setDraft}
                secrets={secrets}
                onSecretChange={setSecrets}
              />
            </Section>
          ))}

          <SaveBar
            onSave={() => save.mutate()}
            saving={save.isPending}
            extra={
              defaults ? (
                <Button
                  variant="default"
                  leftSection={<IconRestore size={18} />}
                  onClick={() => setDraft({ ...defaults })}
                >
                  기본값으로 되돌리기
                </Button>
              ) : null
            }
          />
        </>
      ) : null}
    </>
  )
}
