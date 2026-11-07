# ChronoBrief Live Demo Script & Stale-Cache Court Rehearsal

> **Target Time**: 90 seconds (Lightning Demo) / 3 minutes (Full Technical Presentation)  
> **Tracks Covered**: Bifrost Priority Track 04 (Swarit) + Tavily Track 02 (Pankaj)  
> **Canonical Pitch**: *"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."*

---

## 0. Before Every Demo Run: Reset the Cache

The cache deliberately survives restarts (in memory, and in `data/audit/freshcache-snapshot.json` via the `Cleanup()` flush). A second demo run therefore starts warm: Step 1 of `sec_8k_guard_demo.py` returns `REFUSE_SET_CHANGED` instead of `MISS_COLD`, because the stored entry already holds the newer 8-K evidence from the previous run. Reset first:

```bash
docker compose stop gateway && rm -f data/audit/freshcache-snapshot.json && docker compose up -d gateway
python gateway/examples/sec_8k_guard_demo.py   # Step 1 must print MISS_COLD
```

The demo script now prints a `[WARN]` line if Step 1 is not `MISS_COLD`.

---

## 1. Demo Structure & Speaking Script

```mermaid
sequenceDiagram
    autonumber
    actor Judge as Judges / Audience
    participant UI as Streamlit / Client
    participant GW as Bifrost Gateway (freshcache.so)
    participant Court as Stale-Cache Court (Shadow TTL)
    participant Model as Gemini 3.1 Flash Lite

    Note over Judge,Model: Act 1: The Thesis & TTL Frontier (20s)
    Judge->>GW: "Why not standard 24h TTL or 5m TTL?"
    GW-->>Judge: TTL Frontier Proof (5m=222 false misses; 24h=18.4% stale serves; freshcache=0% stale)

    Note over Judge,Model: Act 2: Cold Miss -> Warm Hit (25s)
    Judge->>UI: Request Stripe Brief (Run 1)
    UI->>GW: POST /v1/chat/completions (x-bf-freshness-*)
    GW->>Model: MISS_COLD -> Model Generates
    Model-->>UI: 10 Claims, Brief rendered
    Judge->>UI: Request Stripe Brief again (Run 2)
    UI->>GW: POST /v1/chat/completions (Unchanged Fingerprint)
    GW-->>UI: HIT (~1 ms live hook overhead, median 0.95 ms; 5,580 tokens saved)

    Note over Judge,Model: Act 3: Simulated Breaking News Injection (30s)
    Judge->>UI: Inject Breaking Acquisition [SIMULATED]
    UI->>GW: POST /v1/chat/completions (Newer Published Date)
    GW->>Court: Compare with 24h TTL
    Court-->>GW: shadow_ttl_24h would have done: HIT_STALE!
    GW->>Model: REFUSE_NEWER_SOURCE -> Force Recompute
    Model-->>UI: New Brief with cited simulated article

    Note over Judge,Model: Act 4: Operator Readiness & SEC 8-K Guard (15s)
    Judge->>GW: POST x-chrono-admin: metrics / Container Restart
    GW-->>Judge: Snapshot Restored in 6s | SEC 8-K Native Key Guarded
```

### Act 1: The Thesis & The Pareto Frontier (20 seconds — Swarit)
- **Visual**: Show the TTL Frontier simulation table (`docs/evidence/ttl-frontier-report.md`) on screen.
- **Spoken**:  
  *"A time-based cache expires answers on a clock—5 minutes, 1 hour, or 24 hours. But business truth does not expire on a clock. When an executive prepares for a meeting, in our 250-lookup synthetic simulation a 5-minute TTL cache recomputed 88.8% of lookups (222 of 250) even though nothing had changed. A 24-hour TTL cache saves tokens, but when breaking news arrives, it keeps serving the outdated answer for up to 24 hours.  
  ChronoBrief caches by evidence: generate once, reuse when the retrieved evidence is unchanged, regenerate when the retrieved facts move."*

### Act 2: Cold Miss -> ~1 ms Warm Hit (25 seconds — Pankaj)
- **Visual**: Streamlit UI (`http://localhost:8501`) or CLI e2e runner querying `"Stripe"`.
- **Spoken**:  
  *"Here is our pre-meeting brief on Stripe. Tavily searches the last 30 days and extracts verified published dates. On Run 1, the cache is cold: Bifrost calls Gemini upstream, validates 10 verifiable claims, and stores the response in our byte-bounded Go LRU.  
  Two minutes later, an analyst preps for the same meeting. They hit Generate again. Watch the banner: instant cache **HIT**. In about 1 millisecond of live hook overhead (HIT median 0.95 ms over 25 runs; the in-memory lookup alone is 11 µs in a Go microbenchmark), the gateway returns the verified brief, saving 5,580 LLM tokens and 100% of the model generation cost."*

### Act 3: Breaking News Injection & "The Stale-Cache Court" (30 seconds — Swarit & Pankaj)
- **Visual**: Open Demo Controls in Streamlit, click **Simulate Breaking News**, click **Generate Brief**.
- **Spoken**:  
  *"Now, breaking news strikes: Stripe announces a major acquisition. The incoming newest timestamp moves forward.  
  Look at what Bifrost's Stale-Cache Court reports: a standard 24-hour TTL cache would have reported **`HIT_STALE`**, serving yesterday's pre-acquisition brief to the executive.  
  Instead, `freshcache.so` immediately refuses with **`REFUSE_NEWER_SOURCE`**, invokes Gemini upstream, and synthesizes the updated brief with the breaking citation clearly highlighted."*

### Act 4: Operator Readiness & Multi-Domain Guard (15 seconds — Swarit)
- **Visual**: Terminal showing `curl /v1/chat/completions` with `x-chrono-admin: metrics` and `gateway/examples/sec_8k_guard_demo.py`.
- **Spoken**:  
  *"We built it to production habits: we run non-root in Docker, isolate tenants fail-closed via virtual keys with zero client spoofing, persist cache snapshots atomically to disk across container restarts, and provide native `x-bf-cache-key` Guard Mode (header stamping/stripping verified in unit tests; end-to-end interaction with Bifrost's `semanticcache` is designed, not yet verified)."*

---

## 2. Rehearsal Logs & Execution Timing

Three consecutive rehearsal runs were conducted end-to-end against the live Bifrost container (`chronobrief-gateway`) using `scripts/e2e_cache.py` on 2 Nov 2026, before the retrieval changes in PR #3. They are kept as a dated record and were not re-run; current figures are in section 3.

| Rehearsal Run | Timestamp (UTC) | Act 1 (Frontier Table) | Act 2 (MISS -> HIT) | Act 3 (Injection -> REFUSE) | Total Run Duration | Result |
|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **Run 1** | `2026-11-02T01:49:39Z` | Verified | 4.8s | 6.0s | **10.81s** | **ALL PASS** |
| **Run 2** | `2026-11-02T01:49:50Z` | Verified | 4.6s | 6.0s | **10.61s** | **ALL PASS** |
| **Run 3** | `2026-11-02T01:50:01Z` | Verified | 4.7s | 6.1s | **10.83s** | **ALL PASS** |

---

## 3. Independent Oracle Audit Ledger (`verify_audit.py --json`)

The independent audit log validator (`scripts/verify_audit.py`) re-checks every audit record without trusting the plugin. Latest saved live run ([`e2e-cache-20261107T170346Z.txt`](evidence/e2e-cache-20261107T170346Z.txt), commit `1572238`), MISS → HIT → REFUSE with the oracle over that run's own audit lines:

```
PASS  1 first request      MISS_COLD              10 claims, 1 call(s)
PASS  2 same evidence      HIT                    10 claims, 1 call(s)
PASS  3 newer article      REFUSE_NEWER_SOURCE    10 claims, 1 call(s)

audit oracle (scripts/verify_audit.py, 3 audit lines from this run only):
lookups 3  hits 1  hit rate 33.3%
stale serves 0  (stale answers prevented: 1)
saved (LLM only): 5580 tokens  $0.0022
```

Every `python scripts/e2e_cache.py` run saves this report, stamped with the git commit, to a new `docs/evidence/e2e-cache-<timestamp>.txt`. Quote numbers for the demo from the latest such file.

### Key Invariant Verified
- **Stale Serves**: **`0`** in every saved oracle report.
- **Stale Answers Prevented**: **`1`** in the latest saved run (the `REFUSE_NEWER_SOURCE` step, counted independently by the oracle).
- **Malformed Lines**: **`0`** (Clean atomic append log).
