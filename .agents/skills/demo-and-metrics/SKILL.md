---
name: demo-and-metrics
description: The scripted demo (hit, then stale refusal), the simulated-article injector, the metrics shown to judges, and fallbacks if something fails live. Use when preparing, rehearsing, or recording the demo or when building the metrics panel.
---

# Demo and metrics

## Acts (target 3 minutes)
0. Optional, 20 s, live mode: one real search to show the pipeline is real. Skip if the network looks unreliable.
1. Cold MISS, 30 s: a replay subject. Show the brief, the source list with dates, the banner `MISS`, latency, tokens.
2. Same subject again, 20 s: banner `HIT`, latency drops, tokens saved appear in the metrics panel.
3. A newer article lands, 60 s: click "Simulate a newer article". Say aloud that it stands in for a real publication. Ask again. Banner `REFUSED STALE` shows both dates; the new brief contains a claim citing the SIMULATED source. Say: a plain cache would have served yesterday's brief here.
4. Metrics, 30 s: hit rate, tokens and cost saved, stale serves 0 (from `verify_audit.py`), stale answers prevented 1. Open the decision log: each row shows decision plus the two dates that caused it.
5. Limits, 20 s: header trust model, in-memory single instance, exact-key matching. Say them yourself before anyone asks.

## Injector (`scripts/inject_article.py`)
Reads `fixtures/tavily/<slug>.json`, appends one article with `"simulated": true`, a title that begins with `[SIMULATED]`, a unique URL on the reserved domain `example.com`, and `published_date` set to the current UTC time. Refuses to run unless `CHRONO_TAVILY_MODE=replay`. A matching "reset" restores the file from `fixtures/tavily/<slug>.base.json`. The retrieval layer carries `simulated` through to the UI badge. Simulated sources must never appear in live mode: the loader ignores the flag outside replay mode and the invariant tests cover this.

## Metric definitions (all computed by `scripts/verify_audit.py`)
- Hit rate = HIT / all lookups.
- Tokens saved = sum of the original computation's prompt and completion tokens for each HIT. Cost saved likewise, from the cost recorded when the entry was stored.
- Stale serves = HITs where the request was newer than the entry. Must print 0. The oracle recomputes this from dates; it does not trust the plugin's label.
- Stale answers prevented = REFUSE_NEWER_SOURCE count: the brief a TTL or plain semantic cache would have served.
- Latency p50 for HIT vs MISS, from the audit log's `latency_us` or the app's own timers, labelled as which.

## End-to-end script (`scripts/e2e_cache.py`, built in `/cb-wire-gateway`)
Runs the acts against the real gateway in replay mode with a fixed subject: MISS, HIT, inject, REFUSE, then asserts decisions in order and that `verify_audit.py` exits 0. This script is also the pre-demo smoke test.

## Fallbacks
Gateway down: show the recorded run and the audit log. Network down: replay mode. Model slow: the HIT path is fast by design, lead with it. Plugin refuses to load on demo day: ADR-002 fallback build, or the recording. Always keep a 90-second screen recording of a clean run in `docs/evidence/`.

## Rehearsal
Three full run-throughs against a timer. After each, note what hesitated or confused, fix it, and rerun `scripts/e2e_cache.py`.
