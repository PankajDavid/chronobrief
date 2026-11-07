"""Command line: python -m chronobrief "Stripe" [--mode replay|live] [--refresh] [--meeting 2026-11-03T10:00:00Z]"""
from __future__ import annotations

import argparse
import sys
from dataclasses import replace
from datetime import UTC, datetime
from pathlib import Path

from chronobrief.gateway import GatewayConfigError, GatewayError
from chronobrief.pipeline import BriefResult, Settings, generate_brief
from chronobrief.prompt import BriefParseError
from chronobrief.retrieval import RetrievalConfigError, RetrievalError

SECTION_TITLES = {"news": "News", "hires": "Hires", "launches": "Launches"}


def render_text(res: BriefResult) -> str:
    lines = [f"# {res.subject} - last 30 days"]
    if res.cache:
        c = res.cache
        saved = f", saved {c.saved_tokens} tokens" if c.hit and c.saved_tokens else ""
        lines.append(f"Cache: {c.decision}{saved}")
    for n in res.notices:
        lines.append(f"! {n}")
    if not res.brief:
        return "\n".join(lines)
    for sec, title in SECTION_TITLES.items():
        claims = [x for x in res.brief.claims if x.section == sec]
        lines.append(f"\n## {title}")
        if not claims:
            lines.append("  (nothing sourced in this window)")
        for cl in claims:
            refs = ", ".join(cl.source_ids)
            age = f", {cl.freshness_age_days}d old" if cl.freshness_age_days is not None else ""
            lines.append(f"- {cl.text} [{refs}{age}]")
    if res.brief.icebreakers:
        lines.append("\n## Questions to ask (suggestions)")
        lines.extend(f"- {q.text}" for q in res.brief.icebreakers)
    lines.append("\n## Sources")
    for sid, s in res.sources.items():
        date = s.published.strftime("%Y-%m-%d") if s.published else "undated"
        sim = " [SIMULATED]" if s.url in res.retrieval.simulated_urls else ""
        lines.append(f"{sid}. {s.title} ({date}){sim} {s.url}")
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    try:
        from dotenv import load_dotenv

        load_dotenv(Path.cwd() / ".env")
    except ImportError:
        pass
    ap = argparse.ArgumentParser(prog="python -m chronobrief")
    ap.add_argument("subject")
    ap.add_argument("--mode", choices=["replay", "live"])
    ap.add_argument("--refresh", action="store_true", help="skip the cache lookup and store a fresh brief")
    ap.add_argument("--meeting", help="meeting time, ISO-8601 UTC")
    args = ap.parse_args(argv)
    settings = Settings.from_env()
    if args.mode:
        settings = replace(settings, mode=args.mode)
    try:
        res = generate_brief(args.subject, settings=settings, now=datetime.now(UTC),
                             cache_mode="refresh" if args.refresh else "normal", meeting_time=args.meeting)
    except (RetrievalConfigError, GatewayConfigError) as exc:
        print(f"Setup needed: {exc}", file=sys.stderr)
        return 2
    except (RetrievalError, GatewayError, BriefParseError, ValueError) as exc:
        print(f"Could not build the brief: {exc}", file=sys.stderr)
        return 1
    print(render_text(res))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
