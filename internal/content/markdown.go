package content

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Loss reports something the readable conversion could not carry over.
type Loss struct {
	Kind  string `json:"kind"`  // macro | layout | mention | image | reference
	Name  string `json:"name"`  // macro name or element
	Count int    `json:"count"`
	Note  string `json:"note,omitempty"`
}

// Converted is a readable rendering of storage.
type Converted struct {
	Text   string `json:"text"`
	Format string `json:"format"`
	Lossy  bool   `json:"lossy"`
	Losses []Loss `json:"losses,omitempty"`
}

// macros whose body is ordinary rich text and can be shown inline.
var panelMacros = map[string]string{
	"info": "정보", "note": "참고", "warning": "경고", "tip": "팁", "panel": "패널",
	"section": "", "column": "", "details": "", "expand": "펼치기",
}

// referenceMacros pull other content in. Their target is never expanded and
// never named, because the requester may not be allowed to see it.
var referenceMacros = map[string]bool{
	"include": true, "excerpt-include": true, "children": true, "pagetree": true,
	"content-by-label": true, "contentbylabel": true, "recently-updated": true,
	"blog-posts": true, "page-index": true, "jira": true, "jiraissues": true,
	"livesearch": true, "contributors": true, "space-details": true, "attachments": true,
	"viewfile": true, "view-file": true, "multimedia": true, "widget": true, "html": true,
	"html-include": true, "rss": true, "iframe": true,
}

type converter struct {
	plain  bool
	losses map[string]*Loss
	out    strings.Builder
}

func (c *converter) loss(kind, name, note string) {
	if c.losses == nil {
		c.losses = map[string]*Loss{}
	}
	key := kind + ":" + name
	if l, ok := c.losses[key]; ok {
		l.Count++
		return
	}
	c.losses[key] = &Loss{Kind: kind, Name: name, Count: 1, Note: note}
}

// ToMarkdown renders storage as Markdown, or as plain text when plain is set.
// Macros are never executed: code and panels are shown inline, everything
// else becomes a marker, and the losses are reported.
func ToMarkdown(storage string, plain bool) (Converted, error) {
	root, err := Parse(storage)
	if err != nil {
		return Converted{}, err
	}
	c := &converter{plain: plain}
	c.blocks(root.Children, "")
	text := tidy(c.out.String())
	out := Converted{Text: text, Format: "markdown"}
	if plain {
		out.Format = "text"
	}
	for _, l := range c.losses {
		out.Losses = append(out.Losses, *l)
	}
	sort.Slice(out.Losses, func(i, j int) bool {
		if out.Losses[i].Kind != out.Losses[j].Kind {
			return out.Losses[i].Kind < out.Losses[j].Kind
		}
		return out.Losses[i].Name < out.Losses[j].Name
	})
	out.Lossy = len(out.Losses) > 0
	return out, nil
}

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isBlock(name string) bool {
	switch name {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "table", "pre", "blockquote", "hr", "div",
		"ac:structured-macro", "ac:macro", "ac:layout", "ac:layout-section", "ac:layout-cell", "ac:task-list",
		"ac:rich-text-body", "section", "article":
		return true
	}
	return false
}

// blocks renders a sequence of nodes as block content with a line prefix
// (used for blockquotes and nested panels).
func (c *converter) blocks(nodes []*Node, prefix string) {
	var inline []*Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		text := strings.TrimSpace(c.inlineText(inline))
		inline = nil
		if text != "" {
			c.para(text, prefix)
		}
	}
	for _, n := range nodes {
		if n.Name == "" || !isBlock(n.Name) {
			inline = append(inline, n)
			continue
		}
		flush()
		c.block(n, prefix)
	}
	flush()
}

func (c *converter) para(text, prefix string) {
	for _, line := range strings.Split(text, "\n") {
		c.out.WriteString(prefix + line + "\n")
	}
	c.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
}

func (c *converter) block(n *Node, prefix string) {
	switch n.Name {
	case "p", "div", "section", "article":
		c.blocks(n.Children, prefix)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(n.Name[1] - '0')
		text := strings.TrimSpace(c.inlineText(n.Children))
		if c.plain {
			c.para(text, prefix)
		} else {
			c.para(strings.Repeat("#", level)+" "+text, prefix)
		}
	case "hr":
		c.para("---", prefix)
	case "ul", "ol":
		c.list(n, prefix, 0)
		c.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case "ac:task-list":
		for _, t := range n.Children {
			if t.Name != "ac:task" {
				continue
			}
			done := strings.EqualFold(strings.TrimSpace(t.Child("ac:task-status").InnerTextSafe()), "complete")
			body := t.Child("ac:task-body")
			text := ""
			if body != nil {
				text = strings.TrimSpace(c.inlineText(body.Children))
			}
			mark := "[ ]"
			if done {
				mark = "[x]"
			}
			c.out.WriteString(prefix + "- " + mark + " " + text + "\n")
		}
		c.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case "pre":
		c.fence("", n.InnerText(), prefix)
	case "blockquote":
		c.blocks(n.Children, prefix+"> ")
	case "table":
		c.table(n, prefix)
	case "ac:layout":
		c.loss("layout", "ac:layout", "레이아웃 구획은 순서대로 펼쳐 표시합니다")
		for _, sec := range n.Children {
			if sec.Name != "ac:layout-section" {
				continue
			}
			for _, cell := range sec.Children {
				if cell.Name == "ac:layout-cell" {
					c.blocks(cell.Children, prefix)
				}
			}
		}
	case "ac:layout-section", "ac:layout-cell", "ac:rich-text-body":
		c.blocks(n.Children, prefix)
	case "ac:structured-macro", "ac:macro":
		c.macro(n, prefix)
	default:
		c.blocks(n.Children, prefix)
	}
}

// InnerTextSafe is InnerText that tolerates a nil node.
func (n *Node) InnerTextSafe() string {
	if n == nil {
		return ""
	}
	return n.InnerText()
}

func (c *converter) fence(lang, code, prefix string) {
	code = strings.Trim(code, "\n")
	if c.plain {
		c.para(code, prefix)
		return
	}
	ticks := "```"
	for strings.Contains(code, ticks) {
		ticks += "`"
	}
	c.out.WriteString(prefix + ticks + lang + "\n")
	for _, l := range strings.Split(code, "\n") {
		c.out.WriteString(prefix + l + "\n")
	}
	c.out.WriteString(prefix + ticks + "\n" + strings.TrimRight(prefix, " ") + "\n")
}

func (c *converter) macro(n *Node, prefix string) {
	name := strings.ToLower(n.Attr("ac:name"))
	switch {
	case name == "code" || name == "noformat":
		body := n.Child("ac:plain-text-body")
		c.fence(n.Param("language"), body.InnerTextSafe(), prefix)
	case panelMacros[name] != "" || name == "section" || name == "column" || name == "details":
		label := panelMacros[name]
		if title := n.Param("title"); title != "" {
			if label != "" {
				label += ": " + title
			} else {
				label = title
			}
		}
		inner := prefix
		if label != "" {
			inner = prefix + "> "
			if c.plain {
				c.para("["+label+"]", inner)
			} else {
				c.para("**["+label+"]**", inner)
			}
		}
		if body := n.Child("ac:rich-text-body"); body != nil {
			c.blocks(body.Children, inner)
		}
	case name == "toc" || name == "toc-zone":
		c.loss("macro", name, "목차는 표시하지 않습니다")
	case name == "status":
		c.para("[상태: "+n.Param("title")+"]", prefix)
	case name == "anchor":
		// Invisible.
	case name == "excerpt":
		if body := n.Child("ac:rich-text-body"); body != nil {
			c.blocks(body.Children, prefix)
		}
	case referenceMacros[name]:
		c.loss("reference", name, "다른 콘텐츠를 가져오는 매크로는 확장하지 않으며 대상도 표시하지 않습니다")
		c.para(fmt.Sprintf("[매크로: %s — 내용은 확장되지 않음]", name), prefix)
	default:
		c.loss("macro", name, "지원하지 않는 매크로입니다")
		if body := n.Child("ac:rich-text-body"); body != nil {
			c.para(fmt.Sprintf("[매크로: %s]", name), prefix)
			c.blocks(body.Children, prefix)
		} else {
			c.para(fmt.Sprintf("[매크로: %s]", name), prefix)
		}
	}
}

func (c *converter) list(n *Node, prefix string, depth int) {
	ordered := n.Name == "ol"
	idx := 0
	indent := strings.Repeat("  ", depth)
	for _, li := range n.Children {
		if li.Name != "li" {
			continue
		}
		idx++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", idx)
		}
		var inline []*Node
		var nested []*Node
		for _, ch := range li.Children {
			if ch.Name == "ul" || ch.Name == "ol" {
				nested = append(nested, ch)
			} else if ch.Name == "p" {
				inline = append(inline, ch.Children...)
				inline = append(inline, &Node{Text: " "})
			} else {
				inline = append(inline, ch)
			}
		}
		text := strings.TrimSpace(c.inlineText(inline))
		text = strings.ReplaceAll(text, "\n", " ")
		c.out.WriteString(prefix + indent + marker + text + "\n")
		for _, sub := range nested {
			c.list(sub, prefix, depth+1)
		}
	}
}

func (c *converter) table(n *Node, prefix string) {
	var rows [][]string
	header := false
	var collect func(*Node)
	collect = func(x *Node) {
		for _, ch := range x.Children {
			switch ch.Name {
			case "thead", "tbody", "tfoot":
				collect(ch)
			case "tr":
				var row []string
				for _, cell := range ch.Children {
					if cell.Name != "td" && cell.Name != "th" {
						continue
					}
					if cell.Name == "th" && len(rows) == 0 {
						header = true
					}
					sub := &converter{plain: true}
					sub.blocks(cell.Children, "")
					for k, v := range sub.losses {
						if c.losses == nil {
							c.losses = map[string]*Loss{}
						}
						if l, ok := c.losses[k]; ok {
							l.Count += v.Count
						} else {
							cp := *v
							c.losses[k] = &cp
						}
					}
					text := tidy(sub.out.String())
					text = strings.ReplaceAll(text, "\n", " <br> ")
					text = strings.ReplaceAll(text, "|", "\\|")
					row = append(row, text)
				}
				rows = append(rows, row)
			}
		}
	}
	collect(n)
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], "")
		}
	}
	if c.plain {
		for _, r := range rows {
			c.out.WriteString(prefix + strings.Join(r, " | ") + "\n")
		}
		c.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
		return
	}
	start := 0
	if header {
		c.out.WriteString(prefix + "| " + strings.Join(rows[0], " | ") + " |\n")
		start = 1
	} else {
		empty := make([]string, width)
		c.out.WriteString(prefix + "| " + strings.Join(empty, " | ") + " |\n")
	}
	sep := make([]string, width)
	for i := range sep {
		sep[i] = "---"
	}
	c.out.WriteString(prefix + "| " + strings.Join(sep, " | ") + " |\n")
	for _, r := range rows[start:] {
		c.out.WriteString(prefix + "| " + strings.Join(r, " | ") + " |\n")
	}
	c.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
}

// inlineText renders inline nodes.
func (c *converter) inlineText(nodes []*Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(c.inline(n))
	}
	return collapseSpaces(sb.String())
}

func collapseSpaces(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range s {
		if r == '\n' {
			sb.WriteRune(r)
			space = false
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == ' ' {
			if !space {
				sb.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}

func (c *converter) wrap(mark string, inner string) string {
	if c.plain || strings.TrimSpace(inner) == "" {
		return inner
	}
	lead := inner[:len(inner)-len(strings.TrimLeft(inner, " "))]
	trail := inner[len(strings.TrimRight(inner, " ")):]
	return lead + mark + strings.TrimSpace(inner) + mark + trail
}

func (c *converter) inline(n *Node) string {
	if n.Name == "" {
		if c.plain {
			return n.Text
		}
		return escapeMarkdown(n.Text)
	}
	switch n.Name {
	case "br":
		return "\n"
	case "strong", "b":
		return c.wrap("**", c.inlineText(n.Children))
	case "em", "i":
		return c.wrap("_", c.inlineText(n.Children))
	case "s", "del", "strike":
		return c.wrap("~~", c.inlineText(n.Children))
	case "u", "span", "sup", "sub", "small", "big", "font", "time":
		if n.Name == "time" && n.Attr("datetime") != "" {
			return n.Attr("datetime")
		}
		return c.inlineText(n.Children)
	case "code":
		t := n.InnerText()
		if c.plain {
			return t
		}
		return "`" + strings.ReplaceAll(t, "`", "'") + "`"
	case "a":
		text := strings.TrimSpace(c.inlineText(n.Children))
		href := n.Attr("href")
		if c.plain || !safeHref(href) {
			return text
		}
		if text == "" {
			text = href
		}
		return "[" + text + "](" + href + ")"
	case "ac:link":
		body := n.Child("ac:link-body")
		plain := n.Child("ac:plain-text-link-body")
		text := strings.TrimSpace(body.InnerTextSafe() + plain.InnerTextSafe())
		switch {
		case n.Child("ri:user") != nil:
			c.loss("mention", "ri:user", "사용자 멘션은 이름 없이 표시합니다")
			return "@사용자"
		case n.Child("ri:attachment") != nil:
			name := n.Child("ri:attachment").Attr("ri:filename")
			if text == "" {
				text = name
			}
			return "[첨부: " + text + "]"
		case n.Child("ri:page") != nil || n.Child("ri:blog-post") != nil:
			// The target title is not shown unless the author wrote link text:
			// the requester may not be allowed to see the target.
			c.loss("reference", "page-link", "다른 페이지 링크는 대상 제목을 표시하지 않습니다")
			if text == "" {
				return "[페이지 링크]"
			}
			return text + " [페이지 링크]"
		case n.Attr("ac:anchor") != "":
			if text == "" {
				text = "#" + n.Attr("ac:anchor")
			}
			return text
		default:
			return text
		}
	case "ac:image":
		if att := n.Child("ri:attachment"); att != nil {
			c.loss("image", "ac:image", "이미지는 파일 이름만 표시합니다")
			return "[이미지: " + att.Attr("ri:filename") + "]"
		}
		c.loss("image", "ac:image", "이미지는 파일 이름만 표시합니다")
		return "[이미지]"
	case "ac:emoticon":
		return ""
	case "ac:structured-macro", "ac:macro":
		name := strings.ToLower(n.Attr("ac:name"))
		switch name {
		case "status":
			return "[상태: " + n.Param("title") + "]"
		case "anchor":
			return ""
		case "code", "noformat":
			return "`" + strings.TrimSpace(n.Child("ac:plain-text-body").InnerTextSafe()) + "`"
		}
		if referenceMacros[name] {
			c.loss("reference", name, "다른 콘텐츠를 가져오는 매크로는 확장하지 않으며 대상도 표시하지 않습니다")
		} else {
			c.loss("macro", name, "지원하지 않는 매크로입니다")
		}
		return "[매크로: " + name + "]"
	case "ri:user":
		c.loss("mention", "ri:user", "사용자 멘션은 이름 없이 표시합니다")
		return "@사용자"
	case "ac:placeholder", "ac:parameter":
		return ""
	default:
		if isBlock(n.Name) {
			sub := &converter{plain: c.plain, losses: c.losses}
			sub.blocks([]*Node{n}, "")
			c.losses = sub.losses
			return strings.TrimSpace(sub.out.String())
		}
		return c.inlineText(n.Children)
	}
}

func safeHref(h string) bool {
	l := strings.ToLower(strings.TrimSpace(h))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "mailto:") ||
		(strings.HasPrefix(l, "/") && !strings.HasPrefix(l, "//"))
}

func escapeMarkdown(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "[", "\\[", "]", "\\]")
	return r.Replace(s)
}

// Chunk is a window of a long text.
type Chunk struct {
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	Length     int    `json:"length"`
	TotalChars int    `json:"totalChars"`
	Truncated  bool   `json:"truncated"`
	NextOffset *int   `json:"nextOffset,omitempty"`
}

// Window cuts text by characters (runes), never splitting a character.
func Window(text string, offset, max int) Chunk {
	total := utf8.RuneCountInString(text)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	runes := []rune(text)
	end := total
	if max > 0 && offset+max < total {
		end = offset + max
	}
	out := Chunk{Text: string(runes[offset:end]), Offset: offset, Length: end - offset, TotalChars: total}
	if end < total {
		out.Truncated = true
		n := end
		out.NextOffset = &n
	}
	return out
}
