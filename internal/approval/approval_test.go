package approval

import "testing"

func TestHashIgnoresApprovalIDAndNormalisesNumbers(t *testing.T) {
	a := Hash("t", map[string]any{"pageId": "1", "expectedVersion": float64(7), "approvalId": "x"})
	b := Hash("t", map[string]any{"pageId": " 1", "expectedVersion": "7"})
	if a != b {
		t.Fatal("equivalent arguments hash differently")
	}
	if a == Hash("t", map[string]any{"pageId": "1", "expectedVersion": "8"}) {
		t.Fatal("different arguments hash the same")
	}
	if a == Hash("u", map[string]any{"pageId": "1", "expectedVersion": "7"}) {
		t.Fatal("tool name not part of the hash")
	}
}

func TestRedactArgs(t *testing.T) {
	out := redactArgs(map[string]any{"password": "p", "spaceKey": "DEV", "body": string(make([]byte, 3000))})
	if out["password"] != "[redacted]" || out["spaceKey"] != "DEV" || len(out["body"].(string)) > 1100 {
		t.Fatalf("%v", out["password"])
	}
}
