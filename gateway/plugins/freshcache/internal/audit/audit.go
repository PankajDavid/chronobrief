package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is a single JSONL audit entry for an inference request.
type Record struct {
	Timestamp            string  `json:"timestamp"`
	TenantID             string  `json:"tenant_id"`
	CacheKey             string  `json:"cache_key"`
	Decision             string  `json:"decision"`
	RequestNewest        string  `json:"request_newest,omitempty"`
	IncomingNewest       string  `json:"incoming_newest,omitempty"`
	RequestCountAtNewest int     `json:"request_count_at_newest,omitempty"`
	IncomingCount        int     `json:"incoming_count,omitempty"`
	RequestSetHash       string  `json:"request_set_hash,omitempty"`
	CachedNewest         string  `json:"cached_newest,omitempty"`
	EntryNewest          string  `json:"entry_newest,omitempty"`
	CachedCountAtNewest  int     `json:"cached_count_at_newest,omitempty"`
	EntryCount           int     `json:"entry_count,omitempty"`
	CachedSetHash        string  `json:"cached_set_hash,omitempty"`
	PromptTokens         int     `json:"prompt_tokens"`
	CompletionTokens     int     `json:"completion_tokens"`
	TotalTokens          int     `json:"total_tokens"`
	SavedTokens          int     `json:"saved_tokens"`
	CostUSD              float64 `json:"cost_usd"`
	SavedCostUSD         float64 `json:"saved_cost_usd"`
	LatencyMS            int64   `json:"latency_ms"`
}

// Logger manages the thread-safe JSONL audit log file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// New creates or opens an audit log file at path.
func New(path string) (*Logger, error) {
	if path == "" {
		return &Logger{}, nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file: f,
		path: path,
	}, nil
}

// Log writes an audit record. Append failure never blocks or fails request processing.
func (l *Logger) Log(rec Record) {
	if l == nil || l.file == nil {
		return
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.file.Write(data)
}

// Close flushes and closes the audit file.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}
