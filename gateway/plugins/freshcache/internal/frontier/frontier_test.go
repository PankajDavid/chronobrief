package frontier

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
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

	// The freshcache stale count above is measured by the same oracle as the
	// TTL policies (see RunFrontierSimulation), not assumed.
}

// The committed report must be exactly what the code generates, so no one can
// hand-edit numbers into it. Regenerate with:
//
//	UPDATE_FRONTIER_REPORT=1 go test ./internal/frontier/
func TestFrontierReportIsUpToDate(t *testing.T) {
	md := GenerateFrontierMarkdown(RunFrontierSimulation(20261102))
	// internal/frontier -> internal -> freshcache -> plugins -> gateway -> repo root
	path := filepath.Join("..", "..", "..", "..", "..", "docs", "evidence", "ttl-frontier-report.md")
	if os.Getenv("UPDATE_FRONTIER_REPORT") == "1" {
		if err := os.WriteFile(path, []byte(md), 0644); err != nil {
			t.Fatalf("writing report: %v", err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading committed report %s: %v", path, err)
	}
	if string(got) != md {
		t.Fatalf("%s is out of date or hand-edited; regenerate with UPDATE_FRONTIER_REPORT=1 go test ./internal/frontier/", path)
	}
}

// parseChartGrid returns the grid rows (without the y-axis label) from a rendered chart.
func parseChartGrid(t *testing.T, chart string) []string {
	t.Helper()
	lines := strings.Split(chart, "\n")
	var grid []string
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "    +") {
			break
		}
		if len(l) < 5 || l[4] != '|' {
			t.Fatalf("unexpected chart line %q", l)
		}
		grid = append(grid, l[5:])
	}
	if len(grid) != ChartRows {
		t.Fatalf("expected %d grid rows, got %d", ChartRows, len(grid))
	}
	return grid
}

// findMarker returns the (row, col) of marker m in the grid, or (-1, -1).
func findMarker(grid []string, m byte) (int, int) {
	for r, line := range grid {
		if c := strings.IndexByte(line, m); c >= 0 {
			return r, c
		}
	}
	return -1, -1
}

// assertChartMatches checks every policy's marker sits where its table values
// put it on the linear axes, and that the legend repeats the table values.
func assertChartMatches(t *testing.T, results []PolicyResult) string {
	t.Helper()
	chart := RenderFrontierChart(results)
	grid := parseChartGrid(t, chart)
	xMax := ChartXMax(results)
	for i, r := range results {
		wantRow, wantCol := ChartPosition(r, xMax)
		row, col := findMarker(grid, ChartMarker(i))
		if row == -1 {
			// allowed only when it shares a cell with another policy
			if grid[wantRow][wantCol] != '*' {
				t.Fatalf("%s: marker %c missing from chart", r.Name, ChartMarker(i))
			}
			continue
		}
		if row != wantRow || col != wantCol {
			t.Fatalf("%s: marker at (%d,%d), table values put it at (%d,%d)", r.Name, row, col, wantRow, wantCol)
		}
		// Invert the position back to values: must be within half a grid step.
		gotY := float64(ChartRows-1-row) * 10
		gotX := float64(col) / float64(ChartCols-1) * xMax
		if math.Abs(gotY-r.HitRate*100) > 5.0001 {
			t.Fatalf("%s: chart shows hit ~%.1f%%, table says %.1f%%", r.Name, gotY, r.HitRate*100)
		}
		if math.Abs(gotX-r.StaleServeRate*100) > xMax/float64(ChartCols-1)/2+0.0001 {
			t.Fatalf("%s: chart shows stale ~%.2f%%, table says %.2f%%", r.Name, gotX, r.StaleServeRate*100)
		}
		legend := fmt.Sprintf("%c = %-10s hit %5.1f%% | stale %5.1f%% (%d) | false misses %d",
			ChartMarker(i), r.Name, r.HitRate*100, r.StaleServeRate*100, r.StaleServes, r.FalseMisses)
		if !strings.Contains(chart, legend) {
			t.Fatalf("%s: legend line %q not found in chart", r.Name, legend)
		}
	}
	return chart
}

func TestFrontierChartMatchesTable(t *testing.T) {
	results := RunFrontierSimulation(20261102)
	assertChartMatches(t, results)

	// The table in the markdown must carry the same numbers.
	md := GenerateFrontierMarkdown(results)
	for _, r := range results {
		row := fmt.Sprintf("| **%s** | %s | **%.1f%%** | `%d` | **%.1f%%** | %d |",
			r.Name, r.TTLDescription, r.HitRate*100, r.StaleServes, r.StaleServeRate*100, r.FalseMisses)
		if !strings.Contains(md, row) {
			t.Fatalf("table row for %s missing or different: %q", r.Name, row)
		}
	}
}

// Guards against a hard-coded chart: feed made-up numbers and the chart must follow them.
func TestFrontierChartIsDataDriven(t *testing.T) {
	fake := []PolicyResult{
		{Name: "P-one", Lookups: 100, Hits: 37, HitRate: 0.37, StaleServes: 12, StaleServeRate: 0.12, FalseMisses: 7},
		{Name: "P-two", Lookups: 100, Hits: 81, HitRate: 0.81, StaleServes: 33, StaleServeRate: 0.33, FalseMisses: 0},
		{Name: "P-three", Lookups: 100, Hits: 4, HitRate: 0.04, StaleServes: 0, StaleServeRate: 0, FalseMisses: 90},
	}
	chart := assertChartMatches(t, fake)
	if ChartXMax(fake) != 35 {
		t.Fatalf("expected x-axis max 35, got %g", ChartXMax(fake))
	}
	for _, leaked := range []string{"93.2", "96.0", "52.4", "6.4%", "18.4", "freshcache", "TTL-"} {
		if strings.Contains(chart, leaked) {
			t.Fatalf("chart for made-up data contains %q: something is hard-coded", leaked)
		}
	}
}
