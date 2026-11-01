#!/usr/bin/env python3
"""Environment check. Prints what is missing and the exact fix. Exit code 1 if anything blocking is missing."""
from __future__ import annotations

import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
blocking: list[str] = []
advice: list[str] = []


def run(cmd: list[str]) -> str:
    try:
        return subprocess.run(cmd, capture_output=True, text=True, timeout=20).stdout.strip()
    except (OSError, subprocess.TimeoutExpired):
        return ""


def load_env() -> dict[str, str]:
    env = dict(os.environ)
    f = ROOT / ".env"
    if f.exists():
        for line in f.read_text(encoding="utf-8").splitlines():
            if "=" in line and not line.lstrip().startswith("#"):
                k, v = line.split("=", 1)
                env.setdefault(k.strip(), v.strip().strip('"'))
    return env


def main() -> int:
    print(f"python  {sys.version.split()[0]}  ({platform.system()} {platform.machine()})")
    if sys.version_info < (3, 11):  # noqa: UP036
        blocking.append("Python 3.11+ required. Install it or use uv: https://docs.astral.sh/uv/")

    if platform.system() == "Windows":
        blocking.append(
            "Go plugins (.so) do not build or load on Windows. Work inside WSL2 (Ubuntu) or a Linux container. "
            "Open the project from the WSL filesystem, not /mnt/c, or Docker bind mounts will be slow and flaky."
        )

    docker = shutil.which("docker")
    print(f"docker  {run(['docker', '--version']) if docker else 'NOT FOUND'}")
    if not docker:
        blocking.append("Docker is required: the gateway and the plugin must be built with the same Go toolchain and core version, and Docker is the only reliable way to guarantee that.")
    elif not run(["docker", "info", "--format", "{{.ServerVersion}}"]):
        blocking.append("Docker is installed but the daemon is not reachable. Start Docker Desktop or the docker service.")

    go = shutil.which("go")
    print(f"go      {run(['go', 'version']) if go else 'not found (fine: the plugin builds inside Docker; only needed for local unit tests)'}")
    if not go:
        advice.append("Install Go locally if you want to run `go test ./...` for internal/decide without Docker.")

    env = load_env()
    for key, why in (("TAVILY_API_KEY", "Tavily search"), ("GEMINI_API_KEY", "the model behind Bifrost")):
        ok = bool(env.get(key)) and "replace-me" not in env.get(key, "")
        print(f"{key:<16}{'set' if ok else 'MISSING'}")
        if not ok:
            blocking.append(f"{key} is not set. Copy .env.example to .env and fill in the key for {why}.")

    if not (ROOT / ".agents").exists():
        blocking.append("No .agents/ directory: re-run chrono_pack.py into this folder (it skips files that already exist).")

    for a in advice:
        print("note   ", a)
    for b in blocking:
        print("BLOCK  ", b)
    print("\nready" if not blocking else f"\n{len(blocking)} blocking issue(s)")
    return 1 if blocking else 0


if __name__ == "__main__":
    sys.exit(main())
