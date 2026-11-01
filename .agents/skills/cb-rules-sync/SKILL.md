---
name: cb-rules-sync
description: Manual command. Run only when the user types /cb-rules-sync. Turns the official hackathon rules into a numbered constraint checklist, aligns the scorecard and plan to them, and flags conflicts with project decisions.
---

# /cb-rules-sync

1. Open `docs/HACKATHON_RULES.md`. If it still contains "PASTE OFFICIAL RULES HERE", ask the user to paste the organisers' rules, judging criteria, and schedule verbatim (and any links or emails from the organisers), then stop.
2. Read the official text. Extract every constraint into the checklist table at the bottom of the file, one row each, with an ID (R1, R2, ...), the exact words from the official text in the "Source line" column, which part of the system it affects, and a status (met, planned, conflict, unclear). Cover at least: event dates and the time window, submission deadline and format, team rules, mandatory technologies or sponsors (Bifrost, Tavily, languages), forbidden technologies or data, allowed use of AI tools, judging criteria and weights, demo or pitch format and length, licensing and IP, credits or API quotas provided.
3. Replace the assumed axes in `docs/SCORECARD.md` with the official criteria and weights. If none were published, say so and keep the assumed axes, marked as assumed.
4. Check the checklist against `docs/DECISIONS.md`. For each conflict (for example a mandated agent framework versus ADR-003, a language requirement versus the Go plugin), write the conflict and a proposed resolution; do not change an ADR without the user's go-ahead.
5. Put unanswerable items under "Facts to confirm with organisers" with a concrete question each.
6. Set `deadline_iso` in `docs/plan.json` and adjust task estimates to the real window. Run `make pert HOURS=<hours left>` and report the slack. If slack is negative, propose cuts from the cut list in the `hackathon-triage` skill.
7. Run `python scripts/check_invariants.py`; the `rules-filled` warning must be gone.

Output: the checklist, conflicts, questions, and the PERT summary. Stop for confirmation. Next: `/cb-recon`.
