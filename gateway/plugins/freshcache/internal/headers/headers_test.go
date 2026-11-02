package headers

import (
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestParseValidHeaders(t *testing.T) {
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-vk":                  "vk-alpha-prod",
			"x-chrono-subject":         "Anthropic",
			"x-chrono-intent":          "pre-meeting",
			"x-chrono-window-days":     "30",
			"x-chrono-prompt-version":  "v1",
			"x-chrono-newest":          "2026-10-31T20:15:00Z",
			"x-chrono-count-at-newest": "2",
			"x-chrono-dated":           "5",
			"x-chrono-undated":         "0",
			"x-chrono-set-hash":        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"x-chrono-cache-mode":      "replay",
		},
	}

	h := Parse(req)
	if h.RawTenant != "vk-alpha-prod" {
		t.Fatalf("expected raw tenant vk-alpha-prod, got %q", h.RawTenant)
	}
	expectedHash := HashTenant("vk-alpha-prod")
	if h.TenantID != expectedHash || len(h.TenantID) != 12 {
		t.Fatalf("expected hashed tenant %q (len 12), got %q", expectedHash, h.TenantID)
	}
	if h.Subject != "Anthropic" {
		t.Fatalf("expected subject Anthropic, got %q", h.Subject)
	}
	if h.Intent != "pre-meeting" {
		t.Fatalf("expected intent pre-meeting, got %q", h.Intent)
	}
	if h.WindowDays != 30 {
		t.Fatalf("expected window 30, got %d", h.WindowDays)
	}
	if h.PromptVersion != "v1" {
		t.Fatalf("expected prompt version v1, got %q", h.PromptVersion)
	}
	if h.CacheMode != "replay" {
		t.Fatalf("expected replay mode, got %q", h.CacheMode)
	}
	if !h.Fingerprint.Valid() {
		t.Fatalf("expected valid fingerprint, got %+v", h.Fingerprint)
	}
	expectedTime, _ := time.Parse(time.RFC3339, "2026-10-31T20:15:00Z")
	if !h.Fingerprint.Newest.Equal(expectedTime) {
		t.Fatalf("expected newest %v, got %v", expectedTime, h.Fingerprint.Newest)
	}
}

func TestParseBifrostAliases(t *testing.T) {
	// Request using x-bf-freshness-* aliases exclusively
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-vk":                       "vk-alias-test",
			"x-bf-freshness-subject":         "Apple",
			"x-bf-freshness-intent":          "sec-8k-analyst",
			"x-bf-freshness-window-days":     "60",
			"x-bf-freshness-prompt-version":  "v2",
			"x-bf-freshness-newest":          "2026-11-01T10:00:00Z",
			"x-bf-freshness-count-at-newest": "1",
			"x-bf-freshness-dated":           "4",
			"x-bf-freshness-undated":         "0",
			"x-bf-freshness-set-hash":        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"x-bf-freshness-mode":            "live",
			"x-bf-freshness-refresh":         "true",
			"x-bf-cache-key":                 "semantic-cache-sec8k",
		},
	}

	h := Parse(req)
	if h.Subject != "Apple" {
		t.Fatalf("expected subject Apple, got %q", h.Subject)
	}
	if h.Intent != "sec-8k-analyst" {
		t.Fatalf("expected intent sec-8k-analyst, got %q", h.Intent)
	}
	if h.WindowDays != 60 {
		t.Fatalf("expected window 60, got %d", h.WindowDays)
	}
	if h.PromptVersion != "v2" {
		t.Fatalf("expected prompt version v2, got %q", h.PromptVersion)
	}
	if !h.Refresh {
		t.Fatal("expected refresh true from x-bf-freshness-refresh")
	}
	if h.NativeCacheKey != "semantic-cache-sec8k" {
		t.Fatalf("expected native cache key, got %q", h.NativeCacheKey)
	}
}

func TestApplyNativeCacheGuard(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	// Case 1: Valid fingerprint -> STAMPED_SAFE
	reqValid := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-cache-key":          "sec8k-model-key",
			"x-bf-freshness-newest":   "2026-11-01T10:00:00Z",
			"x-bf-freshness-dated":    "3",
			"x-bf-freshness-set-hash": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"x-chrono-count-at-newest": "1",
		},
	}
	hValid := Parse(reqValid)
	ApplyNativeCacheGuard(reqValid, &hValid, now)

	if hValid.GuardAction != "STAMPED_SAFE" {
		t.Fatalf("expected STAMPED_SAFE, got %s", hValid.GuardAction)
	}
	if reqValid.Headers["x-bf-cache-key"] != "sec8k-model-key:fc:2026-11-01T10:00:00Z:12345678" {
		t.Fatalf("expected stamped key, got %s", reqValid.Headers["x-bf-cache-key"])
	}

	// Case 2: Missing/invalid fingerprint -> BYPASSED_UNSAFE (stripped)
	reqUnsafe := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-cache-key": "unprotected-key",
			// No freshness headers
		},
	}
	hUnsafe := Parse(reqUnsafe)
	ApplyNativeCacheGuard(reqUnsafe, &hUnsafe, now)

	if hUnsafe.GuardAction != "BYPASSED_UNSAFE" {
		t.Fatalf("expected BYPASSED_UNSAFE, got %s", hUnsafe.GuardAction)
	}
	if _, exists := reqUnsafe.Headers["x-bf-cache-key"]; exists {
		t.Fatal("expected x-bf-cache-key to be stripped when unsafe")
	}
}

func TestParseMissingTenant(t *testing.T) {
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-chrono-subject": "Anthropic",
		},
	}
	h := Parse(req)
	if h.TenantID != "" {
		t.Fatalf("expected empty tenant ID when x-bf-vk missing, got %q", h.TenantID)
	}
}

func TestParseRefreshBypass(t *testing.T) {
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-chrono-refresh": "true",
		},
	}
	h := Parse(req)
	if !h.Refresh {
		t.Fatal("expected refresh true from x-chrono-refresh")
	}

	req2 := &schemas.HTTPRequest{
		Headers: map[string]string{
			"cache-control": "no-cache",
		},
	}
	h2 := Parse(req2)
	if !h2.Refresh {
		t.Fatal("expected refresh true from cache-control: no-cache")
	}
}

func BenchmarkParseHeaders(b *testing.B) {
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-vk":                  "vk-alpha-prod",
			"x-chrono-subject":         "Anthropic",
			"x-chrono-intent":          "pre-meeting",
			"x-chrono-window-days":     "30",
			"x-chrono-prompt-version":  "v1",
			"x-chrono-newest":          "2026-10-31T20:15:00Z",
			"x-chrono-count-at-newest": "2",
			"x-chrono-dated":           "5",
			"x-chrono-undated":         "0",
			"x-chrono-set-hash":        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"x-chrono-cache-mode":      "replay",
		},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Parse(req)
	}
}

func FuzzParseHeaders(f *testing.F) {
	f.Add("Stripe", "2026-11-01T12:00:00Z", "1", "5", "0", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	f.Add("", "", "", "", "", "")
	f.Add("A", "invalid-date", "-1", "-5", "999999", "short-hash")

	f.Fuzz(func(t *testing.T, subject, newest, count, dated, undated, hash string) {
		req := &schemas.HTTPRequest{
			Headers: map[string]string{
				"x-chrono-subject":         subject,
				"x-chrono-newest":          newest,
				"x-chrono-count-at-newest": count,
				"x-chrono-dated":           dated,
				"x-chrono-undated":         undated,
				"x-chrono-set-hash":        hash,
			},
		}
		// Must never panic on arbitrary input
		_ = Parse(req)
	})
}
