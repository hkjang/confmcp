import {
  Alert,
  Anchor,
  Button,
  Group,
  Modal,
  SegmentedControl,
  Select,
  Stack,
  Table,
  Text,
  Textarea,
} from '@mantine/core'
import { IconAlertTriangle, IconRefresh } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import {
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  Section,
  StatList,
  StatusBadge,
  TableScroll,
  notifyError,
  notifyOk,
} from '../../components/ui'
import { api, qs, type OperationRecord } from '../../lib/api'
import {
  actionLabels,
  errorCodeLabels,
  formatDateTime,
  operationStatusColors,
  operationStatusLabels,
} from '../../lib/format'

const statusData = [
  { value: 'all', label: '전체' },
  { value: 'outcome_unknown', label: '결과 불명확' },
  { value: 'executing', label: '실행 중' },
  { value: 'pending', label: '대기' },
  { value: 'succeeded', label: '성공' },
  { value: 'failed', label: '실패' },
]

function toolActionLabel(toolName: string): string {
  return actionLabels[toolName.replace(/^confluence_/, '')] ?? toolName
}

function needsResolve(op: OperationRecord) {
  return op.status === 'outcome_unknown' || op.status === 'executing'
}

export function AdminOperationsPage() {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<string>('all')
  const [target, setTarget] = useState<OperationRecord | null>(null)
  const [resolution, setResolution] = useState<string>('succeeded')
  const [note, setNote] = useState('')

  const list = useQuery({
    queryKey: ['admin', 'operations', status],
    queryFn: () =>
      api.get<OperationRecord[]>(`/api/admin/operations${qs({ status: status === 'all' ? '' : status })}`),
  })

  const resolve = useMutation({
    mutationFn: (vars: { id: string; status: string; note: string }) =>
      api.post<OperationRecord>(`/api/admin/operations/${vars.id}/resolve`, {
        status: vars.status,
        note: vars.note,
      }),
    onSuccess: () => {
      notifyOk('쓰기 작업 상태를 정리했습니다.')
      setTarget(null)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'operations'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
    onError: (err) => notifyError(err, '정리 실패'),
  })

  const openResolve = (op: OperationRecord) => {
    setTarget(op)
    setResolution('succeeded')
    setNote('')
  }

  const rows = list.data ?? []
  const unknownCount = rows.filter((r) => r.status === 'outcome_unknown').length

  return (
    <>
      <PageHeader
        title="쓰기 실행 기록"
        description="승인된 변경이 Confluence 에 실제로 적용된 기록입니다. 같은 요청은 멱등 키로 한 번만 실행됩니다."
        actions={
          <Button
            variant="default"
            leftSection={<IconRefresh size={18} />}
            onClick={() => void list.refetch()}
            loading={list.isFetching && !list.isLoading}
          >
            새로 고침
          </Button>
        }
      />

      {unknownCount > 0 ? (
        <Alert color="orange" variant="light" mb="lg" icon={<IconAlertTriangle size={20} />} title="결과 불명확 작업">
          결과를 확인하지 못한 쓰기 작업이 {unknownCount}건 있습니다. Confluence 에서 실제 반영 여부를 직접 확인한 뒤
          정리하십시오. 정리 전에는 같은 요청을 다시 실행하지 않습니다.
        </Alert>
      ) : null}

      <Section
        title="실행 목록"
        actions={
          <Select
            aria-label="상태"
            data={statusData}
            value={status}
            onChange={(value) => setStatus(value ?? 'all')}
            allowDeselect={false}
            size="sm"
            w={160}
          />
        }
      >
        {list.isLoading ? <LoadingBlock /> : null}
        {list.error ? <ErrorBlock error={list.error} /> : null}
        {list.data && rows.length === 0 ? <EmptyState label="해당하는 실행 기록이 없습니다" /> : null}

        {rows.length > 0 ? (
          <TableScroll minWidth={1080}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>시각</Table.Th>
                  <Table.Th>사용자</Table.Th>
                  <Table.Th>도구</Table.Th>
                  <Table.Th>대상</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>결과 버전</Table.Th>
                  <Table.Th>오류</Table.Th>
                  <Table.Th>실행 계정</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((op) => (
                  <Table.Tr key={op.id}>
                    <Table.Td>
                      <Text size="sm">{formatDateTime(op.createdAt)}</Text>
                      {op.updatedAt && op.updatedAt !== op.createdAt ? (
                        <Text size="xs" c="dimmed">
                          갱신 {formatDateTime(op.updatedAt)}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" fw={600}>
                        {op.username || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{toolActionLabel(op.toolName)}</Text>
                      <Text size="xs" ff="monospace" c="dimmed">
                        {op.toolName}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {op.targetId || op.upstreamId || '—'}
                      </Text>
                      {op.approvalId ? (
                        <Anchor component={Link} to={`/admin/approvals/${op.approvalId}`} size="xs">
                          승인 요청 보기
                        </Anchor>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      <StatusBadge value={op.status} labels={operationStatusLabels} colors={operationStatusColors} />
                      {op.resolvedBy ? (
                        <Text size="xs" c="dimmed" mt={2}>
                          정리: {op.resolvedBy}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{op.resultVersion ? `v${op.resultVersion}` : '—'}</Text>
                    </Table.Td>
                    <Table.Td maw={300}>
                      {op.errorCode ? (
                        <Text size="sm" c="red">
                          {errorCodeLabels[op.errorCode] ?? op.errorCode}
                        </Text>
                      ) : null}
                      <Text size="xs" c="dimmed" lineClamp={3} style={{ overflowWrap: 'anywhere' }}>
                        {op.message || (op.errorCode ? '' : '—')}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {op.executedBy || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap" justify="flex-end">
                        {needsResolve(op) ? (
                          <Button size="xs" color="orange" variant="light" onClick={() => openResolve(op)}>
                            정리
                          </Button>
                        ) : null}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}
      </Section>

      <Modal
        opened={target !== null}
        onClose={() => setTarget(null)}
        title={<Text fw={700}>쓰기 작업 정리</Text>}
        size="lg"
      >
        {target ? (
          <Stack>
            <Alert color="orange" variant="light" icon={<IconAlertTriangle size={20} />}>
              정리하기 전에 Confluence 에서 대상 콘텐츠의 현재 버전과 내용을 직접 확인하십시오. 여기서 고른 결과는
              실행 기록과 감사 로그에 그대로 남으며, 되돌릴 수 없습니다.
            </Alert>
            <StatList
              items={[
                { label: '사용자', value: target.username || '—' },
                { label: '도구', value: `${toolActionLabel(target.toolName)} (${target.toolName})` },
                { label: '대상 콘텐츠', value: target.targetId || '—' },
                { label: '현재 상태', value: operationStatusLabels[target.status] ?? target.status },
                { label: '실행 시각', value: formatDateTime(target.createdAt) },
                { label: '메시지', value: target.message || '—' },
              ]}
            />
            <div>
              <Text size="sm" fw={600} mb={6}>
                확인한 결과
              </Text>
              <SegmentedControl
                fullWidth
                value={resolution}
                onChange={setResolution}
                data={[
                  { value: 'succeeded', label: '반영됨 (성공)' },
                  { value: 'failed', label: '반영 안 됨 (실패)' },
                ]}
              />
            </div>
            <Textarea
              label="메모"
              description="확인한 내용(예: 버전 14 로 반영 확인)을 남기십시오."
              autosize
              minRows={3}
              value={note}
              onChange={(event) => setNote(event.currentTarget.value)}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setTarget(null)}>
                취소
              </Button>
              <Button
                color={resolution === 'failed' ? 'red' : undefined}
                loading={resolve.isPending}
                onClick={() => resolve.mutate({ id: target.id, status: resolution, note: note.trim() })}
              >
                정리
              </Button>
            </Group>
          </Stack>
        ) : null}
      </Modal>
    </>
  )
}
