import { createTheme, type MantineThemeOverride } from '@mantine/core'

// confmcp uses a deliberately larger type scale than Mantine's default: it is
// an operations console read on shared monitors and laptops, so body text is
// 16px and the smallest text is 13px rather than 12px.
export const theme: MantineThemeOverride = createTheme({
  primaryColor: 'confmcp',
  primaryShade: { light: 7, dark: 5 },
  defaultRadius: 'md',
  cursorType: 'pointer',
  fontFamily:
    "'Pretendard Variable', Pretendard, -apple-system, BlinkMacSystemFont, 'Malgun Gothic', 'Apple SD Gothic Neo', 'Noto Sans KR', system-ui, sans-serif",
  fontFamilyMonospace: "'D2Coding', 'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
  headings: {
    fontWeight: '700',
    sizes: {
      h1: { fontSize: '2rem', lineHeight: '1.3' },
      h2: { fontSize: '1.625rem', lineHeight: '1.35' },
      h3: { fontSize: '1.375rem', lineHeight: '1.4' },
      h4: { fontSize: '1.175rem', lineHeight: '1.45' },
      h5: { fontSize: '1.05rem', lineHeight: '1.5' },
    },
  },
  fontSizes: {
    xs: '0.8125rem',
    sm: '0.9375rem',
    md: '1rem',
    lg: '1.125rem',
    xl: '1.3125rem',
  },
  lineHeights: { xs: '1.5', sm: '1.55', md: '1.6', lg: '1.65', xl: '1.7' },
  colors: {
    confmcp: [
      '#edf2ff', '#dbe4ff', '#bac8ff', '#91a7ff', '#748ffc',
      '#5c7cfa', '#4c6ef5', '#4263eb', '#3b5bdb', '#364fc7',
    ],
  },
  components: {
    Button: { defaultProps: { size: 'md' } },
    TextInput: { defaultProps: { size: 'md' } },
    PasswordInput: { defaultProps: { size: 'md' } },
    NumberInput: { defaultProps: { size: 'md' } },
    Textarea: { defaultProps: { size: 'md' } },
    // Select never clears itself on re-click, keeps its dropdown in a portal
    // (so it is not clipped by tables or cards) and shows the check on the right.
    Select: {
      defaultProps: {
        size: 'md',
        allowDeselect: false,
        checkIconPosition: 'right',
        comboboxProps: { withinPortal: true, shadow: 'md' },
        nothingFoundMessage: '일치하는 항목이 없습니다',
      },
    },
    MultiSelect: {
      defaultProps: { size: 'md', comboboxProps: { withinPortal: true, shadow: 'md' }, nothingFoundMessage: '일치하는 항목이 없습니다' },
    },
    TagsInput: { defaultProps: { size: 'md', comboboxProps: { withinPortal: true } } },
    SegmentedControl: { defaultProps: { size: 'sm' } },
    Switch: { defaultProps: { size: 'md' } },
    Checkbox: { defaultProps: { size: 'md' } },
    Table: { defaultProps: { fz: 'sm', verticalSpacing: 'sm', horizontalSpacing: 'md', highlightOnHover: true } },
    Badge: { defaultProps: { size: 'md', tt: 'none', radius: 'sm' } },
    Tooltip: { defaultProps: { withArrow: true, fz: 'sm', openDelay: 200 } },
    Modal: { defaultProps: { centered: true, overlayProps: { blur: 2 }, radius: 'lg' } },
    Drawer: { defaultProps: { overlayProps: { blur: 2 } } },
    Paper: { defaultProps: { radius: 'lg' } },
    Card: { defaultProps: { radius: 'lg' } },
    Tabs: { defaultProps: { keepMounted: false } },
  },
})
