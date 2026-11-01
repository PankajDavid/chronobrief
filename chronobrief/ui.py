"""Pure rendering helpers for the dashboard. No Streamlit import here, so everything is unit-testable.

All text that came from a source or the model is passed through `md_escape` before it reaches
st.markdown, so a headline or claim cannot inject links, images or formatting.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime
from urllib.parse import urlsplit

from chronobrief.fingerprint import Source, fingerprint
from chronobrief.pipeline import BriefResult

SECTIONS: tuple[tuple[str, str], ...] = (("news", "News"), ("hires", "Executive moves"), ("launches", "Product launches"))
_MD_SPECIAL = re.compile(r"([\\`*_{}\[\]()#+\-.!|>~<$])")


def md_escape(text: str) -> str:
    """Escape Markdown so untrusted text renders literally."""
    return _MD_SPECIAL.sub(r"\\\1", " ".join(str(text).split()))


def safe_url(url: str) -> str | None:
    """Only http(s) links from the retrieved set are rendered; parentheses are encoded."""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.hostname:
        return None
    return url.replace("(", "%28").replace(")", "%29").replace(" ", "%20")


def publisher(url: str) -> str:
    host = (urlsplit(url).hostname or "").lower()
    return host[4:] if host.startswith("www.") else host


def fmt_date(value: datetime | str | None) -> str:
    if value is None or value == "":
        return "unknown"
    if isinstance(value, str):
        try:
            parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        except ValueError:
            return value[:25]
        return parsed.strftime("%d %b %Y, %H:%M UTC")
    return value.strftime("%d %b %Y, %H:%M UTC")


@dataclass(frozen=True)
class Banner:
    kind: str  # "success" | "info" | "warning" | "error" (colour is secondary; the text carries the meaning)
    text: str


def cache_banner(res: BriefResult) -> Banner:
    """One sentence that always starts with a word, then an explanation with dates."""
    c = res.cache
    if c is None:
        return Banner("info", "NO CALL: nothing was sent to the model.")
    newest = fingerprint(res.sources.values()).newest if res.sources else None
    req = fmt_date(newest)
    d = c.decision
    if d == "HIT":
        saved = f" Saved about {c.saved_tokens:,} tokens" if c.saved_tokens else ""
        cost = f" (${c.saved_cost_usd:.4f})" if c.saved_cost_usd else ""
        return Banner("success", f"✅ HIT: served from cache. Newest source in this request: {req}. "
                                 f"Newest source behind the cached brief: {fmt_date(c.cached_newest)}.{saved}{cost}")
    if d == "MISS_COLD":
        return Banner("info", "🆕 MISS: first brief for this subject, freshly generated and stored.")
    if d == "REFUSE_NEWER_SOURCE":
        return Banner("warning", f"🔄 REFUSED STALE: a newer source arrived (newest now {req}), "
                                 "so the stored brief was not reused and a fresh one was generated.")
    if d == "REFUSE_SET_CHANGED":
        return Banner("warning", "🔄 REFUSED STALE: the set of sources changed (an article was added, removed "
                                 "or corrected), so the brief was regenerated.")
    if d == "REFUSE_EXPIRED":
        return Banner("warning", "🔄 EXPIRED: the stored brief was older than the cache limit, so it was regenerated.")
    if d == "BYPASS_REFRESH":
        return Banner("info", "↻ REGENERATED: the cache was skipped on request, and this brief replaced the stored one.")
    if d == "REFUSE_NO_FINGERPRINT":
        return Banner("warning", "⚠ NOT CACHED: the evidence was incomplete or undated, so this brief was "
                                 "generated but not stored.")
    if d == "REFUSE_NO_TENANT":
        return Banner("error", "⚠ NOT CACHED: no team key was sent, so caching is switched off for safety.")
    if d == "REFUSE_FUTURE_DATE":
        return Banner("warning", "⚠ NOT CACHED: a source carried a date in the future, so it was not trusted.")
    return Banner("warning", f"? {md_escape(d)}: the gateway did not report a known cache decision.")


def ref_number(source_id: str) -> str:
    return source_id[1:] if source_id.startswith("S") and source_id[1:].isdigit() else source_id


def claim_line(text: str, source_ids: list[str], simulated_ids: set[str], age_days: int | None,
               date_unverified: bool) -> str:
    refs = " ".join(
        f"[{ref_number(s)}{' SIMULATED (demo only)' if s in simulated_ids else ''}]" for s in source_ids
    )
    extra = []
    if age_days is not None:
        extra.append("today" if age_days == 0 else f"{age_days}d old")
    if date_unverified:
        extra.append("date unverified")
    tail = f" _({', '.join(extra)})_" if extra else ""
    return f"- {md_escape(text)} {md_escape(refs)}{tail}"


def source_line(source_id: str, s: Source, simulated: bool) -> str:
    title = md_escape(s.title or publisher(s.url))
    url = safe_url(s.url)
    link = f"[{title}]({url})" if url else title
    badge = " **SIMULATED (demo only)**" if simulated else ""
    date = s.published.strftime("%d %b %Y") if s.published else "undated"
    return f"{ref_number(source_id)}. {link} — {md_escape(publisher(s.url))}, {date}{badge}"


def simulated_ids(res: BriefResult) -> set[str]:
    return {sid for sid, s in res.sources.items() if s.url in res.retrieval.simulated_urls}
