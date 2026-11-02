---
trigger: always_on
description: Never take shortcuts with numbers, statistics, ignored errors, or unverified outputs
---

# No shortcuts with numbers or results

1. Never type a measured or computed number into code, docs, charts or reports. Every number must be produced by code that reads the real result or a saved evidence file, and the output must be pasted as proof.
2. Never calculate statistics (median, mean, p90, percentages, totals) in your head. Run code to compute them and show the code and its output.
3. When asked to make something "show" or "match" a value, fix the method that produces it. Never edit the output to match the target.
4. Every number in docs must point to an evidence file in docs/evidence/ that contains the raw data. If the raw data no longer exists, say so and re-measure. Never carry forward a number from an earlier session.
5. Never ignore errors (no _ = on file writes, no silent except). If a write or check fails, the task fails.
6. Never report "done", "passed" or "verified" while a command is still running. Wait for it to finish, then paste the real output.
7. If a test only passes because of how the code was written (for example, a value that is never actually checked), say so. That is not a pass.
8. If a shortcut is the only way to meet a request, stop and say so instead of taking it.
