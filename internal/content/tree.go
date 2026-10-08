// Package content handles Confluence storage format: parsing it safely,
// converting it to readable Markdown or text, building storage from a limited
// Markdown dialect, and changing existing documents without re-generating
// them, so that macros, layouts and references outside the edited region are
// preserved byte for byte.
package content

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// Node is a parsed storage element or text run.
type Node struct {
	Name     string // "" for text; prefix:local for namespaced tags (ac:link)
	Attrs    map[string]string
	Children []*Node
	Text     string
	CDATA    bool
}

// Attr returns an attribute value by its prefixed name (ac:name, ri:filename).
func (n *Node) Attr(name string) string {
	if n == nil || n.Attrs == nil {
		return ""
	}
	return n.Attrs[name]
}

// Child returns the first direct child element with the given name.
func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Param returns an ac:parameter value of a macro.
func (n *Node) Param(name string) string {
	for _, c := range n.Children {
		if c.Name == "ac:parameter" && c.Attr("ac:name") == name {
			return strings.TrimSpace(c.InnerText())
		}
	}
	return ""
}

// InnerText concatenates all descendant text.
func (n *Node) InnerText() string {
	var sb strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Name == "" {
			sb.WriteString(x.Text)
			return
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// voidElements never have children, even when written as <br> rather than <br/>.
var voidElements = map[string]bool{
	"br": true, "hr": true, "img": true, "col": true, "input": true, "meta": true, "link": true,
}

// ErrMalformed means the storage could not be parsed.
var ErrMalformed = errors.New("storage 형식을 해석할 수 없습니다")

// Parse reads storage format into a tree.
//
// encoding/xml never resolves external entities or fetches a DTD, so a
// document cannot make the gateway read local files or reach the network
// (XXE). HTML named entities such as &nbsp; are mapped to their characters.
func Parse(storage string) (*Node, error) {
	src := `<cm-root xmlns:ac="ac" xmlns:ri="ri" xmlns:at="at">` + storage + `</cm-root>`
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	root := &Node{Name: "#root"}
	stack := []*Node{root}
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrMalformed
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			name := qname(t.Name)
			if name == "cm-root" {
				continue
			}
			n := &Node{Name: name, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.Attrs[qname(a.Name)] = a.Value
			}
			top.Children = append(top.Children, n)
			if !voidElements[name] {
				stack = append(stack, n)
			}
		case xml.EndElement:
			name := qname(t.Name)
			if voidElements[name] {
				continue
			}
			// Pop to the matching element; tolerate sloppy nesting.
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].Name == name {
					stack = stack[:i]
					break
				}
			}
		case xml.CharData:
			text := string(t)
			cdata := false
			top.Children = append(top.Children, &Node{Text: text, CDATA: cdata})
		case xml.Comment, xml.ProcInst, xml.Directive:
			// Dropped: comments and directives carry no readable content and a
			// directive is where an entity declaration would live.
		}
	}
	return root, nil
}

func qname(n xml.Name) string {
	if n.Space == "" {
		return strings.ToLower(n.Local)
	}
	return strings.ToLower(n.Space) + ":" + strings.ToLower(n.Local)
}

// EscapeText escapes text for storage.
func EscapeText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	// encoding/xml escapes newlines as &#xA;; storage keeps them literal.
	return strings.ReplaceAll(b.String(), "&#xA;", "\n")
}

// EscapeAttr escapes an attribute value.
func EscapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
