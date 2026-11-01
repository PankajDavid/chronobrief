from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest
from streamlit.testing.v1 import AppTest

from chronobrief import pipeline as PL
from chronobrief.fingerprint import Source
from chronobrief.gateway import CacheInfo, GatewayReply
from chronobrief.retrieval import RetrievalResult
from chronobrief.ui import cache_banner, claim_line, md_escape, safe_url, source_line

APP = str(Path(__file__).resolve().parent.parent / "app" / "streamlit_app.py")


def _reply(decision: str, **extra: str) -> GatewayReply:
    text = json.dumps({"claims": [{"id": "c1", "section": "news", "text": "Stripe and FedEx teamed up.",
                                   "source_ids": ["S1"]}],
                       "icebreakers": [{"text": "How will SMBs apply?", "motivated_by": "c1"}]})
    return GatewayReply(text=text, cache=CacheInfo.from_headers({"x-chrono-cache": decision, **extra}), usage={})


@pytest.fixture
def fake_gateway(monkeypatch: pytest.MonkeyPatch) -> list[str]:
    decisions = ["MISS_COLD", "HIT"]
    monkeypatch.setenv("CHRONO_TAVILY_MODE", "replay")
    monkeypatch.setenv("CHRONO_VK", "sk-bf-alpha-test")

    def fake_chat(*_a: Any, **_kw: Any) -> GatewayReply:
        d = decisions.pop(0) if decisions else "HIT"
        extra = {"x-chrono-saved-tokens": "1800", "x-chrono-cached-newest": "2026-10-31T17:11:00Z"} if d == "HIT" else {}
        return _reply(d, **extra)

    monkeypatch.setattr(PL, "chat", fake_chat)
    return decisions


def _submit(at: AppTest, subject: str, button: int = 0) -> AppTest:
    at.text_input[0].set_value(subject)
    at.button[button].click()
    return at.run(timeout=30)


def test_empty_input_shows_error(fake_gateway: list[str]) -> None:
    at = AppTest.from_file(APP).run(timeout=30)
    _submit(at, "   ")
    assert any("Enter a company" in e.value for e in at.error)


def test_normal_run_shows_banner_claims_and_sources_then_hit(fake_gateway: list[str]) -> None:
    at = AppTest.from_file(APP).run(timeout=30)
    _submit(at, "Stripe")
    assert any("MISS: first brief" in i.value for i in at.info)
    md = " ".join(m.value for m in at.markdown)
    assert "Stripe and FedEx teamed up" in md and "\\[1\\]" in md
    assert any(h.value == "Sources" for h in at.subheader)
    at.session_state["last_run"] = 0.0  # past the cooldown
    _submit(at, "Stripe")
    assert any("HIT: served from cache" in s.value and "1,800 tokens" in s.value for s in at.success)


def test_cooldown_rejects_double_click(fake_gateway: list[str]) -> None:
    at = AppTest.from_file(APP).run(timeout=30)
    _submit(at, "Stripe")
    _submit(at, "Stripe")
    assert any("wait a moment" in w.value for w in at.warning)


def test_markdown_from_model_cannot_inject_links() -> None:
    line = claim_line("Click [here](https://evil.com) ![x](http://t/p.png)", ["S1"], set(), 2, False)
    assert "](https" not in line and "!\\[" in line


def test_source_line_rejects_non_http_links_and_badges_simulated() -> None:
    js = Source(url="javascript:alert(1)", title="t", published=None)
    assert "](" not in source_line("S1", js, False)
    assert safe_url("javascript:alert(1)") is None and safe_url("https://a.com/x(1)") == "https://a.com/x%281%29"
    sim = Source(url="https://example.com/simulated/x", title="[SIMULATED] Acme", published=datetime(2026, 11, 1, tzinfo=UTC))
    assert "SIMULATED (demo only)" in source_line("S2", sim, True)


@pytest.mark.parametrize("decision,word", [
    ("REFUSE_NEWER_SOURCE", "REFUSED STALE"), ("REFUSE_SET_CHANGED", "REFUSED STALE"),
    ("BYPASS_REFRESH", "REGENERATED"), ("REFUSE_NO_FINGERPRINT", "NOT CACHED"),
    ("REFUSE_NO_TENANT", "NOT CACHED"), ("UNKNOWN", "UNKNOWN"),
])
def test_banner_always_leads_with_a_word(decision: str, word: str) -> None:
    res = PL.BriefResult(subject="S", retrieval=RetrievalResult(subject="S", mode="replay", sources=[]),
                         cache=CacheInfo(decision=decision))
    assert word in cache_banner(res).text


def test_md_escape_collapses_whitespace() -> None:
    assert md_escape("a\n\n# b") == "a \\# b"
