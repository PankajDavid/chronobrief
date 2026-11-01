---
trigger: model_decision
description: Apply when writing tests, claiming a task is done, or preparing evidence of correctness
---

# Testing and evidence

- Test pyramid for this repo: many pure unit tests (fingerprint, decide, validator), a few component tests with fakes (Tavily replay, fake gateway), one end-to-end script against the real gateway container.
- The headline invariant gets a property test on each side: Go `TestNoStaleServeInvariant` and a Python test that feeds fingerprints through the audit oracle. A change that touches freshness without touching these tests is incomplete.
- Fix the clock. Tests pass `now`; none sleep.
- Cover failure paths on purpose: Tavily timeout, Tavily returns zero results, results with no dates, model returns invalid JSON, gateway down, header missing.
- Evidence a task is done: the exact command and its final lines, pasted in your message. For UI work, a browser-agent screenshot or recording saved under `docs/evidence/`.
- Coverage is a signal, not the goal. Report it (`pytest --cov=chronobrief`) but do not write tests that assert nothing to raise it.
- Before saying a milestone is complete run `make check`. If a check is slow or flaky, say so and fix it; do not skip it.
