package policy

import "testing"

func rules() []Rule {
	return []Rule{
		{Kind: KindSpace, Pattern: "DEV", Effect: EffectAllow},
		{Kind: KindSpace, Pattern: "OPS*", Effect: EffectAllow, RiskCap: "WRITE"},
		{Kind: KindSpace, Pattern: "HR", Effect: EffectDeny, Note: "인사"},
	}
}

func TestSpacesAreExplicitAllow(t *testing.T) {
	rs := rules()
	if v := evaluate(rs, Target{SpaceKey: "dev"}); !v.Allowed {
		t.Fatalf("DEV should be allowed: %+v", v)
	}
	if v := evaluate(rs, Target{SpaceKey: "MARKETING"}); v.Allowed {
		t.Fatal("a space without an allow rule must be denied")
	}
	if v := evaluate(rs, Target{SpaceKey: "HR"}); v.Allowed {
		t.Fatal("denied space allowed")
	}
	if v := evaluate(rs, Target{SpaceKey: "OPSX"}); !v.Allowed || v.RiskCap != "WRITE" {
		t.Fatalf("glob allow with cap: %+v", v)
	}
	if v := evaluate(nil, Target{SpaceKey: "DEV"}); v.Allowed {
		t.Fatal("no rules at all must deny")
	}
}

func TestDenyWins(t *testing.T) {
	rs := []Rule{
		{Kind: KindSpace, Pattern: "*", Effect: EffectAllow},
		{Kind: KindSpace, Pattern: "SECRET", Effect: EffectDeny},
	}
	if v := evaluate(rs, Target{SpaceKey: "SECRET"}); v.Allowed {
		t.Fatal("deny must win over a wildcard allow")
	}
}

func TestPageTreeDenyCoversDescendants(t *testing.T) {
	rs := []Rule{
		{Kind: KindSpace, Pattern: "DEV", Effect: EffectAllow},
		{Kind: KindPageTree, Pattern: "100", Effect: EffectDeny},
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "300", Ancestors: []string{"1", "100", "200"}}); v.Allowed {
		t.Fatal("descendant of a denied tree allowed")
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "100"}); v.Allowed {
		t.Fatal("denied tree root allowed")
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "400", Ancestors: []string{"1"}}); !v.Allowed {
		t.Fatal("unrelated page denied")
	}
}

func TestScopedPageTreeAllowNarrowsSpace(t *testing.T) {
	rs := []Rule{
		{Kind: KindSpace, Pattern: "DEV", Effect: EffectAllow},
		{Kind: KindSpace, Pattern: "OPS", Effect: EffectAllow},
		{Kind: KindPageTree, Pattern: "500", SpaceKey: "DEV", Effect: EffectAllow},
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "501", Ancestors: []string{"500"}}); !v.Allowed {
		t.Fatal("page inside the allowed tree denied")
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "600", Ancestors: []string{"1"}}); v.Allowed {
		t.Fatal("page outside the allowed tree of DEV allowed")
	}
	if v := evaluate(rs, Target{SpaceKey: "OPS", ContentID: "700"}); !v.Allowed {
		t.Fatal("tree rule scoped to DEV must not affect OPS")
	}
}

func TestContentTypes(t *testing.T) {
	rs := []Rule{
		{Kind: KindSpace, Pattern: "DEV", Effect: EffectAllow},
		{Kind: KindContentType, Pattern: "attachment", Effect: EffectDeny},
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "att1", ContentType: "attachment"}); v.Allowed {
		t.Fatal("denied content type allowed")
	}
	if v := evaluate(rs, Target{SpaceKey: "DEV", ContentID: "1", ContentType: "page"}); !v.Allowed {
		t.Fatal("page denied")
	}
}

func TestRiskAllowed(t *testing.T) {
	if !RiskAllowed("READ", "WRITE") || RiskAllowed("EXECUTE", "WRITE") || !RiskAllowed("EXECUTE", "") {
		t.Fatal("risk cap ordering")
	}
}
