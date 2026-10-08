package content

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
)

// ErrUnsupported is returned when a change would lose structure confmcp
// cannot reproduce (macros, layouts, mentions, attachment references).
var ErrUnsupported = errors.New("UNSUPPORTED_CONTENT")

// Inventory counts the structures a careless rewrite would destroy.
type Inventory map[string]int

var (
	macroNameRe = regexp.MustCompile(`<ac:(?:structured-)?macro\b[^>]*\bac:name="([^"]*)"`)
	structRes   = map[string]*regexp.Regexp{
		"layout":     regexp.MustCompile(`<ac:layout\b`),
		"mention":    regexp.MustCompile(`<ri:user\b`),
		"attachment": regexp.MustCompile(`<ri:attachment\b`),
		"page-link":  regexp.MustCompile(`<ri:(?:page|blog-post)\b`),
		"image":      regexp.MustCompile(`<ac:image\b`),
		"task":       regexp.MustCompile(`<ac:task\b`),
		"emoticon":   regexp.MustCompile(`<ac:emoticon\b`),
		"placeholder": regexp.MustCompile(`<ac:placeholder\b`),
	}
)

// Take builds the inventory of a storage document.
func Take(storage string) Inventory {
	inv := Inventory{}
	for _, m := range macroNameRe.FindAllStringSubmatch(storage, -1) {
		inv["macro:"+strings.ToLower(m[1])]++
	}
	for k, re := range structRes {
		if n := len(re.FindAllStringIndex(storage, -1)); n > 0 {
			inv[k] = n
		}
	}
	return inv
}

// Lost lists what is in before but less of in after.
func Lost(before, after Inventory) []string {
	var out []string
	for k, n := range before {
		if after[k] < n {
			out = append(out, fmt.Sprintf("%s %d개 → %d개", k, n, after[k]))
		}
	}
	sort.Strings(out)
	return out
}

// HasStructure reports whether storage holds macros, layouts, mentions or
// references. Such a document is never replaced wholesale from Markdown, even
// when the Markdown could express part of it.
func HasStructure(storage string) bool {
	return len(Take(storage)) > 0
}

// segment is a piece of raw storage: markup or text.
type segment struct {
	text   string
	markup bool
}

// split cuts raw storage into markup (tags, CDATA, comments) and text runs
// without parsing or re-serialising anything, so joining the segments gives
// back the input byte for byte.
func split(storage string) []segment {
	var out []segment
	i := 0
	for i < len(storage) {
		lt := strings.IndexByte(storage[i:], '<')
		if lt < 0 {
			out = append(out, segment{text: storage[i:]})
			break
		}
		if lt > 0 {
			out = append(out, segment{text: storage[i : i+lt]})
		}
		i += lt
		end := -1
		switch {
		case strings.HasPrefix(storage[i:], "<![CDATA["):
			if k := strings.Index(storage[i:], "]]>"); k >= 0 {
				end = i + k + 3
			}
		case strings.HasPrefix(storage[i:], "<!--"):
			if k := strings.Index(storage[i:], "-->"); k >= 0 {
				end = i + k + 3
			}
		default:
			end = tagEnd(storage, i)
		}
		if end < 0 {
			out = append(out, segment{text: storage[i:], markup: true})
			break
		}
		out = append(out, segment{text: storage[i:end], markup: true})
		i = end
	}
	return out
}

// tagEnd finds the end of a tag starting at i, respecting quoted attributes.
func tagEnd(s string, i int) int {
	quote := byte(0)
	for k := i + 1; k < len(s); k++ {
		c := s[k]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return k + 1
		}
	}
	return -1
}

// ReplaceText replaces literal text in text runs only. Tags, attributes,
// CDATA (code macro bodies) and comments are untouched, so macros survive.
// It returns the new storage and the number of replacements.
func ReplaceText(storage, find, replace string, all bool) (string, int) {
	if find == "" {
		return storage, 0
	}
	segs := split(storage)
	var sb strings.Builder
	count := 0
	for _, s := range segs {
		if s.markup || (!all && count > 0) {
			sb.WriteString(s.text)
			continue
		}
		decoded := html.UnescapeString(s.text)
		if !strings.Contains(decoded, find) {
			sb.WriteString(s.text)
			continue
		}
		n := strings.Count(decoded, find)
		if all {
			decoded = strings.ReplaceAll(decoded, find, replace)
			count += n
		} else {
			decoded = strings.Replace(decoded, find, replace, 1)
			count++
		}
		sb.WriteString(EscapeText(decoded))
	}
	return sb.String(), count
}

var headingOpenRe = regexp.MustCompile(`^<h([1-6])(\s[^>]*)?>$`)

// Section locates the region under a heading whose text equals title: from
// just after the closing heading tag to the next heading of the same or a
// higher level within the same parent element, or the parent's end.
func Section(storage, title string) (start, end int, err error) {
	segs := split(storage)
	offsets := make([]int, len(segs)+1)
	for k, s := range segs {
		offsets[k+1] = offsets[k] + len(s.text)
	}
	want := strings.TrimSpace(title)
	depth := 0
	found := -1
	level, foundDepth := 0, 0
	for k := 0; k < len(segs); k++ {
		s := segs[k]
		if !s.markup {
			continue
		}
		t := s.text
		switch {
		case strings.HasPrefix(t, "<![CDATA[") || strings.HasPrefix(t, "<!--") || strings.HasSuffix(t, "/>"):
			continue
		case voidElements[tagName(t)]:
			continue
		case strings.HasPrefix(t, "</"):
			if found >= 0 && depth == foundDepth {
				// Leaving the heading's parent ends the section.
				return found, offsets[k], nil
			}
			depth--
			continue
		}
		if m := headingOpenRe.FindStringSubmatch(t); m != nil {
			lv := int(m[1][0] - '0')
			if found >= 0 && depth == foundDepth && lv <= level {
				return found, offsets[k], nil
			}
			if found < 0 {
				// Collect the heading's text up to its closing tag.
				close := "</h" + m[1] + ">"
				var text strings.Builder
				j := k + 1
				for ; j < len(segs); j++ {
					if segs[j].markup && strings.EqualFold(segs[j].text, close) {
						break
					}
					if !segs[j].markup {
						text.WriteString(segs[j].text)
					}
				}
				if j < len(segs) && strings.TrimSpace(html.UnescapeString(text.String())) == want {
					found, level, foundDepth = offsets[j+1], lv, depth
					k = j
					continue
				}
			}
		}
		depth++
	}
	if found >= 0 {
		return found, len(storage), nil
	}
	return 0, 0, fmt.Errorf("제목이 %q 인 구역을 찾을 수 없습니다", title)
}

// Mode names how an update changes an existing document.
const (
	ModeAppend         = "append"          // add Markdown at the end
	ModePrepend        = "prepend"         // add Markdown at the start
	ModeReplaceText    = "replace_text"    // literal text substitution in text runs
	ModeReplaceSection = "replace_section" // replace the content under one heading
	ModeReplaceStorage = "replace_storage" // full storage supplied by the caller
	ModeReplaceMarkdown = "replace_markdown" // full Markdown, only for documents without structure
)

// UpdateInput describes one update.
type UpdateInput struct {
	Mode          string
	Body          string // Markdown, or storage for replace_storage
	Find          string
	ReplaceAll    bool
	Section       string
	BodyFormat    string // markdown | storage (for replace_section)
	AllowRemovals bool
}

// UpdateResult is the new storage and what reviewers should know about it.
type UpdateResult struct {
	Storage  string   `json:"-"`
	Warnings []string `json:"warnings,omitempty"`
	Changes  int      `json:"changes"`
}

// ApplyUpdate computes the new storage for an existing document. It never
// converts the existing document to Markdown and back: every mode either adds
// new content beside the original or edits the original in place.
func ApplyUpdate(old string, in UpdateInput) (UpdateResult, error) {
	var res UpdateResult
	switch in.Mode {
	case ModeAppend:
		if strings.TrimSpace(in.Body) == "" {
			return res, errors.New("추가할 본문(body)이 비어 있습니다")
		}
		res.Storage, res.Changes = old+FromMarkdown(in.Body), 1
	case ModePrepend:
		if strings.TrimSpace(in.Body) == "" {
			return res, errors.New("추가할 본문(body)이 비어 있습니다")
		}
		res.Storage, res.Changes = FromMarkdown(in.Body)+old, 1
	case ModeReplaceText:
		if in.Find == "" {
			return res, errors.New("replace_text 에는 find 가 필요합니다")
		}
		out, n := ReplaceText(old, in.Find, in.Body, in.ReplaceAll)
		if n == 0 {
			return res, fmt.Errorf("본문에서 %q 를 찾을 수 없습니다 (매크로·코드 블록 내부는 바꾸지 않습니다)", in.Find)
		}
		res.Storage, res.Changes = out, n
	case ModeReplaceSection:
		start, end, err := Section(old, in.Section)
		if err != nil {
			return res, err
		}
		region := old[start:end]
		body := in.Body
		if in.BodyFormat != "storage" {
			body = FromMarkdown(in.Body)
		}
		if lost := Lost(Take(region), Take(body)); len(lost) > 0 {
			if !in.AllowRemovals {
				return res, fmt.Errorf("%w: 바꾸려는 구역에 보존할 수 없는 요소가 있습니다 (%s). storage 형식으로 보내거나 allowRemovals 로 제거를 명시하십시오",
					ErrUnsupported, strings.Join(lost, ", "))
			}
			res.Warnings = append(res.Warnings, "제거되는 요소: "+strings.Join(lost, ", "))
		}
		res.Storage, res.Changes = old[:start]+body+old[end:], 1
	case ModeReplaceStorage:
		if _, err := Parse(in.Body); err != nil {
			return res, err
		}
		if lost := Lost(Take(old), Take(in.Body)); len(lost) > 0 {
			if !in.AllowRemovals {
				return res, fmt.Errorf("%w: 새 본문에서 기존 요소가 사라집니다 (%s). 의도한 삭제라면 allowRemovals 를 true 로 보내십시오",
					ErrUnsupported, strings.Join(lost, ", "))
			}
			res.Warnings = append(res.Warnings, "제거되는 요소: "+strings.Join(lost, ", "))
		}
		res.Storage, res.Changes = in.Body, 1
	case ModeReplaceMarkdown:
		if HasStructure(old) {
			return res, fmt.Errorf("%w: 매크로·레이아웃·멘션·첨부 참조가 있는 문서는 Markdown 으로 전체를 덮어쓸 수 없습니다. "+
				"append, replace_text, replace_section 또는 replace_storage 를 사용하십시오", ErrUnsupported)
		}
		res.Storage, res.Changes = FromMarkdown(in.Body), 1
	default:
		return res, fmt.Errorf("알 수 없는 수정 방식입니다: %q", in.Mode)
	}
	if _, err := Parse(res.Storage); err != nil {
		return res, err
	}
	return res, nil
}

// tagName extracts the lowercased element name of an opening tag.
func tagName(tag string) string {
	t := strings.TrimPrefix(strings.TrimPrefix(tag, "<"), "/")
	end := strings.IndexAny(t, " \t\r\n/>")
	if end >= 0 {
		t = t[:end]
	}
	return strings.ToLower(t)
}
