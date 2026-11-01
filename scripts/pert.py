#!/usr/bin/env python3
"""PERT/CPM over docs/plan.json.

  python scripts/pert.py            # expected durations, critical path, remaining work
  python scripts/pert.py --hours 6  # compare remaining critical path to hours left

Expected time te = (o + 4m + p) / 6, variance = ((p - o) / 6) ** 2.
Done tasks count as zero remaining. The critical path is the longest remaining dependency chain;
its summed variance gives a rough confidence range (normal approximation).
"""
from __future__ import annotations

import argparse
import json
import math
import sys
from pathlib import Path

PLAN = Path(__file__).resolve().parent.parent / "docs" / "plan.json"


def load(path: Path) -> dict:
    data = json.loads(path.read_text())
    ids = {t["id"] for t in data["tasks"]}
    for t in data["tasks"]:
        missing = set(t["deps"]) - ids
        if missing:
            sys.exit(f"{t['id']} depends on unknown task(s): {sorted(missing)}")
        if not (t["o"] <= t["m"] <= t["p"]):
            sys.exit(f"{t['id']}: need o <= m <= p")
    return data


def analyse(tasks: list[dict]) -> tuple[dict, list[str], float, float]:
    by_id = {t["id"]: t for t in tasks}
    te = {i: 0.0 if t["status"] == "done" else (t["o"] + 4 * t["m"] + t["p"]) / 6 for i, t in by_id.items()}
    var = {i: 0.0 if t["status"] == "done" else ((t["p"] - t["o"]) / 6) ** 2 for i, t in by_id.items()}
    finish: dict[str, float] = {}
    prev: dict[str, str | None] = {}
    visiting: set[str] = set()

    def f(i: str) -> float:
        if i in finish:
            return finish[i]
        if i in visiting:
            sys.exit(f"dependency cycle through {i}")
        visiting.add(i)
        best, best_dep = 0.0, None
        for d in by_id[i]["deps"]:
            if f(d) > best:
                best, best_dep = f(d), d
        visiting.discard(i)
        finish[i] = best + te[i]
        prev[i] = best_dep
        return finish[i]

    for i in by_id:
        f(i)
    end = max(finish, key=lambda k: finish[k])
    path, cur = [], end
    while cur:
        path.append(cur)
        cur = prev[cur]
    path.reverse()
    sigma = math.sqrt(sum(var[i] for i in path))
    return te, path, finish[end], sigma


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--hours", type=float, help="hours left until the deadline")
    ap.add_argument("--plan", type=Path, default=PLAN)
    args = ap.parse_args()
    data = load(args.plan)
    te, path, total, sigma = analyse(data["tasks"])
    names = {t["id"]: t["name"] for t in data["tasks"]}
    status = {t["id"]: t["status"] for t in data["tasks"]}
    print(f"{'id':<5}{'te(h)':>7}  {'status':<7} name")
    for t in data["tasks"]:
        mark = "*" if t["id"] in path else " "
        print(f"{t['id']:<4}{mark}{te[t['id']]:>7.2f}  {status[t['id']]:<7} {names[t['id']]}")
    print(f"\nCritical path (remaining): {' -> '.join(path)}")
    print(f"Expected remaining: {total:.1f} h   sigma: {sigma:.1f} h   ~90% range: {total:.1f} .. {total + 1.28 * sigma:.1f} h")
    if args.hours is not None:
        slack = args.hours - total
        verdict = "OK" if slack >= 1.28 * sigma else ("TIGHT" if slack >= 0 else "OVER: cut scope now")
        print(f"Hours left: {args.hours:.1f}  slack: {slack:+.1f} h  -> {verdict}")


if __name__ == "__main__":
    main()
