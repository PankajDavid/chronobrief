---
trigger: model_decision
description: Apply when preparing the demo, the README, the recording, metrics shown to judges, or the final submission
---

# Demo and submission

Skills: `.agents/skills/demo-and-metrics/SKILL.md`, `.agents/skills/hackathon-triage/SKILL.md`.

- The money shot is: ask a company twice and see HIT on the second; inject a newer article; ask again and see the cache refuse and recompute, with both dates shown. Script it, time it, rehearse it three times.
- The demo must not depend on live network luck. Replay mode reads fixed Tavily fixtures; live mode is shown once, at the start, as proof the pipeline is real.
- Every number on screen comes from `scripts/verify_audit.py`, not from the app's own counters.
- Injected articles are labelled SIMULATED everywhere. Say out loud, in the pitch, that the injection stands in for a real publication so there is no ambiguity.
- Submission artifacts: public repo or archive as the rules require, README (problem, architecture diagram, how to run in under five commands, threat model, limits), demo recording or script, and the filled `docs/SCORECARD.md`.
- Freeze features at the time set in `docs/plan.json` for T14. After the freeze only fixes from the audit are allowed.
