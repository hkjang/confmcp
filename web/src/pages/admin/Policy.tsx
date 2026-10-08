import {
  Alert,
  Badge,
  Button,
  Group,
  List,
  Modal,
  NumberInput,
  Paper,
  SegmentedControl,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import {
  IconCircleCheck,
  IconCircleX,
  IconInfoCircle,
  IconPencil,
  IconPlus,
  IconScale,
} from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import {
  ConfirmButton,
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  Section,
  StatList,
  TableScroll,
  notifyError,
  notifyOk,
} from '../../components/ui'
import { api, type PermissionDecision, type PolicyRule, type PolicyVerdict } from '../../lib/api'
import {
  contentTypeLabels,
  formatDateTime,
  opLabels,
  opOrder,
  policyKindLabels,
  riskColors,
  riskLabels,
  verdictColors,
  verdictLabels,
} from '../../lib/format'

interface RulesResponse {
  rules: PolicyRule[]
  generation: number
}

interface EvaluateResponse {
  policy: PolicyVerdict
  target: { spaceKey: string; contentId?: string; ancestors?: string[]; contentType?: string }
  permission?: PermissionDecision
  permissionError?: string
}

interface RuleForm {
  kind: string
  pattern: string
  spaceKey: string
  effect: string
  riskCap: string // 'none' | READ | WRITE | EXECUTE
  priority: number
  note: string
}

const kindOptions = Object.entries(policyKindLabels).map(([value, label]) => ({ value, label: `${label} (${value})` }))

const contentTypeOptions = [
  ...Object.entries(contentTypeLabels).map(([value, label]) => ({ value, label: `${label} (${value})` })),
  { value: '*', label: '모든 유형 (*)' },
]

const riskCapOptions = [
  { value: 'none', label: '제한 없음' },
  { value: 'READ', label: '조회만 (READ)' },
  { value: 'WRITE', label: '작성까지 (WRITE)' },
  { value: 'EXECUTE', label: '고위험까지 (EXECUTE)' },
]

const evalTypeOptions = [
  { value: 'any', label: '지정 안 함' },
  ...Object.entries(contentTypeLabels).map(([value, label]) => ({ value, label })),
]

const emptyForm: RuleForm = {
  kind: 'space',
  pattern: '',
  spaceKey: '',
  effect: 'allow',
  riskCap: 'none',
  priority: 100,
  note: '',
}

function toForm(r: PolicyRule): RuleForm {
  const cap = (r.riskCap ?? '').toUpperCase()
  return {
    kind: r.kind in policyKindLabels ? r.kind : 'space',
    pattern: r.pattern,
    spaceKey: r.spaceKey ?? '',
    effect: r.effect === 'deny' ? 'deny' : 'allow',
    riskCap: riskCapOptions.some((o) => o.value === cap) && cap !== '' ? cap : 'none',
    priority: r.priority,
    note: r.note ?? '',
  }
}

export function AdminPolicyPage() {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<{ id: number | null; form: RuleForm } | null>(null)

  const rules = useQuery({
    queryKey: ['admin', 'policy-rules'],
    queryFn: () => api.get<RulesResponse>('/api/admin/policy/rules'),
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['admin', 'policy-rules'] })
    void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
  }

  const saveRule = useMutation({
    mutationFn: ({ id, form }: { id: number | null; form: RuleForm }) => {
      const body = {
        kind: form.kind,
        pattern: form.pattern.trim(),
        spaceKey: form.kind === 'page_tree' ? form.spaceKey.trim().toUpperCase() : '',
        effect: form.effect,
        riskCap: form.riskCap === 'none' ? '' : form.riskCap,
        priority: form.priority,
        note: form.note.trim(),
      }
      return id === null
        ? api.post<PolicyRule>('/api/admin/policy/rules', body)
        : api.put<PolicyRule>(`/api/admin/policy/rules/${id}`, body)
    },
    onSuccess: (_, v) => {
      notifyOk(v.id === null ? '규칙을 추가했습니다.' : '규칙을 수정했습니다.')
      setEditing(null)
      invalidate()
    },
    onError: (err) => notifyError(err, '저장 실패'),
  })

  const deleteRule = useMutation({
    mutationFn: (id: number) => api.del(`/api/admin/policy/rules/${id}`),
    onSuccess: () => {
      notifyOk('규칙을 삭제했습니다.')
      invalidate()
    },
    onError: (err) => notifyError(err, '삭제 실패'),
  })

  return (
    <>
      <PageHeader
        title="접근 정책"
        description="MCP 가 접근할 수 있는 공간·페이지 트리·콘텐츠 유형을 제한합니다. 정책은 권한을 줄이기만 하며, Confluence 권한을 넘어서는 접근을 허용하지 않습니다."
        actions={
          <Button leftSection={<IconPlus size={18} />} onClick={() => setEditing({ id: null, form: { ...emptyForm } })}>
            규칙 추가
          </Button>
        }
      />

      {rules.data ? (
        <Alert variant="light" color="grape" icon={<IconScale size={20} />} mb="md">
          <Group gap="xs" wrap="wrap">
            <Badge color="grape" variant="filled">
              정책 세대 {rules.data.generation}
            </Badge>
            <Text size="sm">변경하면 대기 중 승인과 검색 커서가 무효가 됩니다.</Text>
          </Group>
        </Alert>
      ) : null}

      <Alert variant="light" color="blue" icon={<IconInfoCircle size={20} />} mb="lg" title="규칙이 적용되는 방식">
        <List size="sm" spacing={4}>
          <List.Item>
            <b>공간</b>: 허용 규칙에 맞는 공간만 접근할 수 있습니다(명시적 허용). 차단 규칙이 맞으면 항상 차단이 우선합니다.
          </List.Item>
          <List.Item>
            <b>페이지 트리</b>: 차단은 그 페이지와 모든 하위 페이지를 막습니다. 공간을 지정한 허용 규칙이 있으면 그 공간은 허용된
            트리 안에서만 접근할 수 있습니다.
          </List.Item>
          <List.Item>
            <b>콘텐츠 유형</b>: 차단된 유형은 막습니다. 허용 규칙이 하나라도 있으면 허용된 유형만 접근할 수 있습니다.
          </List.Item>
          <List.Item>
            <b>라벨</b>은 일반 편집자도 바꿀 수 있어 보안 경계로 쓰지 않습니다.
          </List.Item>
          <List.Item>위험도 상한은 맞는 규칙 중 가장 낮은 값이 적용됩니다. 우선순위는 숫자가 작을수록 먼저 평가합니다.</List.Item>
        </List>
      </Alert>

      <Section title="규칙">
        {rules.isLoading ? <LoadingBlock /> : null}
        {rules.error ? <ErrorBlock error={rules.error} /> : null}
        {rules.data && rules.data.rules.length === 0 ? (
          <EmptyState label="규칙이 없습니다. 공간 허용 규칙이 없으면 MCP 는 어떤 공간에도 접근할 수 없습니다." />
        ) : null}
        {rules.data && rules.data.rules.length > 0 ? (
          <TableScroll minWidth={900}>
            <Table striped highlightOnHover verticalSpacing="sm" fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>종류</Table.Th>
                  <Table.Th>대상</Table.Th>
                  <Table.Th>공간</Table.Th>
                  <Table.Th>효과</Table.Th>
                  <Table.Th>위험도 상한</Table.Th>
                  <Table.Th ta="right">우선순위</Table.Th>
                  <Table.Th>메모</Table.Th>
                  <Table.Th>작업</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rules.data.rules.map((r) => (
                  <Table.Tr key={r.id}>
                    <Table.Td style={{ whiteSpace: 'nowrap' }}>{policyKindLabels[r.kind] ?? r.kind}</Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace" fw={600}>
                        {r.pattern}
                      </Text>
                      {r.kind === 'content_type' && contentTypeLabels[r.pattern] ? (
                        <Text size="xs" c="dimmed">
                          {contentTypeLabels[r.pattern]}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>{r.kind === 'page_tree' ? r.spaceKey || '모든 공간' : '—'}</Table.Td>
                    <Table.Td>
                      <Badge color={r.effect === 'allow' ? 'teal' : 'red'} variant="light">
                        {r.effect === 'allow' ? '허용' : '차단'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      {r.riskCap ? (
                        <Badge color={riskColors[r.riskCap] ?? 'gray'} variant="outline">
                          {riskLabels[r.riskCap] ?? r.riskCap}까지
                        </Badge>
                      ) : (
                        <Text size="sm" c="dimmed">
                          —
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td ta="right">{r.priority}</Table.Td>
                    <Table.Td>
                      <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                        {r.note || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap">
                        <Button
                          size="xs"
                          variant="light"
                          leftSection={<IconPencil size={14} />}
                          onClick={() => setEditing({ id: r.id, form: toForm(r) })}
                        >
                          수정
                        </Button>
                        <ConfirmButton
                          size="xs"
                          color="red"
                          title="규칙 삭제"
                          message={`${policyKindLabels[r.kind] ?? r.kind} 규칙 "${r.pattern}" 을(를) 삭제합니다. 정책 세대가 올라가 대기 중 승인이 무효가 됩니다.`}
                          confirmLabel="삭제"
                          loading={deleteRule.isPending && deleteRule.variables === r.id}
                          onConfirm={() => deleteRule.mutate(r.id)}
                        >
                          삭제
                        </ConfirmButton>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}
      </Section>

      <EvaluatePanel />

      <RuleModal
        editing={editing}
        onChange={(form) => setEditing((cur) => (cur ? { ...cur, form } : cur))}
        onClose={() => setEditing(null)}
        saving={saveRule.isPending}
        onSave={() => {
          if (editing) saveRule.mutate(editing)
        }}
      />
    </>
  )
}

function RuleModal({
  editing,
  onChange,
  onClose,
  onSave,
  saving,
}: {
  editing: { id: number | null; form: RuleForm } | null
  onChange: (form: RuleForm) => void
  onClose: () => void
  onSave: () => void
  saving: boolean
}) {
  const form = editing?.form ?? emptyForm
  const set = (patch: Partial<RuleForm>) => onChange({ ...form, ...patch })

  const changeKind = (kind: string) => {
    if (kind === form.kind) return
    if (kind === 'content_type') set({ kind, pattern: 'page', spaceKey: '' })
    else set({ kind, pattern: '', spaceKey: '' })
  }

  const patternValid =
    form.pattern.trim() !== '' && (form.kind !== 'page_tree' || /^\d+$/.test(form.pattern.trim()))
  const contentTypeValue = contentTypeOptions.some((o) => o.value === form.pattern) ? form.pattern : null

  return (
    <Modal
      opened={editing !== null}
      onClose={onClose}
      title={<Text fw={700}>{editing?.id === null ? '규칙 추가' : '규칙 수정'}</Text>}
      size="lg"
    >
      <Stack>
        <Select
          label="종류"
          data={kindOptions}
          value={form.kind}
          onChange={(v) => changeKind(v ?? 'space')}
        />

        {form.kind === 'content_type' ? (
          <Select
            label="콘텐츠 유형"
            data={contentTypeOptions}
            value={contentTypeValue}
            onChange={(v) => set({ pattern: v ?? 'page' })}
          />
        ) : form.kind === 'page_tree' ? (
          <TextInput
            label="페이지 ID"
            description="트리의 최상위 페이지 ID(숫자)입니다. 그 페이지와 모든 하위 페이지에 적용됩니다."
            placeholder="123456"
            inputMode="numeric"
            value={form.pattern}
            error={form.pattern.trim() !== '' && !patternValid ? '숫자 페이지 ID 를 입력하십시오' : undefined}
            onChange={(e) => set({ pattern: e.currentTarget.value })}
          />
        ) : (
          <TextInput
            label="공간 키"
            description="와일드카드를 쓸 수 있습니다. 예: DEV, DEV*, *"
            placeholder="DEV"
            value={form.pattern}
            onChange={(e) => set({ pattern: e.currentTarget.value })}
          />
        )}

        {form.kind === 'page_tree' ? (
          <TextInput
            label="공간 키 (선택)"
            description="허용 규칙에 공간을 지정하면 그 공간은 허용된 트리 안에서만 접근할 수 있습니다."
            placeholder="DEV"
            value={form.spaceKey}
            onChange={(e) => set({ spaceKey: e.currentTarget.value })}
          />
        ) : null}

        <Stack gap={4}>
          <Text size="sm" fw={500}>
            효과
          </Text>
          <SegmentedControl
            fullWidth
            value={form.effect}
            onChange={(v) => set({ effect: v === 'deny' ? 'deny' : 'allow' })}
            data={[
              { value: 'allow', label: '허용' },
              { value: 'deny', label: '차단' },
            ]}
          />
        </Stack>

        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <Select
            label="위험도 상한"
            description="허용하되 이 위험도까지만 쓸 수 있게 합니다."
            data={riskCapOptions}
            value={form.riskCap}
            onChange={(v) => set({ riskCap: v ?? 'none' })}
          />
          <NumberInput
            label="우선순위"
            description="숫자가 작을수록 먼저 평가합니다."
            min={0}
            max={100000}
            allowDecimal={false}
            value={form.priority}
            onChange={(v) => set({ priority: typeof v === 'number' ? v : Number(v) || 0 })}
          />
        </SimpleGrid>

        <TextInput
          label="메모"
          placeholder="규칙을 둔 이유"
          value={form.note}
          onChange={(e) => set({ note: e.currentTarget.value })}
        />

        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            취소
          </Button>
          <Button loading={saving} disabled={!patternValid} onClick={onSave}>
            {editing?.id === null ? '추가' : '저장'}
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

function EvaluatePanel() {
  const [spaceKey, setSpaceKey] = useState('')
  const [contentId, setContentId] = useState('')
  const [contentType, setContentType] = useState('any')
  const [username, setUsername] = useState('')

  const evaluate = useMutation({
    mutationFn: () =>
      api.post<EvaluateResponse>('/api/admin/policy/evaluate', {
        spaceKey: spaceKey.trim().toUpperCase(),
        contentId: contentId.trim(),
        contentType: contentType === 'any' ? '' : contentType,
        username: username.trim(),
      }),
    onError: (err) => notifyError(err, '판정 실패'),
  })

  const canRun = spaceKey.trim() !== '' || contentId.trim() !== ''
  const res = evaluate.data

  return (
    <Section
      title="판정 미리보기"
      description="공간이나 콘텐츠에 대해 정책이 어떻게 판정하는지 확인합니다. 콘텐츠 ID 를 넣으면 실제 공간과 상위 페이지를 Confluence 에서 읽어 판정합니다. 사용자명을 넣으면 그 사용자의 Confluence 권한도 함께 확인합니다."
    >
      <SimpleGrid cols={{ base: 1, sm: 2, md: 4 }} spacing="md">
        <TextInput label="공간 키" placeholder="DEV" value={spaceKey} onChange={(e) => setSpaceKey(e.currentTarget.value)} />
        <TextInput
          label="콘텐츠 ID"
          placeholder="123456"
          inputMode="numeric"
          value={contentId}
          onChange={(e) => setContentId(e.currentTarget.value)}
        />
        <Select
          label="콘텐츠 유형"
          data={evalTypeOptions}
          value={contentType}
          onChange={(v) => setContentType(v ?? 'any')}
        />
        <TextInput
          label="사용자명 (선택)"
          placeholder="alice"
          value={username}
          onChange={(e) => setUsername(e.currentTarget.value)}
        />
      </SimpleGrid>
      <Group justify="flex-end" mt="md">
        <Button
          leftSection={<IconScale size={18} />}
          loading={evaluate.isPending}
          disabled={!canRun}
          onClick={() => evaluate.mutate()}
        >
          판정 미리보기
        </Button>
      </Group>

      {res ? (
        <Stack gap="md" mt="md">
          <Alert
            variant="light"
            color={res.policy.allowed ? 'teal' : 'red'}
            icon={res.policy.allowed ? <IconCircleCheck size={20} /> : <IconCircleX size={20} />}
            title={res.policy.allowed ? '정책: 허용' : '정책: 차단'}
          >
            <StatList
              items={[
                { label: '사유', value: res.policy.reason || (res.policy.allowed ? '허용 규칙에 해당합니다' : '—') },
                { label: '일치한 규칙', value: res.policy.matchedBy || '—' },
                {
                  label: '위험도 상한',
                  value: res.policy.riskCap ? `${riskLabels[res.policy.riskCap] ?? res.policy.riskCap} (${res.policy.riskCap})` : '제한 없음',
                },
                {
                  label: '판정 대상',
                  value: [
                    res.target.spaceKey ? `공간 ${res.target.spaceKey}` : null,
                    res.target.contentId ? `콘텐츠 ${res.target.contentId}` : null,
                    res.target.contentType ? (contentTypeLabels[res.target.contentType] ?? res.target.contentType) : null,
                    res.target.ancestors && res.target.ancestors.length > 0
                      ? `상위 ${res.target.ancestors.join(' › ')}`
                      : null,
                  ]
                    .filter(Boolean)
                    .join(' · ') || '—',
                },
                { label: '정책 세대', value: String(res.policy.generation) },
              ]}
            />
          </Alert>

          {res.permissionError ? (
            <Alert variant="light" color="orange" title="Confluence 권한을 확인하지 못했습니다">
              <Text size="sm" style={{ overflowWrap: 'anywhere' }}>
                {res.permissionError}
              </Text>
            </Alert>
          ) : null}

          {res.permission ? <PermissionTable decision={res.permission} /> : null}
        </Stack>
      ) : null}
    </Section>
  )
}

function PermissionTable({ decision }: { decision: PermissionDecision }) {
  const ops = [
    ...opOrder.filter((op) => op in decision.results),
    ...Object.keys(decision.results).filter((op) => !opOrder.includes(op)),
  ]
  return (
    <Paper withBorder p="md" radius="md">
      <Group justify="space-between" mb="sm" wrap="wrap" gap="xs">
        <Text fw={700}>사용자의 Confluence 권한</Text>
        <Text size="sm" c="dimmed">
          {decision.source === 'plugin' ? '권한 플러그인' : decision.source} · {formatDateTime(decision.evaluatedAt)}
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
              const v = decision.results[op]
              return (
                <Table.Tr key={op}>
                  <Table.Td>{opLabels[op] ?? op}</Table.Td>
                  <Table.Td>
                    <Badge color={verdictColors[v] ?? 'gray'} variant="light">
                      {verdictLabels[v] ?? v}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" c={decision.reasons?.[op] ? undefined : 'dimmed'}>
                      {decision.reasons?.[op] ?? '—'}
                    </Text>
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
