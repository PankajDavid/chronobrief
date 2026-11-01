---
trigger: always_on
description: How to work reliably as Gemini 3.8 Flash at High thinking
---

# Working discipline for this model

This workspace runs on Gemini 3.8 Flash at High thinking. It is fast and strong, and it fails in predictable ways. These rules exist to prevent those failures.

1. **No API from memory.** Bifrost, Tavily, Streamlit and Antigravity change often. Before using any signature, flag, config key or header, open the docs or the pinned source and copy what is there. If you cannot verify it, write `UNVERIFIED:` next to it in the code and in your summary.
2. **Smallest verifiable step.** Make one change, run the check that proves it, then move on. A step that cannot be checked by a command is too big; split it.
3. **Quote evidence, not confidence.** Report the command you ran and the last lines of its output. Phrases like "should work", "looks correct" and "I believe" are not results.
4. **Do not edit tests to make them pass.** If a test fails, the code or the test's expectation is wrong; decide which by reading the spec in `docs/ARCHITECTURE.md` or the skill, and say which.
5. **No silent scope growth.** If you notice a better idea, append it to `docs/DECISIONS.md` as a proposal and carry on with the task. Do not implement it.
6. **Stop conditions.** Stop and ask the user when: the rules file conflicts with the task; a change would touch an invariant in AGENTS.md; the same error survives three fix attempts; or an action costs money or touches credentials.
7. **Fallbacks are pre-agreed.** Each risky area has a fallback in `docs/DECISIONS.md`. When its trigger fires, take it without negotiating.
8. **Plain language in outputs.** No filler, no marketing phrases, no restating the question. State what changed, what you ran, what is left.
9. **Use the other model for review.** When the plan or a diff is large, tell the user to switch the model selector to Claude Sonnet 5.5 (Thinking) for `/cb-audit` and the spec review, if their plan includes it. Do the building on Flash.
