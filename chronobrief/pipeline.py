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
from datetime import UTC, datetime
from pathlib import Path

import httpx

from chronobrief.brief import Brief, Removed, assign_ids, validate_brief
from chronobrief.fingerprint import Fingerprint, Source, fingerprint
from chronobrief.gateway import CacheInfo, CacheMode, GatewayReply, chat
from chronobrief.prompt import PROMPT_VERSION, BriefParseError, build_messages, parse_brief, screen_sources
from chronobrief.retrieval import DEFAULT_FIXTURES_DIR, Mode, RetrievalResult, search


def normalise_meeting_time(raw: str | None) -> str | None:
    """Accept an ISO-8601 date-time (UTC assumed if no offset); return it as YYYY-MM-DDTHH:MM:SSZ."""
    if raw is None or not raw.strip():
        return None
    text = raw.strip()[:40]
    try:
        dt = datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ValueError("Meeting time must look like 2026-11-03T10:00:00Z (UTC).") from exc
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    return dt.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


EMPTY_FINGERPRINT = Fingerprint(newest=None, count_at_newest=0, dated=0, undated=0, set_hash="")


@dataclass(frozen=True)
class Settings:
    bifrost_url: str = "http://localhost:8080"
    virtual_key: str = ""
    model: str = "gemini-3.1-flash-lite"
    mode: Mode = "replay"
    tavily_api_key: str = ""
    window_days: int = 30
    fixtures_dir: Path = DEFAULT_FIXTURES_DIR
    subject_context: str = ""  # live search disambiguator, e.g. "payments company"; replay ignores it
    audit_log: Path = Path("data/audit/freshcache.jsonl")

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
            fixtures_dir=Path(os.environ.get("CHRONO_FIXTURES_DIR") or DEFAULT_FIXTURES_DIR),
            subject_context=os.environ.get("CHRONO_SUBJECT_CONTEXT", ""),
            audit_log=Path(os.environ.get("CHRONO_AUDIT_LOG") or "data/audit/freshcache.jsonl"),
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
    meeting_time = normalise_meeting_time(meeting_time)
    retrieval = search(
        subject, mode=settings.mode, now=now, api_key=settings.tavily_api_key,
        client=tavily_client, window_days=settings.window_days, fixtures_dir=settings.fixtures_dir,
        context=settings.subject_context,
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
            build_messages(result.subject, ids, window_days=settings.window_days, repair_error=repair_error),
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
        # A HIT means the evidence is unchanged, so the validator removes exactly what it removed when
        # the brief was first stored: that is normal, not a reason to regenerate (doing so would turn
        # every HIT into a paid call). Only a cached brief with NO surviving claims is suspect.
        if reply.cache.hit and not brief.claims:
            raise BriefParseError("cached brief has no claims that pass validation")
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
