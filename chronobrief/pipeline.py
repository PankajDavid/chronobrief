"""generate_brief: retrieve -> screen -> ids -> fingerprint -> gateway -> parse -> validate.

Order and rules:
- No sources: return the empty state, never call the model.
- Partial retrieval: send an EMPTY fingerprint so the plugin neither serves nor stores.
- Unparseable reply: one repair attempt with cache_mode=refresh and the parse error appended.
- A cached HIT that fails parsing or loses claims in validation is suspect: refresh once.
"""
from __future__ import annotations

import os
from dataclasses import dataclass, field
from datetime import datetime

import httpx

from chronobrief.brief import Brief, Removed, assign_ids, validate_brief
from chronobrief.fingerprint import Fingerprint, Source, fingerprint
from chronobrief.gateway import CacheInfo, CacheMode, GatewayReply, chat
from chronobrief.prompt import PROMPT_VERSION, BriefParseError, build_messages, parse_brief, screen_sources
from chronobrief.retrieval import Mode, RetrievalResult, search

EMPTY_FINGERPRINT = Fingerprint(newest=None, count_at_newest=0, dated=0, undated=0, set_hash="")


@dataclass(frozen=True)
class Settings:
    bifrost_url: str = "http://localhost:8080"
    virtual_key: str = ""
    model: str = "gemini-2.5-flash"
    mode: Mode = "replay"
    tavily_api_key: str = ""
    window_days: int = 30

    @classmethod
    def from_env(cls) -> Settings:
        mode = os.environ.get("CHRONO_TAVILY_MODE", "replay").strip().lower()
        return cls(
            bifrost_url=os.environ.get("BIFROST_URL", cls.bifrost_url),
            virtual_key=os.environ.get("CHRONO_VK") or os.environ.get("CHRONO_VK_ALPHA", ""),
            model=os.environ.get("CHRONO_MODEL", cls.model),
            mode="live" if mode == "live" else "replay",
            tavily_api_key=os.environ.get("TAVILY_API_KEY", ""),
            window_days=int(os.environ.get("CHRONO_WINDOW_DAYS", "30")),
        )


@dataclass
class BriefResult:
    subject: str
    retrieval: RetrievalResult
    brief: Brief | None = None
    removed: list[Removed] = field(default_factory=list)
    cache: CacheInfo | None = None
    sources: dict[str, Source] = field(default_factory=dict)
    flagged: list[Source] = field(default_factory=list)
    notices: list[str] = field(default_factory=list)
    calls: int = 0  # gateway calls made for this brief (1 normally, 2 after a repair/refresh)


def generate_brief(
    subject: str,
    *,
    settings: Settings,
    now: datetime,
    cache_mode: CacheMode = "normal",
    meeting_time: str | None = None,
    gateway_client: httpx.Client | None = None,
    tavily_client: httpx.Client | None = None,
) -> BriefResult:
    retrieval = search(
        subject, mode=settings.mode, now=now, api_key=settings.tavily_api_key,
        client=tavily_client, window_days=settings.window_days,
    )
    result = BriefResult(subject=retrieval.subject, retrieval=retrieval)
    if retrieval.partial:
        result.notices.append(
            f"{len(retrieval.failed_queries)} of 4 searches failed: coverage may be incomplete, so this brief is not cached."
        )
    if retrieval.mode == "replay":
        result.notices.append(f"Replay mode: recorded search from {retrieval.captured_at or 'an earlier capture'}.")
    if retrieval.simulated_urls:
        result.notices.append(f"{len(retrieval.simulated_urls)} SIMULATED article(s) included for the demo.")

    clean, flagged = screen_sources(retrieval.sources)
    result.flagged = flagged
    if flagged:
        result.notices.append(f"{len(flagged)} source(s) excluded: they contained instruction-like text.")
    if not clean:
        result.notices.append(f"No coverage found in the last {settings.window_days} days.")
        return result

    ids = assign_ids(clean)
    result.sources = ids
    fp = fingerprint(clean) if retrieval.cacheable else EMPTY_FINGERPRINT

    def call(mode: CacheMode, repair_error: str | None = None) -> GatewayReply:
        result.calls += 1
        return chat(
            build_messages(result.subject, ids, window_days=settings.window_days,
                           meeting_time=meeting_time, repair_error=repair_error),
            base_url=settings.bifrost_url, virtual_key=settings.virtual_key, model=settings.model,
            subject=result.subject, window_days=settings.window_days, prompt_version=PROMPT_VERSION,
            fingerprint_headers=fp.to_headers(), cache_mode=mode, client=gateway_client,
        )

    def parse_and_validate(reply: GatewayReply) -> tuple[Brief, list[Removed]]:
        brief = parse_brief(reply.text, subject=result.subject, meeting_time=meeting_time)
        return validate_brief(brief, ids, now, settings.window_days)

    reply = call(cache_mode)
    try:
        brief, removed = parse_and_validate(reply)
        if reply.cache.hit and removed:
            raise BriefParseError(f"cached brief failed validation ({len(removed)} item(s) removed)")
    except BriefParseError as exc:
        if reply.cache.hit:
            result.notices.append("Cached brief failed checks, so it was regenerated.")
            reply = call("refresh")
        else:
            reply = call("refresh", repair_error=str(exc))
        brief, removed = parse_and_validate(reply)  # a second failure propagates to the caller

    result.brief, result.removed, result.cache = brief, removed, reply.cache
    if removed:
        result.notices.append(f"{len(removed)} item(s) removed by the citation checker.")
    return result
