from __future__ import annotations

import json
import shutil
from dataclasses import replace
from datetime import UTC, datetime
from pathlib import Path

import httpx
import pytest

from chronobrief import pipeline as PL
from chronobrief.gateway import CacheInfo, GatewayConfigError, GatewayError, build_headers, chat
from chronobrief.pipeline import Settings, generate_brief
from chronobrief.prompt import PROMPT_VERSION
from scripts.inject_article import inject_simulated_article

NOW = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)
VK = "sk-bf-alpha-0123456789abcdef"
REAL_FIXTURES = Path(__file__).resolve().parent.parent / "fixtures" / "tavily"


class FakeGateway:
    """Minimal stand-in for Bifrost + freshcache: same headers in, same decision headers out."""

    def __init__(self, replies: list[str] | None = None, status: int = 200) -> None:
        self.store: dict[str, tuple[str, str, str]] = {}  # key -> (newest, set_hash, text)
        self.requests: list[httpx.Request] = []
        self.replies = replies or []
        self.status = status

    def _reply(self) -> str:
        return self.replies.pop(0) if self.replies else _good_reply()

    def __call__(self, req: httpx.Request) -> httpx.Response:
        self.requests.append(req)
        if self.status != 200:
            return httpx.Response(self.status, json={"error": {"message": "boom"}})
        h = req.headers
        if not h.get("x-bf-vk"):
            return httpx.Response(200, headers={"x-chrono-cache": "REFUSE_NO_TENANT"}, json=_completion(self._reply()))
        key = f"{h['x-bf-vk']}|{h['x-chrono-subject']}|{h['x-chrono-prompt-version']}"
        newest, set_hash = h.get("x-chrono-newest", ""), h.get("x-chrono-set-hash", "")
        if not newest or not set_hash:
            return httpx.Response(200, headers={"x-chrono-cache": "REFUSE_NO_FINGERPRINT"}, json=_completion(self._reply()))
        entry = self.store.get(key)
        if h.get("x-chrono-cache-mode") == "refresh":
            decision = "BYPASS_REFRESH"
        elif entry is None:
            decision = "MISS_COLD"
        elif newest > entry[0]:
            decision = "REFUSE_NEWER_SOURCE"
        elif set_hash != entry[1]:
            decision = "REFUSE_SET_CHANGED"
        else:
            return httpx.Response(200, headers={"x-chrono-cache": "HIT", "x-chrono-saved-tokens": "1200",
                                                "x-chrono-saved-cost-usd": "0.000900"}, json=_completion(entry[2]))
        text = self._reply()
        self.store[key] = (newest, set_hash, text)
        return httpx.Response(200, headers={"x-chrono-cache": decision}, json=_completion(text))


def _completion(text: str) -> dict[str, object]:
    return {"choices": [{"message": {"role": "assistant", "content": text}}], "usage": {"total_tokens": 1200}}


def _good_reply(text: str = "Stripe and FedEx teamed up on small-business lending.", sid: str = "S1") -> str:
    return json.dumps({"claims": [{"id": "c1", "section": "news", "text": text, "source_ids": [sid]}],
                       "icebreakers": [{"text": "How will the lending tie-up reach SMBs?", "motivated_by": "c1"}]})


@pytest.fixture
def fixtures(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    for name in ("stripe.json", "stripe.base.json"):
        shutil.copy(REAL_FIXTURES / name, tmp_path / name)
    monkeypatch.setattr("chronobrief.retrieval.DEFAULT_FIXTURES_DIR", tmp_path)
    import chronobrief.retrieval as R

    orig = R.search

    def search(*a: object, **kw: object):  # type: ignore[no-untyped-def]
        kw["fixtures_dir"] = tmp_path
        return orig(*a, **kw)  # type: ignore[arg-type]

    monkeypatch.setattr(PL, "search", search)
    return tmp_path


SETTINGS = Settings(bifrost_url="http://gw", virtual_key=VK, mode="replay")


def _run(gw: FakeGateway, **kw: object) -> PL.BriefResult:
    return generate_brief("Stripe", settings=kw.pop("settings", SETTINGS), now=NOW,  # type: ignore[arg-type]
                          gateway_client=httpx.Client(transport=httpx.MockTransport(gw)), **kw)  # type: ignore[arg-type]


def test_miss_then_hit_then_refuse_newer_source(fixtures: Path) -> None:
    gw = FakeGateway()
    first = _run(gw)
    assert first.cache and first.cache.decision == "MISS_COLD" and first.brief and first.brief.claims
    second = _run(gw)
    assert second.cache and second.cache.hit and second.cache.saved_tokens == 1200
    inject_simulated_article("stripe", now=datetime(2026, 11, 1, 11, 30, tzinfo=UTC), fixtures_dir=fixtures, mode="replay")
    third = _run(gw)
    assert third.cache and third.cache.decision == "REFUSE_NEWER_SOURCE"
    assert third.retrieval.simulated_urls and any("SIMULATED" in n for n in third.notices)


def test_request_carries_contract_headers_and_virtual_key(fixtures: Path) -> None:
    gw = FakeGateway()
    _run(gw)
    h = gw.requests[0].headers
    assert h["x-bf-vk"] == VK and h["x-chrono-subject"] == "Stripe" and h["x-chrono-intent"] == "brief"
    assert h["x-chrono-prompt-version"] == PROMPT_VERSION and h["x-chrono-window-days"] == "30"
    assert h["x-chrono-newest"].endswith("Z") and len(h["x-chrono-set-hash"]) == 64
    assert gw.requests[0].url.path == "/v1/chat/completions"


def test_no_sources_never_calls_gateway(fixtures: Path) -> None:
    (fixtures / "stripe.json").write_text(json.dumps({"captured_at": "2026-11-01T10:00:00Z", "results": []}))
    gw = FakeGateway()
    res = _run(gw)
    assert gw.requests == [] and res.brief is None and any("No coverage" in n for n in res.notices)


def test_partial_retrieval_sends_empty_fingerprint(fixtures: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    orig = PL.search

    def partial(*a: object, **kw: object):  # type: ignore[no-untyped-def]
        return replace(orig(*a, **kw), partial=True, failed_queries=("q2",))  # type: ignore[arg-type]

    monkeypatch.setattr(PL, "search", partial)
    gw = FakeGateway()
    res = _run(gw)
    assert gw.requests[0].headers["x-chrono-set-hash"] == "" and gw.requests[0].headers["x-chrono-newest"] == ""
    assert res.cache and res.cache.decision == "REFUSE_NO_FINGERPRINT"


def test_unparseable_reply_gets_one_refresh_repair(fixtures: Path) -> None:
    gw = FakeGateway(replies=["sorry, here is your brief!", _good_reply()])
    res = _run(gw)
    assert res.calls == 2 and res.brief and res.brief.claims
    assert gw.requests[1].headers["x-chrono-cache-mode"] == "refresh"
    assert "could not be used" in json.loads(gw.requests[1].content)["messages"][1]["content"]


def test_second_parse_failure_propagates(fixtures: Path) -> None:
    from chronobrief.prompt import BriefParseError

    with pytest.raises(BriefParseError):
        _run(FakeGateway(replies=["nope", "still nope"]))


def test_cached_hit_failing_validation_is_regenerated(fixtures: Path) -> None:
    gw = FakeGateway(replies=[_good_reply(sid="S99")])  # stored reply cites an unknown source
    _run(gw)
    res = _run(gw)  # HIT on the bad entry -> refresh
    assert res.calls == 2 and gw.requests[-1].headers["x-chrono-cache-mode"] == "refresh"
    assert any("regenerated" in n for n in res.notices)


def test_missing_virtual_key_is_config_error(fixtures: Path) -> None:
    with pytest.raises(GatewayConfigError):
        _run(FakeGateway(), settings=replace(SETTINGS, virtual_key=""))


def test_gateway_5xx_retries_then_fails() -> None:
    gw = FakeGateway(status=503)
    with pytest.raises(GatewayError):
        chat([{"role": "user", "content": "x"}], base_url="http://gw", virtual_key=VK, model="m", subject="s",
             window_days=30, prompt_version="v", fingerprint_headers={}, client=httpx.Client(transport=httpx.MockTransport(gw)),
             sleep=lambda _s: None)
    assert len(gw.requests) == 3


def test_missing_decision_header_is_unknown_not_hit() -> None:
    info = CacheInfo.from_headers({})
    assert info.decision == "UNKNOWN" and not info.hit
    assert CacheInfo.from_headers({"X-Chrono-Entry-Newest": "2026-10-31T00:00:00Z"}).cached_newest


def test_refresh_sets_both_bypass_headers() -> None:
    h = build_headers(virtual_key=VK, subject="S", window_days=30, prompt_version="v",
                      fingerprint_headers={}, cache_mode="refresh")
    assert h["x-chrono-cache-mode"] == "refresh" and h["x-chrono-refresh"] == "true"


def test_cli_renders_brief_against_fake_gateway(fixtures: Path, capsys: pytest.CaptureFixture[str],
                                                 monkeypatch: pytest.MonkeyPatch) -> None:
    from chronobrief import __main__ as M

    gw = FakeGateway()
    real = PL.generate_brief
    monkeypatch.setattr(M, "generate_brief", lambda *a, **kw: real(
        *a, **{**kw, "gateway_client": httpx.Client(transport=httpx.MockTransport(gw))}))
    monkeypatch.setenv("CHRONO_VK", VK)
    monkeypatch.setenv("CHRONO_TAVILY_MODE", "replay")
    assert M.main(["Stripe"]) == 0
    out = capsys.readouterr().out
    assert "Cache: MISS_COLD" in out and "## News" in out and "## Sources" in out


def test_hit_with_partly_removed_claims_is_served_not_regenerated(fixtures: Path) -> None:
    mixed = json.dumps({"claims": [
        {"id": "c1", "section": "news", "text": "Stripe and FedEx teamed up.", "source_ids": ["S1"]},
        {"id": "c2", "section": "news", "text": "Stripe raised 9 billion.", "source_ids": ["S1"]},
    ], "icebreakers": []})
    gw = FakeGateway(replies=[mixed])
    first = _run(gw)
    assert first.brief and len(first.brief.claims) == 1 and first.removed
    second = _run(gw)
    assert second.cache and second.cache.hit and second.calls == 1  # served, no paid regeneration
