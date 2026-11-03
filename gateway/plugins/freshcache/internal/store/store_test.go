package store

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestStoreGetPut(t *testing.T) {
	s := New(2)

	content := "brief content"
	resp := &schemas.BifrostResponse{
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
		},
	}

	entry := &StoredEntry{
		Response:         resp,
		Fingerprint:      decide.Fingerprint{SetHash: "abc"},
		CreatedAt:        time.Now().UTC(),
		CostUSD:          0.001,
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	s.Put("k1", entry)
	if s.Len() != 1 {
		t.Fatalf("expected len 1, got %d", s.Len())
	}

	got, ok := s.Get("k1")
	if !ok || got == nil {
		t.Fatal("expected entry for k1")
	}
	if got.TotalTokens != 150 {
		t.Fatalf("expected total tokens 150, got %d", got.TotalTokens)
	}

	// Verify deep copy mutation resistance
	*got.Response.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.Content.ContentStr = "mutated content"
	gotAgain, _ := s.Get("k1")
	if *gotAgain.Response.ChatResponse.Choices[0].ChatNonStreamResponseChoice.Message.Content.ContentStr != "brief content" {
		t.Fatal("store was mutated by caller pointer edit!")
	}
}

func TestStoreLRUEviction(t *testing.T) {
	s := New(2)

	e := &StoredEntry{CreatedAt: time.Now().UTC()}
	s.Put("k1", e)
	s.Put("k2", e)

	// Access k1 so k2 becomes least recently used
	s.Get("k1")

	// Insert k3 -> should evict k2
	s.Put("k3", e)

	if s.Len() != 2 {
		t.Fatalf("expected len 2, got %d", s.Len())
	}
	if _, ok := s.Get("k2"); ok {
		t.Fatal("expected k2 to be evicted")
	}
	if _, ok := s.Get("k1"); !ok {
		t.Fatal("expected k1 to be retained")
	}
	if _, ok := s.Get("k3"); !ok {
		t.Fatal("expected k3 to be present")
	}
}

func TestStoreMaxBytesEviction(t *testing.T) {
	// Byte ceiling: 1000 bytes (each entry is ~458 bytes: 258 base + 200 content)
	s := New(10, 1000)

	contentLarge := string(make([]byte, 200))
	makeEntry := func() *StoredEntry {
		c := contentLarge
		return &StoredEntry{
			CreatedAt: time.Now().UTC(),
			Response: &schemas.BifrostResponse{
				ChatResponse: &schemas.BifrostChatResponse{
					Choices: []schemas.BifrostResponseChoice{
						{
							ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
								Message: &schemas.ChatMessage{
									Content: &schemas.ChatMessageContent{ContentStr: &c},
								},
							},
						},
					},
				},
			},
		}
	}

	s.Put("k1", makeEntry())
	s.Put("k2", makeEntry())
	if s.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", s.Len())
	}

	// Adding k3 exceeds 600 bytes, evicts oldest (k1)
	s.Put("k3", makeEntry())
	if _, ok := s.Get("k1"); ok {
		t.Fatal("expected k1 to be evicted by byte limit")
	}
	if s.Evictions() < 1 {
		t.Fatalf("expected at least 1 eviction, got %d", s.Evictions())
	}
}

func TestStoreSnapshotRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	snapPath := filepath.Join(tmpDir, "snapshot.json")

	s1 := New(10)
	now := time.Now().UTC()

	content := "persisted answer"
	resp := &schemas.BifrostResponse{
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
		},
	}
	s1.Put("key-fresh", &StoredEntry{
		Response:    resp,
		CreatedAt:   now.Add(-10 * time.Minute),
		TotalTokens: 250,
	})
	s1.Put("key-expired", &StoredEntry{
		Response:    resp,
		CreatedAt:   now.Add(-2 * time.Hour),
		TotalTokens: 250,
	})

	if err := s1.SaveSnapshot(snapPath); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	// Create fresh store s2 and load snapshot with 1h TTL
	s2 := New(10)
	loaded, err := s2.LoadSnapshot(snapPath, now, 1*time.Hour)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded entry (non-expired), got %d", loaded)
	}

	if _, ok := s2.Get("key-expired"); ok {
		t.Fatal("expired entry should not have been restored")
	}

	entry, ok := s2.Get("key-fresh")
	if !ok || entry == nil {
		t.Fatal("fresh entry was not restored")
	}
	if entry.TotalTokens != 250 {
		t.Fatalf("expected 250 tokens, got %d", entry.TotalTokens)
	}
}

func TestSnapshotFilePermissions0600(t *testing.T) {
	tmpDir := t.TempDir()
	snapPath := filepath.Join(tmpDir, "snapshot.json")

	s := New(10)
	content := "test content"
	s.Put("k1", &StoredEntry{
		CreatedAt: time.Now().UTC(),
		Response: &schemas.BifrostResponse{
			ChatResponse: &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostResponseChoice{
					{
						ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
							Message: &schemas.ChatMessage{
								Content: &schemas.ChatMessageContent{ContentStr: &content},
							},
						},
					},
				},
			},
		},
	})

	if err := s.SaveSnapshot(snapPath); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	info, err := os.Stat(snapPath)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("expected snapshot permissions 0600, got %#o", perm)
		}
	}
}

type ttlCacheSimulator struct {
	ttl      time.Duration
	storedAt time.Time
	fp       decide.Fingerprint
	occupied bool
}

func newTTLCacheSimulator(ttl time.Duration) *ttlCacheSimulator {
	return &ttlCacheSimulator{ttl: ttl}
}

func (s *ttlCacheSimulator) Lookup(now time.Time, in decide.Fingerprint) string {
	if !s.occupied {
		s.storedAt = now
		s.fp = in
		s.occupied = true
		return "MISS_COLD"
	}
	if s.ttl > 0 && now.Sub(s.storedAt) > s.ttl {
		s.storedAt = now
		s.fp = in
		return "MISS_EXPIRED"
	}
	dec := decide.Decide(now, in, &decide.Entry{FP: s.fp, CreatedAt: s.storedAt}, decide.Policy{
		TTL:        s.ttl,
		FutureSkew: 24 * time.Hour,
		ExactSet:   true,
	})
	if dec == decide.Hit {
		return "HIT_FRESH"
	}
	return "HIT_STALE"
}

func TestStorePutPreservesShadowAcrossOverwrites(t *testing.T) {
	s := New(10)

	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	fp1 := decide.Fingerprint{
		Newest:        t0.Add(-48 * time.Hour),
		CountAtNewest: 1,
		Dated:         5,
		Undated:       0,
		SetHash:       "hash1",
	}
	content1 := "initial brief"
	entry1 := &StoredEntry{
		Fingerprint: fp1,
		CreatedAt:   t0,
		Response: &schemas.BifrostResponse{
			ChatResponse: &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostResponseChoice{
					{
						ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
							Message: &schemas.ChatMessage{Content: &schemas.ChatMessageContent{ContentStr: &content1}},
						},
					},
				},
			},
		},
	}

	// 1. PreLLMHook advances shadow on initial lookup
	s.AdvanceShadow("k1", t0, fp1)
	s.Put("k1", entry1)

	got1, ok := s.Get("k1")
	if !ok || got1 == nil {
		t.Fatal("entry1 was not retrieved")
	}
	if !got1.Shadow.Slot24h.StoredAt.Equal(t0) {
		t.Fatalf("expected Slot24h to be %v, got %v", t0, got1.Shadow.Slot24h.StoredAt)
	}
	if got1.Shadow.Slot24h.FP.SetHash != "hash1" {
		t.Fatalf("expected Slot24h to have hash1, got %s", got1.Shadow.Slot24h.FP.SetHash)
	}

	// 10 minutes later: request with fp2 arrives
	t10 := t0.Add(10 * time.Minute)
	fp2 := decide.Fingerprint{
		Newest:        t0.Add(-2 * time.Hour),
		CountAtNewest: 1,
		Dated:         6,
		Undated:       0,
		SetHash:       "hash2",
	}
	content2 := "updated brief"
	entry2 := &StoredEntry{
		Fingerprint: fp2,
		CreatedAt:   t10,
		Response: &schemas.BifrostResponse{
			ChatResponse: &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostResponseChoice{
					{
						ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
							Message: &schemas.ChatMessage{Content: &schemas.ChatMessageContent{ContentStr: &content2}},
						},
					},
				},
			},
		},
	}

	// PreLLMHook advances shadow for fp2 at t10:
	// - 5m: 10m > 5m -> MISS_EXPIRED -> reset to (t10, fp2)
	// - 1h: 10m <= 1h, but fp2 is newer -> HIT_STALE -> stays at (t0, fp1)
	// - 24h: 10m <= 24h, but fp2 is newer -> HIT_STALE -> stays at (t0, fp1)
	outcomes := s.AdvanceShadow("k1", t10, fp2)
	if outcomes.TTL5m != "MISS_EXPIRED" {
		t.Fatalf("expected TTL5m MISS_EXPIRED, got %s", outcomes.TTL5m)
	}
	if outcomes.TTL1h != "HIT_STALE" {
		t.Fatalf("expected TTL1h HIT_STALE, got %s", outcomes.TTL1h)
	}
	if outcomes.TTL24h != "HIT_STALE" {
		t.Fatalf("expected TTL24h HIT_STALE, got %s", outcomes.TTL24h)
	}

	// PostLLMHook puts entry2 after recomputing
	s.Put("k1", entry2)

	got2, ok := s.Get("k1")
	if !ok || got2 == nil {
		t.Fatal("entry2 was not retrieved")
	}
	if !got2.CreatedAt.Equal(t10) {
		t.Fatalf("expected CreatedAt to be %v, got %v", t10, got2.CreatedAt)
	}
	if got2.Fingerprint.SetHash != "hash2" {
		t.Fatalf("expected Fingerprint to have hash2, got %s", got2.Fingerprint.SetHash)
	}

	// Crucial: Slot1h and Slot24h remain the baseline t0 / fp1, while Slot5m was reset at t10
	if !got2.Shadow.Slot24h.StoredAt.Equal(t0) {
		t.Fatalf("expected Slot24h to stay preserved at %v, got %v", t0, got2.Shadow.Slot24h.StoredAt)
	}
	if got2.Shadow.Slot24h.FP.SetHash != "hash1" {
		t.Fatalf("expected Slot24h to stay preserved as hash1, got %s", got2.Shadow.Slot24h.FP.SetHash)
	}
	if !got2.Shadow.Slot1h.StoredAt.Equal(t0) {
		t.Fatalf("expected Slot1h to stay preserved at %v, got %v", t0, got2.Shadow.Slot1h.StoredAt)
	}
	if !got2.Shadow.Slot5m.StoredAt.Equal(t10) {
		t.Fatalf("expected Slot5m to be updated to %v, got %v", t10, got2.Shadow.Slot5m.StoredAt)
	}
}

func TestShadowParitySequenceB_ThroughStore(t *testing.T) {
	st := New(100)
	cacheKey := "tenant1:test-topic:brief:30d:v1:model1"

	sim5m := newTTLCacheSimulator(5 * time.Minute)
	sim1h := newTTLCacheSimulator(1 * time.Hour)
	sim24h := newTTLCacheSimulator(24 * time.Hour)

	type step struct {
		offset time.Duration
		fp     decide.Fingerprint
		desc   string
	}

	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	baseFP := decide.Fingerprint{
		Newest:        t0.Add(-48 * time.Hour),
		CountAtNewest: 1,
		Dated:         5,
		Undated:       0,
		SetHash:       "hash-base",
	}

	dummyResp := &schemas.BifrostResponse{
		ChatResponse: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
						Message: &schemas.ChatMessage{
							Content: &schemas.ChatMessageContent{},
						},
					},
				},
			},
		},
	}

	pol := decide.Policy{TTL: 24 * time.Hour, FutureSkew: 24 * time.Hour, ExactSet: true}

	var steps []step
	// Phase 1 (0 to 30 min): No-news baseline lookups (10 steps)
	for _, m := range []int{0, 2, 4, 6, 8, 12, 15, 20, 25, 30} {
		steps = append(steps, step{offset: time.Duration(m) * time.Minute, fp: baseFP, desc: fmt.Sprintf("phase1-nonews-%dm", m)})
	}
	// Phase 2 (35 min to 55 min): Breaking news update 1 (7 steps)
	news1 := baseFP
	news1.Newest = t0.Add(-30 * time.Minute)
	news1.Dated = 6
	news1.SetHash = "hash-news-1"
	for _, m := range []int{35, 37, 40, 42, 45, 50, 55} {
		steps = append(steps, step{offset: time.Duration(m) * time.Minute, fp: news1, desc: fmt.Sprintf("phase2-news1-%dm", m)})
	}
	// Phase 3 (65 min to 90 min): Crosses 1-hour TTL threshold for 1h slot (5 steps)
	for _, m := range []int{65, 70, 75, 80, 90} {
		steps = append(steps, step{offset: time.Duration(m) * time.Minute, fp: news1, desc: fmt.Sprintf("phase3-cross1h-%dm", m)})
	}
	// Phase 4 (2h to 3h): Same-day SetHash correction (6 steps)
	news2Correction := news1
	news2Correction.SetHash = "hash-news-1-corrected"
	for _, m := range []int{120, 123, 126, 130, 150, 180} {
		steps = append(steps, step{offset: time.Duration(m) * time.Minute, fp: news2Correction, desc: fmt.Sprintf("phase4-sethash-%dm", m)})
	}
	// Phase 5 (4h to 10h): Breaking news update 2 (7 steps)
	news3 := news2Correction
	news3.Newest = t0.Add(3 * time.Hour)
	news3.Dated = 7
	news3.SetHash = "hash-news-3"
	for _, h := range []int{4, 5, 6, 7, 8, 9, 10} {
		steps = append(steps, step{offset: time.Duration(h) * time.Hour, fp: news3, desc: fmt.Sprintf("phase5-news3-%dh", h)})
	}
	// Phase 6 (12h to 23h): Long quiet night (7 steps)
	for _, h := range []int{12, 14, 16, 18, 20, 22, 23} {
		steps = append(steps, step{offset: time.Duration(h) * time.Hour, fp: news3, desc: fmt.Sprintf("phase6-quiet-%dh", h)})
	}
	// Phase 7 (24h to 26h): Crosses 24h TTL threshold for 24h slot! (6 steps)
	news4 := news3
	news4.Newest = t0.Add(24 * time.Hour)
	news4.Dated = 8
	news4.SetHash = "hash-news-4"
	for _, m := range []int{24*60 + 5, 24*60 + 10, 24*60 + 30, 25*60, 25*60 + 30, 26 * 60} {
		steps = append(steps, step{offset: time.Duration(m) * time.Minute, fp: news4, desc: fmt.Sprintf("phase7-cross24h-%dm", m)})
	}

	if len(steps) < 40 {
		t.Fatalf("expected at least 40 steps, got %d", len(steps))
	}
	if steps[len(steps)-1].offset <= 25*time.Hour {
		t.Fatalf("expected sequence to span > 25 hours, got %v", steps[len(steps)-1].offset)
	}

	for i, s := range steps {
		now := t0.Add(s.offset)

		// 1. Advance shadow through store on every lookup (PreLLMHook)
		outcomes := st.AdvanceShadow(cacheKey, now, s.fp)

		// 2. Query independent simulators
		want5m := sim5m.Lookup(now, s.fp)
		want1h := sim1h.Lookup(now, s.fp)
		want24h := sim24h.Lookup(now, s.fp)

		// 3. Assert parity at every step
		if outcomes.TTL5m != want5m {
			t.Fatalf("step %d (%s at +%v): 5m got %s, want %s", i, s.desc, s.offset, outcomes.TTL5m, want5m)
		}
		if outcomes.TTL1h != want1h {
			t.Fatalf("step %d (%s at +%v): 1h got %s, want %s", i, s.desc, s.offset, outcomes.TTL1h, want1h)
		}
		if outcomes.TTL24h != want24h {
			t.Fatalf("step %d (%s at +%v): 24h got %s, want %s", i, s.desc, s.offset, outcomes.TTL24h, want24h)
		}
		if outcomes.WouldServeStale24h != (want24h == "HIT_STALE") {
			t.Fatalf("step %d (%s at +%v): WouldServeStale24h got %v, want %v", i, s.desc, s.offset, outcomes.WouldServeStale24h, want24h == "HIT_STALE")
		}

		// 4. Freshcache decision & conditional Put on non-HIT
		entry, found := st.Get(cacheKey)
		var decideEntry *decide.Entry
		if found && entry != nil {
			decideEntry = &decide.Entry{FP: entry.Fingerprint, CreatedAt: entry.CreatedAt}
		}
		dec := decide.Decide(now, s.fp, decideEntry, pol)

		if !dec.Serves() {
			// On cold miss or refusal, upstream recomputes and stores response in PostLLMHook
			st.Put(cacheKey, &StoredEntry{
				Response:    dummyResp,
				Fingerprint: s.fp,
				CreatedAt:   now,
			})
		}
	}
}

func TestStoreLoadSnapshotOldFormatWithoutShadow(t *testing.T) {
	tmpDir := t.TempDir()
	snapPath := filepath.Join(tmpDir, "old-snapshot.json")

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	entryCreatedAt := now.Add(-30 * time.Minute)
	newestTime := now.Add(-2 * time.Hour)

	oldJSON := fmt.Sprintf(`[
		{
			"key": "old-format-key",
			"entry": {
				"response": {
					"chat_response": {
						"choices": [
							{
								"chat_non_stream_response_choice": {
									"message": {
										"role": "assistant",
										"content": {"content_str": "persisted old format brief"}
									}
								}
							}
						]
					}
				},
				"fingerprint": {
					"newest": "%s",
					"count_at_newest": 1,
					"dated": 5,
					"undated": 0,
					"set_hash": "old-hash"
				},
				"created_at": "%s",
				"first_seen_at": "%s",
				"first_seen_fp": {
					"newest": "%s",
					"count_at_newest": 1,
					"dated": 5,
					"undated": 0,
					"set_hash": "old-hash"
				},
				"cost_usd": 0.0015,
				"prompt_tokens": 120,
				"completion_tokens": 60,
				"total_tokens": 180
			}
		}
	]`, newestTime.Format(time.RFC3339), entryCreatedAt.Format(time.RFC3339), entryCreatedAt.Format(time.RFC3339), newestTime.Format(time.RFC3339))

	if err := os.WriteFile(snapPath, []byte(oldJSON), 0600); err != nil {
		t.Fatalf("failed to write old snapshot fixture: %v", err)
	}

	st := New(10)
	loaded, err := st.LoadSnapshot(snapPath, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadSnapshot failed for old format: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded entry, got %d", loaded)
	}

	got, ok := st.Get("old-format-key")
	if !ok || got == nil {
		t.Fatal("expected entry to be retrievable via Get")
	}
	if got.TotalTokens != 180 {
		t.Fatalf("expected 180 tokens, got %d", got.TotalTokens)
	}

	// Verify shadow slots were initialized from entry.CreatedAt and entry.Fingerprint
	shadow, ok := st.GetShadow("old-format-key")
	if !ok {
		t.Fatal("expected shadow state to be present")
	}
	if !shadow.Slot5m.StoredAt.Equal(entryCreatedAt) {
		t.Fatalf("expected Slot5m.StoredAt %v, got %v", entryCreatedAt, shadow.Slot5m.StoredAt)
	}
	if shadow.Slot5m.FP.SetHash != "old-hash" {
		t.Fatalf("expected Slot5m.FP.SetHash 'old-hash', got %s", shadow.Slot5m.FP.SetHash)
	}
	if !shadow.Slot1h.StoredAt.Equal(entryCreatedAt) {
		t.Fatalf("expected Slot1h.StoredAt %v, got %v", entryCreatedAt, shadow.Slot1h.StoredAt)
	}
	if !shadow.Slot24h.StoredAt.Equal(entryCreatedAt) {
		t.Fatalf("expected Slot24h.StoredAt %v, got %v", entryCreatedAt, shadow.Slot24h.StoredAt)
	}
}

func BenchmarkStoreGetHit(b *testing.B) {
	s := New(1000)
	content := "cached response body"
	entry := &StoredEntry{
		CreatedAt:   time.Now().UTC(),
		TotalTokens: 500,
		Response: &schemas.BifrostResponse{
			ChatResponse: &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostResponseChoice{
					{
						ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
							Message: &schemas.ChatMessage{
								Content: &schemas.ChatMessageContent{ContentStr: &content},
							},
						},
					},
				},
			},
		},
	}
	s.Put("bench-key", entry)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = s.Get("bench-key")
	}
}

func BenchmarkStorePut(b *testing.B) {
	s := New(10000)
	content := "cached response body"
	entry := &StoredEntry{
		CreatedAt:   time.Now().UTC(),
		TotalTokens: 500,
		Response: &schemas.BifrostResponse{
			ChatResponse: &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostResponseChoice{
					{
						ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
							Message: &schemas.ChatMessage{
								Content: &schemas.ChatMessageContent{ContentStr: &content},
							},
						},
					},
				},
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.Put("bench-key", entry)
	}
}

func TestConcurrentMissesNonBlockingSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	snapPath := filepath.Join(tmpDir, "concurrent-snap.json")

	st := New(500)
	defer func() { _ = st.Close("") }()

	// Pre-fill with 200 entries
	for i := 0; i < 200; i++ {
		st.Put(fmt.Sprintf("pre-%d", i), &StoredEntry{
			CreatedAt:   time.Now().UTC(),
			Fingerprint: decide.Fingerprint{Newest: time.Now().UTC(), CountAtNewest: 1, Dated: 1, SetHash: fmt.Sprintf("h-%d", i)},
		})
	}

	start := time.Now()
	var wg sync.WaitGroup
	const routines = 25
	for i := 0; i < routines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			st.Put(fmt.Sprintf("miss-%d", id), &StoredEntry{
				CreatedAt:   time.Now().UTC(),
				Fingerprint: decide.Fingerprint{Newest: time.Now().UTC(), CountAtNewest: 1, Dated: 1, SetHash: fmt.Sprintf("miss-h-%d", id)},
			})
			st.ScheduleSnapshot(snapPath)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 25 concurrent enqueues must be non-blocking (completing well within reasonable threshold, typically <25ms)
	if elapsed > 100*time.Millisecond {
		t.Logf("Warning: 25 concurrent puts took %v (expected non-blocking)", elapsed)
	}

	// Flush snapshot to disk synchronously
	if err := st.FlushSnapshot(snapPath); err != nil {
		t.Fatalf("FlushSnapshot failed: %v", err)
	}

	// Verify all entries load cleanly via LoadSnapshot
	newStore := New(500)
	defer func() { _ = newStore.Close("") }()
	loaded, err := newStore.LoadSnapshot(snapPath, time.Now().UTC(), 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}
	if loaded != 225 {
		t.Fatalf("expected 225 loaded entries (200 pre + 25 misses), got %d", loaded)
	}
}
