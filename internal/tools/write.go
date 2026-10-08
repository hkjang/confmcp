package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/hkjang/confmcp/internal/approval"
	"github.com/hkjang/confmcp/internal/confluence"
	"github.com/hkjang/confmcp/internal/content"
	"github.com/hkjang/confmcp/internal/crypto"
	"github.com/hkjang/confmcp/internal/permission"
	"github.com/hkjang/confmcp/internal/policy"
)

func approvalProp(props map[string]any) map[string]any {
	props["approvalId"] = str("승인된 변경안 ID. 처음 호출할 때는 비워 두면 변경안이 만들어집니다")
	props["idempotencyKey"] = str("같은 요청의 중복 실행을 막는 키 (선택)")
	return props
}

func bodyHash(title, storage string) string { return crypto.SHA256Hex(title + "\x00" + storage) }

// writeTools are the P1/P2 tools. Every one prepares the change first; with
// approval required (the default) nothing is written until a person approves
// the stored proposal in the console.
func writeTools() []Definition {
	return []Definition{
		{
			Name:  "confluence_prepare_change",
			Title: "변경안 만들기",
			Description: "변경 도구(tool)와 인자(arguments)를 받아 실제 적용될 본문·diff·경고를 계산하고 승인 요청을 만듭니다. " +
				"Confluence 에는 쓰지 않습니다. 승인 후 응답의 nextCall 그대로 호출하면 적용됩니다.",
			Group:          "change",
			Risk:           RiskRead,
			RequiredPerm:   "NONE",
			Scope:          ScopeWrite,
			Priority:       "P1",
			DefaultEnabled: true,
			MinRole:        RoleWriter,
			InputSchema: obj(map[string]any{
				"tool": enumStr("적용할 변경 도구", "confluence_create_page", "confluence_update_page", "confluence_add_comment",
					"confluence_add_labels", "confluence_remove_labels", "confluence_upload_attachment",
					"confluence_create_blogpost", "confluence_move_page", "confluence_trash_page"),
				"arguments": map[string]any{"type": "object", "description": "그 도구의 인자 (approvalId 제외)"},
			}, "tool", "arguments"),
			Resolve: noTarget,
			Handle: func(ctx context.Context, c *Call) (any, error) {
				tool, err := c.Args.String("tool")
				if err != nil {
					return nil, err
				}
				raw, _ := c.Args["arguments"].(map[string]any)
				if raw == nil {
					return nil, toolErr(CodeBadArguments, "arguments 는 객체여야 합니다")
				}
				delete(raw, "approvalId")
				p := c.Principal
				return c.exec.Propose(ctx, p, tool, Args(raw))
			},
		},
		{
			Name:  "confluence_create_page",
			Title: "페이지 만들기",
			Description: "공간(또는 상위 페이지 아래)에 새 페이지를 만듭니다. body 는 Markdown(기본) 또는 storage 입니다. " +
				"Markdown 지원 범위: " + content.SupportedMarkdown,
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "create",
			Scope:           ScopeWrite,
			Priority:        "P1",
			DefaultEnabled:  true,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"spaceKey": str("공간 키"),
				"parentId": str("상위 페이지 ID (선택, 없으면 공간 최상위)"),
				"title":    str("페이지 제목"),
				"body":     str("본문"),
				"format":   enumStr("본문 형식 (기본 markdown)", "markdown", "storage"),
			}), "spaceKey", "title", "body"),
			Resolve: func(a Args) Target {
				if p := a.OptString("parentId", ""); p != "" {
					return Target{ContentID: p, SpaceKey: strings.ToUpper(a.OptString("spaceKey", ""))}
				}
				return Target{SpaceKey: strings.ToUpper(a.OptString("spaceKey", ""))}
			},
			Prepare: func(ctx context.Context, c *Call) (*Plan, error) {
				return prepareCreate(ctx, c, "page")
			},
		},
		{
			Name:  "confluence_update_page",
			Title: "페이지 수정",
			Description: "기존 페이지를 원문을 보존하며 수정합니다. mode: append·prepend(Markdown 추가), replace_text(텍스트만 치환, 매크로·코드 보존), " +
				"replace_section(제목 아래 구역 교체), replace_storage(storage 전체), replace_markdown(구조 없는 문서만). " +
				"expectedVersion 은 읽은 버전이며 다르면 VERSION_CONFLICT 입니다. 문서를 Markdown 으로 바꿔 통째로 덮어쓰지 마십시오.",
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "edit",
			Scope:           ScopeWrite,
			Priority:        "P1",
			DefaultEnabled:  true,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId":          str("페이지 ID"),
				"expectedVersion": num("읽었을 때의 버전 번호"),
				"mode": enumStr("수정 방식", content.ModeAppend, content.ModePrepend, content.ModeReplaceText,
					content.ModeReplaceSection, content.ModeReplaceStorage, content.ModeReplaceMarkdown),
				"body":           str("추가·교체할 내용 (replace_text 에서는 바꿀 문자열)"),
				"find":           str("replace_text: 찾을 문자열"),
				"replaceAll":     boolean("replace_text: 모두 바꾸기 (기본 false)"),
				"section":        str("replace_section: 구역 제목"),
				"bodyFormat":     enumStr("replace_section: body 형식 (기본 markdown)", "markdown", "storage"),
				"title":          str("새 제목 (선택)"),
				"allowRemovals":  boolean("매크로·첨부 참조 등의 삭제를 의도한 경우 true"),
				"versionComment": str("버전 메모 (선택)"),
			}), "pageId", "expectedVersion", "mode"),
			Resolve: contentTarget("pageId"),
			Prepare: prepareUpdate,
		},
		{
			Name:            "confluence_add_comment",
			Title:           "댓글 달기",
			Description:     "페이지에 댓글(또는 답글)을 답니다. body 는 Markdown 입니다.",
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "comment",
			Scope:           ScopeWrite,
			Priority:        "P1",
			DefaultEnabled:  true,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId":          str("페이지 ID"),
				"body":            str("댓글 내용 (Markdown)"),
				"parentCommentId": str("답글을 달 댓글 ID (선택)"),
			}), "pageId", "body"),
			Resolve: contentTarget("pageId"),
			Prepare: prepareComment,
		},
		{
			Name:            "confluence_add_labels",
			Title:           "라벨 추가",
			Description:     "콘텐츠에 라벨을 추가합니다. 라벨은 공백 없는 소문자입니다.",
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "edit",
			Scope:           ScopeWrite,
			Priority:        "P1",
			DefaultEnabled:  true,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId": str("페이지 또는 블로그 ID"),
				"labels": strArray("추가할 라벨"),
			}), "pageId", "labels"),
			Resolve: contentTarget("pageId"),
			Prepare: func(ctx context.Context, c *Call) (*Plan, error) { return prepareLabels(ctx, c, true) },
		},
		{
			Name:            "confluence_upload_attachment",
			Title:           "첨부 올리기",
			Description:     "콘솔 또는 업로드 API 로 미리 올린 파일(uploadId)을 페이지에 첨부합니다. 파일 경로나 URL 은 받지 않습니다. 같은 이름의 첨부가 있으면 거부합니다.",
			Group:           "attachment",
			Risk:            RiskWrite,
			RequiredPerm:    "attach",
			Scope:           ScopeAttachmentWrite,
			Priority:        "P1",
			DefaultEnabled:  true,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId":   str("페이지 ID"),
				"uploadId": str("업로드 ID"),
				"comment":  str("첨부 설명 (선택)"),
			}), "pageId", "uploadId"),
			Resolve: contentTarget("pageId"),
			Prepare: prepareUpload,
		},
		{
			Name:            "confluence_create_blogpost",
			Title:           "블로그 작성",
			Description:     "공간에 블로그 게시물을 작성합니다. body 는 Markdown(기본) 또는 storage 입니다.",
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "create",
			Scope:           ScopeWrite,
			Priority:        "P2",
			DefaultEnabled:  false,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"spaceKey": str("공간 키"),
				"title":    str("제목"),
				"body":     str("본문"),
				"format":   enumStr("본문 형식 (기본 markdown)", "markdown", "storage"),
			}), "spaceKey", "title", "body"),
			Resolve: spaceTarget,
			Prepare: func(ctx context.Context, c *Call) (*Plan, error) { return prepareCreate(ctx, c, "blogpost") },
		},
		{
			Name:            "confluence_remove_labels",
			Title:           "라벨 제거",
			Description:     "콘텐츠에서 라벨을 제거합니다.",
			Group:           "change",
			Risk:            RiskWrite,
			RequiredPerm:    "edit",
			Scope:           ScopeWrite,
			Priority:        "P2",
			DefaultEnabled:  false,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId": str("페이지 또는 블로그 ID"),
				"labels": strArray("제거할 라벨"),
			}), "pageId", "labels"),
			Resolve: contentTarget("pageId"),
			Prepare: func(ctx context.Context, c *Call) (*Plan, error) { return prepareLabels(ctx, c, false) },
		},
		{
			Name:  "confluence_move_page",
			Title: "페이지 이동",
			Description: "페이지를 같은 공간의 다른 상위 페이지 아래로 옮깁니다. 새 상위 문서의 열람 제한이 상속되어 공개 범위가 바뀔 수 있으므로 " +
				"별도 승인자의 승인이 필요합니다.",
			Group:           "change",
			Risk:            RiskExecute,
			RequiredPerm:    "move",
			Scope:           ScopeExecute,
			Priority:        "P2",
			DefaultEnabled:  false,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId":          str("옮길 페이지 ID"),
				"targetParentId":  str("새 상위 페이지 ID (같은 공간)"),
				"expectedVersion": num("읽었을 때의 버전"),
			}), "pageId", "targetParentId", "expectedVersion"),
			Resolve: contentTarget("pageId"),
			Prepare: prepareMove,
		},
		{
			Name:            "confluence_trash_page",
			Title:           "휴지통으로 이동",
			Description:     "현재 페이지를 휴지통으로 옮깁니다. 영구 삭제는 제공하지 않습니다. 별도 승인자의 승인이 필요합니다.",
			Group:           "change",
			Risk:            RiskExecute,
			RequiredPerm:    "delete",
			Scope:           ScopeExecute,
			Priority:        "P2",
			DefaultEnabled:  false,
			DefaultApproval: true,
			InputSchema: obj(approvalProp(map[string]any{
				"pageId":          str("페이지 ID"),
				"expectedVersion": num("읽었을 때의 버전"),
			}), "pageId", "expectedVersion"),
			Resolve: contentTarget("pageId"),
			Prepare: prepareTrash,
		},
	}
}

func cleanTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", fmt.Errorf("필수 인자 title 이 비어 있습니다")
	}
	if utf8.RuneCountInString(t) > 255 {
		return "", fmt.Errorf("인자 title 은 255자 이하여야 합니다")
	}
	if strings.ContainsAny(t, "\r\n\t") {
		return "", fmt.Errorf("인자 title 에 줄바꿈을 쓸 수 없습니다")
	}
	return t, nil
}

func bodyToStorage(body, format string) (string, error) {
	switch format {
	case "", "markdown":
		return content.FromMarkdown(body), nil
	case "storage":
		if _, err := content.Parse(body); err != nil {
			return "", err
		}
		return body, nil
	}
	return "", fmt.Errorf("인자 format 은 markdown 또는 storage 입니다")
}

func (c *Call) verifyFn(expectType string) func(ctx context.Context, id string) (int, error) {
	return func(ctx context.Context, id string) (int, error) {
		ct, err := c.Adapter.Content(ctx, c.Cred, id, "version", "space")
		if err != nil {
			return 0, err
		}
		if expectType != "" && ct.Type != expectType {
			return 0, fmt.Errorf("예상한 유형(%s)이 아닙니다: %s", expectType, ct.Type)
		}
		return ct.Version.Number, nil
	}
}

func prepareCreate(ctx context.Context, c *Call, kind string) (*Plan, error) {
	title, err := cleanTitle(c.Args.OptString("title", ""))
	if err != nil {
		return nil, err
	}
	body, err := c.Args.String("body")
	if err != nil {
		return nil, err
	}
	storage, err := bodyToStorage(body, c.Args.OptString("format", "markdown"))
	if err != nil {
		return nil, err
	}
	space := c.Resolved.SpaceKey
	requested := strings.ToUpper(c.Args.OptString("spaceKey", ""))
	if requested != "" && !strings.EqualFold(requested, space) {
		return nil, toolErr(CodeBadArguments, "상위 페이지는 공간 %s 에 있습니다 (요청: %s)", space, requested)
	}
	parentID, parentTitle := "", ""
	if kind == "page" && c.Resolved.Content != nil {
		parentID, parentTitle = c.Resolved.Content.ID, c.Resolved.Content.Title
	}
	if kind == "page" {
		if existing, _, err := c.Adapter.ContentList(ctx, c.Cred, confluence.ContentQuery{Type: "page", SpaceKey: space, Title: title, Limit: 1}); err == nil && len(existing) > 0 {
			return nil, toolErr(CodeBadArguments, "공간 %s 에 같은 제목의 페이지가 이미 있습니다", space)
		}
	}
	diff, stats := content.Diff("", storage, 0)
	readable, _ := content.ToMarkdown(storage, false)
	plan := &Plan{
		Action: "create_" + kind, SpaceKey: space,
		BaseHash: crypto.SHA256Hex("create|" + space + "|" + parentID),
		Preview: approval.Preview{
			Action: "create_" + kind, Summary: fmt.Sprintf("%s 공간에 %q %s 생성", space, title, koKind(kind)),
			Title: title, ParentID: parentID, ParentTitle: parentTitle, Diff: diff, NewBody: readable.Text,
			BodyFormat: "markdown", Stats: stats,
		},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		created, err := c.Adapter.CreateContent(ctx, c.Cred, confluence.NewContent{
			Type: kind, Title: title, SpaceKey: space, ParentID: parentID, Storage: storage,
		})
		if err != nil {
			return "", nil, err
		}
		return created.ID, map[string]any{"id": created.ID, "title": created.Title, "spaceKey": created.SpaceKey,
			"version": created.Version.Number, "url": c.Adapter.WebURL(created.WebPath)}, nil
	}
	plan.Verify = c.verifyFn(kind)
	return plan, nil
}

func koKind(kind string) string {
	switch kind {
	case "page":
		return "페이지"
	case "blogpost":
		return "블로그"
	case "comment":
		return "댓글"
	}
	return kind
}

// current loads the target with its body and checks the caller's expected
// version against it.
func (c *Call) current(ctx context.Context) (*confluence.Content, error) {
	ct, err := c.Adapter.Content(ctx, c.Cred, c.Resolved.ContentID, "body.storage", "space", "version", "ancestors")
	if err != nil {
		return nil, err
	}
	if c.Args.OptInt("expectedVersion", -1) != ct.Version.Number {
		return nil, toolErr(CodeVersionConflict, "expectedVersion(%d)이 현재 버전(%d)과 다릅니다. 최신 문서를 다시 읽고 변경안을 만드십시오",
			c.Args.OptInt("expectedVersion", -1), ct.Version.Number)
	}
	return ct, nil
}

func prepareUpdate(ctx context.Context, c *Call) (*Plan, error) {
	ct, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	if ct.Type != "page" && ct.Type != "blogpost" {
		return nil, toolErr(CodeBadArguments, "페이지·블로그만 수정할 수 있습니다 (%s)", ct.Type)
	}
	mode, err := c.Args.String("mode")
	if err != nil {
		return nil, err
	}
	in := content.UpdateInput{
		Mode: mode, Body: c.Args.OptString("body", ""), Find: c.Args.OptString("find", ""),
		ReplaceAll: c.Args.OptBool("replaceAll", false), Section: c.Args.OptString("section", ""),
		BodyFormat: c.Args.OptString("bodyFormat", "markdown"), AllowRemovals: c.Args.OptBool("allowRemovals", false),
	}
	if mode == content.ModeReplaceText {
		// body is the replacement; it may legitimately be empty (deletion).
		if v, ok := c.Args["body"].(string); ok {
			in.Body = v
		}
	}
	res, err := content.ApplyUpdate(ct.Storage, in)
	if err != nil {
		return nil, err
	}
	title := ct.Title
	if t := strings.TrimSpace(c.Args.OptString("title", "")); t != "" {
		if title, err = cleanTitle(t); err != nil {
			return nil, err
		}
	}
	if res.Storage == ct.Storage && title == ct.Title {
		return nil, toolErr(CodeBadArguments, "변경 내용이 없습니다")
	}
	diff, stats := content.Diff(ct.Storage, res.Storage, 2)
	stats["changes"] = res.Changes
	version := ct.Version.Number
	summary := fmt.Sprintf("%q 수정 (%s, v%d → v%d)", ct.Title, mode, version, version+1)
	warnings := res.Warnings
	if title != ct.Title {
		warnings = append(warnings, fmt.Sprintf("제목 변경: %q → %q", ct.Title, title))
	}
	plan := &Plan{
		Action: "update_page", TargetID: ct.ID, SpaceKey: ct.SpaceKey, BaseVersion: &version,
		BaseHash: bodyHash(ct.Title, ct.Storage),
		Preview: approval.Preview{
			Action: "update_page", Summary: summary, Title: title, OldTitle: ct.Title,
			TargetURL: c.Adapter.WebURL(ct.WebPath), Diff: diff, Warnings: warnings, Stats: stats,
		},
	}
	note := c.Args.OptString("versionComment", "")
	newStorage := res.Storage
	plan.Apply = func(ctx context.Context) (string, any, error) {
		updated, err := c.Adapter.UpdateContent(ctx, c.Cred, ct.ID, confluence.ContentUpdate{
			Type: ct.Type, Title: title, NewVersion: version + 1, Storage: newStorage, VersionNote: note,
		})
		if err != nil {
			return "", nil, err
		}
		return ct.ID, map[string]any{"id": ct.ID, "title": updated.Title, "version": updated.Version.Number,
			"url": c.Adapter.WebURL(ct.WebPath)}, nil
	}
	plan.Verify = func(ctx context.Context, id string) (int, error) {
		v, err := c.verifyFn("")(ctx, id)
		if err != nil {
			return 0, err
		}
		if v < version+1 {
			return v, fmt.Errorf("버전이 올라가지 않았습니다 (v%d)", v)
		}
		return v, nil
	}
	return plan, nil
}

func prepareComment(ctx context.Context, c *Call) (*Plan, error) {
	body, err := c.Args.String("body")
	if err != nil {
		return nil, err
	}
	page := c.Resolved.Content
	if page.Type != "page" && page.Type != "blogpost" {
		return nil, toolErr(CodeBadArguments, "페이지·블로그에만 댓글을 달 수 있습니다")
	}
	parent := c.Args.OptString("parentCommentId", "")
	if parent != "" {
		pc, err := c.Adapter.Content(ctx, c.Cred, parent, "container", "version")
		if err != nil || pc.Type != "comment" || pc.Container == nil || pc.Container.ID != page.ID {
			return nil, toolErr(CodeBadArguments, "parentCommentId 가 이 페이지의 댓글이 아닙니다")
		}
	}
	storage := content.FromMarkdown(body)
	diff, stats := content.Diff("", storage, 0)
	plan := &Plan{
		Action: "add_comment", TargetID: page.ID, SpaceKey: page.SpaceKey,
		BaseHash: crypto.SHA256Hex("comment|" + page.ID + "|" + parent),
		Preview: approval.Preview{
			Action: "add_comment", Summary: fmt.Sprintf("%q 에 댓글 추가", page.Title), Title: page.Title,
			TargetURL: c.Adapter.WebURL(page.WebPath), Diff: diff, NewBody: body, BodyFormat: "markdown", Stats: stats,
		},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		created, err := c.Adapter.CreateContent(ctx, c.Cred, confluence.NewContent{
			Type: "comment", ContainerID: page.ID, ContainerType: page.Type, SpaceKey: page.SpaceKey,
			ParentID: parent, Storage: storage,
		})
		if err != nil {
			return "", nil, err
		}
		return created.ID, map[string]any{"commentId": created.ID, "pageId": page.ID}, nil
	}
	plan.Verify = c.verifyFn("comment")
	return plan, nil
}

var labelNameRe = regexp.MustCompile(`^[\p{Ll}\p{Lo}0-9_\-.:]{1,255}$`)

func prepareLabels(ctx context.Context, c *Call, add bool) (*Plan, error) {
	raw := c.Args.StringSlice("labels")
	if len(raw) == 0 {
		return nil, fmt.Errorf("필수 인자 labels 가 비어 있습니다")
	}
	labels := []string{}
	seen := map[string]bool{}
	for _, l := range raw {
		l = strings.ToLower(strings.TrimSpace(l))
		if !labelNameRe.MatchString(l) {
			return nil, toolErr(CodeBadArguments, "라벨 형식이 올바르지 않습니다: %q (공백·특수문자 불가)", l)
		}
		if !seen[l] {
			seen[l] = true
			labels = append(labels, l)
		}
	}
	page := c.Resolved.Content
	existing, _, err := c.Adapter.Labels(ctx, c.Cred, page.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, l := range existing {
		have[l.Name] = true
	}
	var change []string
	for _, l := range labels {
		if add != have[l] {
			change = append(change, l)
		}
	}
	if len(change) == 0 {
		if add {
			return nil, toolErr(CodeBadArguments, "모든 라벨이 이미 있습니다")
		}
		return nil, toolErr(CodeBadArguments, "제거할 라벨이 없습니다")
	}
	action := "add_labels"
	verb := "추가"
	if !add {
		action, verb = "remove_labels", "제거"
	}
	plan := &Plan{
		Action: action, TargetID: page.ID, SpaceKey: page.SpaceKey,
		Preview: approval.Preview{Action: action, Summary: fmt.Sprintf("%q 라벨 %s: %s", page.Title, verb, strings.Join(change, ", ")),
			Title: page.Title, TargetURL: c.Adapter.WebURL(page.WebPath), Labels: change},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		if add {
			if _, err := c.Adapter.AddLabels(ctx, c.Cred, page.ID, change); err != nil {
				return "", nil, err
			}
		} else {
			for _, l := range change {
				if err := c.Adapter.RemoveLabel(ctx, c.Cred, page.ID, l); err != nil {
					return "", nil, err
				}
			}
		}
		return page.ID, map[string]any{"pageId": page.ID, action: change}, nil
	}
	plan.Verify = func(ctx context.Context, id string) (int, error) {
		after, _, err := c.Adapter.Labels(ctx, c.Cred, id, 0, 200)
		if err != nil {
			return 0, err
		}
		now := map[string]bool{}
		for _, l := range after {
			now[l.Name] = true
		}
		for _, l := range change {
			if now[l] != add {
				return 0, fmt.Errorf("라벨 %s 가 반영되지 않았습니다", l)
			}
		}
		return page.Version.Number, nil
	}
	return plan, nil
}

func prepareUpload(ctx context.Context, c *Call) (*Plan, error) {
	raw, err := c.Args.String("uploadId")
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, toolErr(CodeBadArguments, "uploadId 형식이 올바르지 않습니다")
	}
	up, err := c.exec.Uploads.Get(ctx, c.Principal.UserID, id, false)
	if err != nil {
		return nil, err
	}
	page := c.Resolved.Content
	atts, _, err := c.Adapter.Attachments(ctx, c.Cred, page.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		if strings.EqualFold(a.Title, up.Filename) {
			return nil, toolErr(CodeBadArguments, "같은 이름의 첨부(%s)가 이미 있습니다. 기존 첨부 덮어쓰기는 지원하지 않습니다", up.Filename)
		}
	}
	comment := c.Args.OptString("comment", "")
	plan := &Plan{
		Action: "upload_attachment", TargetID: page.ID, SpaceKey: page.SpaceKey,
		// Bound to the file's content: a different file under the same upload
		// ID would be a different approval.
		BaseHash: up.SHA256,
		Preview: approval.Preview{
			Action: "upload_attachment", Summary: fmt.Sprintf("%q 에 %s 첨부", page.Title, up.Filename),
			Title: page.Title, TargetURL: c.Adapter.WebURL(page.WebPath),
			Attachment: map[string]any{"filename": up.Filename, "size": up.Size, "sha256": up.SHA256,
				"mediaType": up.MediaType, "overwrite": false, "comment": comment},
		},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		full, err := c.exec.Uploads.Get(ctx, c.Principal.UserID, id, true)
		if err != nil {
			return "", nil, err
		}
		if full.SHA256 != up.SHA256 {
			return "", nil, toolErr(CodeApprovalStale, "업로드 파일이 승인 이후 바뀌었습니다")
		}
		att, err := c.Adapter.UploadAttachment(ctx, c.Cred, page.ID, full.Filename, full.MediaType, full.Data, comment)
		if err != nil {
			return "", nil, err
		}
		c.exec.Uploads.Consume(ctx, id)
		return att.ID, map[string]any{"attachmentId": att.ID, "filename": att.Title, "pageId": page.ID}, nil
	}
	plan.Verify = c.verifyFn("attachment")
	return plan, nil
}

func prepareMove(ctx context.Context, c *Call) (*Plan, error) {
	ct, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	if ct.Type != "page" {
		return nil, toolErr(CodeBadArguments, "페이지만 이동할 수 있습니다")
	}
	targetID, err := c.Args.String("targetParentId")
	if err != nil {
		return nil, err
	}
	if targetID == ct.ID {
		return nil, toolErr(CodeBadArguments, "자기 자신 아래로 옮길 수 없습니다")
	}
	// The requester must be able to create under the new parent.
	dec, err := c.exec.Resolver.Check(ctx, c.Principal.Subject(), permission.ContentTarget(targetID), permission.OpRead, permission.OpCreate)
	if err != nil {
		return nil, toolErr(CodePermissionUnknown, "%v", err)
	}
	if !dec.Allowed(permission.OpRead) {
		return nil, toolErr(CodePermissionDenied, hiddenMessage)
	}
	if !dec.Allowed(permission.OpCreate) {
		return nil, toolErr(CodePermissionDenied, "새 상위 페이지 아래에 작성할 권한이 없습니다")
	}
	target, err := c.Adapter.Content(ctx, c.Cred, targetID, "space", "version", "ancestors")
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(target.SpaceKey, ct.SpaceKey) {
		return nil, toolErr(CodeBadArguments, "같은 공간 안에서만 이동할 수 있습니다")
	}
	for _, a := range target.Ancestors {
		if a.ID == ct.ID {
			return nil, toolErr(CodeBadArguments, "자신의 하위 페이지 아래로 옮길 수 없습니다")
		}
	}
	v, err := c.exec.Policy.Evaluate(ctx, policy.Target{SpaceKey: target.SpaceKey, ContentID: ct.ID,
		Ancestors: append(target.AncestorIDs(), target.ID), ContentType: "page"})
	if err != nil || !v.Allowed {
		reason := "새 위치가 MCP 정책상 허용되지 않습니다"
		if err == nil {
			reason += ": " + v.Reason
		}
		return nil, toolErr(CodePolicyDenied, "%s", reason)
	}
	oldParent := ""
	if len(ct.Ancestors) > 0 {
		oldParent = ct.Ancestors[len(ct.Ancestors)-1].Title
	}
	version := ct.Version.Number
	plan := &Plan{
		Action: "move_page", TargetID: ct.ID, SpaceKey: ct.SpaceKey, BaseVersion: &version,
		BaseHash: crypto.SHA256Hex(bodyHash(ct.Title, ct.Storage) + ":" + target.ID),
		Preview: approval.Preview{
			Action: "move_page", Summary: fmt.Sprintf("%q 를 %q 아래로 이동", ct.Title, target.Title), Title: ct.Title,
			TargetURL: c.Adapter.WebURL(ct.WebPath), ParentTitle: oldParent, NewParentID: target.ID,
			Visibility: "새 상위 문서 " + fmt.Sprintf("%q", target.Title) + " 의 열람 제한이 이 페이지와 하위 페이지에 상속됩니다. 이동 후 볼 수 있는 사람이 달라질 수 있습니다.",
			Warnings:   []string{"이동은 공개 범위를 바꿀 수 있습니다"},
		},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		updated, err := c.Adapter.UpdateContent(ctx, c.Cred, ct.ID, confluence.ContentUpdate{
			Type: ct.Type, Title: ct.Title, NewVersion: version + 1, Storage: ct.Storage, ParentID: target.ID,
			VersionNote: "confmcp: 페이지 이동",
		})
		if err != nil {
			return "", nil, err
		}
		return ct.ID, map[string]any{"pageId": ct.ID, "newParentId": target.ID, "version": updated.Version.Number}, nil
	}
	plan.Verify = func(ctx context.Context, id string) (int, error) {
		after, err := c.Adapter.Content(ctx, c.Cred, id, "version", "ancestors")
		if err != nil {
			return 0, err
		}
		if len(after.Ancestors) == 0 || after.Ancestors[len(after.Ancestors)-1].ID != target.ID {
			return 0, fmt.Errorf("상위 페이지가 바뀌지 않았습니다")
		}
		return after.Version.Number, nil
	}
	return plan, nil
}

func prepareTrash(ctx context.Context, c *Call) (*Plan, error) {
	ct, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	if ct.Type != "page" && ct.Type != "blogpost" {
		return nil, toolErr(CodeBadArguments, "페이지·블로그만 휴지통으로 옮길 수 있습니다")
	}
	children, _, err := c.Adapter.Children(ctx, c.Cred, ct.ID, 0, 1)
	if err == nil && len(children) > 0 {
		return nil, toolErr(CodeBadArguments, "하위 페이지가 있는 페이지는 휴지통으로 옮길 수 없습니다")
	}
	version := ct.Version.Number
	plan := &Plan{
		Action: "trash_page", TargetID: ct.ID, SpaceKey: ct.SpaceKey, BaseVersion: &version,
		BaseHash: bodyHash(ct.Title, ct.Storage),
		Preview: approval.Preview{
			Action: "trash_page", Summary: fmt.Sprintf("%q 를 휴지통으로 이동 (영구 삭제 아님)", ct.Title), Title: ct.Title,
			TargetURL: c.Adapter.WebURL(ct.WebPath), Warnings: []string{"공간 관리자는 휴지통에서 복원할 수 있습니다"},
		},
	}
	plan.Apply = func(ctx context.Context) (string, any, error) {
		if err := c.Adapter.TrashContent(ctx, c.Cred, ct.ID); err != nil {
			return "", nil, err
		}
		return ct.ID, map[string]any{"pageId": ct.ID, "status": "trashed"}, nil
	}
	plan.Verify = func(ctx context.Context, id string) (int, error) {
		after, err := c.Adapter.Content(ctx, c.Cred, id, "version")
		if err != nil {
			if confluence.IsStatus(err, 404) {
				return version, nil
			}
			return 0, err
		}
		if after.Status != "trashed" {
			return 0, fmt.Errorf("상태가 trashed 가 아닙니다 (%s)", after.Status)
		}
		return after.Version.Number, nil
	}
	return plan, nil
}
