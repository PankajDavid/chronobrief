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
	if h.TenantID != "vk-alpha-prod" {
		t.Fatalf("expected tenant vk-alpha-prod, got %q", h.TenantID)
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
