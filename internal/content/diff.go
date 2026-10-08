package content

import (
	"fmt"
	"regexp"
	"strings"
)

var blockEndRe = regexp.MustCompile(`(</(?:p|h[1-6]|li|tr|table|ul|ol|blockquote|pre|ac:structured-macro|ac:layout-section|ac:layout-cell|ac:task)>|<br\s*/?>|<hr\s*/?>)`)

// Lines splits storage into reviewable lines at block boundaries. It is for
// display only; nothing written to Confluence goes through it.
func Lines(storage string) []string {
	s := blockEndRe.ReplaceAllString(storage, "$1\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// Diff renders a unified line diff between two documents, with context lines.
// Very large inputs fall back to a summary rather than an expensive diff.
func Diff(before, after string, context int) (string, map[string]int) {
	a, b := Lines(before), Lines(after)
	stats := map[string]int{"before": len(a), "after": len(b)}
	if len(a)*len(b) > 4_000_000 {
		stats["added"], stats["removed"] = len(b), len(a)
		return fmt.Sprintf("@@ 문서가 커서 줄 단위 비교를 생략했습니다 (이전 %d줄, 이후 %d줄) @@", len(a), len(b)), stats
	}
	// LCS table.
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		kind byte
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	var sb strings.Builder
	lastPrinted := -1
	for k, o := range ops {
		switch o.kind {
		case '+':
			stats["added"]++
		case '-':
			stats["removed"]++
		}
		near := o.kind != ' '
		if !near {
			for d := 1; d <= context; d++ {
				if (k-d >= 0 && ops[k-d].kind != ' ') || (k+d < len(ops) && ops[k+d].kind != ' ') {
					near = true
					break
				}
			}
		}
		if !near {
			continue
		}
		if lastPrinted >= 0 && k-lastPrinted > 1 {
			sb.WriteString("@@ … @@\n")
		}
		sb.WriteByte(o.kind)
		sb.WriteByte(' ')
		sb.WriteString(o.text)
		sb.WriteByte('\n')
		lastPrinted = k
	}
	if sb.Len() == 0 {
		return "(변경 없음)", stats
	}
	return sb.String(), stats
}
