from __future__ import annotations

import json
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path

import httpx
import pytest

from chronobrief.fingerprint import fingerprint
from chronobrief.retrieval import (
    EXCLUDED_DOMAINS,
    NEWS_PUBLISHERS,
    RetrievalConfigError,
    RetrievalError,
    normalise_subject,
    query_plan,
    search,
    slugify,
)

NOW = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)
KEY = "tvly-test-key"


def _r(url: str, date: str | None, title: str = "t") -> dict[str, object]:
    d: dict[str, object] = {"url": url, "title": title, "content": "c"}
    if date is not None:
        d["published_date"] = date
    return d


def _client(handler: Callable[[httpx.Request], httpx.Response]) -> httpx.Client:
    return httpx.Client(transport=httpx.MockTransport(handler))


def _by_query(responses: dict[int, httpx.Response | Exception]) -> tuple[httpx.Client, list[str]]:
    """Answer the Nth distinct query with responses[N]; record queries seen."""
    seen: list[str] = []
    plan = query_plan("Acme")

    def handler(req: httpx.Request) -> httpx.Response:
        body = json.loads(req.content)
        seen.append(body["query"])
        assert req.headers["authorization"] == f"Bearer {KEY}"
        assert body["topic"] == "news" and body["start_date"] == "2026-10-02" and body["end_date"] == "2026-11-01"
        assert body["exclude_domains"] == ["google.com", "www.google.com", "news.google.com"]
        assert body["include_domains"] == list(NEWS_PUBLISHERS)
        out = responses.get(plan.index(body["query"]), httpx.Response(200, json={"results": []}))
        if isinstance(out, Exception):
            raise out
        return out

    return _client(handler), seen


def _live(client: httpx.Client, **kw: object):  # type: ignore[no-untyped-def]
    return search("Acme", mode="live", now=NOW, api_key=KEY, client=client, sleep=lambda _s: None, **kw)  # type: ignore[arg-type]


def test_happy_path_merges_and_dedupes_across_queries() -> None:
    client, seen = _by_query({
        0: httpx.Response(200, json={"results": [_r("https://www.a.com/x?utm_source=z", "2026-10-30T10:00:00Z")]}),
        2: httpx.Response(200, json={"results": [_r("https://a.com/x", "2026-10-30T10:00:00Z"), _r("https://b.com/y", "2026-10-20")]}),
    })
    res = _live(client)
    assert len(seen) == 4
    assert [s.url for s in res.sources] == ["https://a.com/x", "https://b.com/y"]
    assert not res.partial and res.cacheable


def test_zero_results_is_empty_not_error() -> None:
    client, _ = _by_query({})
    res = _live(client)
    assert res.sources == [] and not res.partial and not res.cacheable


def test_one_query_failing_marks_partial_and_uncacheable() -> None:
    client, _ = _by_query({
        0: httpx.Response(200, json={"results": [_r("https://a.com/x", "2026-10-30")]}),
        1: httpx.Response(400, json={"detail": "bad"}),
    })
    res = _live(client)
    assert res.partial and not res.cacheable
    assert len(res.failed_queries) == 1


def test_every_query_failing_raises_instead_of_meaning_no_news() -> None:
    client, _ = _by_query({i: httpx.Response(503) for i in range(4)})
    with pytest.raises(RetrievalError):
        _live(client)


def test_timeout_then_success_retries() -> None:
    calls = {"n": 0}

    def handler(req: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        if calls["n"] == 1:
            raise httpx.ReadTimeout("slow", request=req)
        return httpx.Response(200, json={"results": [_r("https://a.com/x", "2026-10-30")]})

    res = _live(_client(handler))
    assert not res.partial and len(res.sources) == 1
    assert calls["n"] == 5  # 1 timeout + retry, then 3 more queries


def test_rejected_key_is_config_error() -> None:
    client, _ = _by_query({0: httpx.Response(401)})
    with pytest.raises(RetrievalConfigError):
        _live(client)


@pytest.mark.parametrize("key", [None, "", "replace-me"])
def test_missing_key_is_config_error(key: str | None) -> None:
    with pytest.raises(RetrievalConfigError):
        search("Acme", mode="live", now=NOW, api_key=key)


def test_undated_kept_old_dropped_and_cap_keeps_newest() -> None:
    results = [_r(f"https://n.com/{i}", f"2026-10-{10 + i:02d}") for i in range(10)]
    results += [_r("https://u.com/undated", None), _r("https://old.com/x", "2026-09-01")]
    client, _ = _by_query({0: httpx.Response(200, json={"results": results})})
    res = _live(client, cap=5)
    urls = {s.url for s in res.sources}
    assert "https://old.com/x" not in urls  # outside the 30-day window
    assert urls == {f"https://n.com/{i}" for i in range(5, 10)}  # five newest; undated ranked last
    fp = fingerprint(res.sources)
    assert fp.newest == datetime(2026, 10, 19, tzinfo=UTC) and fp.undated == 0


def test_live_rejects_simulated_articles() -> None:
    sim = {**_r("https://example.com/simulated/x", "2026-10-31"), "simulated": True}
    client, _ = _by_query({0: httpx.Response(200, json={"results": [sim]})})
    with pytest.raises(RetrievalError):
        _live(client)


def _fixture(tmp_path: Path, results: list[dict[str, object]], captured: str = "2026-10-30T18:00:00Z") -> Path:
    (tmp_path / "acme.json").write_text(json.dumps({"subject": "Acme", "captured_at": captured, "results": results}))
    return tmp_path


def test_replay_uses_fixture_and_captured_at_as_window_reference(tmp_path: Path) -> None:
    sim = {**_r("https://example.com/simulated/1", "2026-10-30T17:00:00Z"), "simulated": True}
    d = _fixture(tmp_path, [_r("https://a.com/x", "2026-10-05"), sim])
    later = datetime(2027, 1, 1, tzinfo=UTC)  # far in the future: window still measured from captured_at
    res = search("acme", mode="replay", now=later, fixtures_dir=d)
    assert res.mode == "replay" and res.captured_at == "2026-10-30T18:00:00Z"
    assert {s.url for s in res.sources} == {"https://a.com/x", "https://example.com/simulated/1"}
    assert res.simulated_urls == frozenset({"https://example.com/simulated/1"})


def test_replay_missing_fixture_is_config_error(tmp_path: Path) -> None:
    with pytest.raises(RetrievalConfigError, match="tavily_probe.py --capture"):
        search("Nobody Inc", mode="replay", now=NOW, fixtures_dir=tmp_path)


def test_replay_and_live_produce_same_fingerprint_for_same_results(tmp_path: Path) -> None:
    results = [_r("https://a.com/x", "2026-10-30T10:00:00Z", "Headline A"), _r("https://b.com/y", None, "B")]
    d = _fixture(tmp_path, results, captured="2026-11-01T12:00:00Z")
    client, _ = _by_query({0: httpx.Response(200, json={"results": results})})
    assert fingerprint(_live(client).sources) == fingerprint(search("Acme", mode="replay", now=NOW, fixtures_dir=d).sources)


def test_subject_normalisation_and_slug() -> None:
    assert normalise_subject("  Open   AI \n") == "Open AI"
    assert len(normalise_subject("x" * 500)) == 200
    with pytest.raises(ValueError):
        normalise_subject("   ")
    assert slugify("Open AI, Inc.") == "open-ai-inc"


def test_real_captured_stripe_fixture_parses_every_date() -> None:
    """The committed fixture is a real Tavily capture (RFC 2822 dates). Every result must parse."""
    res = search("Stripe", mode="replay", now=NOW)
    fp = fingerprint(res.sources)
    assert res.sources and not res.simulated_urls
    assert fp.undated == 0 and fp.newest is not None and fp.set_hash


# --- live retrieval quality: no opaque redirect hosts ---

def _capture_bodies() -> tuple[httpx.Client, list[dict[str, object]]]:
    bodies: list[dict[str, object]] = []

    def handler(req: httpx.Request) -> httpx.Response:
        bodies.append(json.loads(req.content))
        return httpx.Response(200, json={"results": []})

    return _client(handler), bodies


def test_every_live_request_excludes_google_redirect_hosts() -> None:
    client, bodies = _capture_bodies()
    _live(client)
    assert len(bodies) == 4
    for body in bodies:
        assert set(body["exclude_domains"]) >= {"google.com", "www.google.com", "news.google.com"}  # type: ignore[arg-type]
    assert "google.com" in EXCLUDED_DOMAINS


def test_news_publishers_is_a_clean_bare_domain_list() -> None:
    assert len(NEWS_PUBLISHERS) == len(set(NEWS_PUBLISHERS))
    assert all("/" not in d and "*" not in d and d == d.strip().lower() for d in NEWS_PUBLISHERS)
    assert not set(NEWS_PUBLISHERS) & set(EXCLUDED_DOMAINS)
    assert {"reuters.com", "techcrunch.com", "pymnts.com", "thepaypers.com"} <= set(NEWS_PUBLISHERS)
    assert not any(d.endswith("google.com") or d == "stripe.com" for d in NEWS_PUBLISHERS)


def test_every_live_request_is_restricted_to_news_publishers() -> None:
    client, bodies = _capture_bodies()
    _live(client)
    assert len(bodies) == 4 and all(b["include_domains"] == list(NEWS_PUBLISHERS) for b in bodies)


def test_people_query_requires_subject_as_the_hirer() -> None:
    people = query_plan("Acme")[1]
    for phrase in ('"Acme appoints"', '"Acme hires"', '"Acme names"', '"joins Acme as"'):
        assert phrase in people
    assert "CFO" not in people and "executive" not in people
