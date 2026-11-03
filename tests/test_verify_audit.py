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


# --- added: timestamp shapes, shared report format, e2e oracle slicing -------------------------
import json as _json  # noqa: E402
from pathlib import Path as _Path  # noqa: E402

from scripts.e2e_cache import oracle_report  # noqa: E402
from scripts.verify_audit import format_summary, is_newer, summarise_text  # noqa: E402


def _rec(decision: str, inc: str, ent: str, inc_n: int = 1, ent_n: int = 1) -> dict[str, object]:
    return {"decision": decision, "incoming_newest": inc, "entry_newest": ent, "incoming_count": inc_n, "entry_count": ent_n}


def test_is_newer_compares_instants_not_text() -> None:
    # Same instant, different shapes: not newer.
    assert not is_newer(_rec("HIT", "2026-10-31T17:11:00+00:00", "2026-10-31T17:11:00Z"))
    # Text comparison gets this wrong ('.' sorts before 'Z'); as instants it is newer.
    assert is_newer(_rec("HIT", "2026-10-31T17:11:00.500Z", "2026-10-31T17:11:00Z"))
    # An offset that is really earlier in UTC is not newer, though it sorts later as text.
    assert not is_newer(_rec("HIT", "2026-10-31T20:00:00+05:30", "2026-10-31T17:11:00Z"))
    # Same instant, more articles at newest: newer.
    assert is_newer(_rec("HIT", "2026-10-31T17:11:00Z", "2026-10-31T17:11:00+00:00", 2, 1))


def test_format_summary_is_what_main_prints() -> None:
    s = summarise_text(_json.dumps({"decision": "MISS_COLD"}) + "\n")
    lines = format_summary(s)
    assert lines[0].startswith("lookups 1  hits 0")
    assert lines[1] == "stale serves 0  (stale answers prevented: 0)"


def test_oracle_report_reads_only_this_runs_lines(tmp_path: _Path) -> None:
    log = tmp_path / "freshcache.jsonl"
    old = _rec("HIT", "2026-10-31T18:00:00Z", "2026-10-31T17:00:00Z")  # a stale serve from an EARLIER run
    log.write_text(_json.dumps(old) + "\n", encoding="utf-8")
    offset = log.stat().st_size
    with log.open("a", encoding="utf-8") as fh:
        fh.write(_json.dumps({"decision": "MISS_COLD"}) + "\n")
        fh.write(_json.dumps(_rec("HIT", "2026-10-31T17:00:00Z", "2026-10-31T17:00:00Z")) + "\n")
        fh.write(_json.dumps({"decision": "REFUSE_NEWER_SOURCE"}) + "\n")
    lines, ok = oracle_report(log, offset)
    assert ok, lines
    assert "3 audit lines from this run only" in lines[0]
    assert "stale serves 0  (stale answers prevented: 1)" in lines


def test_oracle_report_fails_without_log_or_lines(tmp_path: _Path) -> None:
    lines, ok = oracle_report(tmp_path / "missing.jsonl", 0)
    assert not ok and lines[0].startswith("FAIL: no audit log")
    log = tmp_path / "empty.jsonl"
    log.write_text("", encoding="utf-8")
    lines, ok = oracle_report(log, 0)
    assert not ok and any("wrote no audit lines" in line for line in lines)
