"""Fail if docs quote live-overhead numbers that differ from the evidence file.

Rule (.agents/rules/no-shortcuts.md): every number in docs must come from a
saved evidence file. This script is the enforcement for the live hook-overhead
numbers. It:

1. Parses every per-run row in docs/evidence/live-overhead-25runs.txt.
2. Computes min / median / mean / p90 / max itself (p90 = nearest-rank).
3. Checks the evidence file's own Summary table against those values.
4. Checks every doc listed in DOCS: any "median|mean|min|p90|max <n> ms|µs"
   that follows a HIT or MISS label must equal the computed value (rounded to
   the number of decimals the doc wrote).
5. Fails if a doc still quotes a retired number from an unsaved run.

Usage: python scripts/check_evidence.py      (exit 1 on any mismatch)
       python scripts/check_evidence.py --print   (show computed stats)
"""

from __future__ import annotations

import math
import re
import statistics
import sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent.parent
EVIDENCE = ROOT / "docs" / "evidence" / "live-overhead-25runs.txt"
DOCS = [
    "docs/DEMO_SCRIPT.md",
    "docs/GATEWAY_README_SECTION.md",
    "docs/PITCH.md",
    "docs/SCORECARD.md",
    "docs/SPEC_CACHE.md",
    "docs/evidence/INDEX.md",
    "gateway/FRESHNESS_CONTRACT.md",
    "gateway/examples/sec_8k_guard_demo.py",
    "README.md",
]
# Numbers from runs whose raw data was never saved. They must not reappear.
RETIRED = ["32.39", "32,387", "1.78 ms", "1,780", "29.81", "29,806", "43.57", "0.90 ms", "975 µs", "29,899"]

ROW = re.compile(r"^\|\s*(\d+)\s*\|\s*`?(\w+)`?\s*\|\s*(\d+)\s*µs\s*\|[^|]*\|\s*`?(\w+)`?\s*\|\s*(\d+)\s*µs\s*\|")
LABEL = re.compile(r"\b(HIT|MISS)\b")
STAT = re.compile(r"\b(median|mean|min|p90|max)\b\W{0,3}(\d[\d,]*(?:\.\d+)?)\s*(ms|µs)")
SUMMARY = re.compile(r"^\|\s*\*\*(Min|Median|Mean|P90|Max)\*\*\s*\|\s*(\d+)\s*µs[^|]*\|\s*(\d+)\s*µs")


def p90(values: list[int]) -> int:
    ordered = sorted(values)
    return ordered[math.ceil(0.9 * len(ordered)) - 1]


def compute(values: list[int]) -> dict[str, float]:
    return {
        "min": float(min(values)),
        "median": float(statistics.median(values)),
        "mean": float(round(statistics.mean(values))),
        "p90": float(p90(values)),
        "max": float(max(values)),
    }


def load_runs() -> tuple[list[int], list[int], list[str]]:
    miss: list[int] = []
    hit: list[int] = []
    lines = EVIDENCE.read_text(encoding="utf-8").splitlines()
    for line in lines:
        m = ROW.match(line)
        if m:
            miss.append(int(m.group(3)))
            hit.append(int(m.group(5)))
    return miss, hit, lines


def matches(written: str, unit: str, true_us: float) -> bool:
    value = float(written.replace(",", ""))
    decimals = len(written.split(".")[1]) if "." in written else 0
    true = true_us / 1000 if unit == "ms" else true_us
    return round(true, decimals) == value


def main() -> int:
    errors: list[str] = []
    miss, hit, ev_lines = load_runs()
    if len(miss) != 25 or len(hit) != 25:
        errors.append(f"expected 25 runs in {EVIDENCE.name}, parsed MISS={len(miss)} HIT={len(hit)}")
        print("\n".join(errors))
        return 1
    stats = {"MISS": compute(miss), "HIT": compute(hit)}

    if "--print" in sys.argv:
        for label, s in stats.items():
            print(label, {k: f"{v / 1000:.2f} ms" for k, v in s.items()})

    # 3. evidence file's own summary table
    for line in ev_lines:
        m = SUMMARY.match(line)
        if m:
            key = m.group(1).lower()
            for label, written in (("MISS", m.group(2)), ("HIT", m.group(3))):
                if float(written) != stats[label][key]:
                    errors.append(
                        f"{EVIDENCE.name} summary {m.group(1)} {label}: says {written} µs, rows give {stats[label][key]:.0f} µs"
                    )

    # 4 + 5. docs
    for rel in DOCS:
        path = ROOT / rel
        if not path.exists():
            continue
        for n, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            line = unquote(raw)
            for old in RETIRED:
                if old in line:
                    errors.append(f"{rel}:{n}: retired number '{old}' (no saved evidence)")
            if "microbenchmark" in line.lower() and not LABEL.search(line):
                continue
            current: str | None = None
            for tok in re.split(r"(\bHIT\b|\bMISS\b)", line):
                if tok in ("HIT", "MISS"):
                    current = tok
                    continue
                for sm in STAT.finditer(tok):
                    if current is None:
                        errors.append(f"{rel}:{n}: '{sm.group(0)}' has no HIT/MISS label before it")
                        continue
                    true_us = stats[current][sm.group(1)]
                    if not matches(sm.group(2), sm.group(3), true_us):
                        errors.append(
                            f"{rel}:{n}: {current} {sm.group(1)} written as {sm.group(2)} {sm.group(3)}, "
                            f"evidence gives {true_us / 1000:.2f} ms"
                        )

    if errors:
        print("evidence check FAILED:")
        print("\n".join(f"  - {e}" for e in errors))
        return 1
    print(f"evidence check: all clear ({len(DOCS)} docs vs {EVIDENCE.name})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
