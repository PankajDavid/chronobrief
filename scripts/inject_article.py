#!/usr/bin/env python3
"""Simulated newer-article injector for the ChronoBrief offline replay demo.

In replay mode (`CHRONO_TAVILY_MODE=replay`), appends a clearly badged `[SIMULATED]`
article with `published_date` set to current UTC time (`now`) so the Bifrost
`freshcache` plugin detects a newer source timestamp (`REFUSE_NEWER_SOURCE`) and
forces a live recomputation instead of serving yesterday's brief.

Usage:
  python scripts/inject_article.py stripe --headline "Stripe acquires Bridge for 1.1 billion"
  python scripts/inject_article.py stripe --reset
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
from datetime import UTC, datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FIXTURES_DIR = ROOT / "fixtures" / "tavily"


def inject_simulated_article(
    slug: str,
    headline: str = "announces major strategic product expansion and executive appointment",
    now: datetime | None = None,
    fixtures_dir: Path = FIXTURES_DIR,
    mode: str | None = None,
) -> dict:
    active_mode = (mode or os.environ.get("CHRONO_TAVILY_MODE", "replay")).lower()
    if active_mode != "replay":
        raise RuntimeError("inject_simulated_article is only allowed when CHRONO_TAVILY_MODE=replay")

    target = fixtures_dir / f"{slug}.json"
    base = fixtures_dir / f"{slug}.base.json"
    if not target.exists():
        if base.exists():
            shutil.copyfile(base, target)
        else:
            raise FileNotFoundError(f"Fixture not found: {target}")
    if not base.exists():
        # First injection for a newly captured subject: keep the clean recording so Reset works.
        shutil.copyfile(target, base)

    ts = (now or datetime.now(UTC)).astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
    data = json.loads(target.read_text(encoding="utf-8"))
    results = data.get("results", data if isinstance(data, list) else [])

    clean_headline = headline.removeprefix("[SIMULATED]").strip()
    article = {
        "title": f"[SIMULATED] {slug.title()} {clean_headline}",
        "url": f"https://example.com/simulated/{slug}-{int((now or datetime.now(UTC)).timestamp())}",
        "published_date": ts,
        "content": (
            f"[SIMULATED] Breaking update at {ts}: {slug.title()} {clean_headline}. "
            "Executives confirmed the initiative launches across global enterprise channels this quarter."
        ),
        "simulated": True,
    }
    results.append(article)
    if isinstance(data, dict):
        data["results"] = results
    else:
        data = results
    target.write_text(json.dumps(data, indent=2), encoding="utf-8")
    return article


def reset_fixture(slug: str, fixtures_dir: Path = FIXTURES_DIR) -> Path:
    target = fixtures_dir / f"{slug}.json"
    base = fixtures_dir / f"{slug}.base.json"
    if not base.exists():
        raise FileNotFoundError(f"Base fixture not found: {base}")
    shutil.copyfile(base, target)
    return target


def main() -> int:
    ap = argparse.ArgumentParser(description="Inject or reset a simulated breaking news article in replay fixtures")
    ap.add_argument("slug", help="Fixture slug (e.g., stripe)")
    ap.add_argument("--headline", default="announces next-gen AI settlement engine and appoints new Chief AI Officer")
    ap.add_argument("--reset", action="store_true", help="Restore fixture from <slug>.base.json")
    args = ap.parse_args()

    if args.reset:
        p = reset_fixture(args.slug)
        print(f"reset {p.relative_to(ROOT)} from base fixture")
        return 0

    art = inject_simulated_article(args.slug, headline=args.headline)
    print(f"injected simulated article: {art['title']} ({art['published_date']})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
