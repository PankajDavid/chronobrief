"""Brief schema and the deterministic citation validator.

The model proposes claims and open questions. This module decides which survive.
Nothing here calls a model. Aligned with Tavily Track Problem 02:
"Given a company or person and a meeting time, produce a one-page brief covering
the last 30 days of news, hires, launches, and open questions, with every claim sourced."
"""
from __future__ import annotations

import re
from datetime import datetime, timedelta
from typing import Literal

from pydantic import BaseModel, Field

from chronobrief.fingerprint import FUTURE_SKEW, Source

Section = Literal["news", "hires", "launches"]
_NUMBER = re.compile(r"\d[\d,]*(?:\.\d+)?")


class Claim(BaseModel):
    id: str
    section: Section
    text: str = Field(min_length=1, max_length=400)
    source_ids: list[str] = Field(default_factory=list)
    date_unverified: bool = False  # set by the validator, never by the model
    freshness_age_days: int | None = None  # computed deterministically from newest cited source


class Icebreaker(BaseModel):
    """Represents a strategic Open Question / Icebreaker for the meeting, tied to a verified claim."""
    text: str = Field(min_length=1, max_length=300)
    motivated_by: str  # id of the claim that makes this open question relevant


class Brief(BaseModel):
    subject: str
    meeting_time: str | None = None  # ISO-8601 UTC meeting time (Tavily Problem 02 requirement)
    claims: list[Claim] = Field(default_factory=list)
    icebreakers: list[Icebreaker] = Field(default_factory=list)

    @property
    def open_questions(self) -> list[Icebreaker]:
        """Alias matching Tavily Problem 02 terminology ('open questions')."""
        return self.icebreakers


class Removed(BaseModel):
    ref: str
    reason: str


def assign_ids(sources: list[Source]) -> dict[str, Source]:
    """Stable IDs S1..Sn in canonical-URL order, so the same source set always gets the same IDs."""
    return {f"S{i}": s for i, s in enumerate(sorted(sources, key=lambda s: s.url), 1)}


def _numbers(text: str) -> set[str]:
    return {m.group(0).replace(",", "") for m in _NUMBER.finditer(text)}


def validate_brief(
    brief: Brief,
    sources: dict[str, Source],
    now: datetime,
    window_days: int = 30,
) -> tuple[Brief, list[Removed]]:
    """Return (clean brief, removed items with reasons). Failing claims are dropped, never rewritten."""
    removed: list[Removed] = []
    kept: list[Claim] = []
    seen: set[str] = set()
    earliest = now - timedelta(days=window_days)

    for c in brief.claims:
        if c.id in seen:
            removed.append(Removed(ref=c.id, reason="duplicate claim id"))
            continue
        seen.add(c.id)
        if not c.source_ids:
            removed.append(Removed(ref=c.id, reason="no supporting source"))
            continue
        unknown = [sid for sid in c.source_ids if sid not in sources]
        if unknown:
            removed.append(Removed(ref=c.id, reason=f"cites unknown source {unknown[0]}"))
            continue
        cited = [sources[sid] for sid in c.source_ids]
        dated = [s for s in cited if s.published is not None]
        if dated and all(s.published < earliest or s.published > now + FUTURE_SKEW for s in dated):  # type: ignore[operator]
            removed.append(Removed(ref=c.id, reason="cited sources are outside the window"))
            continue
        support = " ".join(f"{s.title} {s.snippet}" for s in cited)
        missing = _numbers(c.text) - _numbers(support)
        if missing:
            removed.append(Removed(ref=c.id, reason=f"number {sorted(missing)[0]} not found in cited sources"))
            continue
        newest_cited = max((s.published for s in dated if s.published is not None), default=None)
        age_days = max(0, int((now - newest_cited).total_seconds() // 86400)) if newest_cited else None
        kept.append(c.model_copy(update={"date_unverified": not dated, "freshness_age_days": age_days}))

    kept_ids = {c.id for c in kept}
    icebreakers = []
    for i, q in enumerate(brief.icebreakers, 1):
        if q.motivated_by in kept_ids:
            icebreakers.append(q)
        else:
            removed.append(Removed(ref=f"icebreaker {i}", reason="motivating claim was removed or does not exist"))
    return Brief(subject=brief.subject, meeting_time=brief.meeting_time, claims=kept, icebreakers=icebreakers), removed
