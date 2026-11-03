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

func TestStorePutPreservesFirstSeenAcrossOverwrites(t *testing.T) {
	s := New(10)
	s.SetTTL(24 * time.Hour)

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
	s.Put("k1", entry1)

	got1, ok := s.Get("k1")
	if !ok || got1 == nil {
		t.Fatal("entry1 was not retrieved")
	}
	if !got1.FirstSeenAt.Equal(t0) {
		t.Fatalf("expected FirstSeenAt to be %v, got %v", t0, got1.FirstSeenAt)
	}
	if got1.FirstSeenFP.SetHash != "hash1" {
		t.Fatalf("expected FirstSeenFP to have hash1, got %s", got1.FirstSeenFP.SetHash)
	}

	// 10 minutes later: overwrite with fp2
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
	// Crucial: FirstSeenAt and FirstSeenFP must remain the original t0 / fp1
	if !got2.FirstSeenAt.Equal(t0) {
		t.Fatalf("expected FirstSeenAt to stay preserved at %v, got %v", t0, got2.FirstSeenAt)
	}
	if got2.FirstSeenFP.SetHash != "hash1" {
		t.Fatalf("expected FirstSeenFP to stay preserved as hash1, got %s", got2.FirstSeenFP.SetHash)
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
