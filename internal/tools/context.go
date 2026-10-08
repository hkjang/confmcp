package tools

import (
	"context"

	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/permission"
)

// contextTools assemble allowed source material in one call. They never call
// an LLM: summarising is the client's job.
func contextTools() []Definition {
	return []Definition{
		{
			Name:  "confluence_page_context",
			Title: "페이지 컨텍스트",
			Description: "페이지 본문과, 요청자가 볼 수 있는 상위 경로·하위 페이지·댓글·첨부 메타데이터·라벨을 한 번에 모읍니다. " +
				"include 로 항목을 고르고 budget(문자 수)으로 전체 크기를 제한합니다.",
			Group:          "context",
			Risk:           RiskRead,
			RequiredPerm:   "read",
			Scope:          ScopeRead,
			Priority:       "P0",
			DefaultEnabled: true,
			HighLevel:      true,
			InputSchema: obj(map[string]any{
				"pageId":  str("페이지 ID"),
				"include": strArray("포함 항목: ancestors, children, comments, attachments, labels (기본 전체)"),
				"budget":  num("전체 문자 수 한도 (기본 80000)"),
			}, "pageId"),
			Resolve: contentTarget("pageId"),
			Handle:  pageContext,
		},
		{
			Name:  "confluence_space_context",
			Title: "공간 컨텍스트",
			Description: "공간 개요, 홈 페이지 요약 본문, 요청자가 볼 수 있는 페이지 트리(깊이·노드 수 제한)를 한 번에 모읍니다.",
			Group:          "context",
			Risk:           RiskRead,
			RequiredPerm:   "read",
			Scope:          ScopeRead,
			Priority:       "P0",
			DefaultEnabled: true,
			HighLevel:      true,
			InputSchema: obj(map[string]any{
				"spaceKey": str("공간 키"),
				"budget":   num("전체 문자 수 한도 (기본 80000)"),
				"depth":    num("트리 깊이 (기본 3, 최대 관리자 설정)"),
			}, "spaceKey"),
			Resolve: spaceTarget,
			Handle:  spaceContext,
		},
	}
}

func (c *Call) budget() int {
	b := c.Args.OptInt("budget", c.Limits.ContextChars)
	if b <= 0 || b > c.Limits.ContextChars {
		b = c.Limits.ContextChars
	}
	return b
}

func includes(c *Call, what string) bool {
	list := c.Args.StringSlice("include")
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == what {
			return true
		}
	}
	return false
}

func pageContext(ctx context.Context, c *Call) (any, error) {
	ct, err := c.Adapter.Content(ctx, c.Cred, c.Resolved.ContentID, "body.storage", "space", "version", "ancestors", "metadata.labels")
	if err != nil {
		return nil, err
	}
	budget := c.budget()
	docs := 1
	bodyShare := budget * 6 / 10
	body, warnings, err := c.body(ct, "markdown", 0, bodyShare)
	if err != nil {
		return nil, err
	}
	used := len([]rune(body["content"].(string)))
	out := map[string]any{"page": c.item(ct), "body": body}
	sources := []Source{c.source(ct)}

	if includes(c, "ancestors") && len(ct.Ancestors) > 0 {
		checks := make([]permission.Check, 0, len(ct.Ancestors))
		for _, a := range ct.Ancestors {
			checks = append(checks, permission.Check{Target: permission.ContentTarget(a.ID), Ops: []permission.Op{permission.OpRead}})
		}
		path := []map[string]any{}
		if decs, err := c.exec.Resolver.Batch(ctx, c.Principal.Subject(), checks); err == nil {
			for i, a := range ct.Ancestors {
				if decs[i].Allowed(permission.OpRead) {
					path = append(path, map[string]any{"id": a.ID, "title": a.Title})
				} else {
					path = append(path, map[string]any{"title": "(볼 수 없는 상위 문서)"})
				}
			}
		}
		out["ancestors"] = path
	}
	if includes(c, "children") && docs < c.Limits.ContextDocs {
		children, _, _, unknown, err := c.collect(ctx, func(ctx context.Context, s, l int) ([]confluence.Content, confluence.Page, error) {
			return c.Adapter.Children(ctx, c.Cred, ct.ID, s, l)
		}, 0, c.Limits.ContextDocs)
		if err == nil {
			list, srcs := c.items(children)
			out["children"] = list
			sources = append(sources, srcs...)
			warnings = append(warnings, unknownWarning(unknown))
		}
	}
	if includes(c, "comments") && used < budget {
		comments, _, err := c.Adapter.Comments(ctx, c.Cred, ct.ID, 0, 25)
		if err == nil {
			list := []map[string]any{}
			for i := range comments {
				remaining := budget - used
				if remaining <= 200 {
					warnings = append(warnings, "예산을 넘어 일부 댓글을 생략했습니다")
					break
				}
				max := remaining
				if max > 2000 {
					max = 2000
				}
				b, _, err := c.body(&comments[i], "markdown", 0, max)
				if err != nil {
					continue
				}
				text := b["content"].(string)
				used += len([]rune(text))
				list = append(list, map[string]any{"id": comments[i].ID, "parentCommentId": comments[i].ParentCommentID,
					"author": comments[i].CreatedBy, "createdAt": comments[i].CreatedAt, "body": text})
			}
			out["comments"] = list
		}
	}
	if includes(c, "attachments") {
		if atts, _, err := c.Adapter.Attachments(ctx, c.Cred, ct.ID, 0, 50); err == nil {
			list := []map[string]any{}
			for _, a := range atts {
				list = append(list, map[string]any{"id": a.ID, "filename": a.Title, "mediaType": a.MediaType, "fileSize": a.FileSize})
			}
			out["attachments"] = list
		}
	}
	if includes(c, "labels") {
		out["labels"] = ct.Labels
	}
	if err := c.recheck(ctx, ct.ID); err != nil {
		return nil, err
	}
	out["budget"] = map[string]int{"limit": budget, "used": used}
	return c.result(out, sources, nil, warnings...), nil
}

type treeNode struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	URL      string      `json:"url,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
	More     bool        `json:"more,omitempty"`
}

func spaceContext(ctx context.Context, c *Call) (any, error) {
	sp, err := c.Adapter.Space(ctx, c.Cred, c.Resolved.SpaceKey)
	if err != nil {
		return nil, err
	}
	budget := c.budget()
	depth := c.Args.OptInt("depth", 3)
	if depth <= 0 || depth > c.Limits.TreeDepth {
		depth = c.Limits.TreeDepth
	}
	out := map[string]any{"space": map[string]any{"key": sp.Key, "name": sp.Name, "description": sp.Description, "url": sp.WebURL}}
	sources := []Source{{SpaceKey: sp.Key, Title: sp.Name, URL: sp.WebURL, Type: "space"}}
	var warnings []string

	var roots []confluence.Content
	if sp.Homepage != nil {
		if home, err := c.Adapter.Content(ctx, c.Cred, sp.Homepage.ID, "body.storage", "space", "version", "ancestors"); err == nil {
			if kept, _, err := c.visible(ctx, []confluence.Content{*home}); err == nil && len(kept) == 1 {
				body, w, err := c.body(home, "markdown", 0, budget/3)
				if err == nil {
					out["homepage"] = map[string]any{"page": c.item(home), "body": body}
					sources = append(sources, c.source(home))
					warnings = append(warnings, w...)
				}
				roots = []confluence.Content{*home}
			}
		}
	}
	if roots == nil {
		// No visible homepage: use top-level pages of the space.
		items, _, _, _, err := c.collect(ctx, func(ctx context.Context, s, l int) ([]confluence.Content, confluence.Page, error) {
			list, page, err := c.Adapter.ContentList(ctx, c.Cred, confluence.ContentQuery{Type: "page", SpaceKey: sp.Key, Start: s, Limit: l})
			top := list[:0]
			for _, it := range list {
				if len(it.Ancestors) == 0 {
					top = append(top, it)
				}
			}
			return top, page, err
		}, 0, c.Limits.ContextDocs)
		if err == nil {
			roots = items
		}
	}

	nodes := 0
	unknown := 0
	var walk func(ct confluence.Content, level int) *treeNode
	walk = func(ct confluence.Content, level int) *treeNode {
		nodes++
		n := &treeNode{ID: ct.ID, Title: ct.Title, URL: c.Adapter.WebURL(ct.WebPath)}
		if level >= depth || nodes >= c.Limits.TreeNodes {
			return n
		}
		children, _, more, unk, err := c.collect(ctx, func(ctx context.Context, s, l int) ([]confluence.Content, confluence.Page, error) {
			return c.Adapter.Children(ctx, c.Cred, ct.ID, s, l)
		}, 0, 50)
		if err != nil {
			return n
		}
		unknown += unk
		n.More = more
		for _, ch := range children {
			if nodes >= c.Limits.TreeNodes {
				n.More = true
				break
			}
			n.Children = append(n.Children, walk(ch, level+1))
		}
		return n
	}
	tree := []*treeNode{}
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r.ID] || nodes >= c.Limits.TreeNodes {
			continue
		}
		seen[r.ID] = true
		tree = append(tree, walk(r, 0))
	}
	if nodes >= c.Limits.TreeNodes {
		warnings = append(warnings, "트리 노드 한도에 도달해 일부만 표시합니다")
	}
	warnings = append(warnings, unknownWarning(unknown))
	out["tree"] = tree
	out["limits"] = map[string]int{"depth": depth, "nodes": nodes, "maxNodes": c.Limits.TreeNodes}
	return c.result(out, sources, nil, warnings...), nil
}
