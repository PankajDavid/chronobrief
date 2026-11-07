# ChronoBrief Evidence Index

This directory contains empirical, non-simulated proof artifacts demonstrating the correctness, performance, security, and stability of ChronoBrief and the Bifrost `freshcache` plugin.

---

## 1. Gateway & Plugin Proofs (Bifrost Track 04)

| Artifact | Type | Description |
|---|---|---|
| [`ttl-frontier-report.md`](ttl-frontier-report.md) | Simulation Report | Deterministic 250-query 48h simulation (synthetic queries/news arrivals) proving truth-based caching beats time-based TTL (`93.2%` hit vs `6.4%` on 5m short TTL; `0.0%` stale vs `18.4%` on 24h TTL). |
| [`gateway-benchmarks.txt`](gateway-benchmarks.txt) | Microbenchmarks & Fuzzing | Go 1.27.3 microbenchmarks (`20.17 ns/op` decision engine, `11.36 µs/op` LRU hit, 122k fuzz iters) (in-memory, no disk I/O). |
| [`live-overhead-25runs.txt`](live-overhead-25runs.txt) | Live Overhead Measurement | Raw 25-run live measurement against Docker gateway logging per-run `MISS_COLD` and `HIT` hook overheads verbatim. |
| [`gateway-negative-tests.txt`](gateway-negative-tests.txt) | Regression Suite | Live gateway output testing 9 negative scenarios: missing fingerprint, future timestamps, streaming bypass, refresh bypass, ADR-006 tenant isolation, and ADR-005 headline changes. |
| [`plugin-load.txt`](plugin-load.txt) | Docker Build Proof | Docker build log proving `freshcache.so` compiles with `CGO_ENABLED=1` and dynamically loads all 7 Bifrost plugin symbols without symbol mismatch. |
| [`hello-world-load.txt`](hello-world-load.txt) | Upstream Baseline | Upstream Bifrost hello-world plugin build and symbol verification baseline from recon phase. |

---

## 2. End-to-End Cache Lifecycle Proofs

| Artifact | Type | Description |
|---|---|---|
| [`e2e-cache-20261107T170601Z.txt`](e2e-cache-20261107T170601Z.txt) | Live E2E Proof (latest, recorded take) | Commit `20107f1`, the run shown in the recorded demo (branch `demo/recording-20107f1`): `MISS_COLD` -> `HIT` -> `REFUSE_NEWER_SOURCE`, 9/9/10 claims, oracle: 0 stale serves, 5496 tokens / $0.0016 saved. |
| [`e2e-cache-20261107T170346Z.txt`](e2e-cache-20261107T170346Z.txt) | Live E2E Proof | Commit `1572238`, after PR #3 (news publishers, relevance filter): `MISS_COLD` -> `HIT` -> `REFUSE_NEWER_SOURCE`, 10 claims each, oracle: 0 stale serves, 5580 tokens / $0.0022 saved. |
| [`e2e-cache-20261102T013824Z.txt`](e2e-cache-20261102T013824Z.txt) | Live E2E Proof | Live proof cycle against `http://localhost:8080`: `MISS_COLD` -> `HIT` -> Simulated Breaking News -> `REFUSE_NEWER_SOURCE`. |
| [`e2e-cache-20261102T014939Z.txt`](e2e-cache-20261102T014939Z.txt) | Rehearsal Run 1 | Demo rehearsal pass 1 confirming exact same evidence yields `HIT` and injected news triggers instant refusal. |
| [`e2e-cache-20261102T014950Z.txt`](e2e-cache-20261102T014950Z.txt) | Rehearsal Run 2 | Demo rehearsal pass 2 measuring timing (10.61s) and token savings. |
| [`e2e-cache-20261102T015001Z.txt`](e2e-cache-20261102T015001Z.txt) | Rehearsal Run 3 | Demo rehearsal pass 3 validating 0 stale serves and consistent claim citation verification. |

---

## 3. Retrieval & UI Verification (Tavily Track 02)

| Artifact | Type | Description |
|---|---|---|
| [`tavily-probe-2026-11-01.json`](tavily-probe-2026-11-01.json) | Retrieval Probe | Raw capture from Tavily 30-day search API demonstrating real published dates, source URLs, and title shapes. |
| [`t11-dashboard-proof.txt`](t11-dashboard-proof.txt) | UI Verification | Test run confirming all 12 Streamlit `AppTest` cases and strict accessibility invariants pass without warnings. |
| [`ui-walkthrough/`](ui-walkthrough) | Screenshot Gallery | Visual walkthrough images of Streamlit UI: brief view, executive summary, claim cards, cache status badges, and cache scoreboard. |
