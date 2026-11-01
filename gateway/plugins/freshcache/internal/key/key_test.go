package key

import (
	"testing"
)

func TestComputeDeterminism(t *testing.T) {
	k1 := Compute("team-a", "Anthropic", "pre-meeting", 30, "v1", "gemini-2.5-flash")
	k2 := Compute("team-a", "Anthropic", "pre-meeting", 30, "v1", "gemini-2.5-flash")
	if k1 != k2 {
		t.Fatalf("expected identical keys, got %q vs %q", k1, k2)
	}
	if len(k1) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got len %d", len(k1))
	}
}

func TestTenantIsolation(t *testing.T) {
	kAlpha := Compute("team-alpha", "Anthropic", "pre-meeting", 30, "v1", "gemini-2.5-flash")
	kBeta := Compute("team-beta", "Anthropic", "pre-meeting", 30, "v1", "gemini-2.5-flash")
	if kAlpha == kBeta {
		t.Fatalf("expected distinct keys for different tenants, both got %q", kAlpha)
	}
}

func TestCollisionResistance(t *testing.T) {
	// Without length prefixing, "a" + "bc" == "ab" + "c"
	k1 := Compute("t", "a", "bc", 30, "v1", "model")
	k2 := Compute("t", "ab", "c", 30, "v1", "model")
	if k1 == k2 {
		t.Fatalf("collision detected between adjacent string fields!")
	}
}
