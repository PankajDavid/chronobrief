package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditLogWriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := New(auditPath)
	if err != nil {
		t.Fatalf("failed to create audit logger: %v", err)
	}

	rec := Record{
		TenantID:     "team-alpha",
		CacheKey:     "hash123",
		Decision:     "HIT",
		PromptTokens: 100,
		TotalTokens:  150,
		CostUSD:      0.002,
		SavedCostUSD: 0.002,
		SavedTokens:  150,
	}

	logger.Log(rec)
	if err := logger.Close(); err != nil {
		t.Fatalf("failed to close audit logger: %v", err)
	}

	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("failed to open audit log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one log line in audit log")
	}

	var parsed Record
	if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSONL audit line: %v", err)
	}

	if parsed.TenantID != "team-alpha" {
		t.Fatalf("expected tenant_id team-alpha, got %q", parsed.TenantID)
	}
	if parsed.Decision != "HIT" {
		t.Fatalf("expected decision HIT, got %q", parsed.Decision)
	}
	if parsed.SavedTokens != 150 {
		t.Fatalf("expected 150 saved tokens, got %d", parsed.SavedTokens)
	}
}
