---
trigger: always_on
description: Hackathon rules and scoring gate every plan and every shipped feature
---

# Hackathon constraints

The official rules live in @docs/HACKATHON_RULES.md and the scoring sheet in @docs/SCORECARD.md.

1. Before proposing a plan, read both. If HACKATHON_RULES.md still contains "PASTE OFFICIAL RULES HERE", do nothing else until the user pastes the rules and you have run `/cb-rules-sync`.
2. Every feature you build must trace to a constraint ID (R1, R2...) or a scoring axis. If it traces to neither, it is out of scope: say so and skip it.
3. Anything the rules forbid or require (technology, data, team size, time window, submission format) overrides this repository's preferences. Report the conflict; do not silently pick a side.
4. Never state a rule you have not read in HACKATHON_RULES.md. If you are unsure whether the rules allow something, list it under "Facts to confirm with organisers" instead of assuming.
5. The submission is judged on what runs. Prefer a smaller working slice, proven by command output, over a larger one described in prose.
