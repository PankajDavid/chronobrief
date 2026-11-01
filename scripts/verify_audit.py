#!/usr/bin/env python3
"""Independent oracle for the cache audit log.

The plugin writes one JSON line per lookup. This script re-checks every line WITHOUT trusting the
plugin's own decision: a HIT is a stale serve if the request's freshness was newer than the entry's.
The stale-serve count it prints is the number we show the judges, so it must come from here.

Savings = LLM cost and tokens only: Tavily search runs on every request to gather live source
timestamps and titles; cache hits save 100% of LLM generation cost and latency.

Comparison Benchmark (Expected Behaviour until measured against audit log):
  - 5-min TTL (Expected): High false-miss rate (recomputes needlessly when teammates prep >5 mins apart with unchanged news).
  - 24h TTL (Expected): High stale-serve rate (serves yesterday's brief after breaking news or headline corrections).
  - ChronoBrief (Expected): 0% stale serves while retaining full 24h cache hits when evidence is unchanged.

Pitch Line: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."

  python scripts/verify_audit.py data/audit/freshcache.jsonl
  python scripts/verify_audit.py data/audit/freshcache.jsonl --json    # machine-readable, used by the metrics panel

Exit code 1 if any stale serve is found or any line is malformed.
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from pathlib import Path


def is_newer(rec: dict) -> bool:
    inc, ent = rec.get("incoming_newest"), rec.get("entry_newest")
    if not inc or not ent:
        return False
    if inc > ent:  # RFC3339 UTC strings of identical shape compare correctly as text
        return True
    return inc == ent and int(rec.get("incoming_count", 0)) > int(rec.get("entry_count", 0))


def summarise(path: Path) -> dict:
    decisions: Counter[str] = Counter()
    stale = malformed = 0
    saved_tokens = 0
    saved_cost = 0.0
    served_without_evidence = 0
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            rec = json.loads(line)
            decision = rec["decision"]
        except (json.JSONDecodeError, KeyError):
            malformed += 1
            continue
        decisions[decision] += 1
        if decision == "HIT":
            if is_newer(rec):
                stale += 1
            if not rec.get("incoming_newest"):
                served_without_evidence += 1
            saved_tokens += int(rec.get("saved_tokens", 0))
            saved_cost += float(rec.get("saved_cost_usd", 0.0))
    lookups = sum(decisions.values())
    hits = decisions["HIT"]
    return {
        "lookups": lookups,
        "hits": hits,
        "hit_rate": round(hits / lookups, 4) if lookups else 0.0,
        "decisions": dict(decisions),
        "stale_serves": stale,
        "stale_serve_rate": round(stale / hits, 4) if hits else 0.0,
        "served_without_evidence": served_without_evidence,
        "stale_answers_prevented": decisions["REFUSE_NEWER_SOURCE"],
        "saved_tokens": saved_tokens,
        "saved_cost_usd": round(saved_cost, 6),
        "malformed_lines": malformed,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("log", type=Path)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    if not args.log.exists():
        print(f"no audit log at {args.log}", file=sys.stderr)
        return 1
    s = summarise(args.log)
    if args.json:
        print(json.dumps(s, indent=2))
    else:
        print(f"lookups {s['lookups']}  hits {s['hits']}  hit rate {s['hit_rate']:.1%}")
        print(f"stale serves {s['stale_serves']}  (stale answers prevented: {s['stale_answers_prevented']})")
        print(f"saved (LLM only): {s['saved_tokens']} tokens  ${s['saved_cost_usd']:.4f}")
        print(f"decisions {s['decisions']}")
        print(
            "benchmark (expected until measured against audit log): "
            "5-min TTL (high false miss) vs 24h TTL (high stale serve) vs ChronoBrief (0% stale serve)"
        )
        print('pitch: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."')
        if s["malformed_lines"]:
            print(f"WARNING {s['malformed_lines']} malformed line(s)")
    return 1 if s["stale_serves"] or s["served_without_evidence"] or s["malformed_lines"] else 0


if __name__ == "__main__":
    sys.exit(main())
