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
	Shadow           decide.ShadowState       `json:"shadow,omitempty"`
	CostUSD          float64                  `json:"cost_usd"`
	PromptTokens     int                      `json:"prompt_tokens"`
	CompletionTokens int                      `json:"completion_tokens"`
	TotalTokens      int                      `json:"total_tokens"`
	shadowOnly       bool
}

// DeepCopy clones a StoredEntry including a fully detached BifrostResponse.
func (e *StoredEntry) DeepCopy() *StoredEntry {
	if e == nil {
		return nil
	}
	cp := &StoredEntry{
		Fingerprint:      e.Fingerprint,
		CreatedAt:        e.CreatedAt,
		Shadow:           e.Shadow,
		CostUSD:          e.CostUSD,
		PromptTokens:     e.PromptTokens,
		CompletionTokens: e.CompletionTokens,
		TotalTokens:      e.TotalTokens,
		shadowOnly:       e.shadowOnly,
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
	ttl          time.Duration

	snapCh    chan string
	stopCh    chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	onSnapErr func(string, error)
	errMu     sync.RWMutex
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
	s := &Store{
		maxEntries: maxEntries,
		maxBytes:   mb,
		items:      make(map[string]*list.Element),
		evictList:  list.New(),
		snapCh:     make(chan string, 1),
		stopCh:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.snapshotWorker()
	return s
}

func (s *Store) snapshotWorker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			// Drain any queued snapshot write before exiting
			select {
			case path := <-s.snapCh:
				if err := s.SaveSnapshot(path); err != nil {
					s.handleSnapErr(path, err)
				}
			default:
			}
			return
		case path := <-s.snapCh:
			if err := s.SaveSnapshot(path); err != nil {
				s.handleSnapErr(path, err)
			}
		}
	}
}

func (s *Store) handleSnapErr(path string, err error) {
	s.errMu.RLock()
	cb := s.onSnapErr
	s.errMu.RUnlock()
	if cb != nil {
		cb(path, err)
	}
}

// SetSnapshotErrorHandler registers an optional error callback for asynchronous snapshot writes.
func (s *Store) SetSnapshotErrorHandler(fn func(string, error)) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	s.onSnapErr = fn
}

// ScheduleSnapshot enqueues a non-blocking snapshot write coalescing concurrent bursts.
func (s *Store) ScheduleSnapshot(path string) {
	if path == "" {
		return
	}
	select {
	case <-s.stopCh:
		return
	default:
	}
	select {
	case s.snapCh <- path:
	default:
		// A snapshot write is already queued; coalescing burst misses
	}
}

// FlushSnapshot drains the background worker and executes a final synchronous SaveSnapshot.
func (s *Store) FlushSnapshot(path string) error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.wg.Wait()
	})
	if path == "" {
		return nil
	}
	return s.SaveSnapshot(path)
}

// Close is an alias for FlushSnapshot.
func (s *Store) Close(path string) error {
	return s.FlushSnapshot(path)
}

// SetTTL configures the baseline TTL window for FirstSeen preservation.
func (s *Store) SetTTL(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = d
}

// Get retrieves an entry by key, moving it to front, and returns a deep copy.
// If an entry exists only to hold shadow slots (Response == nil), Get returns (nil, false).
func (s *Store) Get(key string) (*StoredEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, exists := s.items[key]
	if !exists {
		return nil, false
	}
	item := elem.Value.(*lruItem)
	if item.entry == nil || item.entry.shadowOnly || (item.entry.Response == nil && item.entry.shadowOnly) {
		return nil, false
	}
	s.evictList.MoveToFront(elem)
	return item.entry.DeepCopy(), true
}

// GetShadow returns the shadow state for a key if present in the store.
func (s *Store) GetShadow(key string) (decide.ShadowState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	elem, exists := s.items[key]
	if !exists || elem.Value.(*lruItem).entry == nil {
		return decide.ShadowState{}, false
	}
	return elem.Value.(*lruItem).entry.Shadow, true
}

// AdvanceShadow updates the per-TTL shadow slots for a cache key and returns the shadow outcomes.
// It is called on EVERY lookup in PreLLMHook, regardless of whether the freshcache lookup is a hit, miss, refusal, or refresh.
func (s *Store) AdvanceShadow(key string, now time.Time, in decide.Fingerprint) decide.ShadowOutcomes {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, exists := s.items[key]; exists {
		s.evictList.MoveToFront(elem)
		item := elem.Value.(*lruItem)
		outcomes, newState := decide.AdvanceShadowState(now, in, item.entry.Shadow)
		item.entry.Shadow = newState
		return outcomes
	}

	var emptyState decide.ShadowState
	outcomes, newState := decide.AdvanceShadowState(now, in, emptyState)
	entry := &StoredEntry{
		CreatedAt:  now,
		Shadow:     newState,
		shadowOnly: true,
	}
	itemSize := entry.EstimatedSize(key)
	elem := s.evictList.PushFront(&lruItem{key: key, entry: entry, bytes: itemSize})
	s.items[key] = elem
	s.currentBytes += itemSize
	s.evictToLimits()
	return outcomes
}

// Put inserts or updates an entry, evicting the oldest if capacity or byte limit is exceeded.
func (s *Store) Put(key string, entry *StoredEntry) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entryCopy := entry.DeepCopy()
	entryCopy.shadowOnly = false
	if entryCopy.CreatedAt.IsZero() {
		entryCopy.CreatedAt = time.Now().UTC()
	}

	if elem, exists := s.items[key]; exists {
		s.evictList.MoveToFront(elem)
		oldItem := elem.Value.(*lruItem)
		oldEntry := oldItem.entry
		if oldEntry != nil && (!oldEntry.Shadow.Slot5m.StoredAt.IsZero() || !oldEntry.Shadow.Slot1h.StoredAt.IsZero() || !oldEntry.Shadow.Slot24h.StoredAt.IsZero()) {
			entryCopy.Shadow = oldEntry.Shadow
		} else if entryCopy.Shadow.Slot5m.StoredAt.IsZero() && entryCopy.Fingerprint.Valid() {
			entryCopy.Shadow = decide.ShadowState{
				Slot5m:  decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
				Slot1h:  decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
				Slot24h: decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
			}
		}

		itemSize := entryCopy.EstimatedSize(key)
		s.currentBytes -= oldItem.bytes
		oldItem.entry = entryCopy
		oldItem.bytes = itemSize
		s.currentBytes += itemSize
		s.evictToLimits()
		return
	}

	if entryCopy.Shadow.Slot5m.StoredAt.IsZero() && entryCopy.Fingerprint.Valid() {
		entryCopy.Shadow = decide.ShadowState{
			Slot5m:  decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
			Slot1h:  decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
			Slot24h: decide.ShadowSlot{StoredAt: entryCopy.CreatedAt, FP: entryCopy.Fingerprint},
		}
	}
	itemSize := entryCopy.EstimatedSize(key)
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
		if item.entry != nil && !item.entry.shadowOnly {
			records = append(records, snapshotRecord{Key: k, Entry: item.entry.DeepCopy()})
		}
	}
	s.mu.RUnlock()

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
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
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("failed to chmod tmp snapshot: %w", err)
	}

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
		// Backward compatibility for snapshots:
		// If a loaded snapshot entry has a valid Fingerprint and non-zero CreatedAt
		// but empty Shadow slots, initialize its three slots to {StoredAt: entry.CreatedAt, FP: entry.Fingerprint}
		if rec.Entry.Fingerprint.Valid() && !rec.Entry.CreatedAt.IsZero() {
			if rec.Entry.Shadow.Slot5m.StoredAt.IsZero() && rec.Entry.Shadow.Slot1h.StoredAt.IsZero() && rec.Entry.Shadow.Slot24h.StoredAt.IsZero() {
				rec.Entry.Shadow = decide.ShadowState{
					Slot5m:  decide.ShadowSlot{StoredAt: rec.Entry.CreatedAt, FP: rec.Entry.Fingerprint},
					Slot1h:  decide.ShadowSlot{StoredAt: rec.Entry.CreatedAt, FP: rec.Entry.Fingerprint},
					Slot24h: decide.ShadowSlot{StoredAt: rec.Entry.CreatedAt, FP: rec.Entry.Fingerprint},
				}
			}
		}
		s.Put(rec.Key, rec.Entry)
		loaded++
	}

	return loaded, nil
}
