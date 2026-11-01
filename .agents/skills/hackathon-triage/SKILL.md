---
name: hackathon-triage
description: Time-boxing, scope cutting, risk-first ordering and audit loops for a short hackathon. Use when planning, when behind schedule, when deciding what to cut, or when choosing what to do in the last hours.
---

# Hackathon triage

Lessons carried from earlier builds, applied to this project.

1. **Lock the scoring rubric first.** Everything is traced to it (`docs/HACKATHON_RULES.md`, `docs/SCORECARD.md`). A feature that moves no axis is cut.
2. **Walking skeleton before polish.** Within the first third of the time, one thin path must run end to end: replay sources -> prompt -> gateway -> plugin MISS, HIT, REFUSE -> banner. Polish only starts after that exists.
3. **Risk first.** The riskiest step is getting a Go `.so` to load in the gateway. The hello-world load proof (`/cb-recon`) comes before any real plugin logic. A time-box with a pre-agreed fallback (ADR-002) beats open-ended debugging.
4. **The model proposes, code decides.** Deterministic code owns dates, hashes, validation, cost and cache decisions. This is what makes the system testable, and tests are a scored axis.
5. **Tests are the evidence.** Keep a visible test count and `make check` green. A property test for the headline invariant is worth more than ten shallow tests.
6. **Audit loops, not one big finish.** Run `/cb-audit` at roughly 50% and 80% of the time. Fix the lowest axis first each round. Earlier submissions gained the most from the second round.
7. **Replan on a clock.** At each phase boundary run `make pert HOURS=<left>`. If slack is negative, cut from the list below, in order, and log it in `docs/DECISIONS.md`.

## Cut list (first to last)
1. Optional cache snapshot to disk
2. Ambiguity UX for names shared by a person and a company
3. Print layout polish beyond "fits one page"
4. Cost chart (keep the numbers)
5. Query 4 of the retrieval plan (partnership, funding)
6. Live-mode demo act (keep replay)

## Never cut
The invariants in AGENTS.md; `internal/decide` tests; the audit oracle; replay mode; the README with a threat model; the scorecard.

## Last 20% of the time
Feature freeze. Only fixes from the audit. Rehearse the demo three times. Build the submission pack. Tag the commit that was demoed and do not touch it afterwards without re-running `scripts/e2e_cache.py`.
