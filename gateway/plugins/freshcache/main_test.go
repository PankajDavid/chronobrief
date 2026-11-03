package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

func setupTestPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "test-audit.jsonl")
	snapPath := filepath.Join(tmpDir, "test-snapshot.json")

	cfg := map[string]any{
		"audit_path":          auditPath,
		"snapshot_path":       snapPath,
		"max_entries":         float64(100),
		"max_bytes":           float64(10 * 1024 * 1024),
		"ttl_seconds":         float64(3600),
		"future_skew_seconds": float64(3600),
		"exact_set":           true,
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
	if httpResp.Headers["x-bf-freshness-cache"] != "MISS_COLD" {
		t.Fatalf("expected x-bf-freshness-cache alias MISS_COLD, got %q", httpResp.Headers["x-bf-freshness-cache"])
	}
	if httpResp.Headers["x-chrono-shadow-ttl-5m"] != "MISS_COLD" {
		t.Fatalf("expected shadow TTL-5m MISS_COLD, got %q", httpResp.Headers["x-chrono-shadow-ttl-5m"])
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
	if httpRespHit.Headers["x-bf-freshness-cache"] != "HIT" {
		t.Fatalf("expected x-bf-freshness-cache HIT, got %q", httpRespHit.Headers["x-bf-freshness-cache"])
	}
	if httpRespHit.Headers["x-chrono-saved-tokens"] != "150" {
		t.Fatalf("expected saved tokens 150, got %q", httpRespHit.Headers["x-chrono-saved-tokens"])
	}
	if httpRespHit.Headers["x-chrono-shadow-ttl-24h"] != "HIT_FRESH" {
		t.Fatalf("expected shadow TTL-24h HIT_FRESH, got %q", httpRespHit.Headers["x-chrono-shadow-ttl-24h"])
	}
	if httpRespHit.Headers["x-chrono-plugin-overhead-us"] == "" {
		t.Fatal("expected x-chrono-plugin-overhead-us to be populated")
	}

	// 4. Cleanup
	if err := Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
}

func TestServerSideRequestID(t *testing.T) {
	bCtx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	req := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-freshcache-id": "client-spoofed-fc-id",
			"x-request-id":    "client-spoofed-req-id",
			"x-bf-vk":         "team-alpha",
		},
	}
	_, err := HTTPTransportPreHook(bCtx, req)
	if err != nil {
		t.Fatalf("HTTPTransportPreHook failed: %v", err)
	}

	serverReqID, ok := bCtx.Value(keyReqID).(string)
	if !ok || serverReqID == "" {
		t.Fatal("expected server-generated reqID in context")
	}
	if serverReqID == "client-spoofed-fc-id" || serverReqID == "client-spoofed-req-id" {
		t.Fatalf("security violation: client header was used as reqID: %s", serverReqID)
	}
	if !strings.HasPrefix(serverReqID, "fc-") {
		t.Fatalf("expected server-generated reqID to start with 'fc-', got %s", serverReqID)
	}
	if req.Headers["x-freshcache-id"] != serverReqID {
		t.Fatalf("expected request header x-freshcache-id to be overwritten with %s, got %s", serverReqID, req.Headers["x-freshcache-id"])
	}
}

func TestPruneOldReqOutcomes(t *testing.T) {
	now := time.Now()
	// Clear any existing
	reqOutcomes.Range(func(k, v any) bool {
		reqOutcomes.Delete(k)
		return true
	})

	// 1. Old entry (3 minutes ago) -> should be deleted
	reqOutcomes.Store("old-entry-1", &reqOutcome{
		createdAt: now.Add(-3 * time.Minute),
	})
	// 2. Old entry (5 minutes ago) -> should be deleted
	reqOutcomes.Store("old-entry-2", &reqOutcome{
		createdAt: now.Add(-5 * time.Minute),
	})
	// 3. Fresh entry (10 seconds ago) -> should be kept
	reqOutcomes.Store("fresh-entry", &reqOutcome{
		createdAt: now.Add(-10 * time.Second),
	})

	pruneOldReqOutcomes(now)

	if _, ok := reqOutcomes.Load("old-entry-1"); ok {
		t.Error("expected old-entry-1 to be pruned")
	}
	if _, ok := reqOutcomes.Load("old-entry-2"); ok {
		t.Error("expected old-entry-2 to be pruned")
	}
	if _, ok := reqOutcomes.Load("fresh-entry"); !ok {
		t.Error("expected fresh-entry to be preserved")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	setupTestPlugin(t)

	bCtx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	req := &schemas.HTTPRequest{
		Path:    "/x-chrono/metrics",
		Headers: map[string]string{},
	}

	resp, err := HTTPTransportPreHook(bCtx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected short-circuit HTTPResponse for metrics endpoint")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if resp.Headers["content-type"] != "application/json" {
		t.Fatalf("expected json content-type, got %s", resp.Headers["content-type"])
	}

	var payload metricsPayload
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("failed to decode metrics payload: %v", err)
	}
	if payload.Status != "healthy" {
		t.Fatalf("expected status healthy, got %s", payload.Status)
	}
}

func TestHookPanicRecoveryFailOpen(t *testing.T) {
	// Hooks must never panic or crash the Bifrost process
	resp, err := HTTPTransportPreHook(nil, nil)
	if err != nil || resp != nil {
		t.Errorf("expected nil resp and nil err on nil inputs, got %v, %v", resp, err)
	}

	dummyReq := &schemas.BifrostRequest{ChatRequest: &schemas.BifrostChatRequest{Model: "gemini-test"}}
	bReq, shortCircuit, err := PreLLMHook(nil, dummyReq)
	if err != nil || shortCircuit != nil {
		t.Errorf("expected nil shortCircuit and nil err on nil inputs, got %v, %v", shortCircuit, err)
	}
	if bReq != dummyReq {
		t.Errorf("expected bReq to be preserved intact, got %v", bReq)
	}

	dummyResp := &schemas.BifrostResponse{}
	bResp, bErr, err := PostLLMHook(nil, dummyResp, nil)
	if err != nil || bErr != nil {
		t.Errorf("expected nil bErr and nil err on nil inputs, got %v, %v", bErr, err)
	}
	if bResp != dummyResp {
		t.Errorf("expected bResp to be preserved intact, got %v", bResp)
	}

	err = HTTPTransportPostHook(nil, nil, nil)
	if err != nil {
		t.Errorf("expected nil err on nil inputs, got %v", err)
	}
}

func TestPruneOldReqOutcomesEvictsOldestUnderCap(t *testing.T) {
	now := time.Now()
	// Clear any existing
	reqOutcomes.Range(func(k, v any) bool {
		reqOutcomes.Delete(k)
		return true
	})

	// Insert 5,010 entries with distinct timestamps within the 2-minute TTL
	const total = 5010
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("req-%05d", i)
		// i=0 is oldest (50s ago), i=5009 is newest (0s ago)
		created := now.Add(-time.Duration(50000-i*10) * time.Millisecond)
		reqOutcomes.Store(key, &reqOutcome{
			createdAt: created,
		})
	}

	pruneOldReqOutcomes(now)

	// Count remaining
	remaining := 0
	reqOutcomes.Range(func(k, v any) bool {
		remaining++
		return true
	})

	if remaining != maxReqOutcomes {
		t.Fatalf("expected exactly %d remaining entries, got %d", maxReqOutcomes, remaining)
	}

	// The 10 oldest (req-00000 through req-00009) should have been evicted
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("req-%05d", i)
		if _, ok := reqOutcomes.Load(key); ok {
			t.Errorf("expected oldest entry %s to be evicted", key)
		}
	}

	// Newer entries (e.g. req-00010 through req-00019) should still exist
	for i := 10; i < 20; i++ {
		key := fmt.Sprintf("req-%05d", i)
		if _, ok := reqOutcomes.Load(key); !ok {
			t.Errorf("expected newer entry %s to be preserved", key)
		}
	}
}

func TestConcurrentReqOutcomesIsolation(t *testing.T) {
	setupTestPlugin(t)
	defer func() { _ = Cleanup() }()

	fixedHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Pre-seed a cache entry for "HIT-Subject"
	hitCtx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	hitReq := &schemas.HTTPRequest{
		Headers: map[string]string{
			"x-bf-vk":                  "team-alpha",
			"x-chrono-subject":         "HIT-Subject",
			"x-chrono-intent":          "pre-meeting",
			"x-chrono-window-days":     "30",
			"x-chrono-prompt-version":  "v1",
			"x-chrono-newest":          "2026-10-31T20:15:00Z",
			"x-chrono-count-at-newest": "1",
			"x-chrono-dated":           "5",
			"x-chrono-undated":         "0",
			"x-chrono-set-hash":        fixedHash,
			"x-chrono-cache-mode":      "replay",
		},
	}
	_, _ = HTTPTransportPreHook(hitCtx, hitReq)
	bifrostHitReq := &schemas.BifrostRequest{ChatRequest: &schemas.BifrostChatRequest{Model: "gemini-2.5-flash"}}
	_, _, _ = PreLLMHook(hitCtx, bifrostHitReq)
	content := "hit brief content"
	dummyResp := &schemas.BifrostResponse{
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
			Usage: &schemas.BifrostLLMUsage{TotalTokens: 100},
		},
	}
	_, _, _ = PostLLMHook(hitCtx, dummyResp, nil)
	httpRespHit := &schemas.HTTPResponse{Headers: make(map[string]string)}
	_ = HTTPTransportPostHook(hitCtx, hitReq, httpRespHit)
	if httpRespHit.Headers["x-chrono-cache"] != "MISS_COLD" {
		t.Fatalf("setup: expected MISS_COLD on seed, got %s", httpRespHit.Headers["x-chrono-cache"])
	}

	// Now run 30 concurrent requests with mixed types
	const goroutines = 30
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			bCtx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
			reqType := id % 3

			var expectedDecision string
			var httpReq *schemas.HTTPRequest

			switch reqType {
			case 0: // Missing tenant -> REFUSE_NO_TENANT
				expectedDecision = "REFUSE_NO_TENANT"
				httpReq = &schemas.HTTPRequest{
					Headers: map[string]string{
						"x-chrono-subject": fmt.Sprintf("NoTenant-%d", id),
					},
				}
			case 1: // Identical to pre-seeded entry -> HIT
				expectedDecision = "HIT"
				httpReq = &schemas.HTTPRequest{
					Headers: map[string]string{
						"x-bf-vk":                  "team-alpha",
						"x-chrono-subject":         "HIT-Subject",
						"x-chrono-intent":          "pre-meeting",
						"x-chrono-window-days":     "30",
						"x-chrono-prompt-version":  "v1",
						"x-chrono-newest":          "2026-10-31T20:15:00Z",
						"x-chrono-count-at-newest": "1",
						"x-chrono-dated":           "5",
						"x-chrono-undated":         "0",
						"x-chrono-set-hash":        fixedHash,
						"x-chrono-cache-mode":      "replay",
					},
				}
			case 2: // Unique subject and hash -> MISS_COLD
				expectedDecision = "MISS_COLD"
				httpReq = &schemas.HTTPRequest{
					Headers: map[string]string{
						"x-bf-vk":                  "team-alpha",
						"x-chrono-subject":         fmt.Sprintf("Unique-Subject-%d", id),
						"x-chrono-intent":          "pre-meeting",
						"x-chrono-window-days":     "30",
						"x-chrono-prompt-version":  "v1",
						"x-chrono-newest":          "2026-10-31T20:15:00Z",
						"x-chrono-count-at-newest": "1",
						"x-chrono-dated":           "5",
						"x-chrono-undated":         "0",
						"x-chrono-set-hash":        fmt.Sprintf("%064x", id+1),
						"x-chrono-cache-mode":      "replay",
					},
				}
			}

			_, err := HTTPTransportPreHook(bCtx, httpReq)
			if err != nil {
				errs <- fmt.Errorf("[%d] HTTPTransportPreHook: %w", id, err)
				return
			}

			bReq := &schemas.BifrostRequest{
				ChatRequest: &schemas.BifrostChatRequest{Model: "gemini-2.5-flash"},
			}
			_, shortCircuit, err := PreLLMHook(bCtx, bReq)
			if err != nil {
				errs <- fmt.Errorf("[%d] PreLLMHook: %w", id, err)
				return
			}

			if shortCircuit != nil {
				_, _, err = PostLLMHook(bCtx, shortCircuit.Response, nil)
			} else {
				respContent := fmt.Sprintf("content-%d", id)
				llmResp := &schemas.BifrostResponse{
					ChatResponse: &schemas.BifrostChatResponse{
						Choices: []schemas.BifrostResponseChoice{
							{
								ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
									Message: &schemas.ChatMessage{
										Role:    schemas.ChatMessageRoleAssistant,
										Content: &schemas.ChatMessageContent{ContentStr: &respContent},
									},
								},
							},
						},
						Usage: &schemas.BifrostLLMUsage{TotalTokens: 100},
					},
				}
				_, _, err = PostLLMHook(bCtx, llmResp, nil)
			}
			if err != nil {
				errs <- fmt.Errorf("[%d] PostLLMHook: %w", id, err)
				return
			}

			httpResp := &schemas.HTTPResponse{Headers: make(map[string]string)}
			err = HTTPTransportPostHook(bCtx, httpReq, httpResp)
			if err != nil {
				errs <- fmt.Errorf("[%d] HTTPTransportPostHook: %w", id, err)
				return
			}

			gotDecision := httpResp.Headers["x-chrono-cache"]
			if gotDecision != expectedDecision {
				errs <- fmt.Errorf("[%d] expected decision %s, got %s (subject=%s)", id, expectedDecision, gotDecision, httpReq.Headers["x-chrono-subject"])
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrency error: %v", err)
	}
}
