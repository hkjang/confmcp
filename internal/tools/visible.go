package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/content"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
)

// policyTarget builds the policy view of a content item.
func policyTarget(ct *confluence.Content) policy.Target {
	anc := ct.AncestorIDs()
	if ct.Container != nil && ct.Container.ID != "" && (ct.Type == "comment" || ct.Type == "attachment") {
		anc = append(anc, ct.Container.ID)
	}
	return policy.Target{SpaceKey: ct.SpaceKey, ContentID: ct.ID, Ancestors: anc, ContentType: ct.Type}
}

// visible keeps the items the requester may read: first the MCP policy, then
// the requester's own Confluence permission for each remaining item. Items
// whose permission is unknown are dropped and counted, never shown.
func (c *Call) visible(ctx context.Context, items []confluence.Content) ([]confluence.Content, int, error) {
	if len(items) == 0 {
		return items, 0, nil
	}
	candidates := make([]confluence.Content, 0, len(items))
	for i := range items {
		v, err := c.exec.Policy.Evaluate(ctx, policyTarget(&items[i]))
		if err != nil {
			return nil, 0, err
		}
		if v.Allowed && items[i].Status != "trashed" && items[i].Status != "draft" {
			candidates = append(candidates, items[i])
		}
	}
	if len(candidates) == 0 {
		return candidates, 0, nil
	}
	checks := make([]permission.Check, 0, len(candidates))
	for _, it := range candidates {
		checks = append(checks, permission.Check{Target: permission.ContentTarget(it.ID), Ops: []permission.Op{permission.OpRead}})
	}
	decs, err := c.exec.Resolver.Batch(ctx, c.Principal.Subject(), checks)
	if err != nil {
		return nil, 0, err
	}
	out := make([]confluence.Content, 0, len(candidates))
	unknown := 0
	for i, d := range decs {
		switch d.Verdict(permission.OpRead) {
		case permission.Allow:
			// The plugin reports the content's real space; a mismatch means the
			// listing and the permission engine disagree, so drop it.
			if d.SpaceKey != "" && candidates[i].SpaceKey != "" && !strings.EqualFold(d.SpaceKey, candidates[i].SpaceKey) {
				unknown++
				continue
			}
			out = append(out, candidates[i])
		case permission.Unknown:
			unknown++
		}
	}
	return out, unknown, nil
}

// visibleSpaces keeps the spaces the requester may read.
func (c *Call) visibleSpaces(ctx context.Context, spaces []confluence.Space) ([]confluence.Space, int, error) {
	candidates := make([]confluence.Space, 0, len(spaces))
	for _, s := range spaces {
		if c.exec.Policy.SpaceAllowed(ctx, s.Key) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return candidates, 0, nil
	}
	checks := make([]permission.Check, 0, len(candidates))
	for _, s := range candidates {
		checks = append(checks, permission.Check{Target: permission.SpaceTarget(s.Key), Ops: []permission.Op{permission.OpRead}})
	}
	decs, err := c.exec.Resolver.Batch(ctx, c.Principal.Subject(), checks)
	if err != nil {
		return nil, 0, err
	}
	out := make([]confluence.Space, 0, len(candidates))
	unknown := 0
	for i, d := range decs {
		switch d.Verdict(permission.OpRead) {
		case permission.Allow:
			out = append(out, candidates[i])
		case permission.Unknown:
			unknown++
		}
	}
	return out, unknown, nil
}

// recheck asks again, right before content leaves the gateway, whether the
// requester may still read it. Permission caching is off by default, so this
// narrows the window between the first check and the response.
func (c *Call) recheck(ctx context.Context, id string) error {
	d, err := c.exec.Resolver.Check(ctx, c.Principal.Subject(), permission.ContentTarget(id), permission.OpRead)
	if err != nil {
		return toolErr(CodePermissionUnknown, "%v", err)
	}
	switch d.Verdict(permission.OpRead) {
	case permission.Allow:
		return nil
	case permission.Deny:
		return toolErr(CodePermissionDenied, hiddenMessage)
	default:
		return toolErr(CodePermissionUnknown, "요청자의 열람 권한을 확인할 수 없습니다")
	}
}

// fetchFn reads one upstream page.
type fetchFn func(ctx context.Context, start, limit int) ([]confluence.Content, confluence.Page, error)

// collect pages through an upstream listing, filtering as it goes, until it
// has `limit` visible items or has read `extra` additional upstream pages.
// It returns the upstream offset to continue from.
func (c *Call) collect(ctx context.Context, fetch fetchFn, start, limit int) ([]confluence.Content, int, bool, int, error) {
	out := []confluence.Content{}
	unknown := 0
	offset := start
	batch := limit
	if batch < 25 {
		batch = 25
	}
	for round := 0; round <= c.Limits.SearchExtraPages; round++ {
		items, page, err := fetch(ctx, offset, batch)
		if err != nil {
			return nil, 0, false, 0, err
		}
		kept, unk, err := c.visible(ctx, items)
		if err != nil {
			return nil, 0, false, 0, err
		}
		unknown += unk
		keep := map[string]bool{}
		for _, k := range kept {
			keep[k.ID] = true
		}
		for i, it := range items {
			if !keep[it.ID] {
				continue
			}
			out = append(out, it)
			if len(out) >= limit {
				next := offset + i + 1
				more := i+1 < len(items) || page.HasNext
				return out, next, more, unknown, nil
			}
		}
		offset += len(items)
		if !page.HasNext || len(items) == 0 {
			return out, offset, false, unknown, nil
		}
	}
	return out, offset, true, unknown, nil
}

// pageOut builds the paging envelope with a signed continuation cursor.
func (c *Call) pageOut(ctx context.Context, queryKey string, returned, limit, next int, more bool) *PageOut {
	p := &PageOut{Returned: returned, Limit: limit, HasMore: more}
	if more {
		gen, _ := c.exec.Policy.Generation(ctx)
		p.NextCursor = c.exec.Signer.newCursor(c.Principal.KeycloakSub, queryKey, gen, next)
	}
	return p
}

// startFrom decodes the caller's cursor for a listing.
func (c *Call) startFrom(ctx context.Context, queryKey string) (int, error) {
	gen, err := c.exec.Policy.Generation(ctx)
	if err != nil {
		return 0, err
	}
	off, err := c.exec.Signer.openCursor(c.Args.OptString("cursor", ""), c.Principal.KeycloakSub, queryKey, gen)
	if err != nil {
		return 0, toolErr(CodeBadArguments, "%v", err)
	}
	return off, nil
}

func unknownWarning(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("권한을 확인할 수 없는 항목 %d건은 제외했습니다", n)
}

// item is the listing shape of one content item.
func (c *Call) item(ct *confluence.Content) map[string]any {
	out := map[string]any{
		"id": ct.ID, "type": ct.Type, "title": ct.Title, "spaceKey": ct.SpaceKey,
		"version": ct.Version.Number, "updatedAt": ct.Version.When, "updatedBy": ct.Version.By,
		"url": c.Adapter.WebURL(ct.WebPath),
	}
	if len(ct.Ancestors) > 0 {
		out["parentId"] = ct.Ancestors[len(ct.Ancestors)-1].ID
	}
	return out
}

// body renders a document body in the requested format, windowed.
func (c *Call) body(ct *confluence.Content, format string, offset, maxChars int) (map[string]any, []string, error) {
	if maxChars <= 0 || maxChars > c.Limits.BodyChars {
		maxChars = c.Limits.BodyChars
	}
	var text string
	var warnings []string
	out := map[string]any{"format": format}
	switch format {
	case "storage":
		text = ct.Storage
		warnings = append(warnings, "storage 원문입니다. 매크로는 실행되지 않았고 참조 대상의 권한은 확인되지 않았습니다")
	case "text", "markdown", "":
		if format == "" {
			format = "markdown"
		}
		conv, err := content.ToMarkdown(ct.Storage, format == "text")
		if err != nil {
			return nil, nil, toolErr(CodeUnsupportedContent, "본문을 해석할 수 없습니다: %v", err)
		}
		text = conv.Text
		out["format"] = conv.Format
		out["lossy"] = conv.Lossy
		if conv.Lossy {
			out["losses"] = conv.Losses
			warnings = append(warnings, "변환 손실이 있습니다: 매크로·참조는 실행하거나 확장하지 않았습니다")
		}
	default:
		return nil, nil, toolErr(CodeBadArguments, "format 은 markdown, text, storage 중 하나입니다")
	}
	chunk := content.Window(text, offset, maxChars)
	out["content"] = chunk.Text
	out["offset"], out["length"], out["totalChars"] = chunk.Offset, chunk.Length, chunk.TotalChars
	out["truncated"] = chunk.Truncated
	if chunk.NextOffset != nil {
		out["nextOffset"] = *chunk.NextOffset
	}
	return out, warnings, nil
}
