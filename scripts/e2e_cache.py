"""T10 end-to-end proof against the REAL gateway: MISS -> HIT -> REFUSE_NEWER_SOURCE.

Run on the machine where the gateway container is up and .env has CHRONO_VK_ALPHA and GEMINI_API_KEY:
    docker compose up -d
    python scripts/e2e_cache.py            # uses the real Stripe replay fixture
    python scripts/e2e_cache.py --subject Stripe

Steps (replay mode, so only the model call is live; costs 2 Gemini calls):
  0. reset the fixture to its recorded base
  1. normal   -> first request                     expected MISS_COLD (or HIT if run before)
  2. normal   -> same evidence                     expected HIT
  3. inject a SIMULATED newer article into the fixture
  4. normal   -> newer evidence                    expected REFUSE_NEWER_SOURCE
  5. reset the fixture
Writes docs/evidence/e2e-cache-<timestamp>.txt and exits non-zero if any expectation fails.
"""
from __future__ import annotations

import argparse
import sys
from dataclasses import replace
from datetime import UTC, datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from chronobrief.pipeline import Settings, generate_brief  # noqa: E402
from chronobrief.retrieval import slugify  # noqa: E402
from scripts.inject_article import inject_simulated_article, reset_fixture  # noqa: E402


def main() -> int:
    try:
        from dotenv import load_dotenv

        load_dotenv(ROOT / ".env")
    except ImportError:
        pass
    ap = argparse.ArgumentParser()
    ap.add_argument("--subject", default="Stripe")
    args = ap.parse_args()
    settings = replace(Settings.from_env(), mode="replay")
    slug = slugify(args.subject)
    rows: list[tuple[str, str, str, bool]] = []

    def step(label: str, mode: str, expect: tuple[str, ...]) -> None:
        res = generate_brief(args.subject, settings=settings, now=datetime.now(UTC), cache_mode=mode)  # type: ignore[arg-type]
        got = res.cache.decision if res.cache else "NO_CALL"
        claims = len(res.brief.claims) if res.brief else 0
        rows.append((label, got, f"{claims} claims, {res.calls} call(s)", got in expect))

    reset_fixture(slug)
    try:
        step("1 first request", "normal", ("MISS_COLD", "HIT"))  # HIT if an earlier run already stored it
        step("2 same evidence", "normal", ("HIT",))
        inject_simulated_article(slug, mode="replay")
        step("3 newer article", "normal", ("REFUSE_NEWER_SOURCE",))
    finally:
        reset_fixture(slug)

    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    report = [f"e2e cache proof {stamp} subject={args.subject} model={settings.model} gateway={settings.bifrost_url}"]
    report += [f"{'PASS' if ok else 'FAIL'}  {label:<20} {got:<22} {detail}" for label, got, detail, ok in rows]
    text = "\n".join(report)
    print(text)
    out = ROOT / "docs" / "evidence" / f"e2e-cache-{stamp}.txt"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(text + "\n", encoding="utf-8")
    print(f"\nSaved -> {out.relative_to(ROOT)}")
    return 0 if all(ok for *_, ok in rows) else 1


if __name__ == "__main__":
    raise SystemExit(main())
