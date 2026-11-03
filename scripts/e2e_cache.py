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
Then runs the independent audit oracle (scripts/verify_audit.py) over ONLY the audit-log lines this
run produced, and saves both reports in one file, docs/evidence/e2e-cache-<timestamp>.txt, headed by
the git commit. Docs must quote that file verbatim (scripts/check_evidence.py enforces it).
Exits non-zero if any expectation fails, the audit log is missing, or the oracle finds a stale serve.
"""
from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from dataclasses import replace
from datetime import UTC, datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from chronobrief.pipeline import Settings, generate_brief  # noqa: E402
from chronobrief.retrieval import slugify  # noqa: E402
from scripts.inject_article import inject_simulated_article, reset_fixture  # noqa: E402
from scripts.verify_audit import format_summary, summarise_text  # noqa: E402


def _commit() -> str:
    git = shutil.which("git")
    if git is None:
        return "unknown"
    try:
        out = subprocess.run([git, "rev-parse", "--short", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=True)
        dirty = subprocess.run([git, "status", "--porcelain", "--untracked-files=no"], cwd=ROOT, capture_output=True, text=True, check=True)
        return out.stdout.strip() + ("-dirty" if dirty.stdout.strip() else "")
    except (OSError, subprocess.CalledProcessError):
        return "unknown"


def oracle_report(audit_log: Path, start_offset: int) -> tuple[list[str], bool]:
    """Run the independent oracle over the audit lines written after start_offset (this run only)."""
    if not audit_log.exists():
        return [f"FAIL: no audit log at {audit_log}; the oracle could not check this run"], False
    with audit_log.open("rb") as fh:
        fh.seek(start_offset)
        new_lines = fh.read().decode("utf-8", errors="replace")
    summary = summarise_text(new_lines)
    lines = [f"audit oracle (scripts/verify_audit.py, {summary['lookups']} audit lines from this run only):"]
    lines += format_summary(summary)
    if summary["lookups"] == 0:
        lines.append("FAIL: the gateway wrote no audit lines during this run (check the data/audit bind mount)")
    ok = (
        summary["lookups"] > 0
        and summary["stale_serves"] == 0
        and summary["served_without_evidence"] == 0
        and summary["malformed_lines"] == 0
    )
    return lines, ok


def main() -> int:
    try:
        from dotenv import load_dotenv

        load_dotenv(ROOT / ".env")
    except ImportError:
        pass
    ap = argparse.ArgumentParser()
    ap.add_argument("--subject", default="Stripe")
    ap.add_argument("--audit-log", type=Path, default=ROOT / "data" / "audit" / "freshcache.jsonl")
    args = ap.parse_args()
    settings = replace(Settings.from_env(), mode="replay")
    slug = slugify(args.subject)
    rows: list[tuple[str, str, str, bool]] = []

    def step(label: str, mode: str, expect: tuple[str, ...]) -> None:
        res = generate_brief(args.subject, settings=settings, now=datetime.now(UTC), cache_mode=mode)  # type: ignore[arg-type]
        got = res.cache.decision if res.cache else "NO_CALL"
        claims = len(res.brief.claims) if res.brief else 0
        rows.append((label, got, f"{claims} claims, {res.calls} call(s)", got in expect))

    start_offset = args.audit_log.stat().st_size if args.audit_log.exists() else 0
    reset_fixture(slug)
    try:
        step("1 first request", "normal", ("MISS_COLD", "HIT", "REFUSE_SET_CHANGED"))  # HIT or REFUSE if run before
        step("2 same evidence", "normal", ("HIT",))
        inject_simulated_article(slug, mode="replay")
        step("3 newer article", "normal", ("REFUSE_NEWER_SOURCE",))
    finally:
        reset_fixture(slug)

    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    report = [
        f"e2e cache proof {stamp} subject={args.subject} model={settings.model} "
        f"gateway={settings.bifrost_url} commit={_commit()}"
    ]
    report += [f"{'PASS' if ok else 'FAIL'}  {label:<20} {got:<22} {detail}" for label, got, detail, ok in rows]
    report.append("")
    oracle_lines, oracle_ok = oracle_report(args.audit_log, start_offset)
    report += oracle_lines
    text = "\n".join(report)
    print(text)
    out = ROOT / "docs" / "evidence" / f"e2e-cache-{stamp}.txt"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(text + "\n", encoding="utf-8")
    print(f"\nSaved -> {out.relative_to(ROOT)}")
    return 0 if oracle_ok and all(ok for *_, ok in rows) else 1


if __name__ == "__main__":
    raise SystemExit(main())
