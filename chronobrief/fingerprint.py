"""Source freshness fingerprint.

This is the client half of the freshness contract. The Go plugin (internal/decide) is the
server half. Both must agree on the meaning of every field, so change them together.

The fingerprint is derived from the sources Tavily returned, NOT from anything the model said.
Dates come from the search provider's published_date; a source without a parseable date is
counted as undated and never contributes to "newest".
"""
from __future__ import annotations

import hashlib
import re
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

_TRACKING_PARAMS = re.compile(r"^(utm_.*|fbclid|gclid|mc_cid|mc_eid|ref|ref_src|igshid)$", re.I)

# Sources dated further ahead than this are treated as untrustworthy and excluded.
FUTURE_SKEW = timedelta(hours=24)


@dataclass(frozen=True)
class Source:
    url: str  # canonical
    title: str
    published: datetime | None  # aware, UTC; None when undated or unparseable
    snippet: str = ""


@dataclass(frozen=True)
class Fingerprint:
    newest: datetime | None
    count_at_newest: int
    dated: int
    undated: int
    set_hash: str

    def to_headers(self) -> dict[str, str]:
        """Headers the gateway plugin reads. Empty strings mean 'no evidence'."""
        return {
            "x-chrono-newest": self.newest.strftime("%Y-%m-%dT%H:%M:%SZ") if self.newest else "",
            "x-chrono-count-at-newest": str(self.count_at_newest),
            "x-chrono-dated": str(self.dated),
            "x-chrono-undated": str(self.undated),
            "x-chrono-set-hash": self.set_hash,
        }


def canonical_url(url: str) -> str:
    """Stable identity for a source: lower-case host, no www, no tracking params, no fragment,
    no trailing slash. Two links to the same article must collapse to one entry."""
    parts = urlsplit(url.strip())
    host = (parts.hostname or "").lower()
    if host.startswith("www."):
        host = host[4:]
    query = urlencode(sorted((k, v) for k, v in parse_qsl(parts.query) if not _TRACKING_PARAMS.match(k)))
    path = parts.path.rstrip("/") or ""
    return urlunsplit((parts.scheme.lower() or "https", host, path, query, ""))


def parse_published(value: object) -> datetime | None:
    """Parse the date formats search providers emit. Returns aware UTC or None.

    Handles RFC 2822 ('Mon, 29 Sep 2025 14:00:00 GMT'), ISO 8601 with or without 'Z',
    and bare 'YYYY-MM-DD' (taken as 00:00 UTC). Anything else is None, never a guess.
    """
    if not isinstance(value, str) or not value.strip():
        return None
    s = value.strip()
    dt: datetime | None = None
    try:
        dt = parsedate_to_datetime(s)
    except (TypeError, ValueError, IndexError):
        dt = None
    if dt is None:
        try:
            dt = datetime.fromisoformat(s.replace("Z", "+00:00"))
        except ValueError:
            return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    return dt.astimezone(UTC)


def sources_from_tavily(results: Iterable[Mapping[str, object]], now: datetime) -> list[Source]:
    """Normalise raw Tavily result dicts: canonicalise URLs, parse dates, drop future-dated
    sources, collapse duplicates keeping the newest-dated copy."""
    by_url: dict[str, Source] = {}
    for r in results:
        raw_url = str(r.get("url") or "").strip()
        if not raw_url:
            continue
        url = canonical_url(raw_url)
        published = parse_published(r.get("published_date"))
        if published is not None and published > now + FUTURE_SKEW:
            published = None  # untrustworthy date: treat as undated rather than trust it
        src = Source(url=url, title=str(r.get("title") or ""), published=published, snippet=str(r.get("content") or ""))
        prev = by_url.get(url)
        if prev is None or (src.published and (prev.published is None or src.published > prev.published)):
            by_url[url] = src
    return sorted(by_url.values(), key=lambda s: s.url)


def fingerprint(sources: Iterable[Source]) -> Fingerprint:
    srcs = list(sources)
    dated = [s for s in srcs if s.published is not None]
    undated = len(srcs) - len(dated)
    newest = max((s.published for s in dated if s.published is not None), default=None)
    count_at_newest = sum(1 for s in dated if s.published == newest) if newest else 0
    lines = sorted(
        f"{s.url}|{s.published.strftime('%Y-%m-%dT%H:%M:%SZ') if s.published else 'undated'}|{' '.join(s.title.strip().split())}"
        for s in srcs
    )
    set_hash = hashlib.sha256("\n".join(lines).encode()).hexdigest() if lines else ""
    return Fingerprint(newest=newest, count_at_newest=count_at_newest, dated=len(dated), undated=undated, set_hash=set_hash)
