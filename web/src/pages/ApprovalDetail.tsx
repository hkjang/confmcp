import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Grid,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  Textarea,
  Title,
} from '@mantine/core'
import {
  IconAlertTriangle,
  IconArrowLeft,
  IconCheck,
  IconExternalLink,
  IconEye,
  IconInfoCircle,
  IconShieldCheck,
  IconX,
} from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import {
  CopyField,
  DiffView,
  ErrorBlock,
  JsonBlock,
  LoadingBlock,
  PageHeader,
  Section,
  StatList,
  StatusBadge,
  TextBlock,
  notifyError,
  notifyOk,
} from '../components/ui'
import { api, type ApprovalDetail } from '../lib/api'
import {
  actionLabels,
  approvalStatusColors,
  approvalStatusLabels,
  formatBytes,
  formatDateTime,
  formatRelative,
  riskColors,
  riskLabels,
} from '../lib/format'

export function ApprovalDetailPage({ scope }: { scope: 'me' | 'admin' }) {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const [note, setNote] = useState('')
  const base = scope === 'admin' ? '/api/admin/approvals' : '/api/me/approvals'
  const back = scope === 'admin' ? '/admin/approvals' : '/me/approvals'

  const detail = useQuery({
    queryKey: ['approval', id],
    queryFn: () => api.get<ApprovalDetail>(`${base}/${encodeURIComponent(id)}`),
    refetchInterval: 15_000,
  })

  const decide = useMutation({
    mutationFn: (approve: boolean) => api.post(`${base}/${encodeURIComponent(id)}/decide`, { approve, note }),
    onSuccess: (_, approve) => {
      notifyOk(approve ? '승인했습니다. AI 클라이언트에서 같은 요청을 approvalId 와 함께 다시 실행하면 적용됩니다.' : '거절했습니다.')
      void qc.invalidateQueries({ queryKey: ['approval', id] })
      void qc.invalidateQueries({ queryKey: ['my-approvals'] })
      void qc.invalidateQueries({ queryKey: ['approval-queue'] })
    },
    onError: (err) => notifyError(err, '처리하지 못했습니다'),
  })

  if (detail.isLoading) return <LoadingBlock />
  if (detail.isError) return <ErrorBlock error={detail.error} title="변경안을 불러올 수 없습니다" />
  const d = detail.data!
  const r = d.request
  const p = r.preview
  const action = p?.action ?? r.toolName.replace(/^confluence_/, '')
  const pending = r.status === 'pending'

  return (
    <>
      <PageHeader
        title={actionLabels[action] ?? r.toolName}
        description={p?.summary ?? r.resource}
        actions={
          <Button component={Link} to={back} variant="default" leftSection={<IconArrowLeft size={18} />}>
            목록으로
          </Button>
        }
      />

      <Grid gutter="lg">
        <Grid.Col span={{ base: 12, lg: 8 }}>
          {p?.warnings?.length ? (
            <Alert color="orange" icon={<IconAlertTriangle size={20} />} title="확인이 필요한 사항" mb="md">
              <Stack gap={4}>
                {p.warnings.map((w) => (
                  <Text key={w} size="sm">
                    {w}
                  </Text>
                ))}
              </Stack>
            </Alert>
          ) : null}
          {p?.visibility ? (
            <Alert color="red" variant="light" icon={<IconEye size={20} />} title="공개 범위 변화" mb="md">
              {p.visibility}
            </Alert>
          ) : null}

          <Section
            title="실제로 적용될 변경"
            description="Confluence storage 원문 기준의 줄 단위 비교입니다. 초록색 줄이 추가, 빨간색 줄이 삭제됩니다. 승인하면 이 내용 그대로 적용되며, 그 사이 문서가 바뀌면 적용되지 않습니다."
          >
            {p?.diff ? <DiffView diff={p.diff} /> : <Text c="dimmed">본문 변경이 없는 작업입니다.</Text>}
            {p?.stats ? (
              <Group gap="xs" mt="sm">
                <Badge color="teal" variant="light">
                  추가 {p.stats.added ?? 0}줄
                </Badge>
                <Badge color="red" variant="light">
                  삭제 {p.stats.removed ?? 0}줄
                </Badge>
                {p.stats.changes ? (
                  <Badge color="blue" variant="light">
                    변경 {p.stats.changes}곳
                  </Badge>
                ) : null}
              </Group>
            ) : null}
          </Section>

          {p?.newBody ? (
            <Section title="작성 내용 (Markdown)">
              <TextBlock text={p.newBody} maxHeight={360} />
            </Section>
          ) : null}

          {p?.labels?.length ? (
            <Section title="라벨">
              <Group gap="xs">
                {p.labels.map((l) => (
                  <Badge key={l} variant="outline">
                    {l}
                  </Badge>
                ))}
              </Group>
            </Section>
          ) : null}

          {p?.attachment ? (
            <Section title="첨부 파일">
              <StatList
                items={[
                  { label: '파일 이름', value: String(p.attachment.filename ?? '') },
                  { label: '크기', value: formatBytes(Number(p.attachment.size ?? 0)) },
                  { label: '형식', value: String(p.attachment.mediaType ?? '') },
                  { label: 'SHA-256', value: <Text ff="monospace" size="xs">{String(p.attachment.sha256 ?? '')}</Text> },
                  { label: '기존 첨부 덮어쓰기', value: p.attachment.overwrite ? '예' : '아니요' },
                ]}
              />
            </Section>
          ) : null}

          <Accordion variant="separated" radius="lg">
            <Accordion.Item value="args">
              <Accordion.Control icon={<IconInfoCircle size={18} />}>도구 인자 (민감 값 제외)</Accordion.Control>
              <Accordion.Panel>
                <JsonBlock value={r.arguments ?? {}} />
              </Accordion.Panel>
            </Accordion.Item>
          </Accordion>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Paper withBorder p="lg" mb="lg">
            <Group justify="space-between" mb="sm">
              <Title order={4}>승인 정보</Title>
              <StatusBadge value={r.status} labels={approvalStatusLabels} colors={approvalStatusColors} variant="filled" />
            </Group>
            <StatList
              items={[
                { label: '요청자', value: r.username },
                { label: '도구', value: <Text size="sm" ff="monospace">{r.toolName}</Text> },
                {
                  label: '위험도',
                  value: (
                    <Badge color={riskColors[r.risk] ?? 'gray'} variant="light">
                      {riskLabels[r.risk] ?? r.risk}
                    </Badge>
                  ),
                },
                { label: '공간', value: r.spaceKey || '—' },
                { label: '대상 ID', value: r.targetId || '—' },
                { label: '기준 버전', value: r.targetVersion ? `v${r.targetVersion}` : '—' },
                { label: '요청 시각', value: formatDateTime(r.createdAt) },
                { label: '만료', value: `${formatDateTime(r.expiresAt)} (${formatRelative(r.expiresAt)})` },
                { label: '정책 세대', value: r.policyGeneration },
                { label: '결정', value: r.decidedBy ? `${r.decidedBy} · ${r.decisionNote || '메모 없음'}` : '—' },
              ]}
            />
            {p?.targetUrl ? (
              <Anchor href={p.targetUrl} target="_blank" rel="noreferrer" size="sm" mt="sm" display="inline-flex">
                <Group gap={4}>
                  Confluence 에서 열기 <IconExternalLink size={14} />
                </Group>
              </Anchor>
            ) : null}
          </Paper>

          <Paper withBorder p="lg">
            <Stack gap="sm">
              {r.requiresApprover ? (
                <Alert color="grape" variant="light" icon={<IconShieldCheck size={20} />}>
                  요청자가 아닌 승인자의 승인이 필요한 작업입니다.
                </Alert>
              ) : null}
              {pending ? (
                <>
                  <Textarea
                    label="결정 메모"
                    placeholder="선택 입력"
                    autosize
                    minRows={2}
                    value={note}
                    onChange={(e) => setNote(e.currentTarget.value)}
                  />
                  <SimpleGrid cols={2}>
                    <Button
                      color="red"
                      variant="light"
                      leftSection={<IconX size={18} />}
                      disabled={!d.canReject}
                      loading={decide.isPending && decide.variables === false}
                      onClick={() => decide.mutate(false)}
                    >
                      거절
                    </Button>
                    <Button
                      color="teal"
                      leftSection={<IconCheck size={18} />}
                      disabled={!d.canApprove}
                      loading={decide.isPending && decide.variables === true}
                      onClick={() => decide.mutate(true)}
                      data-testid="approve-button"
                    >
                      승인
                    </Button>
                  </SimpleGrid>
                  {!d.canApprove ? (
                    <Text size="xs" c="dimmed">
                      {d.own ? '본인 승인이 허용되지 않는 작업입니다. 승인자에게 요청하십시오.' : '승인 권한이 없습니다.'}
                    </Text>
                  ) : null}
                </>
              ) : (
                <Text size="sm" c="dimmed">
                  이미 처리된 변경안입니다.
                </Text>
              )}
              {r.status === 'approved' ? (
                <Alert color="teal" variant="light" title="다음 단계">
                  <Text size="sm" mb={6}>
                    AI 클라이언트에서 같은 도구를 아래 approvalId 와 함께 호출하면 적용됩니다. 응답의 nextCall 을 그대로 쓰면 됩니다.
                  </Text>
                  <CopyField value={r.id} label="approvalId 복사" />
                </Alert>
              ) : null}
            </Stack>
          </Paper>
        </Grid.Col>
      </Grid>
    </>
  )
}
