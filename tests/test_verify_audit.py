import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def run(tmp_path: Path, rows: list[dict]) -> subprocess.CompletedProcess:
    log = tmp_path / "a.jsonl"
    log.write_text("\n".join(json.dumps(r) for r in rows))
    return subprocess.run([sys.executable, str(ROOT / "scripts" / "verify_audit.py"), str(log), "--json"], capture_output=True, text=True)


def test_clean_log_passes_and_counts_savings(tmp_path):
    rows = [
        {"decision": "MISS_COLD", "incoming_newest": "2026-10-30T00:00:00Z"},
        {"decision": "HIT", "incoming_newest": "2026-10-30T00:00:00Z", "entry_newest": "2026-10-30T00:00:00Z", "incoming_count": 1, "entry_count": 1, "saved_tokens": 900, "saved_cost_usd": 0.002},
        {"decision": "REFUSE_NEWER_SOURCE", "incoming_newest": "2026-11-01T00:00:00Z", "entry_newest": "2026-10-30T00:00:00Z"},
    ]
    r = run(tmp_path, rows)
    out = json.loads(r.stdout)
    assert r.returncode == 0
    assert out["hits"] == 1 and out["stale_serves"] == 0 and out["stale_answers_prevented"] == 1
    assert out["saved_tokens"] == 900


def test_stale_hit_is_caught_even_if_plugin_said_hit(tmp_path):
    rows = [{"decision": "HIT", "incoming_newest": "2026-11-01T00:00:00Z", "entry_newest": "2026-10-30T00:00:00Z", "incoming_count": 1, "entry_count": 1}]
    r = run(tmp_path, rows)
    assert r.returncode == 1 and json.loads(r.stdout)["stale_serves"] == 1


def test_same_timestamp_with_more_articles_is_stale(tmp_path):
    rows = [{"decision": "HIT", "incoming_newest": "2026-10-30T00:00:00Z", "entry_newest": "2026-10-30T00:00:00Z", "incoming_count": 3, "entry_count": 2}]
    assert run(tmp_path, rows).returncode == 1


def test_hit_without_evidence_fails(tmp_path):
    assert run(tmp_path, [{"decision": "HIT", "entry_newest": "2026-10-30T00:00:00Z"}]).returncode == 1
