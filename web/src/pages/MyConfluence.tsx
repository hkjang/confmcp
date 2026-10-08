import { Alert, Badge, Button, Grid, Group, PasswordInput, Stack, Text, TextInput } from '@mantine/core'
import { IconAlertTriangle, IconCircleCheck, IconInfoCircle, IconLink, IconRefresh, IconShieldCheck } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { ConfirmButton, ErrorBlock, LoadingBlock, PageHeader, Section, StatList, notifyError, notifyOk } from '../components/ui'
import { api, type Mapping, type MyConfluence } from '../lib/api'
import { useAuth } from '../lib/auth'
import { evidenceLabels, executionModeLabels, formatDateTime, permissionModeLabels } from '../lib/format'

interface VerifyResult {
  ok: boolean
  error?: string
  mapping?: Mapping
}

export function MyConfluencePage() {
  const qc = useQueryClient()
  const { refresh } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const info = useQuery({ queryKey: ['me', 'confluence'], queryFn: () => api.get<MyConfluence>('/api/me/confluence') })

  const afterChange = async () => {
    await qc.invalidateQueries({ queryKey: ['me', 'confluence'] })
    await refresh()
  }
  const verifyMapping = useMutation({
    mutationFn: () => api.post<VerifyResult>('/api/me/confluence/mapping/verify'),
    onSuccess: async (r) => {
      if (r.ok) notifyOk('Confluence 계정을 다시 확인했습니다.')
      else notifyError(new Error(r.error), '확인 실패')
      await afterChange()
    },
    onError: (e) => notifyError(e),
  })
  const saveCred = useMutation({
    mutationFn: () => api.put<VerifyResult>('/api/me/confluence/credential', { username: username.trim(), password }),
    onSuccess: async (r) => {
      setPassword('')
      if (r.ok) notifyOk('Confluence 로그인으로 계정을 확인했습니다.')
      else notifyError(new Error(r.error), '로그인 확인 실패')
      await afterChange()
    },
    onError: (e) => notifyError(e),
  })
  const verifyCred = useMutation({
    mutationFn: () => api.post<VerifyResult>('/api/me/confluence/credential/verify'),
    onSuccess: async (r) => {
      if (r.ok) notifyOk('자격증명이 유효합니다.')
      else notifyError(new Error(r.error), '확인 실패')
      await afterChange()
    },
  })
  const deleteCred = useMutation({
    mutationFn: () => api.del('/api/me/confluence/credential'),
    onSuccess: async () => {
      notifyOk('연결한 자격증명을 삭제했습니다.')
      await afterChange()
    },
  })

  if (info.isLoading) return <LoadingBlock />
  if (info.isError) return <ErrorBlock error={info.error} />
  const d = info.data!
  const m = d.mapping
  const delegated = d.permissionMode === 'delegated' || d.executionMode === 'delegated'

  return (
    <>
      <PageHeader
        title="내 Confluence"
        description="confmcp 는 회원님의 회사 계정을 Confluence 사용자(userKey)와 한 번 연결해 두고, 매 호출마다 그 사용자의 권한을 확인합니다."
      />

      <Grid gutter="lg">
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Section
            title="계정 연결 상태"
            actions={
              m ? (
                <Button size="xs" variant="light" leftSection={<IconRefresh size={16} />} loading={verifyMapping.isPending} onClick={() => verifyMapping.mutate()}>
                  다시 확인
                </Button>
              ) : null
            }
          >
            {m ? (
              <Stack gap="md">
                <Group gap="xs">
                  <Badge color={m.active ? 'teal' : 'gray'} leftSection={<IconCircleCheck size={14} />} variant="light" size="lg">
                    {m.active ? '연결됨' : '비활성'}
                  </Badge>
                  <Badge variant="light" color="blue">
                    {evidenceLabels[m.evidence] ?? m.evidence}
                  </Badge>
                </Group>
                <StatList
                  items={[
                    { label: 'Confluence 사용자', value: `${m.confluenceDisplay} (${m.confluenceUsername})` },
                    { label: 'userKey', value: <Text ff="monospace" size="sm">{m.confluenceUserKey}</Text> },
                    { label: '인스턴스', value: m.instanceId },
                    { label: '연결 시각', value: formatDateTime(m.mappedAt) },
                    { label: '마지막 확인', value: formatDateTime(m.verifiedAt) },
                  ]}
                />
                {m.lastError ? (
                  <Alert color="orange" icon={<IconAlertTriangle size={20} />}>
                    {m.lastError}
                  </Alert>
                ) : null}
              </Stack>
            ) : (
              <Alert color="orange" icon={<IconAlertTriangle size={20} />} title="아직 연결되지 않았습니다">
                <Text size="sm">{d.mappingError || '연결된 Confluence 사용자가 없습니다.'}</Text>
                <Text size="sm" mt="xs">
                  {d.allowUserCredential
                    ? '오른쪽에서 Confluence 계정으로 한 번 로그인하면 본인 계정임이 확인되어 바로 연결됩니다.'
                    : '서비스 관리자에게 사용자 매핑을 요청하십시오.'}
                </Text>
              </Alert>
            )}
          </Section>

          <Section title="이 서비스의 동작 방식">
            <StatList
              items={[
                { label: 'Confluence', value: d.baseUrl || '미설정' },
                { label: '감지된 버전', value: d.detectedVersion ? `v${d.detectedVersion}` : '—' },
                { label: '권한 판정', value: permissionModeLabels[d.permissionMode] ?? d.permissionMode },
                { label: '실행 방식', value: executionModeLabels[d.executionMode] ?? d.executionMode },
                { label: '서비스 계정', value: d.serviceAccount || '—' },
              ]}
            />
            <Alert variant="light" color="teal" icon={<IconShieldCheck size={20} />} mt="md">
              {d.executionMode === 'service'
                ? 'Confluence 호출은 서비스 계정으로 실행되지만, 무엇을 볼 수 있는지는 회원님 본인의 권한으로 매번 판정합니다. Confluence 기록의 작성자는 서비스 계정으로 남을 수 있으며, confmcp 감사 기록에 실제 요청자가 함께 남습니다.'
                : 'Confluence 호출이 회원님 본인의 자격증명으로 실행되므로 Confluence 가 직접 권한을 적용하고, 작성자도 본인으로 기록됩니다.'}
            </Alert>
          </Section>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Section
            title="Confluence 로그인으로 연결"
            description={
              delegated
                ? '이 서비스는 사용자 위임 모드입니다. 도구를 쓰려면 본인의 Confluence 자격증명을 연결해야 합니다.'
                : '본인 Confluence 계정으로 한 번 로그인해 계정 소유를 확인합니다. 확인된 계정이 매핑됩니다.'
            }
          >
            {!d.allowUserCredential ? (
              <Alert variant="light" color="gray" icon={<IconInfoCircle size={20} />}>
                관리자가 사용자 자격증명 연결을 허용하지 않았습니다.
              </Alert>
            ) : (
              <Stack gap="md">
                {d.credential?.hasSecret ? (
                  <Alert variant="light" color={d.credential.verifiedAt ? 'teal' : 'orange'} icon={<IconLink size={20} />}>
                    <Text size="sm">
                      {d.credential.confluenceUsername} 계정이 연결되어 있습니다 ·{' '}
                      {d.credential.verifiedAt ? `확인 ${formatDateTime(d.credential.verifiedAt)}` : '확인되지 않음'}
                    </Text>
                    <Group gap="xs" mt="xs">
                      <Button size="xs" variant="light" loading={verifyCred.isPending} onClick={() => verifyCred.mutate()}>
                        다시 확인
                      </Button>
                      <ConfirmButton
                        size="xs"
                        color="red"
                        title="자격증명 삭제"
                        message="연결한 Confluence 자격증명을 삭제합니다. 사용자 위임 모드에서는 도구를 쓸 수 없게 됩니다."
                        confirmLabel="삭제"
                        onConfirm={() => deleteCred.mutate()}
                      >
                        삭제
                      </ConfirmButton>
                    </Group>
                  </Alert>
                ) : null}
                <TextInput
                  label="Confluence 사용자명"
                  placeholder={m?.confluenceUsername ?? '예: hong.gildong'}
                  value={username}
                  onChange={(e) => setUsername(e.currentTarget.value)}
                  autoComplete="off"
                />
                <PasswordInput
                  label="Confluence 비밀번호"
                  value={password}
                  onChange={(e) => setPassword(e.currentTarget.value)}
                  autoComplete="new-password"
                  description="AES-256-GCM 으로 암호화해 저장하며 화면에 다시 표시하지 않습니다."
                />
                <Group>
                  <Button
                    leftSection={<IconLink size={18} />}
                    disabled={!username.trim() || !password}
                    loading={saveCred.isPending}
                    onClick={() => saveCred.mutate()}
                  >
                    로그인하고 연결
                  </Button>
                </Group>
              </Stack>
            )}
          </Section>
        </Grid.Col>
      </Grid>
    </>
  )
}
