package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

func setupTestPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "test-audit.jsonl")

	cfg := map[string]any{
		"audit_path": auditPath,
		"max_entries": float64(100),
		"ttl_seconds": float64(3600),
		"future_skew_seconds": float64(3600),
		"exact_set": true,
	}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestPluginLifecycle(t *testing.T) {
	setupTestPlugin(t)

	if name := GetName(); name != "freshcache" {
		t.Fatalf("expected name freshcache, got %q", name)
	}

	bCtx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	content := "brief content"

	// 1. Missing tenant fails closed (REFUSE_NO_TENANT)
	reqHttpNoTenant := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-chrono-subject": "Anthropic",
		},
	}
	_, err := HTTPTransportPreHook(bCtx, reqHttpNoTenant)
	if err != nil {
		t.Fatalf("HTTPTransportPreHook failed: %v", err)
	}

	bifrostReq := &schemas.BifrostRequest{
		ChatRequest: &schemas.BifrostChatRequest{
			Model: "gemini-2.5-flash",
		},
	}
	_, shortCircuit, err := PreLLMHook(bCtx, bifrostReq)
	if err != nil {
		t.Fatalf("PreLLMHook failed: %v", err)
	}
	if shortCircuit != nil {
		t.Fatal("expected no short-circuit when tenant missing")
	}
	dec, _ := bCtx.Value(keyDecision).(decide.Decision)
	if dec != decide.RefuseNoTenant {
		t.Fatalf("expected REFUSE_NO_TENANT, got %q", dec)
	}

	// 2. Normal Request with Tenant (MISS_COLD -> Store -> HIT)
	bCtx2 := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	reqHttpValid := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-vk":                  "team-alpha",
			"x-chrono-subject":         "Anthropic",
			"x-chrono-intent":          "pre-meeting",
			"x-chrono-window-days":     "30",
			"x-chrono-prompt-version":  "v1",
			"x-chrono-newest":          "2026-10-31T20:15:00Z",
			"x-chrono-count-at-newest": "1",
			"x-chrono-dated":           "5",
			"x-chrono-undated":         "0",
			"x-chrono-set-hash":        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"x-chrono-cache-mode":      "replay",
		},
	}

	_, _ = HTTPTransportPreHook(bCtx2, reqHttpValid)
	_, shortCircuit2, _ := PreLLMHook(bCtx2, bifrostReq)
	if shortCircuit2 != nil {
		t.Fatal("expected cold miss on first request")
	}
	dec2, _ := bCtx2.Value(keyDecision).(decide.Decision)
	if dec2 != decide.MissCold {
		t.Fatalf("expected MISS_COLD, got %q", dec2)
	}

	// Simulate successful LLM response & PostLLMHook store
	bifrostResp := &schemas.BifrostResponse{
		ChatResponse: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
						Message: &schemas.ChatMessage{
							Role:    schemas.ChatMessageRoleAssistant,
							Content: &schemas.ChatMessageContent{ContentStr: &content},
						},
					},
				},
			},
			Usage: &schemas.BifrostLLMUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
	}

	_, _, err = PostLLMHook(bCtx2, bifrostResp, nil)
	if err != nil {
		t.Fatalf("PostLLMHook failed: %v", err)
	}

	// Verify HTTPTransportPostHook response headers
	httpResp := &schemas.HTTPResponse{Headers: make(map[string]string)}
	_ = HTTPTransportPostHook(bCtx2, reqHttpValid, httpResp)
	if httpResp.Headers["x-chrono-cache"] != "MISS_COLD" {
		t.Fatalf("expected x-chrono-cache MISS_COLD, got %q", httpResp.Headers["x-chrono-cache"])
	}

	// 3. Second Identical Request -> Cache HIT
	bCtx3 := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	_, _ = HTTPTransportPreHook(bCtx3, reqHttpValid)
	_, shortCircuit3, err := PreLLMHook(bCtx3, bifrostReq)
	if err != nil {
		t.Fatalf("PreLLMHook failed: %v", err)
	}
	if shortCircuit3 == nil || shortCircuit3.Response == nil {
		t.Fatal("expected cache HIT short-circuit on second request")
	}
	dec3, _ := bCtx3.Value(keyDecision).(decide.Decision)
	if dec3 != decide.Hit {
		t.Fatalf("expected HIT decision, got %q", dec3)
	}

	// PostLLMHook on HIT should not fail or re-store
	_, _, err = PostLLMHook(bCtx3, shortCircuit3.Response, nil)
	if err != nil {
		t.Fatalf("PostLLMHook on HIT failed: %v", err)
	}

	httpRespHit := &schemas.HTTPResponse{Headers: make(map[string]string)}
	_ = HTTPTransportPostHook(bCtx3, reqHttpValid, httpRespHit)
	if httpRespHit.Headers["x-chrono-cache"] != "HIT" {
		t.Fatalf("expected x-chrono-cache HIT, got %q", httpRespHit.Headers["x-chrono-cache"])
	}
	if httpRespHit.Headers["x-chrono-saved-tokens"] != "150" {
		t.Fatalf("expected saved tokens 150, got %q", httpRespHit.Headers["x-chrono-saved-tokens"])
	}

	// 4. Cleanup
	if err := Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
}
