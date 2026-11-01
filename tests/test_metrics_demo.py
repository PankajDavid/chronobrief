from __future__ import annotations

import json
import shutil
from pathlib import Path

import pytest
from streamlit.testing.v1 import AppTest

from chronobrief import pipeline as PL
from chronobrief.metrics import load_scoreboard, recent_decisions
from tests.test_ui import APP, _reply

ROOT = Path(__file__).resolve().parent.parent

LOG = [
    {"timestamp": "2026-11-01T13:00:00Z", "decision": "MISS_COLD", "incoming_newest": "2026-10-31T17:11:00Z",
     "latency_ms": 2400, "saved_tokens": 0},
    {"timestamp": "2026-11-01T13:01:00Z", "decision": "HIT", "incoming_newest": "2026-10-31T17:11:00Z",
     "entry_newest": "2026-10-31T17:11:00Z", "incoming_count": 1, "entry_count": 1,
     "latency_ms": 3, "saved_tokens": 5186, "saved_cost_usd": 0.001476},
    {"timestamp": "2026-11-01T13:02:00Z", "decision": "REFUSE_NEWER_SOURCE", "incoming_newest": "2026-11-01T13:01:30Z",
     "entry_newest": "2026-10-31T17:11:00Z", "latency_ms": 2100},
]


def _write_log(path: Path, extra: str = "") -> Path:
    path.write_text("\n".join(json.dumps(r) for r in LOG) + "\n" + extra, encoding="utf-8")
    return path


def test_scoreboard_matches_oracle_and_tolerates_bad_lines(tmp_path: Path) -> None:
    board = load_scoreboard(_write_log(tmp_path / "a.jsonl", "not json\n"))
    assert board is not None
    s = board.summary
    assert s["lookups"] == 3 and s["hits"] == 1 and s["stale_serves"] == 0
    assert s["stale_answers_prevented"] == 1 and s["saved_tokens"] == 5186 and s["malformed_lines"] == 1
    assert board.latency_ms == {"MISS_COLD": 2400, "HIT": 3, "REFUSE_NEWER_SOURCE": 2100}
    assert [r["decision"] for r in board.recent] == ["REFUSE_NEWER_SOURCE", "HIT", "MISS_COLD"]


def test_scoreboard_absent_when_no_log(tmp_path: Path) -> None:
    assert load_scoreboard(tmp_path / "missing.jsonl") is None
    (tmp_path / "empty.jsonl").write_text("")
    assert load_scoreboard(tmp_path / "empty.jsonl") is None


def test_recent_limits_rows(tmp_path: Path) -> None:
    assert len(recent_decisions(_write_log(tmp_path / "a.jsonl"), n=2)) == 2


@pytest.fixture
def demo_env(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    fx = tmp_path / "fixtures"
    fx.mkdir()
    shutil.copy(ROOT / "fixtures" / "tavily" / "stripe.json", fx / "stripe.json")
    monkeypatch.setenv("CHRONO_TAVILY_MODE", "replay")
    monkeypatch.setenv("CHRONO_VK", "sk-bf-alpha-test")
    monkeypatch.setenv("CHRONO_FIXTURES_DIR", str(fx))
    monkeypatch.setenv("CHRONO_AUDIT_LOG", str(_write_log(tmp_path / "audit.jsonl")))
    monkeypatch.setattr(PL, "chat", lambda *_a, **_kw: _reply("REFUSE_NEWER_SOURCE"))
    return fx



def test_app_shows_scoreboard_and_demo_inject_then_reset(demo_env: Path) -> None:
    at = AppTest.from_file(APP).run(timeout=30)
    labels = [m.label for m in at.metric]
    assert "Stale briefs served" in labels and "Reuse (hit) rate" in labels
    assert any(m.label == "AI tokens saved" and m.value == "5,186" for m in at.metric)
    assert any("PASS" in m.value for m in at.markdown)

    simulate = next(b for b in at.button if b.label == "Simulate breaking news")
    simulate.click()
    at.run(timeout=30)
    data = json.loads((demo_env / "stripe.json").read_text())
    assert data["results"][-1]["simulated"] is True and (demo_env / "stripe.base.json").exists()
    assert any("Added:" in i.value and "SIMULATED" in i.value for i in at.info)

    at.text_input[0].set_value("Stripe")
    next(b for b in at.button if b.label == "Generate brief").click()
    at.run(timeout=30)
    assert any("REFUSED STALE" in w.value for w in at.warning)
    assert any("SIMULATED (demo only)" in m.value for m in at.markdown)

    next(b for b in at.button if b.label == "Reset demo").click()
    at.run(timeout=30)
    data = json.loads((demo_env / "stripe.json").read_text())
    assert not any(r.get("simulated") for r in data["results"])


def test_demo_controls_hidden_in_live_mode(demo_env: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("CHRONO_TAVILY_MODE", "live")
    at = AppTest.from_file(APP).run(timeout=30)
    assert not any(b.label == "Simulate breaking news" for b in at.button)


