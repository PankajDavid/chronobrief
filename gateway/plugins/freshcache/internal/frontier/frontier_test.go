package frontier

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrontierSimulation(t *testing.T) {
	results := RunFrontierSimulation(20261102)
	if len(results) != 4 {
		t.Fatalf("expected 4 policy results, got %d", len(results))
	}

	// 1. freshcache invariant: zero stale serves
	fc := results[3]
	if fc.Name != "freshcache" {
		t.Fatalf("expected policy 3 to be freshcache, got %s", fc.Name)
	}
	if fc.StaleServes != 0 {
		t.Fatalf("freshcache invariant violation: stale serves = %d (must be 0)", fc.StaleServes)
	}
	if fc.HitRate <= 0.30 {
		t.Fatalf("expected freshcache hit rate > 30%%, got %.2f%%", fc.HitRate*100)
	}

	// 2. TTL-24h has stale serves
	ttl24h := results[2]
	if ttl24h.StaleServes <= 0 {
		t.Fatalf("expected TTL-24h to have stale serves in presence of news, got %d", ttl24h.StaleServes)
	}

	// 3. TTL-5m has high false misses
	ttl5m := results[0]
	if ttl5m.FalseMisses <= 0 {
		t.Fatalf("expected TTL-5m to have false misses, got %d", ttl5m.FalseMisses)
	}

	// Generate and save Markdown report
	md := GenerateFrontierMarkdown(results)
	evidencePath := filepath.Join("..", "..", "..", "..", "docs", "evidence", "ttl-frontier-report.md")
	if abs, err := filepath.Abs(evidencePath); err == nil {
		_ = os.WriteFile(abs, []byte(md), 0644)
	}
}
