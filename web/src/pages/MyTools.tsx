import { Accordion, Badge, Group, Stack, Text, TextInput } from '@mantine/core'
import { IconSearch } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { EmptyState, ErrorBlock, JsonBlock, LoadingBlock, PageHeader, Section } from '../components/ui'
import { api, type ToolRecord } from '../lib/api'
import { opLabels, riskColors, riskLabels, roleLabels, toolGroupLabels } from '../lib/format'

export function MyToolsPage() {
  const [query, setQuery] = useState('')
  const tools = useQuery({ queryKey: ['me', 'tools'], queryFn: () => api.get<ToolRecord[]>('/api/me/tools') })

  const grouped = useMemo(() => {
    const filtered = (tools.data ?? []).filter((tool) => {
      if (!query.trim()) return true
      const needle = query.trim().toLowerCase()
      return (
        tool.name.toLowerCase().includes(needle) ||
        tool.title.toLowerCase().includes(needle) ||
        tool.description.toLowerCase().includes(needle)
      )
    })
    const map = new Map<string, ToolRecord[]>()
    for (const tool of filtered) {
      const list = map.get(tool.group) ?? []
      list.push(tool)
      map.set(tool.group, list)
    }
    return [...map.entries()]
  }, [tools.data, query])

  return (
    <>
      <PageHeader
        title="사용 가능한 도구"
        description="내 역할과 키 스코프로 호출할 수 있는 MCP 도구입니다. 실제 실행 때는 내 Confluence 권한과 MCP 접근 정책이 매번 다시 확인됩니다."
        actions={
          <TextInput
            placeholder="도구 검색"
            leftSection={<IconSearch size={18} />}
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            w={260}
          />
        }
      />

      {tools.isLoading ? <LoadingBlock /> : null}
      {tools.error ? <ErrorBlock error={tools.error} /> : null}
      {tools.data && grouped.length === 0 ? <EmptyState label="조건에 맞는 도구가 없습니다." /> : null}

      {grouped.map(([group, list]) => (
        <Section key={group} title={`${toolGroupLabels[group] ?? group} (${list.length})`}>
          <Accordion variant="separated" radius="md">
            {list.map((tool) => (
              <Accordion.Item key={tool.name} value={tool.name}>
                <Accordion.Control>
                  <Group justify="space-between" wrap="wrap" gap="xs">
                    <Stack gap={2}>
                      <Group gap="xs">
                        <Text fw={600}>{tool.title}</Text>
                        <Badge color={riskColors[tool.risk]} variant="light" size="sm">
                          {riskLabels[tool.risk] ?? tool.risk}
                        </Badge>
                        {tool.requiresApproval ? (
                          <Badge color="orange" variant="light" size="sm">
                            승인 필요
                          </Badge>
                        ) : null}
                        <Badge variant="light" color="gray" size="sm">
                          {tool.priority}
                        </Badge>
                        {tool.highLevel ? (
                          <Badge color="grape" variant="light" size="sm">
                            고수준
                          </Badge>
                        ) : null}
                      </Group>
                      <Text size="sm" ff="monospace" c="dimmed">
                        {tool.name}
                      </Text>
                    </Stack>
                  </Group>
                </Accordion.Control>
                <Accordion.Panel>
                  <Stack gap="sm">
                    <Text>{tool.description}</Text>
                    <Group gap="xs">
                      <Badge variant="outline" color="gray">
                        필요 권한 {opLabels[tool.requiredPermission] ?? (tool.requiredPermission === 'NONE' ? '없음' : tool.requiredPermission)}
                      </Badge>
                      <Badge variant="outline" color="gray">
                        스코프 {tool.scope || '—'}
                      </Badge>
                      <Badge variant="outline" color="gray">
                        최소 역할 {roleLabels[tool.minRole] ?? tool.minRole}
                      </Badge>
                    </Group>
                    {tool.inputSchema ? (
                      <>
                        <Text size="sm" c="dimmed">
                          입력 스키마
                        </Text>
                        <JsonBlock value={tool.inputSchema} maxHeight={280} />
                      </>
                    ) : null}
                  </Stack>
                </Accordion.Panel>
              </Accordion.Item>
            ))}
          </Accordion>
        </Section>
      ))}
    </>
  )
}
