package content

import (
	"regexp"
	"strconv"
	"strings"
)

// SupportedMarkdown documents the dialect FromMarkdown accepts.
const SupportedMarkdown = "제목(#~######), 문단, 굵게(**), 기울임(_ 또는 *), 취소선(~~), 인라인 코드(`), " +
	"링크([텍스트](http/https/mailto URL)), 글머리·번호 목록(2칸 들여쓰기 중첩), 체크 목록(- [ ]), 인용(>), " +
	"코드 블록(``` 언어), 표(| 머리 | 구분선 |), 구분선(---). HTML 태그와 이미지 문법은 텍스트로 처리됩니다."

// FromMarkdown builds storage format from a limited Markdown dialect. Any
// raw HTML is escaped as text, so the output can only contain the elements
// this function writes itself.
func FromMarkdown(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out strings.Builder
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			i++
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence := trimmed[:3]
			lang := strings.TrimSpace(strings.TrimLeft(trimmed, "`~"))
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				code = append(code, lines[i])
				i++
			}
			i++ // closing fence
			out.WriteString(codeMacro(lang, strings.Join(code, "\n")))
		case headingRe.MatchString(trimmed):
			m := headingRe.FindStringSubmatch(trimmed)
			level := len(m[1])
			out.WriteString("<h" + string(rune('0'+level)) + ">" + inlineMD(strings.TrimSpace(m[2])) + "</h" + string(rune('0'+level)) + ">")
			i++
		case hrRe.MatchString(trimmed):
			out.WriteString("<hr />")
			i++
		case strings.HasPrefix(trimmed, ">"):
			var quote []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				q := strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")
				quote = append(quote, strings.TrimPrefix(q, " "))
				i++
			}
			out.WriteString("<blockquote>" + FromMarkdown(strings.Join(quote, "\n")) + "</blockquote>")
		case isTableStart(lines, i):
			n := writeTable(&out, lines[i:])
			i += n
		case listRe.MatchString(line):
			n := writeList(&out, lines[i:])
			i += n
		default:
			var para []string
			for i < len(lines) {
				l := lines[i]
				t := strings.TrimSpace(l)
				if t == "" || headingRe.MatchString(t) || strings.HasPrefix(t, "```") || strings.HasPrefix(t, ">") ||
					hrRe.MatchString(t) || listRe.MatchString(l) || isTableStart(lines, i) {
					break
				}
				para = append(para, t)
				i++
			}
			parts := make([]string, len(para))
			for k, p := range para {
				parts[k] = inlineMD(p)
			}
			out.WriteString("<p>" + strings.Join(parts, "<br />") + "</p>")
		}
	}
	return out.String()
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	hrRe      = regexp.MustCompile(`^(\*\s*){3,}$|^(-\s*){3,}$|^(_\s*){3,}$`)
	listRe    = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	taskRe    = regexp.MustCompile(`^\[( |x|X)\]\s+(.*)$`)
)

func codeMacro(lang, code string) string {
	var sb strings.Builder
	sb.WriteString(`<ac:structured-macro ac:name="code">`)
	if lang != "" && regexp.MustCompile(`^[A-Za-z0-9_+#.-]{1,32}$`).MatchString(lang) {
		sb.WriteString(`<ac:parameter ac:name="language">` + EscapeText(strings.ToLower(lang)) + `</ac:parameter>`)
	}
	// "]]>" cannot appear inside CDATA; split it across two sections.
	safe := strings.ReplaceAll(code, "]]>", "]]]]><![CDATA[>")
	sb.WriteString(`<ac:plain-text-body><![CDATA[` + safe + `]]></ac:plain-text-body></ac:structured-macro>`)
	return sb.String()
}

type listItem struct {
	indent  int
	ordered bool
	text    string
}

func writeList(out *strings.Builder, lines []string) int {
	var items []listItem
	n := 0
	for n < len(lines) {
		m := listRe.FindStringSubmatch(lines[n])
		if m == nil {
			// A continuation line indented under an item joins it.
			if t := strings.TrimSpace(lines[n]); t != "" && len(items) > 0 && strings.HasPrefix(lines[n], "  ") {
				items[len(items)-1].text += " " + t
				n++
				continue
			}
			break
		}
		items = append(items, listItem{
			indent:  len(strings.ReplaceAll(m[1], "\t", "    ")) / 2,
			ordered: !strings.ContainsAny(m[2], "-*+"),
			text:    m[3],
		})
		n++
	}
	renderList(out, items, 0)
	return n
}

// renderList writes items at depth and returns how many it consumed.
func renderList(out *strings.Builder, items []listItem, depth int) int {
	if len(items) == 0 {
		return 0
	}
	tag := "ul"
	if items[0].ordered {
		tag = "ol"
	}
	allTasks := true
	for _, it := range items {
		if it.indent == depth && !taskRe.MatchString(it.text) {
			allTasks = false
			break
		}
	}
	if allTasks && tag == "ul" {
		out.WriteString("<ac:task-list>")
	} else {
		out.WriteString("<" + tag + ">")
	}
	i := 0
	for i < len(items) {
		it := items[i]
		if it.indent < depth {
			break
		}
		if it.indent > depth {
			i += renderList(out, items[i:], depth+1)
			continue
		}
		if allTasks && tag == "ul" {
			m := taskRe.FindStringSubmatch(it.text)
			status := "incomplete"
			if strings.EqualFold(m[1], "x") {
				status = "complete"
			}
			out.WriteString("<ac:task><ac:task-status>" + status + "</ac:task-status><ac:task-body>" +
				inlineMD(m[2]) + "</ac:task-body></ac:task>")
			i++
			continue
		}
		out.WriteString("<li>" + inlineMD(it.text))
		i++
		if i < len(items) && items[i].indent > depth {
			i += renderList(out, items[i:], depth+1)
		}
		out.WriteString("</li>")
	}
	if allTasks && tag == "ul" {
		out.WriteString("</ac:task-list>")
	} else {
		out.WriteString("</" + tag + ">")
	}
	return i
}

var tableSepRe = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)

func isTableStart(lines []string, i int) bool {
	return i+1 < len(lines) && strings.Contains(lines[i], "|") && tableSepRe.MatchString(lines[i+1])
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	for k := 0; k < len(line); k++ {
		if line[k] == '\\' && k+1 < len(line) && line[k+1] == '|' {
			cur.WriteByte('|')
			k++
			continue
		}
		if line[k] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[k])
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func writeTable(out *strings.Builder, lines []string) int {
	header := splitRow(lines[0])
	out.WriteString("<table><tbody><tr>")
	for _, h := range header {
		out.WriteString("<th>" + inlineMD(h) + "</th>")
	}
	out.WriteString("</tr>")
	n := 2
	for n < len(lines) && strings.Contains(lines[n], "|") && strings.TrimSpace(lines[n]) != "" {
		cells := splitRow(lines[n])
		out.WriteString("<tr>")
		for k := range header {
			v := ""
			if k < len(cells) {
				v = cells[k]
			}
			out.WriteString("<td>" + inlineMD(v) + "</td>")
		}
		out.WriteString("</tr>")
		n++
	}
	out.WriteString("</tbody></table>")
	return n
}

var (
	codeSpanRe = regexp.MustCompile("`([^`]+)`")
	linkRe     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	boldRe     = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	italicRe   = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*]*)\*|(^|[^_\w])_([^_\s][^_]*)_`)
	strikeRe   = regexp.MustCompile(`~~([^~]+)~~`)
)

// inlineMD converts inline Markdown. Text is escaped first, so anything that
// looks like HTML stays text; only the markers below become elements.
func inlineMD(s string) string {
	// Protect code spans from further formatting.
	var spans []string
	s = codeSpanRe.ReplaceAllStringFunc(s, func(m string) string {
		spans = append(spans, "<code>"+EscapeText(m[1:len(m)-1])+"</code>")
		return "\uE000c" + strconv.Itoa(len(spans)-1) + "\uE001"
	})
	var links []string
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		text, href := sub[1], sub[2]
		if !safeHref(href) {
			return text
		}
		links = append(links, `<a href="`+EscapeAttr(href)+`">`+EscapeText(text)+`</a>`)
		return "\uE000l" + strconv.Itoa(len(links)-1) + "\uE001"
	})
	s = EscapeText(s)
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := boldRe.FindStringSubmatch(m)
		inner := sub[1]
		if inner == "" {
			inner = sub[2]
		}
		return "<strong>" + inner + "</strong>"
	})
	s = strikeRe.ReplaceAllString(s, "<s>$1</s>")
	s = italicRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := italicRe.FindStringSubmatch(m)
		if sub[2] != "" {
			return sub[1] + "<em>" + sub[2] + "</em>"
		}
		return sub[3] + "<em>" + sub[4] + "</em>"
	})
	for k, v := range links {
		s = strings.Replace(s, "\uE000l"+strconv.Itoa(k)+"\uE001", v, 1)
	}
	for k, v := range spans {
		s = strings.Replace(s, "\uE000c"+strconv.Itoa(k)+"\uE001", v, 1)
	}
	return s
}
