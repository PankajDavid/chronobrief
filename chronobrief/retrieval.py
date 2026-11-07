"""Retrieval: the last N days of news about a subject, from Tavily (live) or a fixture (replay).

Retrieval decides which sources the model sees, and therefore the fingerprint the cache trusts.
Both modes run the same normalisation path (`fingerprint.sources_from_tavily`); only the source of
the raw result dicts differs.

Failure rules (see .agents/skills/tavily-retrieval):
- Missing/invalid key or missing fixture -> RetrievalConfigError (UI shows a setup message).
- Every query failed -> RetrievalError (UI shows "search unavailable"; never treated as "no news").
- Some queries failed -> partial=True. The pipeline must then send an EMPTY fingerprint so the
  plugin neither serves nor stores a brief built from incomplete evidence.
- All queries succeeded but found nothing -> empty source list, partial=False.
"""
from __future__ import annotations

import json
import re
import time
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from pathlib import Path
from typing import Literal

import httpx

from chronobrief.fingerprint import Source, canonical_url, parse_published, sources_from_tavily

TAVILY_SEARCH_URL = "https://api.tavily.com/search"
DEFAULT_FIXTURES_DIR = Path(__file__).resolve().parent.parent / "fixtures" / "tavily"
MAX_SUBJECT_CHARS = 200
TIMEOUT_SECONDS = 15.0
RETRY_BACKOFF = (0.5, 1.5)  # two retries, on 429 / 5xx / timeout only
# Excluded at the Tavily request: these hosts return opaque redirect links (google.com/url?q=<token>)
# that name no publisher, so a citation to them cannot be checked by a reader. Never unwrapped.
EXCLUDED_DOMAINS: tuple[str, ...] = ("google.com", "www.google.com", "news.google.com")
# Live searches are restricted to these business, payments and tech news publishers (Tavily
# include_domains, bare domains only). Without it, topic="news" still returned job listings,
# product pages and social posts. stripe.com/newsroom is left out: path-level matching is unverified.
NEWS_PUBLISHERS: tuple[str, ...] = (
    "reuters.com", "bloomberg.com", "cnbc.com", "wsj.com", "ft.com", "axios.com", "fortune.com",
    "businessinsider.com", "theinformation.com", "techcrunch.com", "theverge.com", "geekwire.com",
    "pymnts.com", "thepaypers.com", "paymentsdive.com", "finextra.com", "paymentsjournal.com",
)

Mode = Literal["live", "replay"]


class RetrievalConfigError(Exception):
    """Setup problem the user can fix: missing key, rejected key, missing replay fixture."""


class RetrievalError(Exception):
    """Search was unavailable. Never interpret this as 'nothing happened'."""


@dataclass(frozen=True)
class RetrievalResult:
    subject: str
    mode: Mode
    sources: list[Source]
    partial: bool = False
    failed_queries: tuple[str, ...] = ()
    simulated_urls: frozenset[str] = field(default_factory=frozenset)
    captured_at: str | None = None  # replay only: when the fixture was recorded
    off_topic_dropped: int = 0  # sources whose title and snippet never name the subject

    @property
    def cacheable(self) -> bool:
        """A brief may only be cached when the evidence behind it is complete."""
        return not self.partial and bool(self.sources)


def normalise_subject(raw: str) -> str:
    """Strip, collapse whitespace, cap length. The same string feeds the cache key everywhere."""
    s = " ".join(str(raw).split())[:MAX_SUBJECT_CHARS].strip()
    if not s:
        raise ValueError("Enter a company or person name.")
    return s


def slugify(subject: str) -> str:
    """Fixture file name: lower-case, non-alphanumerics collapsed to '-'."""
    slug = re.sub(r"[^a-z0-9]+", "-", subject.lower()).strip("-")
    if not slug:
        raise ValueError("Subject has no letters or digits.")
    return slug


def query_plan(subject: str) -> list[str]:
    """Four news searches per subject: general, people, launches, strategy."""
    q = f'"{subject}"'
    return [
        q,
        # The subject must be the one hiring: phrase-quoted so a hire that merely mentions it is not a match.
        f'"{subject} appoints" OR "{subject} hires" OR "{subject} names" OR "joins {subject} as"',
        f"{q} launches OR announces OR releases product OR feature",
        f"{q} partnership OR acquisition OR funding OR strategy",
    ]


def _subject_pattern(subject: str) -> re.Pattern[str]:
    """Whole-word, case-insensitive match of the subject; spaces in it match any whitespace.
    "Stripe" matches "stripe" and "Stripe's" but not "Stripes" or "Stars and Stripes"."""
    words = [re.escape(w) for w in subject.split()]
    return re.compile(r"(?<!\w)" + r"\s+".join(words) + r"(?!\w)", re.IGNORECASE)


def mentions_subject(subject: str, title: str, snippet: str) -> bool:
    """Deterministic relevance check: the subject must be named in the title or the snippet."""
    pat = _subject_pattern(subject)
    return bool(pat.search(title) or pat.search(snippet))


def _cap_newest(sources: list[Source], cap: int) -> list[Source]:
    """Keep the `cap` newest sources (undated last), then restore stable URL order."""
    ranked = sorted(sources, key=lambda s: (s.published is None, -(s.published.timestamp() if s.published else 0), s.url))
    return sorted(ranked[:cap], key=lambda s: s.url)


def _within_window(sources: list[Source], ref: datetime, window_days: int) -> list[Source]:
    """Drop dated sources older than the window. Undated sources are kept (and counted as undated)."""
    start = ref - timedelta(days=window_days)
    return [s for s in sources if s.published is None or s.published >= start]


def _post_with_retry(
    client: httpx.Client,
    payload: Mapping[str, object],
    api_key: str,
    sleep: Callable[[float], None],
) -> list[Mapping[str, object]]:
    """One Tavily search. Retries 429/5xx/timeouts twice; raises RetrievalConfigError on 401/403."""
    last_err = "unknown error"
    for attempt in range(len(RETRY_BACKOFF) + 1):
        try:
            resp = client.post(
                TAVILY_SEARCH_URL,
                json=dict(payload),
                headers={"Authorization": f"Bearer {api_key}"},
                timeout=TIMEOUT_SECONDS,
            )
        except httpx.TimeoutException:
            last_err = "timeout"
        except httpx.HTTPError as exc:
            last_err = type(exc).__name__
        else:
            if resp.status_code in (401, 403):
                raise RetrievalConfigError("Tavily rejected the API key. Check TAVILY_API_KEY in .env.")
            if resp.status_code == 429 or resp.status_code >= 500:
                last_err = f"HTTP {resp.status_code}"
            elif resp.status_code >= 400:
                raise RetrievalError(f"Tavily returned HTTP {resp.status_code}")
            else:
                try:
                    body = resp.json()
                except ValueError as exc:
                    raise RetrievalError("Tavily returned a non-JSON body") from exc
                results = body.get("results") if isinstance(body, dict) else None
                if not isinstance(results, list):
                    raise RetrievalError("Tavily response has no 'results' list")
                return [r for r in results if isinstance(r, dict)]
        if attempt < len(RETRY_BACKOFF):
            sleep(RETRY_BACKOFF[attempt])
    raise RetrievalError(f"Tavily unavailable after retries ({last_err})")


def _live_results(
    subject: str,
    *,
    now: datetime,
    window_days: int,
    max_results: int,
    api_key: str | None,
    client: httpx.Client | None,
    sleep: Callable[[float], None],
) -> tuple[list[Mapping[str, object]], tuple[str, ...]]:
    if not api_key or api_key == "replace-me":
        raise RetrievalConfigError("TAVILY_API_KEY is not set. Add it to .env (never commit it).")
    start = (now - timedelta(days=window_days)).date().isoformat()
    end = now.date().isoformat()
    own_client = client is None
    http = client or httpx.Client()
    merged: list[Mapping[str, object]] = []
    failed: list[str] = []
    plan = query_plan(subject)
    try:
        for query in plan:
            payload = {
                "query": query,
                "topic": "news",
                "start_date": start,
                "end_date": end,
                "max_results": max_results,
                "search_depth": "basic",
                "include_domains": list(NEWS_PUBLISHERS),
                "exclude_domains": list(EXCLUDED_DOMAINS),
            }
            try:
                merged.extend(_post_with_retry(http, payload, api_key, sleep))
            except RetrievalError:
                failed.append(query)
    finally:
        if own_client:
            http.close()
    if len(failed) == len(plan):
        raise RetrievalError("Every Tavily search failed. Coverage is unknown, so no brief was generated.")
    return merged, tuple(failed)


def load_fixture(subject: str, fixtures_dir: Path = DEFAULT_FIXTURES_DIR) -> dict[str, object]:
    path = fixtures_dir / f"{slugify(subject)}.json"
    if not path.is_file():
        raise RetrievalConfigError(
            f"No replay fixture for '{subject}'. Capture one with: python scripts/tavily_probe.py --capture \"{subject}\""
        )
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or not isinstance(data.get("results"), list):
        raise RetrievalConfigError(f"Fixture {path.name} is malformed: expected an object with a 'results' list.")
    return data


def search(
    subject: str,
    *,
    mode: Mode,
    now: datetime,
    api_key: str | None = None,
    client: httpx.Client | None = None,
    fixtures_dir: Path = DEFAULT_FIXTURES_DIR,
    window_days: int = 30,
    max_results: int = 10,
    cap: int = 25,
    sleep: Callable[[float], None] = time.sleep,
) -> RetrievalResult:
    """Retrieve, normalise, window-filter and cap sources for `subject`."""
    subj = normalise_subject(subject)
    captured_at: str | None = None
    failed: tuple[str, ...] = ()
    ref = now
    if mode == "replay":
        data = load_fixture(subj, fixtures_dir)
        rows = data["results"]
        raw: Sequence[Mapping[str, object]] = [r for r in rows if isinstance(r, dict)] if isinstance(rows, list) else []
        captured_at = str(data.get("captured_at") or "") or None
        # Window is measured from when the fixture was recorded, so replay stays deterministic.
        parsed_ref = parse_published(captured_at)
        if parsed_ref is not None:
            ref = parsed_ref
    elif mode == "live":
        raw, failed = _live_results(
            subj, now=now, window_days=window_days, max_results=max_results,
            api_key=api_key, client=client, sleep=sleep,
        )
    else:  # pragma: no cover - guarded by the Literal type
        raise ValueError(f"unknown mode {mode!r}")

    simulated = frozenset(
        canonical_url(str(r.get("url") or "")) for r in raw if r.get("simulated") is True and r.get("url")
    )
    if mode == "live" and simulated:
        raise RetrievalError("Simulated articles are only allowed in replay mode.")
    # Future-date guard uses the later of (fixture time, real now): a simulated article injected
    # during a replay demo is dated "now", which must count as newer, not as untrustworthy.
    normalised = sources_from_tavily(raw, max(ref, now))
    # Drop sources that never name the subject (both modes, before window and cap, so the cap
    # keeps the newest relevant sources). Search engines match words like "Stripes" or text
    # outside the article; such sources only add noise and churn to the fingerprint.
    relevant = [s for s in normalised if mentions_subject(subj, s.title, s.snippet)]
    sources = _cap_newest(_within_window(relevant, ref, window_days), cap)
    return RetrievalResult(
        subject=subj,
        mode=mode,
        sources=sources,
        partial=bool(failed),
        failed_queries=failed,
        simulated_urls=simulated & {s.url for s in sources},
        captured_at=captured_at,
        off_topic_dropped=len(normalised) - len(relevant),
    )
