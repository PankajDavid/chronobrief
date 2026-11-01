---
name: cb-kickoff
description: Manual command. Run only when the user types /cb-kickoff. Day-zero setup that verifies the machine, installs dependencies, proves the seeded tests pass, and produces the implementation plan for approval.
---

# /cb-kickoff

Goal: leave the project in a known-good state with an approved plan. Do not write product code in this command.

1. **Model check.** Tell the user, in one line, to confirm the model selector shows Gemini 3.8 Flash at High thinking (Antigravity offers Low, Medium and High), and that the built-in `/effort` command is the quick way to change it. Continue without waiting.
2. **Environment.** Run `make doctor`. If it prints any BLOCK line, show the lines and stop. On Windows the fix is WSL2; do not try to work around it.
3. **Dependencies.** `python -m venv .venv`, activate it, `pip install -e ".[dev]"`, then `pip freeze > requirements.lock`. If an install fails, report the error and stop.
4. **Baseline.** Run `make check`. Expected: invariants shows only the `rules-filled` warning; lint, Python tests and Go decide tests pass (Go tests need a local Go; if absent, say so and rely on the Docker build later). If anything else fails, fix it or stop and report; do not continue on a red baseline.
5. **Repository.** If there is no git repo, `git init` and commit the pack as "pack: initial scaffold". Create empty directories `app/`, `fixtures/tavily/`, `data/audit/`, `docs/evidence/` with `.gitkeep`.
6. **Read.** Read `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `docs/plan.json`, and the descriptions of every skill in `.agents/skills/`.
7. **Plan artifact.** Produce an implementation plan covering: the walking skeleton and its milestones, task order from `docs/plan.json` with the critical path from `make pert`, which skill each task uses, the three biggest risks (plugin load, Tavily date quality, demo reliability) and their fallbacks from `docs/DECISIONS.md`, and what you need from the user (official rules, API keys, time window).
8. Stop and wait for approval. Next command: `/cb-rules-sync`.

Done when: `make check` output is quoted, the plan artifact exists, the user has approved or amended it.
