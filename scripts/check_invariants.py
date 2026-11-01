#!/usr/bin/env python3
"""Static guards for the invariants that must never regress.

  python scripts/check_invariants.py            # warnings allowed
  python scripts/check_invariants.py --strict   # warnings fail too (used by /cb-audit and /cb-submit)

Checks
  1. plugin-purity   the freshcache plugin imports nothing that can reach the network or spawn processes
  2. no-st-cache     no Streamlit caching wrapped around brief generation (it would fake cache hits in the demo)
  3. no-secrets      no API keys in tracked files
  4. pack-lint       .agents rules, skills and workflows respect Antigravity's documented limits
  5. rules-filled    docs/HACKATHON_RULES.md was actually filled in
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SKIP_DIRS = {".git", ".venv", "venv", "node_modules", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", "build", "dist"}
errors: list[str] = []
warnings: list[str] = []


def files(suffixes: tuple[str, ...], under: str = "") -> list[Path]:
    base = ROOT / under
    if not base.exists():
        return []
    return [p for p in base.rglob("*") if p.is_file() and p.suffix in suffixes and not (set(p.relative_to(ROOT).parts) & SKIP_DIRS)]


def check_plugin_purity() -> None:
    banned = {"net", "net/http", "net/rpc", "os/exec", "crypto/tls", "plugin"}
    import_re = re.compile(r'^\s*(?:import\s+)?(?:[\w.]+\s+)?"([^"]+)"\s*$')
    for p in files((".go",), "gateway/plugins/freshcache"):
        if p.name.endswith("_test.go"):
            continue
        in_block = False
        for n, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
            s = line.strip()
            if s.startswith("import ("):
                in_block = True
                continue
            if in_block and s == ")":
                in_block = False
                continue
            if in_block or s.startswith("import "):
                m = import_re.match(line)
                if m and m.group(1) in banned:
                    errors.append(f"plugin-purity: {p.relative_to(ROOT)}:{n} imports {m.group(1)!r}; the plugin makes no network calls")


def check_no_st_cache() -> None:
    pat = re.compile(r"st\.cache_data|st\.cache_resource|@st\.cache\b|functools\.lru_cache|@cache\b")
    for p in files((".py",), "chronobrief") + files((".py",), "app"):
        for n, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
            if pat.search(line) and "# cache-ok:" not in line:
                errors.append(f"no-st-cache: {p.relative_to(ROOT)}:{n} caches in the request path; if it is not brief generation, add '# cache-ok: <reason>'")


def check_no_secrets() -> None:
    pats = [re.compile(r"tvly-[A-Za-z0-9_-]{16,}"), re.compile(r"AIza[0-9A-Za-z_-]{30,}"), re.compile(r"\bsk-[A-Za-z0-9]{20,}")]
    for p in files((".py", ".go", ".md", ".json", ".yml", ".yaml", ".toml", ".txt", ".sh", ".env.example", "")):
        if p.name == ".env" or p.stat().st_size > 2_000_000:
            continue
        try:
            text = p.read_text(encoding="utf-8", errors="ignore")
        except (UnicodeDecodeError, OSError):
            continue
        for pat in pats:
            if pat.search(text):
                errors.append(f"no-secrets: {p.relative_to(ROOT)} contains something shaped like an API key")


def parse_frontmatter(text: str) -> dict[str, str] | None:
    if not text.startswith("---\n"):
        return None
    end = text.find("\n---", 4)
    if end == -1:
        return None
    out: dict[str, str] = {}
    for line in text[4:end].splitlines():
        if ":" in line:
            k, v = line.split(":", 1)
            out[k.strip()] = v.strip().strip('"')
    return out


def check_pack_lint() -> None:
    always_chars = 0
    for f in (ROOT / "AGENTS.md", ROOT / "GEMINI.md"):
        if f.exists():
            always_chars += len(f.read_text(encoding="utf-8"))
    for p in sorted((ROOT / ".agents" / "rules").glob("*.md")):
        text = p.read_text(encoding="utf-8")
        fm = parse_frontmatter(text)
        rel = p.relative_to(ROOT)
        if fm is None:
            errors.append(f"pack-lint: {rel} has no YAML frontmatter; Antigravity silently discards it")
            continue
        trig = fm.get("trigger")
        if trig not in {"always_on", "model_decision", "glob", "manual"}:
            errors.append(f"pack-lint: {rel} has invalid trigger {trig!r}")
        if trig == "model_decision" and not fm.get("description"):
            errors.append(f"pack-lint: {rel} is model_decision but has no description")
        if trig == "glob" and not (fm.get("globs") or fm.get("glob")):
            errors.append(f"pack-lint: {rel} is glob but has no globs")
        if len(text.encode()) > 24_000:
            errors.append(f"pack-lint: {rel} exceeds the 24,000 byte per-rule limit")
        if trig == "always_on":
            always_chars += len(text)
    if always_chars / 4 > 12_000:
        warnings.append(f"pack-lint: always-on context is about {always_chars // 4} tokens; the shared budget is 20,000. Demote a rule to model_decision.")
    for p in sorted((ROOT / ".agents" / "workflows").glob("*.md")):  # legacy; Antigravity retired workflows
        warnings.append(f"pack-lint: {p.relative_to(ROOT)} is a legacy workflow file; move it to a skill")
    for p in sorted((ROOT / ".agents" / "skills").glob("*/SKILL.md")):
        fm = parse_frontmatter(p.read_text(encoding="utf-8"))
        if not fm or not fm.get("description"):
            errors.append(f"pack-lint: {p.relative_to(ROOT)} needs frontmatter with a description")
            continue
        if fm.get("name") and fm["name"] != p.parent.name:
            errors.append(f"pack-lint: {p.relative_to(ROOT)} name {fm['name']!r} does not match its folder")
        if p.parent.name.startswith("cb-") and not fm["description"].startswith("Manual command."):
            errors.append(f"pack-lint: {p.relative_to(ROOT)} is a slash command; its description must start with 'Manual command.' so the agent does not run it on its own")


def check_rules_filled() -> None:
    f = ROOT / "docs" / "HACKATHON_RULES.md"
    if not f.exists() or "PASTE OFFICIAL RULES HERE" in f.read_text(encoding="utf-8"):
        warnings.append("rules-filled: docs/HACKATHON_RULES.md still holds the placeholder. Run /cb-rules-sync before planning anything.")


def main() -> int:
    strict = "--strict" in sys.argv
    for fn in (check_plugin_purity, check_no_st_cache, check_no_secrets, check_pack_lint, check_rules_filled):
        fn()
    for w in warnings:
        print("WARN ", w)
    for e in errors:
        print("FAIL ", e)
    if not errors and not warnings:
        print("invariants: all clear")
    return 1 if errors or (strict and warnings) else 0


if __name__ == "__main__":
    sys.exit(main())
