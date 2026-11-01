---
name: cb-audit
description: Manual command. Run only when the user types /cb-audit. Scores the project against the rubric with evidence, runs strict checks, lists the top gaps, and fixes the lowest axis first.
---

# /cb-audit

Run at about 50% and 80% of the time, and once before submission. If the user's plan includes it, suggest switching the model selector to Claude Sonnet 5.5 (Thinking) for this command, then back to Gemini 3.8 Flash for the fixes.

1. **Strict checks.** `python scripts/check_invariants.py --strict`, `make check`, `pytest --cov=chronobrief`, `mypy chronobrief`, `make e2e`, `python scripts/verify_audit.py data/audit/freshcache.jsonl`. Quote the last lines of each.
2. **Invariant review.** For each of the six invariants in `AGENTS.md`, point to the code and the test that enforce it. Mark any without both as a gap.
3. **Rubric.** Open `docs/SCORECARD.md`. For every axis give a 0 to 100 self-score and the evidence (command output, file, screenshot path). No evidence means a low score, not a guess. Include: the Go and Python test counts; coverage; mypy and ruff status; the accessibility checklist result; whether each official requirement R-ID in `docs/HACKATHON_RULES.md` is met, with a pointer.
4. **Adversarial pass.** Try to break it and report what happened: empty input; a 5,000-character target; a subject with no coverage; Tavily key removed; gateway stopped mid-run; two quick clicks; a cached brief that fails validation; a source whose snippet contains an instruction; the plugin given forged headers claiming an old newest date (document that this is the known trust limit, not a bug).
5. **Top three gaps**, ordered by score impact per hour, with an estimate from `docs/plan.json`.
6. **Fix the lowest axis first**, one gap at a time, each with a commit and a re-run of the relevant check. Update the scorecard after each fix.
7. Run `make pert HOURS=<left>` and say whether the remaining plan still fits.

Done when: the scorecard is current, every score has evidence, and the top gap from this round is closed or consciously deferred in `docs/DECISIONS.md`.
