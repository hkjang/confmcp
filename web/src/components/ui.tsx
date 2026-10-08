import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Paper,
  ScrollArea,
  Stack,
  Text,
  Title,
  Tooltip,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import {
  IconAlertTriangle,
  IconCheck,
  IconCircleCheck,
  IconCircleX,
  IconCopy,
  IconMinus,
} from '@tabler/icons-react'
import { useState, type ReactNode } from 'react'
import type { HealthComponent } from '../lib/api'

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <Group justify="space-between" align="flex-start" wrap="wrap" gap="md" mb="lg">
      <Box style={{ flex: '1 1 320px', minWidth: 0 }}>
        <Title order={2}>{title}</Title>
        {description ? (
          <Text c="dimmed" mt={6} size="md">
            {description}
          </Text>
        ) : null}
      </Box>
      {actions ? <Group gap="sm">{actions}</Group> : null}
    </Group>
  )
}

export function Section({
  title,
  description,
  actions,
  children,
}: {
  title?: string
  description?: ReactNode
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <Paper withBorder radius="md" p="lg" mb="lg">
      {title || actions ? (
        <Group justify="space-between" align="flex-start" mb={description ? 4 : 'md'} wrap="wrap">
          {title ? <Title order={4}>{title}</Title> : <span />}
          {actions ? <Group gap="xs">{actions}</Group> : null}
        </Group>
      ) : null}
      {description ? (
        <Text c="dimmed" size="sm" mb="md">
          {description}
        </Text>
      ) : null}
      {children}
    </Paper>
  )
}

export function LoadingBlock({ label = '불러오는 중입니다…' }: { label?: string }) {
  return (
    <Group justify="center" py="xl" gap="sm">
      <Loader size="sm" />
      <Text c="dimmed">{label}</Text>
    </Group>
  )
}

export function ErrorBlock({ error, title = '오류' }: { error: unknown; title?: string }) {
  const message = error instanceof Error ? error.message : String(error ?? '알 수 없는 오류')
  return (
    <Alert color="red" icon={<IconAlertTriangle size={20} />} title={title} variant="light">
      {message}
    </Alert>
  )
}

export function EmptyState({ label }: { label: string }) {
  return (
    <Text c="dimmed" ta="center" py="xl">
      {label}
    </Text>
  )
}

export function HealthBadge({ name, value }: { name: string; value: HealthComponent }) {
  const color = value.skipped ? 'gray' : value.ok ? 'teal' : 'red'
  const icon = value.skipped ? (
    <IconMinus size={16} />
  ) : value.ok ? (
    <IconCircleCheck size={16} />
  ) : (
    <IconCircleX size={16} />
  )
  return (
    <Tooltip label={value.detail || name} multiline maw={360}>
      <Badge color={color} variant="light" leftSection={icon} style={{ cursor: 'help' }}>
        {name}
      </Badge>
    </Tooltip>
  )
}

export function CopyField({ value, label }: { value: string; label?: string }) {
  return (
    <Group gap="xs" wrap="nowrap" align="center">
      <Code style={{ overflowWrap: 'anywhere' }}>{value}</Code>
      <CopyButton value={value} timeout={1500}>
        {({ copied, copy }) => (
          <Tooltip label={copied ? '복사했습니다' : (label ?? '복사')}>
            <ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} onClick={copy} aria-label="복사">
              {copied ? <IconCheck size={18} /> : <IconCopy size={18} />}
            </ActionIcon>
          </Tooltip>
        )}
      </CopyButton>
    </Group>
  )
}

export function TableScroll({ children, minWidth = 720 }: { children: ReactNode; minWidth?: number }) {
  return (
    <ScrollArea type="auto" offsetScrollbars scrollbarSize={10}>
      <Box miw={minWidth}>{children}</Box>
    </ScrollArea>
  )
}

/** Preformatted text such as shell commands, shown as written. */
export function TextBlock({ text, maxHeight = 420 }: { text: string; maxHeight?: number }) {
  return (
    <Box
      component="pre"
      className="confmcp-code confmcp-scroll-surface"
      p="sm"
      m={0}
      style={{
        maxHeight,
        overflow: 'auto',
        whiteSpace: 'pre',
        borderRadius: 'var(--mantine-radius-sm)',
        background: 'var(--mantine-color-default-hover)',
      }}
    >
      {text}
    </Box>
  )
}

export function JsonBlock({ value, maxHeight = 420 }: { value: unknown; maxHeight?: number }) {
  return (
    <Box
      className="confmcp-code confmcp-scroll-surface"
      p="sm"
      style={{
        maxHeight,
        overflow: 'auto',
        borderRadius: 'var(--mantine-radius-sm)',
        background: 'var(--mantine-color-default-hover)',
      }}
    >
      {JSON.stringify(value, null, 2)}
    </Box>
  )
}

export function SaveBar({
  onSave,
  saving,
  dirty,
  extra,
}: {
  onSave: () => void
  saving?: boolean
  dirty?: boolean
  extra?: ReactNode
}) {
  return (
    <Group justify="flex-end" mt="lg" gap="sm">
      {extra}
      <Button onClick={onSave} loading={saving} disabled={dirty === false}>
        저장
      </Button>
    </Group>
  )
}

export function StatList({ items }: { items: { label: string; value: ReactNode }[] }) {
  return (
    <Stack gap="xs">
      {items.map((item) => (
        <Group key={item.label} justify="space-between" wrap="nowrap" gap="md">
          <Text c="dimmed" size="sm">
            {item.label}
          </Text>
          <Text size="sm" fw={600} style={{ textAlign: 'right', overflowWrap: 'anywhere' }}>
            {item.value}
          </Text>
        </Group>
      ))}
    </Stack>
  )
}

/** StatCard is a dashboard tile: a label, a large number and an optional hint. */
export function StatCard({
  label,
  value,
  hint,
  color,
  icon,
  onClick,
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  color?: string
  icon?: ReactNode
  onClick?: () => void
}) {
  return (
    <Paper
      withBorder
      p="md"
      className="confmcp-stat"
      onClick={onClick}
      style={{ cursor: onClick ? 'pointer' : undefined }}
    >
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Text size="sm" c="dimmed" fw={600}>
          {label}
        </Text>
        {icon ? <Box c={color ?? 'confmcp'}>{icon}</Box> : null}
      </Group>
      <Text fz={28} fw={800} mt={4} c={color} lh={1.2}>
        {value}
      </Text>
      {hint ? (
        <Text size="xs" c="dimmed" mt={4}>
          {hint}
        </Text>
      ) : null}
    </Paper>
  )
}

/** DiffView renders a unified diff produced by the gateway. Text only: no HTML from documents is ever rendered. */
export function DiffView({ diff }: { diff?: string }) {
  if (!diff) return <EmptyState label="표시할 변경 내용이 없습니다" />
  const lines = diff.replace(/\n$/, '').split('\n')
  return (
    <Box component="pre" className="confmcp-diff confmcp-scroll-surface" data-testid="diff-view">
      {lines.map((line, i) => {
        const cls = line.startsWith('+ ')
          ? 'confmcp-diff-add'
          : line.startsWith('- ')
            ? 'confmcp-diff-del'
            : line.startsWith('@@')
              ? 'confmcp-diff-hunk'
              : ''
        return (
          <span key={i} className={`confmcp-diff-line ${cls}`}>
            {line || ' '}
          </span>
        )
      })}
    </Box>
  )
}

/** StatusBadge shows a value with a label and colour from lookup tables. */
export function StatusBadge({
  value,
  labels,
  colors,
  variant = 'light',
}: {
  value?: string
  labels: Record<string, string>
  colors: Record<string, string>
  variant?: 'light' | 'filled' | 'outline' | 'dot'
}) {
  if (!value) return <Text c="dimmed">—</Text>
  return (
    <Badge color={colors[value] ?? 'gray'} variant={variant}>
      {labels[value] ?? value}
    </Badge>
  )
}

/** ConfirmButton asks before running a destructive or important action. */
export function ConfirmButton({
  children,
  title,
  message,
  confirmLabel = '확인',
  color,
  onConfirm,
  loading,
  disabled,
  variant,
  size,
  leftSection,
}: {
  children: ReactNode
  title: string
  message: ReactNode
  confirmLabel?: string
  color?: string
  onConfirm: () => void | Promise<void>
  loading?: boolean
  disabled?: boolean
  variant?: string
  size?: string
  leftSection?: ReactNode
}) {
  const [opened, setOpened] = useState(false)
  return (
    <>
      <Button
        color={color}
        variant={variant ?? 'light'}
        size={size ?? 'sm'}
        onClick={() => setOpened(true)}
        loading={loading}
        disabled={disabled}
        leftSection={leftSection}
      >
        {children}
      </Button>
      <Modal opened={opened} onClose={() => setOpened(false)} title={<Text fw={700}>{title}</Text>}>
        <Stack>
          <Text size="sm">{message}</Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setOpened(false)}>
              취소
            </Button>
            <Button
              color={color}
              onClick={async () => {
                setOpened(false)
                await onConfirm()
              }}
            >
              {confirmLabel}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </>
  )
}

/** notifyError shows an API failure as a toast. */
export function notifyError(err: unknown, title = '요청 실패') {
  notifications.show({ color: 'red', title, message: err instanceof Error ? err.message : String(err) })
}

export function notifyOk(message: string, title = '완료') {
  notifications.show({ color: 'teal', title, message })
}
