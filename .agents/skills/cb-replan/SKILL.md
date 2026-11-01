---
name: cb-replan
description: Manual command. Run only when the user types /cb-replan. Recomputes the critical path and slack from docs/plan.json, updates task status, and applies the cut list when time is short.
---

# /cb-replan

1. Read `docs/plan.json`. Set each task's `status` from reality: done only if its "done when" evidence exists in the repo or the conversation.
2. Get the hours left: ask the user, or compute from `deadline_iso` and `date -u`. If both are missing, ask for one number.
3. Run `make pert HOURS=<left>` and report: remaining expected hours, the critical path, the 90% range, slack and the verdict.
4. If a task is taking longer than its pessimistic estimate, update its `o`, `m`, `p` with the user's input and say why.
5. If the verdict is TIGHT or OVER, apply the cut list from the `hackathon-triage` skill in order, one item at a time, re-running `make pert` after each cut until the verdict is OK, and write each cut and its reason into `docs/DECISIONS.md`. Never cut anything on the "never cut" list; if that is not enough, tell the user plainly and propose a smaller demo.
6. Summarise in six lines or fewer: hours left, remaining critical path, verdict, cuts made, next task, biggest current risk.
