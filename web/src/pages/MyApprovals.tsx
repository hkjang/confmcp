import { Badge, Group, SegmentedControl, Table, Tabs, Text } from '@mantine/core'
import { IconInbox, IconUserCheck } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { EmptyState, ErrorBlock, LoadingBlock, PageHeader, Section, StatusBadge, TableScroll } from '../components/ui'
import { api, qs, type ApprovalRequest } from '../lib/api'
import { useAuth } from '../lib/auth'
import {
  actionLabels,
  approvalStatusColors,
  approvalStatusLabels,
  formatDateTime,
  formatRelative,
  riskColors,
  riskLabels,
} from '../lib/format'

function actionOf(r: ApprovalRequest): string {
  const a = r.toolName.replace(/^confluence_/, '')
  return actionLabels[a] ?? r.toolName
}

function ApprovalTable({ rows, showUser }: { rows: ApprovalRequest[]; showUser?: boolean }) {
  const navigate = useNavigate()
  if (rows.length === 0) return <EmptyState label="표시할 변경안이 없습니다" />
  return (
    <TableScroll minWidth={760}>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>요청 시각</Table.Th>
            {showUser ? <Table.Th>요청자</Table.Th> : null}
            <Table.Th>작업</Table.Th>
            <Table.Th>대상</Table.Th>
            <Table.Th>위험도</Table.Th>
            <Table.Th>상태</Table.Th>
            <Table.Th>만료</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r) => (
            <Table.Tr key={r.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/me/approvals/${r.id}`)}>
              <Table.Td>
                <Text size="sm">{formatDateTime(r.createdAt)}</Text>
              </Table.Td>
              {showUser ? <Table.Td>{r.username}</Table.Td> : null}
              <Table.Td>
                <Text size="sm" fw={600}>
                  {actionOf(r)}
                </Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" lineClamp={2}>
                  {r.resource}
                </Text>
              </Table.Td>
              <Table.Td>
                <Group gap={4} wrap="nowrap">
                  <Badge color={riskColors[r.risk] ?? 'gray'} variant="light">
                    {riskLabels[r.risk] ?? r.risk}
                  </Badge>
                  {r.requiresApprover ? (
                    <Badge color="grape" variant="light">
                      승인자
                    </Badge>
                  ) : null}
                </Group>
              </Table.Td>
              <Table.Td>
                <StatusBadge value={r.status} labels={approvalStatusLabels} colors={approvalStatusColors} />
              </Table.Td>
              <Table.Td>
                <Text size="sm" c="dimmed">
                  {r.status === 'pending' ? formatRelative(r.expiresAt) : '—'}
                </Text>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </TableScroll>
  )
}

const statusOptions = [
  { value: 'all', label: '전체' },
  { value: 'pending', label: '대기' },
  { value: 'approved', label: '승인됨' },
  { value: 'consumed', label: '적용됨' },
  { value: 'rejected', label: '거절됨' },
  { value: 'expired', label: '만료' },
]

export function MyApprovalsPage() {
  const { me } = useAuth()
  const [status, setStatus] = useState('all')
  const mine = useQuery({
    queryKey: ['my-approvals', status],
    queryFn: () => api.get<ApprovalRequest[]>(`/api/me/approvals${qs({ status: status === 'all' ? '' : status })}`),
    refetchInterval: 20_000,
  })
  const queue = useQuery({
    queryKey: ['approval-queue'],
    queryFn: () => api.get<ApprovalRequest[]>('/api/me/approval-queue'),
    enabled: Boolean(me?.canApprove),
    refetchInterval: 20_000,
  })

  return (
    <>
      <PageHeader
        title="변경 승인"
        description="AI 가 만든 변경안은 여기서 diff 를 확인하고 승인해야 Confluence 에 적용됩니다. 승인 후 문서나 인자가 바뀌면 그 승인은 무효가 됩니다."
      />
      <Tabs defaultValue="mine" variant="pills" radius="md">
        <Tabs.List mb="md">
          <Tabs.Tab value="mine" leftSection={<IconInbox size={18} />}>
            내 변경안
          </Tabs.Tab>
          {me?.canApprove ? (
            <Tabs.Tab
              value="queue"
              leftSection={<IconUserCheck size={18} />}
              rightSection={
                queue.data?.length ? (
                  <Badge size="sm" color="yellow" circle>
                    {queue.data.length}
                  </Badge>
                ) : null
              }
            >
              승인 대기열
            </Tabs.Tab>
          ) : null}
        </Tabs.List>

        <Tabs.Panel value="mine">
          <Section>
            <Group mb="md">
              <SegmentedControl data={statusOptions} value={status} onChange={setStatus} />
            </Group>
            {mine.isLoading ? <LoadingBlock /> : mine.isError ? <ErrorBlock error={mine.error} /> : <ApprovalTable rows={mine.data ?? []} />}
          </Section>
        </Tabs.Panel>
        <Tabs.Panel value="queue">
          <Section description="다른 사용자의 변경안 가운데 내가 승인할 수 있는 것입니다. 본인이 Confluence 에서 볼 수 있는 문서만 승인할 수 있습니다.">
            {queue.isLoading ? (
              <LoadingBlock />
            ) : queue.isError ? (
              <ErrorBlock error={queue.error} />
            ) : (
              <ApprovalTable rows={queue.data ?? []} showUser />
            )}
          </Section>
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
