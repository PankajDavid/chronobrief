package store

import (
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
