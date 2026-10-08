// Package apikey issues, rotates and verifies personal API keys, and owns the
// editable key permission scheme (roles → scopes).
package apikey

import "strings"

// Scope names. A key carries a subset; on every request the effective scopes
// are the key's scopes intersected with what the owner's current roles allow,
// and the owner's own Confluence permission still applies on top.
const (
	ScopeRead            = "confluence:read"
	ScopeWrite           = "confluence:write"
	ScopeAttachmentRead  = "confluence:attachment:read"
	ScopeAttachmentWrite = "confluence:attachment:write"
	ScopeExecute         = "confluence:execute"
	ScopeAIInvoke        = "ai:invoke"
	ScopeAdminRead       = "admin:read"
	ScopeAdminWrite      = "admin:write"
)

// ScopeInfo describes a scope for the UI.
type ScopeInfo struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
}

// AllScopes is the catalogue shown in the key management UI.
func AllScopes() []ScopeInfo {
	return []ScopeInfo{
		{ScopeRead, "문서 조회", "공간·페이지·블로그·댓글·라벨 조회와 검색", "READ"},
		{ScopeAttachmentRead, "첨부 조회", "첨부 목록과 다운로드 링크 발급", "READ"},
		{ScopeWrite, "문서 작성", "변경안 작성, 페이지 생성·수정, 댓글, 라벨 (승인 필요)", "WRITE"},
		{ScopeAttachmentWrite, "첨부 올리기", "파일 업로드와 첨부 (승인 필요)", "WRITE"},
		{ScopeExecute, "고위험 작업", "페이지 이동·휴지통 (별도 승인자 필요)", "EXECUTE"},
		{ScopeAIInvoke, "AI 호출", "콘솔 AI 스트리밍 호출", "WRITE"},
		{ScopeAdminRead, "관리 조회", "관리자 설정·감사 로그 읽기", "ADMIN"},
		{ScopeAdminWrite, "관리 변경", "관리자 설정 변경", "ADMIN"},
	}
}

// BuiltinRoles are seeded on first boot and may be edited afterwards.
func BuiltinRoles() []Role {
	return []Role{
		{Name: "reader", Description: "조회 전용", Scopes: []string{ScopeRead, ScopeAttachmentRead}, Builtin: true},
		{Name: "writer", Description: "조회 + 변경안 작성", Scopes: []string{ScopeRead, ScopeAttachmentRead, ScopeWrite, ScopeAttachmentWrite, ScopeAIInvoke}, Builtin: true},
		{Name: "executor", Description: "작성 + 이동·휴지통", Scopes: []string{ScopeRead, ScopeAttachmentRead, ScopeWrite, ScopeAttachmentWrite, ScopeExecute, ScopeAIInvoke}, Builtin: true},
		{Name: "admin", Description: "관리 API 포함 전체", Scopes: []string{ScopeRead, ScopeAttachmentRead, ScopeWrite, ScopeAttachmentWrite, ScopeExecute, ScopeAIInvoke, ScopeAdminRead, ScopeAdminWrite}, Builtin: true},
	}
}

// ValidScope reports whether name is a known scope.
func ValidScope(name string) bool {
	for _, s := range AllScopes() {
		if s.Name == name {
			return true
		}
	}
	return false
}

// HasScope reports whether scopes contains want.
func HasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
