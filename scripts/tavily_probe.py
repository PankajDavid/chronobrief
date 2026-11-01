"""T03 Tavily probe and fixture capture. Run this on YOUR machine (it needs TAVILY_API_KEY in .env).

Two jobs:

1. Probe (default): one real news search, saved raw so we build against what Tavily actually sends.
       python scripts/tavily_probe.py
       python scripts/tavily_probe.py --subject "Stripe"
   Writes docs/evidence/tavily-probe-<date>.json and prints a plain-English summary to paste into
   docs/TAVILY_PROBE.md: which fields exist, whether every result has a date, the date format,
   latency, and anything outside the 30-day window.

2. Capture: run the full four-query plan once and save a replay fixture for the demo.
       python scripts/tavily_probe.py --capture "Stripe"
   Writes fixtures/tavily/stripe.json (real results, simulated=false). Replay mode reads it offline.

The key is read from the environment and never printed or saved.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
from datetime import UTC, datetime, timedelta
from pathlib import Path

import httpx

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from chronobrief.fingerprint import parse_published  # noqa: E402
from chronobrief.retrieval import (  # noqa: E402
    DEFAULT_FIXTURES_DIR,
    TAVILY_SEARCH_URL,
    RetrievalConfigError,
    RetrievalError,
    normalise_subject,
    query_plan,
    slugify,
)

KEEP_FIELDS = ("title", "url", "published_date", "content", "score")


def _load_key() -> str:
    try:
        from dotenv import load_dotenv

        load_dotenv(ROOT / ".env")
    except ImportError:
        pass
    key = os.environ.get("TAVILY_API_KEY", "")
    if not key or key == "replace-me":
        sys.exit("TAVILY_API_KEY is not set. Put it in .env (never paste it into chat or commit it).")
    return key


def _date_format(value: object) -> str:
    if not isinstance(value, str) or not value.strip():
        return "MISSING"
    s = value.strip()
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}", s):
        return "date-only (YYYY-MM-DD)"
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z", s):
        return "ISO-8601 with Z"
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?([+-]\d{2}:?\d{2})?", s):
        return "ISO-8601 (offset or naive)"
    if re.match(r"[A-Z][a-z]{2}, \d{1,2} [A-Z][a-z]{2} \d{4}", s):
        return "RFC 2822 (e.g. 'Mon, 29 Sep 2025 14:00:00 GMT')"
    return f"OTHER: {s[:40]!r}"


def _search(client: httpx.Client, key: str, query: str, start: str, end: str) -> tuple[dict[str, object], float]:
    payload = {"query": query, "topic": "news", "start_date": start, "end_date": end,
               "max_results": 10, "search_depth": "basic"}
    t0 = time.perf_counter()
    resp = client.post(TAVILY_SEARCH_URL, json=payload, headers={"Authorization": f"Bearer {key}"}, timeout=20)
    elapsed = time.perf_counter() - t0
    if resp.status_code in (401, 403):
        sys.exit(f"Tavily rejected the key (HTTP {resp.status_code}). Check TAVILY_API_KEY in .env.")
    if resp.status_code >= 400:
        sys.exit(f"Tavily returned HTTP {resp.status_code}: {resp.text[:300]}")
    body = resp.json()
    if not isinstance(body, dict):
        sys.exit("Unexpected response: not a JSON object.")
    return body, elapsed


def probe(subject: str) -> None:
    key = _load_key()
    now = datetime.now(UTC)
    start, end = (now - timedelta(days=30)).date().isoformat(), now.date().isoformat()
    with httpx.Client() as client:
        body, elapsed = _search(client, key, f'"{subject}"', start, end)
    results = [r for r in body.get("results", []) if isinstance(r, dict)]  # type: ignore[union-attr]
    out = ROOT / "docs" / "evidence" / f"tavily-probe-{now.date().isoformat()}.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps({"request": {"query": f'"{subject}"', "topic": "news", "start_date": start,
                                           "end_date": end, "max_results": 10, "search_depth": "basic"},
                               "latency_seconds": round(elapsed, 2), "response": body}, indent=2), encoding="utf-8")

    fields = sorted({k for r in results for k in r})
    formats: dict[str, int] = {}
    outside = 0
    for r in results:
        fmt = _date_format(r.get("published_date"))
        formats[fmt] = formats.get(fmt, 0) + 1
        parsed = parse_published(r.get("published_date"))
        if parsed is not None and parsed < now - timedelta(days=30):
            outside += 1
    print(f"Saved raw response -> {out.relative_to(ROOT)}")
    print("\n--- paste into docs/TAVILY_PROBE.md ---")
    print(f"Probe date (UTC): {now.isoformat(timespec='seconds')}")
    print(f"Query: \"{subject}\", topic=news, {start}..{end}, max_results=10, search_depth=basic")
    print(f"Results returned: {len(results)}   Latency: {elapsed:.2f}s")
    print(f"Top-level response keys: {sorted(body)}")
    print(f"Fields seen on results: {fields}")
    print("published_date formats:")
    for fmt, n in sorted(formats.items(), key=lambda kv: -kv[1]):
        print(f"  {n:>2} x {fmt}")
    print(f"Results dated before the window start ({start}): {outside}")
    if "usage" in body or "credits" in body:
        print(f"Usage/credits reported: {body.get('usage') or body.get('credits')}")
    print("--- end ---")


def capture(subject: str, fixtures_dir: Path = DEFAULT_FIXTURES_DIR) -> None:
    key = _load_key()
    subj = normalise_subject(subject)
    now = datetime.now(UTC)
    start, end = (now - timedelta(days=30)).date().isoformat(), now.date().isoformat()
    merged: list[dict[str, object]] = []
    with httpx.Client() as client:
        for q in query_plan(subj):
            body, _ = _search(client, key, q, start, end)
            for r in body.get("results", []):  # type: ignore[union-attr]
                if isinstance(r, dict):
                    item = {k: r[k] for k in KEEP_FIELDS if k in r}
                    item["simulated"] = False
                    merged.append(item)
    path = fixtures_dir / f"{slugify(subj)}.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"subject": subj, "captured_at": now.strftime("%Y-%m-%dT%H:%M:%SZ"),
                                "results": merged}, indent=2), encoding="utf-8")
    print(f"Captured {len(merged)} raw results for '{subj}' -> {path.relative_to(ROOT)}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--subject", default="Stripe", help="company or person for the probe (default: Stripe)")
    ap.add_argument("--capture", metavar="SUBJECT", help="run the four-query plan and save a replay fixture")
    args = ap.parse_args()
    try:
        if args.capture:
            capture(args.capture)
        else:
            probe(args.subject)
    except (RetrievalConfigError, RetrievalError, ValueError) as exc:
        sys.exit(str(exc))
    except httpx.HTTPError as exc:
        sys.exit(f"Network error talking to Tavily: {type(exc).__name__}")


if __name__ == "__main__":
    main()
