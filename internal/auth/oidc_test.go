package auth

import (
	"errors"
	"testing"
)

func TestCheckAudienceRejectsAuthorizedPartyAlone(t *testing.T) {
	// azp names who asked for the token, not whom it is for (AUTH-03).
	claims := &Claims{AuthorizedParty: "confmcp", Audience: []string{"account"}}
	if err := checkAudience(claims, []string{"confmcp"}); err == nil {
		t.Fatal("azp-only match was accepted")
	}
}

func TestCheckAudienceAcceptsAudience(t *testing.T) {
	claims := &Claims{AuthorizedParty: "some-cli", Audience: []string{"account", "confmcp"}}
	if err := checkAudience(claims, []string{"confmcp"}); err != nil {
		t.Fatalf("aud match rejected: %v", err)
	}
}

func TestCheckAudienceRejectsForeignClient(t *testing.T) {
	// A token minted for an unrelated client in the same realm verifies
	// cryptographically, so the audience check is what keeps it out.
	claims := &Claims{AuthorizedParty: "grafana", Audience: []string{"account"}}
	err := checkAudience(claims, []string{"confmcp-mcp"})
	if err == nil {
		t.Fatal("token from a different client was accepted")
	}
	if !errors.Is(err, ErrTokenAudience) {
		t.Fatalf("expected ErrTokenAudience, got %v", err)
	}
}

func TestCheckAudienceFailsClosedWithoutConfiguration(t *testing.T) {
	claims := &Claims{AuthorizedParty: "anything", Audience: []string{"anything"}}
	if err := checkAudience(claims, nil); err == nil {
		t.Fatal("an unconfigured gateway accepted every token")
	}
}

func TestCheckAudienceIsCaseInsensitive(t *testing.T) {
	claims := &Claims{Audience: []string{"CONFMCP-MCP"}}
	if err := checkAudience(claims, []string{"confmcp-mcp"}); err != nil {
		t.Fatalf("case-insensitive match rejected: %v", err)
	}
}

func TestHasScope(t *testing.T) {
	claims := &Claims{Scopes: []string{"openid", "profile", "mcp"}}
	if !claims.HasScope("mcp") {
		t.Error("declared scope not found")
	}
	if !claims.HasScope("MCP") {
		t.Error("scope match should ignore case")
	}
	if claims.HasScope("admin") {
		t.Error("undeclared scope reported as present")
	}
}

func TestRolesAtReadsNestedClaimPath(t *testing.T) {
	raw := map[string]any{
		"realm_access": map[string]any{
			"roles": []any{"confluence-mcp-reader", "offline_access"},
		},
	}
	roles := rolesAt(raw, "realm_access.roles")
	if len(roles) != 2 || roles[0] != "confluence-mcp-reader" {
		t.Fatalf("roles = %v", roles)
	}
	if got := rolesAt(raw, "missing.path"); len(got) != 0 {
		t.Fatalf("missing path should yield no roles, got %v", got)
	}
}
