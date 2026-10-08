import { Alert, Badge, Button, Group, SegmentedControl, SimpleGrid, Stack, Table, Text, TextInput } from '@mantine/core'
import { IconAlertTriangle, IconSearch, IconShieldCheck } from '@tabler/icons-react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { ErrorBlock, PageHeader, Section, StatList, TableScroll } from '../components/ui'
import { api, qs, type PermissionCheck } from '../lib/api'
import { useAuth } from '../lib/auth'
import { contentTypeLabels, formatDateTime, opLabels, opOrder, permissionModeLabels, verdictColors, verdictLabels } from '../lib/format'

const reasonText: Record<string, string> = {
  NOT_VISIBLE: '볼 수 없거나 존재하지 않음',
  PAGE_EDIT_RESTRICTED: '페이지 편집 제한',
  NO_SPACE_CREATE: '공간 작성 권한 없음',
  NO_SPACE_COMMENT: '공간 댓글 권한 없음',
  NO_ATTACH: '첨부 권한 없음',
  NO_SPACE_DELETE: '공간 삭제 권한 없음',
  NO_SPACE_PERMISSION: '공간 권한 없음',
  NOT_PERMITTED: '권한 없음',
  OPERATION_NOT_REPORTED: 'Confluence 가 알려 주지 않음',
  OPERATIONS_UNAVAILABLE: '확인 불가',
}

export function MyPermissionsPage() {
  const { me } = useAuth()
  const [kind, setKind] = useState('space')
  const [target, setTarget] = useState('')

  const check = useMutation({
    mutationFn: () =>
      api.get<PermissionCheck>(
        `/api/me/permissions${qs(kind === 'space' ? { spaceKey: target.trim().toUpperCase() } : { contentId: target.trim() })}`,
      ),
  })
  const d = check.data?.data

  return (
    <>
      <PageHeader
        title="내 권한 확인"
        description="공간이나 문서에서 내가 할 수 있는 작업을 Confluence 판정 그대로 보여 줍니다. AI 도구도 이 판정과 MCP 접근 정책을 함께 적용합니다."
      />
      {!me?.confluence ? (
        <Alert color="orange" variant="light" icon={<IconAlertTriangle size={20} />} mb="lg">
          Confluence 계정이 연결되지 않아 권한을 확인할 수 없습니다.
        </Alert>
      ) : null}

      <Section>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (target.trim()) check.mutate()
          }}
        >
          <Group align="flex-end" wrap="wrap" gap="sm">
            <SegmentedControl
              data={[
                { value: 'space', label: '공간' },
                { value: 'content', label: '페이지·첨부 ID' },
              ]}
              value={kind}
              onChange={setKind}
            />
            <TextInput
              label={kind === 'space' ? '공간 키' : '콘텐츠 ID'}
              placeholder={kind === 'space' ? '예: DEV' : '예: 65538'}
              value={target}
              onChange={(e) => setTarget(e.currentTarget.value)}
              w={260}
            />
            <Button type="submit" leftSection={<IconSearch size={18} />} loading={check.isPending} disabled={!target.trim() || !me?.confluence}>
              확인
            </Button>
          </Group>
        </form>
      </Section>

      {check.isError ? <ErrorBlock error={check.error} /> : null}
      {d ? (
        <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
          <Section title="작업별 판정">
            <TableScroll minWidth={360}>
              <Table>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>작업</Table.Th>
                    <Table.Th>판정</Table.Th>
                    <Table.Th>사유</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {opOrder.map((op) => {
                    const o = d.operations[op]
                    if (!o) return null
                    return (
                      <Table.Tr key={op}>
                        <Table.Td fw={600}>{opLabels[op] ?? op}</Table.Td>
                        <Table.Td>
                          <Badge color={verdictColors[o.verdict] ?? 'gray'} variant="light">
                            {verdictLabels[o.verdict] ?? o.verdict}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" c="dimmed">
                            {o.reason ? (reasonText[o.reason] ?? o.reason) : '—'}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    )
                  })}
                </Table.Tbody>
              </Table>
            </TableScroll>
          </Section>
          <Stack gap={0}>
            <Section title="대상">
              <StatList
                items={[
                  { label: '종류', value: d.target.kind === 'space' ? '공간' : '콘텐츠' },
                  { label: '제목', value: d.title ?? '—' },
                  { label: '공간', value: d.spaceKey ?? d.target.spaceKey ?? '—' },
                  { label: '유형', value: d.type ? (contentTypeLabels[d.type] ?? d.type) : '—' },
                  { label: '판정 출처', value: permissionModeLabels[d.source] ?? d.source },
                  { label: '판정 시각', value: formatDateTime(d.evaluatedAt) },
                ]}
              />
              {d.note ? (
                <Text size="sm" c="dimmed" mt="sm">
                  {d.note}
                </Text>
              ) : null}
            </Section>
            {d.policy ? (
              <Section title="MCP 접근 정책">
                <Group gap="xs" mb="xs">
                  <Badge color={d.policy.allowed ? 'teal' : 'red'} leftSection={<IconShieldCheck size={14} />}>
                    {d.policy.allowed ? 'AI 접근 허용' : 'AI 접근 차단'}
                  </Badge>
                  {d.policy.riskCap ? <Badge variant="light">위험도 상한 {d.policy.riskCap}</Badge> : null}
                </Group>
                {d.policy.reason ? <Text size="sm">{d.policy.reason}</Text> : null}
              </Section>
            ) : null}
          </Stack>
        </SimpleGrid>
      ) : null}
    </>
  )
}
