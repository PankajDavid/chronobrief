package store

import (
	"os"
	"path/filepath"
	"runtime"
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
