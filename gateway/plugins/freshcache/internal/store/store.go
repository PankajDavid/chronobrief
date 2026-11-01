package store

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

// StoredEntry represents a cached response along with its provenance and pricing data.
type StoredEntry struct {
	Response         *schemas.BifrostResponse
	Fingerprint      decide.Fingerprint
	CreatedAt        time.Time
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
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

type lruItem struct {
	key   string
	entry *StoredEntry
}

// Store is a thread-safe LRU cache.
type Store struct {
	mu         sync.RWMutex
	maxEntries int
	items      map[string]*list.Element
	evictList  *list.List
}

// New creates an LRU store with a given maximum capacity.
func New(maxEntries int) *Store {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	return &Store{
		maxEntries: maxEntries,
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

// Put inserts or updates an entry, evicting the oldest if capacity is exceeded.
func (s *Store) Put(key string, entry *StoredEntry) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entryCopy := entry.DeepCopy()

	if elem, exists := s.items[key]; exists {
		s.evictList.MoveToFront(elem)
		elem.Value.(*lruItem).entry = entryCopy
		return
	}

	elem := s.evictList.PushFront(&lruItem{key: key, entry: entryCopy})
	s.items[key] = elem

	if s.evictList.Len() > s.maxEntries {
		oldest := s.evictList.Back()
		if oldest != nil {
			s.evictList.Remove(oldest)
			kv := oldest.Value.(*lruItem)
			delete(s.items, kv.key)
		}
	}
}

// Len returns current number of cached entries.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}
