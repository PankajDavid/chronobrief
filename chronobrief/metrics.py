"""Scoreboard data for the dashboard, read from the plugin's audit log.

The summary numbers come from scripts/verify_audit.py (the independent oracle), so the panel and
the command-line check can never disagree. Nothing here is cached: the file is re-read on every
render, which is cheap (one JSON line per lookup).
"""
from __future__ import annotations

import importlib.util
import json
import statistics
from dataclasses import dataclass
from pathlib import Path
from types import ModuleType
from typing import Any

_ROOT = Path(__file__).resolve().parent.parent
_ORACLE_PATH = _ROOT / "scripts" / "verify_audit.py"


def _oracle() -> ModuleType:
    spec = importlib.util.spec_from_file_location("chronobrief_verify_audit", _ORACLE_PATH)
    if spec is None or spec.loader is None:  # pragma: no cover - file ships with the repo
        raise RuntimeError("scripts/verify_audit.py not found")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


@dataclass(frozen=True)
class Scoreboard:
    summary: dict[str, Any]
    recent: list[dict[str, Any]]
    latency_ms: dict[str, int]  # median latency by decision, from the audit log


def _resolve(path: Path) -> Path:
    return path if path.is_absolute() else _ROOT / path


def recent_decisions(path: Path, n: int = 10) -> list[dict[str, Any]]:
    """Last n well-formed records, newest first, reduced to what the panel shows."""
    rows: list[dict[str, Any]] = []
    lines = _resolve(path).read_text(encoding="utf-8").splitlines()
    for line in reversed(lines):
        if len(rows) >= n:
            break
        try:
            rec = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(rec, dict) or "decision" not in rec:
            continue
        rows.append({
            "time (UTC)": str(rec.get("timestamp", ""))[:19].replace("T", " "),
            "decision": str(rec["decision"])[:40],
            "newest source in request": str(rec.get("incoming_newest") or rec.get("request_newest") or "—")[:20],
            "newest source behind cached brief": str(rec.get("entry_newest") or rec.get("cached_newest") or "—")[:20],
            "tokens saved": int(rec.get("saved_tokens") or 0),
            "latency ms": int(rec.get("latency_ms") or 0),
        })
    return rows


def _latency_by_decision(path: Path) -> dict[str, int]:
    buckets: dict[str, list[int]] = {}
    for line in _resolve(path).read_text(encoding="utf-8").splitlines():
        try:
            rec = json.loads(line)
            buckets.setdefault(str(rec["decision"]), []).append(int(rec.get("latency_ms") or 0))
        except (json.JSONDecodeError, KeyError, TypeError, ValueError):
            continue
    return {d: int(statistics.median(v)) for d, v in buckets.items() if v}


def load_scoreboard(path: Path, n: int = 10) -> Scoreboard | None:
    """None when the gateway has not written an audit log yet."""
    p = _resolve(path)
    if not p.is_file() or p.stat().st_size == 0:
        return None
    summary: dict[str, Any] = _oracle().summarise(p)
    return Scoreboard(summary=summary, recent=recent_decisions(p, n), latency_ms=_latency_by_decision(p))
