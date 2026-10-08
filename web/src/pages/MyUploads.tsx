import { Alert, Badge, Button, FileButton, Group, Stack, Table, Text } from '@mantine/core'
import { IconCloudUpload, IconInfoCircle } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ConfirmButton, CopyField, EmptyState, ErrorBlock, LoadingBlock, PageHeader, Section, TableScroll, notifyError, notifyOk } from '../components/ui'
import { api, type Upload } from '../lib/api'
import { formatBytes, formatDateTime, formatRelative } from '../lib/format'

/**
 * MyUploadsPage stages files for confluence_upload_attachment. The MCP tool
 * receives only the upload ID: never a local path or a URL to fetch.
 */
export function MyUploadsPage() {
  const qc = useQueryClient()
  const [last, setLast] = useState<Upload | null>(null)
  const uploads = useQuery({ queryKey: ['me', 'uploads'], queryFn: () => api.get<Upload[]>('/api/me/uploads') })

  const upload = useMutation({
    mutationFn: (file: File) => {
      const form = new FormData()
      form.append('file', file)
      return api.upload<Upload>('/api/me/uploads', form)
    },
    onSuccess: (u) => {
      setLast(u)
      notifyOk(`${u.filename} 을 올렸습니다. 업로드 ID 를 AI 에게 알려 주면 첨부 변경안을 만듭니다.`)
      void qc.invalidateQueries({ queryKey: ['me', 'uploads'] })
    },
    onError: (e) => notifyError(e, '업로드 실패'),
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/me/uploads/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['me', 'uploads'] }),
  })

  return (
    <>
      <PageHeader
        title="첨부 업로드"
        description="Confluence 에 첨부할 파일을 먼저 여기에 올립니다. AI 도구에는 업로드 ID 만 전달되며, 첨부는 승인 후에 반영됩니다."
        actions={
          <FileButton onChange={(f) => f && upload.mutate(f)}>
            {(props) => (
              <Button {...props} leftSection={<IconCloudUpload size={18} />} loading={upload.isPending}>
                파일 올리기
              </Button>
            )}
          </FileButton>
        }
      />

      {last ? (
        <Alert color="teal" variant="light" mb="lg" title="업로드 완료">
          <Stack gap={6}>
            <Text size="sm">
              {last.filename} · {formatBytes(last.size)} · {formatRelative(last.expiresAt)} 만료
            </Text>
            <CopyField value={last.uploadId} label="업로드 ID 복사" />
            <Text size="sm" c="dimmed">
              예: “페이지 65538 에 업로드 {last.uploadId.slice(0, 8)}… 를 첨부해 줘” 라고 요청하면 confluence_upload_attachment 변경안이 만들어집니다.
            </Text>
          </Stack>
        </Alert>
      ) : null}

      <Alert variant="light" color="blue" icon={<IconInfoCircle size={20} />} mb="lg">
        실행 파일과 스크립트(.exe, .js, .sh 등)는 올릴 수 없습니다. 파일 크기 한도와 보관 시간은 관리자가 정합니다. 승인 화면에서는 파일 이름·크기·SHA-256 을 확인할 수 있습니다.
      </Alert>

      <Section title="최근 업로드">
        {uploads.isLoading ? (
          <LoadingBlock />
        ) : uploads.isError ? (
          <ErrorBlock error={uploads.error} />
        ) : (uploads.data?.length ?? 0) === 0 ? (
          <EmptyState label="올린 파일이 없습니다" />
        ) : (
          <TableScroll minWidth={760}>
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>파일</Table.Th>
                  <Table.Th>크기</Table.Th>
                  <Table.Th>업로드 ID</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>올린 시각</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {uploads.data!.map((u) => {
                  const expired = new Date(u.expiresAt).getTime() < Date.now()
                  return (
                    <Table.Tr key={u.uploadId}>
                      <Table.Td>
                        <Text size="sm" fw={600}>
                          {u.filename}
                        </Text>
                        <Text size="xs" c="dimmed">
                          {u.mediaType}
                        </Text>
                      </Table.Td>
                      <Table.Td>{formatBytes(u.size)}</Table.Td>
                      <Table.Td>
                        <CopyField value={u.uploadId} />
                      </Table.Td>
                      <Table.Td>
                        {u.consumedAt ? (
                          <Badge color="blue" variant="light">
                            첨부됨
                          </Badge>
                        ) : expired ? (
                          <Badge color="gray" variant="light">
                            만료
                          </Badge>
                        ) : (
                          <Badge color="teal" variant="light">
                            사용 가능
                          </Badge>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{formatDateTime(u.createdAt)}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Group justify="flex-end">
                          <ConfirmButton
                            size="xs"
                            color="red"
                            title="업로드 삭제"
                            message={`${u.filename} 업로드를 삭제합니다.`}
                            confirmLabel="삭제"
                            onConfirm={() => remove.mutate(u.uploadId)}
                          >
                            삭제
                          </ConfirmButton>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          </TableScroll>
        )}
      </Section>
    </>
  )
}
