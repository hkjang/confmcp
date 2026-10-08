import { Badge, Button, Group, Select, Table, Text } from '@mantine/core'
import { IconRefresh } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import {
  EmptyState,
  ErrorBlock,
  LoadingBlock,
  PageHeader,
  Section,
  StatusBadge,
  TableScroll,
} from '../../components/ui'
import { api, qs, type ApprovalRequest } from '../../lib/api'
import {
  actionLabels,
  approvalStatusColors,
  approvalStatusLabels,
  formatDateTime,
  formatRelative,
  riskColors,
  riskLabels,
} from '../../lib/format'

const statusData = [
  { value: 'pending', label: '승인 대기' },
  { value: 'approved', label: '승인됨' },
  { value: 'consumed', label: '적용됨' },
  { value: 'rejected', label: '거절됨' },
  { value: 'expired', label: '만료' },
  { value: 'all', label: '전체' },
]

function toolActionLabel(toolName: string): string {
  const short = toolName.replace(/^confluence_/, '')
  return actionLabels[short] ?? toolName
}

export function AdminApprovalsPage() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<string>('pending')

  const list = useQuery({
    queryKey: ['admin', 'approvals', status],
    queryFn: () =>
      api.get<ApprovalRequest[]>(`/api/admin/approvals${qs({ status: status === 'all' ? '' : status })}`),
  })

  const rows = list.data ?? []

  return (
    <>
      <PageHeader
        title="승인 요청"
        description="모든 사용자의 쓰기 승인 요청을 봅니다. 행을 누르면 변경 내용(diff)을 확인하고 승인하거나 거절할 수 있습니다."
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

      <Section
        title="요청 목록"
        actions={
          <Select
            aria-label="상태"
            data={statusData}
            value={status}
            onChange={(value) => setStatus(value ?? 'pending')}
            allowDeselect={false}
            size="sm"
            w={160}
          />
        }
      >
        {list.isLoading ? <LoadingBlock /> : null}
        {list.error ? <ErrorBlock error={list.error} /> : null}
        {list.data && rows.length === 0 ? <EmptyState label="해당하는 승인 요청이 없습니다" /> : null}

        {rows.length > 0 ? (
          <TableScroll minWidth={980}>
            <Table striped fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>요청 시각</Table.Th>
                  <Table.Th>요청자</Table.Th>
                  <Table.Th>도구/작업</Table.Th>
                  <Table.Th>대상</Table.Th>
                  <Table.Th>위험도</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>만료</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((req) => (
                  <Table.Tr
                    key={req.id}
                    style={{ cursor: 'pointer' }}
                    onClick={() => navigate(`/admin/approvals/${req.id}`)}
                    tabIndex={0}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') navigate(`/admin/approvals/${req.id}`)
                    }}
                  >
                    <Table.Td>
                      <Text size="sm">{formatDateTime(req.createdAt)}</Text>
                      <Text size="xs" c="dimmed">
                        {formatRelative(req.createdAt)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" fw={600}>
                        {req.username || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{toolActionLabel(req.toolName)}</Text>
                      <Text size="xs" ff="monospace" c="dimmed">
                        {req.toolName}
                      </Text>
                    </Table.Td>
                    <Table.Td maw={320}>
                      <Text size="sm" lineClamp={2} style={{ overflowWrap: 'anywhere' }}>
                        {req.preview?.title || req.resource || '—'}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {[req.spaceKey ? `공간 ${req.spaceKey}` : '', req.targetId ? `ID ${req.targetId}` : '']
                          .filter(Boolean)
                          .join(' · ') || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="wrap">
                        <Badge variant="light" color={riskColors[req.risk] ?? 'gray'}>
                          {riskLabels[req.risk] ?? req.risk}
                        </Badge>
                        {req.requiresApprover ? (
                          <Badge variant="outline" color="red" size="sm">
                            별도 승인자 필요
                          </Badge>
                        ) : null}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <StatusBadge value={req.status} labels={approvalStatusLabels} colors={approvalStatusColors} />
                      {req.decidedBy ? (
                        <Text size="xs" c="dimmed" mt={2}>
                          {req.decidedBy}
                        </Text>
                      ) : null}
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{formatDateTime(req.expiresAt)}</Text>
                      {req.status === 'pending' ? (
                        <Text size="xs" c="dimmed">
                          {formatRelative(req.expiresAt)}
                        </Text>
                      ) : null}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </TableScroll>
        ) : null}
      </Section>
    </>
  )
}
