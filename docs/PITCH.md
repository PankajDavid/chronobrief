# ChronoBrief: 3-Minute Dual-Track Pitch Script

> **Hackathon Tracks**: Bifrost Priority Track 04 (Swarit) + Tavily Track 02 (Pankaj)  
> **Pitch Format**: 3 Minutes Total (90s Swarit / 90s Pankaj)  
> **Canonical Thesis**: *"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."*

---

## Part 1: Bifrost Priority Track 04 (Swarit — 90 Seconds)

### 0:00–0:25 — The Problem: Why Time-Based Gateway Caches Fail
*"Hello judges. Every open-source AI gateway today—including Bifrost, LiteLLM, and Portkey—caches LLM completions by time: 5-minute TTL, 1-hour TTL, or 24-hour TTL.  
But in business and finance, facts do not expire on a clock.  
We ran a deterministic 48-hour simulation (250 synthetic lookups, assumed news arrivals):
- A **5-minute TTL cache (short TTL)** has a dismal **6.4% hit rate**, causing **222 false misses**—throwing away money and token bandwidth even when not a single fact changed.
- A **24-hour TTL cache** achieves a 96% hit rate, but suffers an unacceptable **18.4% stale-serve rate**—serving yesterday's earnings and pre-acquisition rumors for hours after breaking news broke.  
Bifrost was missing a truth-aware caching primitive. That is why we built **`freshcache`**."*

### 0:25–0:55 — The Architecture: High-Performance Go Plugin
*"Freshcache is a compiled native Go plugin (`freshcache.so`) hooked into all 7 lifecycle stages of Bifrost:
1. It introduces the **Counterfactual TTL Shadow Ledger**: on every single request, we evaluate in parallel what a 5m, 1h, and 24h TTL cache would have done.
2. In our **Stale-Cache Court**, we prove live when a standard 24h cache would have served a stale answer.
3. Our decision logic is property-tested and fast: zero allocations, **20.17 nanoseconds per decision** (> 50 million ops/sec), and **11.36 microseconds** for in-memory LRU retrieval. In Docker, live end-to-end hook overhead over 25 runs is: HIT **~1 ms** (median 0.95 ms); MISS **~1–2 ms** (median 1.06 ms, mean 1.58 ms) (including synchronous JSONL audit flush and atomic disk snapshot write). Across 300,000 randomized property-test cases the decision engine never served a stale answer, and 122,000 fuzz executions of the header parser produced no crashes."*

### 0:55–1:30 — Production Operator Readiness & Native Guard Mode
*"We built this for enterprise operations:
- **Non-Root & Hardened**: Runs as `uid=100(bifrost)` in Docker; pinned to Bifrost commit `e869c3f9a`.
- **Fail-Closed ADR-006 Virtual Key Isolation**: Tenants are strictly partitioned by `sha256(vk)[:12]` from Bifrost's authenticated context. If the key is missing, caching fails closed. No cross-tenant data leakage is physically possible.
- **Atomic Snapshot Persistence**: Cache state survives abrupt container restarts via atomic disk snapshots.
- **Native Cache Guard Mode**: When callers pass Bifrost's built-in `x-bf-cache-key`, freshcache stamps it with evidence provenance (`STAMPED_SAFE`) or strips it (`BYPASSED_UNSAFE`) to protect Bifrost's internal cache from contamination (header stamping/stripping is unit-tested inside freshcache; end-to-end interaction with Bifrost's `semanticcache` is designed, not yet verified). We provide both a multi-domain SEC 8-K financial demo and a zero-dependency TypeScript SDK helper.  
Now over to Pankaj for the Tavily agent and live demonstration."*

---

## Part 2: Tavily Track 02 & Live Demo (Pankaj — 90 Seconds)

### 1:30–1:55 — The Agent: 30-Day Pre-Meeting Research
*"Thank you Swarit. On the agent side, executives prepping for an external meeting don't have time to wade through Google searches or hallucinated AI summaries.  
ChronoBrief uses Tavily's search API to execute a 4-query targeted retrieval plan over the last 30 days:
1. Product and feature launches
2. Executive leadership and board changes
3. Pricing and financing updates
4. Strategic partnerships and competitor moves.  
Crucially, every single claim in our generated brief carries a deterministic source ID tied to a canonical URL and verified published date. Our validator checks numbers, dates, and URLs deterministically: if a claim cannot be verified against the retrieved sources, it is dropped—never softened."*

### 1:55–2:35 — The Live Demonstration: MISS -> HIT -> REFUSE
*(Pankaj points to the live screen running Streamlit / CLI runner)*  
*"Let's see it live on Stripe:
- **Run 1 (Cold Miss)**: Tavily gathers the latest 30-day evidence. The gateway records `MISS_COLD`, Gemini 3.1 Flash generates the brief, and our citation validator verifies 9 claims.
- **Run 2 (Warm Reuse)**: Another team member prepares for the same meeting 10 minutes later. They click Generate. Look at the top banner: **HIT in ~1 ms of live hook overhead (median 0.95 ms)**. It returns the verified brief instantly, saving **5,496 LLM tokens** and **100% of LLM cost**.
- **Run 3 (Simulated Breaking News & Stale-Cache Court)**: Now, let's inject a breaking acquisition using our demo controls. We click Generate again.  
Notice the verdict from the Stale-Cache Court: `shadow_ttl_24h` reports **`HIT_STALE`**—meaning any standard time-based cache would have served outdated information. But freshcache detects the newer source timestamp, returns **`REFUSE_NEWER_SOURCE`**, invokes Gemini upstream, and delivers the new brief with the acquisition cited."*

### 2:35–3:00 — Honest Disclosures & Conclusion
*"We believe in radical engineering honesty:
- Real web search involves non-deterministic ranking, so our live HIT demonstrations use replay mode to ensure identical set hashes.
- Breaking news in our demo is simulated and visibly badged `[SIMULATED]`.
- All metrics are independently audited by an oracle script (`verify_audit.py`).  
ChronoBrief turns Bifrost into the first truth-aware AI gateway and gives executives pre-meeting briefs they can bet their reputation on.  
Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move. Thank you."*
