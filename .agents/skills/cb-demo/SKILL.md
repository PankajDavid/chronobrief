---
name: cb-demo
description: Manual command. Run only when the user types /cb-demo. Prepares fixtures and the simulated-article injector, rehearses the scripted demo three times, and records a clean fallback run.
---

# /cb-demo

Preconditions: `/cb-wire-gateway` passed. Skill: `demo-and-metrics`.

1. **Fixtures.** Choose two demo subjects with the user (one company with steady coverage, one person). Run each once in live mode, save the merged raw results to `fixtures/tavily/<slug>.json` and a pristine copy `<slug>.base.json`. Check each has at least 6 dated sources inside the window; if not, pick another subject.
2. **Injector** `scripts/inject_article.py` and its reset, per the skill: replay-only, `[SIMULATED]` title, reserved `example.com` URL, current UTC `published_date`, `"simulated": true`. Tests: refuses outside replay mode; injected article makes the fingerprint's newest later than the base; reset restores the base file byte for byte; live mode ignores the flag.
3. **Script** `docs/DEMO_SCRIPT.md`: the acts, what to click, what to say, expected banner text and numbers, and the fallback for each act.
4. **Rehearsal ×3.** Reset to pristine state (`docker compose down -v`, clear `data/audit`, reset fixtures), run the acts with a timer. After each run write the time per act and anything that hesitated. Between runs fix only what hesitated.
5. **Numbers.** After the third run execute `python scripts/verify_audit.py data/audit/freshcache.jsonl` and copy its output into `docs/DEMO_SCRIPT.md` as the expected end state: hit rate, tokens and cost saved, stale serves 0, stale answers prevented 1.
6. **Recording.** Record a 90-second clean run (browser agent recording or screen capture) to `docs/evidence/demo-run.mp4` or the recording the tool produces; note its location.
7. `make check` and `make e2e`.

Done when: three rehearsals fit the time target, the end-state numbers are recorded, a fallback recording exists.
