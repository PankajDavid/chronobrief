package store

import (
	"container/list"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

// StoredEntry represents a cached response along with its provenance and pricing data.
type StoredEntry struct {
	Response         *schemas.BifrostResponse `json:"response"`
	Fingerprint      decide.Fingerprint       `json:"fingerprint"`
	CreatedAt        time.Time                `json:"created_at"`
	CostUSD          float64                  `json:"cost_usd"`
	PromptTokens     int                      `json:"prompt_tokens"`
	CompletionTokens int                      `json:"completion_tokens"`
	TotalTokens      int                      `json:"total_tokens"`
}

// DeepCopy clones a StoredEntry including a fully detached BifrostResponse.
func (e *StoredEntry) DeepCopy() *StoredEntry {
	if e == nil {
		return nil
	}
	cp := &StoredEntry{
		Fingerprint:      e.Fingerprint,
		CreatedAt:        e.CreatedAt,
		CostUSD:          e.CostUSD,
		PromptTokens:     e.PromptTokens,
		CompletionTokens: e.CompletionTokens,
		TotalTokens:      e.TotalTokens,
	}
	if e.Response != nil {
		if data, err := json.Marshal(e.Response); err == nil {
			var respCopy schemas.BifrostResponse
			if err := json.Unmarshal(data, &respCopy); err == nil {
				cp.Response = &respCopy
			}
		}
	}
	return cp
}

// EstimatedSize returns an approximation of the memory footprint in bytes.
func (e *StoredEntry) EstimatedSize(key string) int64 {
	base := int64(len(key) + len(e.Fingerprint.SetHash) + 256)
	if e.Response != nil && e.Response.ChatResponse != nil {
		for _, choice := range e.Response.ChatResponse.Choices {
			if choice.ChatNonStreamResponseChoice != nil &&
				choice.ChatNonStreamResponseChoice.Message != nil &&
				choice.ChatNonStreamResponseChoice.Message.Content != nil &&
				choice.ChatNonStreamResponseChoice.Message.Content.ContentStr != nil {
				base += int64(len(*choice.ChatNonStreamResponseChoice.Message.Content.ContentStr))
			}
		}
	}
	return base
}

type lruItem struct {
	key   string
	entry *StoredEntry
	bytes int64
}

// Store is a thread-safe, dual-bounded LRU cache (bounded by entry count and bytes).
type Store struct {
	mu           sync.RWMutex
	maxEntries   int
	maxBytes     int64
	currentBytes int64
	evictions    int64
	items        map[string]*list.Element
	evictList    *list.List
}

// New creates an LRU store with a given maximum capacity.
// If maxBytes is <= 0, default 64 MB (64 * 1024 * 1024) is used.
func New(maxEntries int, maxBytes ...int64) *Store {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	var mb int64 = 64 * 1024 * 1024
	if len(maxBytes) > 0 && maxBytes[0] > 0 {
		mb = maxBytes[0]
	}
	return &Store{
		maxEntries: maxEntries,
		maxBytes:   mb,
		items:      make(map[string]*list.Element),
		evictList:  list.New(),
	}
}

// Get retrieves an entry by key, moving it to front, and returns a deep copy.
func (s *Store) Get(key string) (*StoredEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, exists := s.items[key]
	if !exists {
		return nil, false
	}
	s.evictList.MoveToFront(elem)
	item := elem.Value.(*lruItem)
	return item.entry.DeepCopy(), true
}

// Put inserts or updates an entry, evicting the oldest if capacity or byte limit is exceeded.
func (s *Store) Put(key string, entry *StoredEntry) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entryCopy := entry.DeepCopy()
	itemSize := entryCopy.EstimatedSize(key)

	if elem, exists := s.items[key]; exists {
		s.evictList.MoveToFront(elem)
		oldItem := elem.Value.(*lruItem)
		s.currentBytes -= oldItem.bytes
		oldItem.entry = entryCopy
		oldItem.bytes = itemSize
		s.currentBytes += itemSize
		s.evictToLimits()
		return
	}

	elem := s.evictList.PushFront(&lruItem{key: key, entry: entryCopy, bytes: itemSize})
	s.items[key] = elem
	s.currentBytes += itemSize

	s.evictToLimits()
}

func (s *Store) evictToLimits() {
	for s.evictList.Len() > s.maxEntries || (s.maxBytes > 0 && s.currentBytes > s.maxBytes) {
		oldest := s.evictList.Back()
		if oldest == nil {
			break
		}
		s.evictList.Remove(oldest)
		kv := oldest.Value.(*lruItem)
		delete(s.items, kv.key)
		s.currentBytes -= kv.bytes
		s.evictions++
	}
}

// Len returns current number of cached entries.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// CurrentBytes returns total estimated memory usage of cached entries in bytes.
func (s *Store) CurrentBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentBytes
}

// MaxBytes returns configured maximum byte capacity.
func (s *Store) MaxBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxBytes
}

// Evictions returns total number of entries evicted since creation.
func (s *Store) Evictions() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evictions
}

// snapshotRecord stores an entry for JSON serialization.
type snapshotRecord struct {
	Key   string       `json:"key"`
	Entry *StoredEntry `json:"entry"`
}

// SaveSnapshot saves all non-expired cached entries to disk atomically via a temporary file.
func (s *Store) SaveSnapshot(path string) error {
	if path == "" {
		return nil
	}
	s.mu.RLock()
	records := make([]snapshotRecord, 0, len(s.items))
	for k, elem := range s.items {
		item := elem.Value.(*lruItem)
		records = append(records, snapshotRecord{Key: k, Entry: item.entry.DeepCopy()})
	}
	s.mu.RUnlock()

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write tmp snapshot: %w", err)
	}
	_ = os.Chmod(tmpPath, 0600)

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to atomically rename snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot restores non-expired entries from disk.
func (s *Store) LoadSnapshot(path string, now time.Time, ttl time.Duration) (int, error) {
	if path == "" {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var records []snapshotRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return 0, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	loaded := 0
	for _, rec := range records {
		if rec.Entry == nil || rec.Key == "" {
			continue
		}
		// Skip expired entries if TTL is set
		if ttl > 0 && now.Sub(rec.Entry.CreatedAt) > ttl {
			continue
		}
		s.Put(rec.Key, rec.Entry)
		loaded++
	}

	return loaded, nil
}
