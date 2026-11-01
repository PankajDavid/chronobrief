import json
from datetime import UTC, datetime
from pathlib import Path

import pytest

from chronobrief.fingerprint import fingerprint, sources_from_tavily
from scripts.inject_article import inject_simulated_article, reset_fixture

NOW = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)


def test_injector_advances_fingerprint_and_reset_restores_byte_for_byte(tmp_path: Path):
    base_data = {
        "subject": "Acme",
        "results": [
            {"title": "Acme base story", "url": "https://a.com/1", "published_date": "2026-10-28T10:00:00Z", "content": "Base"}
        ],
    }
    base_file = tmp_path / "acme.base.json"
    live_file = tmp_path / "acme.json"
    raw_bytes = json.dumps(base_data, indent=2).encode("utf-8")
    base_file.write_bytes(raw_bytes)
    live_file.write_bytes(raw_bytes)

    before_results = json.loads(live_file.read_text())["results"]
    fp_before = fingerprint(sources_from_tavily(before_results, NOW))

    art = inject_simulated_article("acme", headline="launches quantum router", now=NOW, fixtures_dir=tmp_path, mode="replay")
    assert art["simulated"] is True
    assert art["title"].startswith("[SIMULATED]")

    after_results = json.loads(live_file.read_text())["results"]
    fp_after = fingerprint(sources_from_tavily(after_results, NOW))
    assert fp_after.newest > fp_before.newest

    reset_fixture("acme", fixtures_dir=tmp_path)
    assert live_file.read_bytes() == raw_bytes


def test_injector_refuses_live_mode(tmp_path: Path):
    with pytest.raises(RuntimeError, match="only allowed when CHRONO_TAVILY_MODE=replay"):
        inject_simulated_article("acme", fixtures_dir=tmp_path, mode="live")
