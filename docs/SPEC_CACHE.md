# Freshness-Aware Cache Specification (`freshcache`)

> **Frozen Specification (Aligned with Bifrost v2.4.0 / Core v1.14.1)**  
> Canonical Pitch: *"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."*

---

## 1. Core Principles & Invariants

1. **Pure Single-Decision Gateway**: The plugin makes exactly one decision: serve cached answer or recompute. No network calls, no model calls, and no Tavily API queries happen inside the gateway plugin.
2. **Zero Stale-Serve Guarantee**: An answer is never served when the incoming request carries evidence of a newer source. No evidence means no serve (`REFUSE_NO_FINGERPRINT`).
3. **Deterministic ADR-006 Fail-Closed Tenant Isolation**: `tenant_id` is extracted strictly from the authenticated Bifrost virtual key (`x-bf-vk` or virtual key context) and **NEVER** from a client `x-chrono-*` header.
   - **Fail Closed**: If `x-bf-vk` is missing or empty, the plugin treats the request as unisolated and refuses caching (`REFUSE_NO_TENANT`). It neither reads from nor stores into the cache. It NEVER falls back to a shared `"default"` tenant.
4. **Deterministic Replay for Cache HIT Demos**: Under our default `exact_set` policy (`ExactSet: true`), live Tavily web searches can yield slight variations between consecutive queries. Therefore, demonstrations of cache `HIT` run in deterministic replay mode (`CHRONO_TAVILY_MODE=replay`).

---

## 2. Request Contract (`x-chrono-*` Headers & `x-bf-freshness-*` Aliases)

Incoming requests to Bifrost's `/v1/chat/completions` carry the search evidence in HTTP request headers:

| Primary Header (`x-chrono-*`) | Universal Alias (`x-bf-freshness-*`) | Type | Description | Example / Range |
|---|---|---|---|---|
| `x-bf-vk` | - | string | Authenticated Bifrost Virtual Key (enforces tenant isolation) | `vk-team-alpha-secret` |
| `x-chrono-subject` | `x-bf-freshness-subject` | string | Subject/company/topic of the brief | `Anthropic` (max 256 chars) |
| `x-chrono-intent` | `x-bf-freshness-intent` | string | Intent/goal of the brief | `pre-meeting` (max 128 chars) |
| `x-chrono-window-days` | `x-bf-freshness-window-days` | integer | Retrieval lookback window in days | `30` (1–365) |
| `x-chrono-prompt-version` | `x-bf-freshness-prompt-version` | string | Schema & prompt template version | `v1` (max 64 chars) |
| `x-chrono-newest` | `x-bf-freshness-newest` | RFC3339 | Newest dated source timestamp (UTC) | `2026-10-31T20:15:00Z` |
| `x-chrono-count-at-newest` | `x-bf-freshness-count-at-newest` | integer | Number of sources sharing the newest timestamp | `2` (>= 1) |
| `x-chrono-dated` | `x-bf-freshness-dated` | integer | Count of sources with valid published date | `5` (>= 0) |
| `x-chrono-undated` | `x-bf-freshness-undated` | integer | Count of sources without published date | `0` (>= 0) |
| `x-chrono-set-hash` | `x-bf-freshness-set-hash` | hex sha256 | SHA-256 over canonical sorted `(url, timestamp, title)` | `e3b0c442...` (64 hex chars) |
| `x-chrono-cache-mode` | `x-bf-freshness-mode` | string | `live` or `replay` | `replay` |
| `x-chrono-refresh` | `x-bf-freshness-refresh` | boolean | Optional bypass (`true` forces recomputation and store refresh) | `true` |
| - | `x-bf-cache-key` | string | Native Bifrost semantic cache key (guarded by freshcache) | `sec-intel:AAPL:quarterly` |

---

## 3. Collision-Resistant Cache Key

Cache keys isolate tenants and differentiate prompt versions and model selections without mixing sources into the key (ADR-004):

$$\text{CacheKey} = \text{sha256}\Big(\text{LP}(\text{tenant\_id}) \parallel \text{LP}(\text{subject}) \parallel \text{LP}(\text{intent}) \parallel \text{LP}(\text{window\_days}) \parallel \text{LP}(\text{prompt\_version}) \parallel \text{LP}(\text{model})\Big)$$

Where length-prefixing $\text{LP}(s) = \text{len}(s) \mathbin{:} s$ prevents multi-field concatenation collisions (e.g. `subject="a", intent="bc"` vs `subject="ab", intent="c"`).

---

## 4. Decision Ordering (`decide.Decide`)

The decision engine evaluates conditions in strict priority:

```
1. Missing / Empty Tenant ID       --> REFUSE_NO_TENANT (Fail closed: no lookup, no store)
2. Invalid / Empty Request FP      --> REFUSE_NO_FINGERPRINT (Never serve without evidence)
3. Newest timestamp in future      --> REFUSE_FUTURE_DATE (Poisoned timestamp cannot mask news)
4. Cache Miss (Key not in LRU)     --> MISS_COLD (Proceed to model, store response)
5. Entry Exceeded Hard TTL         --> REFUSE_EXPIRED (Proceed to model, update entry)
6. Request has newer source        --> REFUSE_NEWER_SOURCE (The world moved; recompute)
7. Request count at newest > entry --> REFUSE_NEWER_SOURCE (Same date, additional newer source)
8. Undated sources count changed   --> REFUSE_SET_CHANGED (Cannot verify undated isn't newer)
9. ExactSet policy & set_hash diff --> REFUSE_SET_CHANGED (Sources differ; regenerate)
10. Otherwise                      --> HIT (Serve stored response copy)
```

---

## 5. Hook Lifecycle (Bifrost Gateway Architecture)

```
[Client Request]
       │
       ▼
1. HTTPTransportPreHook (server.go -> middlewares.go)
   - Intercept administrative /x-chrono/metrics endpoint (status 200 JSON short-circuit)
   - Read & strictly parse incoming x-chrono-* or x-bf-freshness-* headers
   - Extract tenant_id from authenticated x-bf-vk
   - Apply Native Cache Guard: stamp x-bf-cache-key if safe, strip if unsafe/poisoned
   - Generate server-side request ID (fc-<nano>-<seq>)
   - Store parsed Fingerprint & CacheKey on BifrostContext
       │
       ▼
2. PreLLMHook (client.go -> plugin runner)
   - If streaming: bypass with BYPASS_STREAM
   - If x-chrono-refresh: true: bypass lookup with BYPASS_REFRESH
   - If REFUSE_NO_TENANT: skip lookup, proceed to LLM
   - Call decide.Decide(now, reqFP, storedEntry, policy)
   - Compute counterfactual TTL shadow outcomes (TTL-5m, TTL-1h, TTL-24h)
   - On HIT:
     * Deep-copy stored BifrostResponse
     * Return (req, &schemas.LLMPluginShortCircuit{Response: &copy}, nil)
     * Set "chrono-served" = true on context
   - On MISS/REFUSE:
     * Return (req, nil, nil) to invoke provider upstream
       │
       ▼
[Provider Execution / Short-Circuit]
       │
       ▼
3. PostLLMHook (client.go -> plugin runner)
   - If context has "chrono-served": skip storing
   - If provider returned error or empty response: do not store
   - If tenant_id present and decide.Storable(now, reqFP, policy):
     * Read token usage (resp.ChatResponse.Usage)
     * Calculate cost synchronously: cost := ctx.CalculateCost(resp)
     * Put entry (deep copy of response, reqFP, cost, tokens, now) into LRU
     * Save atomic disk snapshot (/var/log/bifrost/freshcache-snapshot.json)
   - Append 1 JSONL audit log record with shadow TTL outcomes & plugin overhead
       │
       ▼
4. HTTPTransportPostHook (middlewares.go -> response)
   - Attach response headers:
     * x-chrono-cache: HIT | MISS_COLD | REFUSE_NEWER_SOURCE | REFUSE_SET_CHANGED | REFUSE_NO_TENANT
     * x-bf-freshness-cache: (mirrored universal alias)
     * x-chrono-cache-key: <sha256>
     * x-chrono-shadow-ttl-5m, x-chrono-shadow-ttl-1h, x-chrono-shadow-ttl-24h
     * x-chrono-plugin-overhead-us: <int microseconds>
     * x-bf-freshness-guard: STAMPED_SAFE | BYPASSED_UNSAFE
     * x-chrono-saved-tokens: <int> (on HIT)
     * x-chrono-saved-cost-usd: <float> (on HIT)
     * x-chrono-served-at: <RFC3339>
       │
       ▼
5. Cleanup
   - Atomically persist cache snapshot to disk
   - Flush and close audit file descriptor on gateway shutdown
```

---

## 6. Audit Log Schema (JSONL)

Every request processed by the plugin appends a single JSON line to `/var/log/bifrost/freshcache.jsonl`:

```json
{
  "timestamp": "2026-11-01T14:30:00Z",
  "tenant_id": "d7f9a1b2c3e4",
  "cache_key": "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
  "decision": "HIT",
  "request_newest": "2026-10-31T20:15:00Z",
  "request_count_at_newest": 2,
  "request_set_hash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "cached_newest": "2026-10-31T20:15:00Z",
  "cached_count_at_newest": 2,
  "cached_set_hash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "prompt_tokens": 1420,
  "completion_tokens": 480,
  "total_tokens": 1900,
  "saved_tokens": 1900,
  "cost_usd": 0.000425,
  "saved_cost_usd": 0.000425,
  "latency_ms": 12,
  "shadow_ttl_5m": "MISS_EXPIRED",
  "shadow_ttl_1h": "HIT_FRESH",
  "shadow_ttl_24h": "HIT_FRESH",
  "would_serve_stale_24h": false,
  "plugin_overhead_us": 45
}
```

---

## 7. Measured 4-Way Policy Comparison (TTL Frontier Simulation)

Measured empirically across 250 queries over 48 hours in `internal/frontier` (see `docs/evidence/ttl-frontier-report.md`):

| Strategy | Cache Decision Rule | Hit Rate | Stale Serves | Stale Serve Rate | False Misses | Tokens Saved | Cost Saved |
|:---|:---|:---:|:---:|:---:|:---:|:---:|:---:|
| **TTL-5m** | Standard 5-minute TTL (Bifrost default) | **6.4%** | `0` | **0.0%** | 222 | 83,200 | $0.0312 |
| **TTL-1h** | Standard 1-hour TTL | **52.4%** | `5` | **2.0%** | 107 | 681,200 | $0.2555 |
| **TTL-24h** | Standard 24-hour TTL | **96.0%** | `46` | **18.4%** | 5 | 1,248,000 | $0.4680 |
| **freshcache** | ChronoBrief ExactSet + 24h ceiling | **93.2%** | `0` | **0.0%** | 0 | 1,211,600 | $0.4543 |

---

## 8. Counterfactual TTL Shadow Ledger & Stale-Cache Court

Every cache lookup in `freshcache` evaluates what a standard time-based TTL cache would have done under the same traffic conditions via pure function `decide.ComputeShadowOutcomes`:
- **`shadow_ttl_5m`**: `"MISS_COLD"`, `"MISS_EXPIRED"`, `"HIT_FRESH"`, or `"HIT_STALE"`
- **`shadow_ttl_1h`**: `"MISS_COLD"`, `"MISS_EXPIRED"`, `"HIT_FRESH"`, or `"HIT_STALE"`
- **`shadow_ttl_24h`**: `"MISS_COLD"`, `"MISS_EXPIRED"`, `"HIT_FRESH"`, or `"HIT_STALE"`
- **`would_serve_stale_24h`**: boolean indicating if a 24h TTL cache would have hallucinated stale evidence.

These outcomes are recorded in every JSONL audit record and stamped on response headers (`x-chrono-shadow-ttl-*`), enabling the Stale-Cache Court to prove false cache misses prevented and hallucinations averted in real time.

---

## 9. Operator-Readiness Pack

1. **Dual-Bounded LRU Cache**:
   - Cache size is bounded by both maximum entry count (`max_entries`, default 1,000) and memory footprint (`max_bytes`, default 64 MB).
   - Estimated entry size dynamically tracks payload length and fingerprint metadata.
2. **Atomic Disk Snapshot Persistence**:
   - Cache state is serialized to disk atomically via temporary file rename (`freshcache-snapshot.json.tmp` -> `freshcache-snapshot.json`).
   - On container restart or gateway boot, valid non-expired entries are restored immediately.
3. **Live Telemetry & Stale-Cache Court Endpoint (`/x-chrono/metrics`)**:
   - Built directly into `HTTPTransportPreHook`, short-circuiting administrative requests without forwarding to upstream providers:
   ```json
   {
     "status": "healthy",
     "uptime_seconds": 1240,
     "lookups_total": 84,
     "hits_total": 41,
     "stale_prevented_total": 12,
     "court_stale_prevented_ttl_24h": 12,
     "court_stale_prevented_ttl_1h": 3,
     "court_ttl_5m_false_misses": 38,
     "overhead_us_avg": 42.5,
     "overhead_us_max": 180,
     "cache_entries": 14,
     "cache_bytes": 68420,
     "cache_max_bytes": 67108864,
     "cache_evictions": 0,
     "snapshot_path": "/var/log/bifrost/freshcache-snapshot.json",
     "snapshot_entries_loaded": 14
   }
   ```
4. **Panic-Recovery Fail-Open Guarantee**:
   - Every exported plugin hook wraps execution in deferred recovery blocks, preventing gateway crashes or upstream downtime if an unexpected runtime condition arises.

---

## 10. Universal `x-bf-freshness-*` Contract & Native Cache Guard Mode

1. **Universal Header Contract**:
   - All `x-chrono-*` headers accept standard Bifrost aliases:
     - `x-bf-freshness-subject`, `x-bf-freshness-intent`, `x-bf-freshness-window-days`, `x-bf-freshness-prompt-version`
     - `x-bf-freshness-newest`, `x-bf-freshness-count-at-newest`, `x-bf-freshness-dated`, `x-bf-freshness-undated`, `x-bf-freshness-set-hash`
     - `x-bf-freshness-mode`, `x-bf-freshness-refresh`
   - Response headers mirror decision: `x-bf-freshness-cache: HIT | MISS_COLD | REFUSE_NEWER_SOURCE`.
2. **Native Bifrost Cache Guard Mode**:
   - When requests carry native `x-bf-cache-key`:
     - If evidence is valid: freshcache stamps the key with provenance hash: `<key>:fc:<newest_rfc3339>:<hash8>`, setting `x-bf-freshness-guard: STAMPED_SAFE`.
     - If evidence is invalid, missing, or poisoned: freshcache strips `x-bf-cache-key`, setting `x-bf-freshness-guard: BYPASSED_UNSAFE` to protect upstream semantic cache from contamination.

---

## 11. Performance Benchmarks

Measured under Go 1.27.3 on Linux AMD64 (see `docs/evidence/gateway-benchmarks.txt`):

- **Decision Engine (`BenchmarkDecide`)**: `20.17 ns/op` | `0 B/op` | `0 allocs/op` (> 50M ops/sec)
- **Cache Key Generation (`BenchmarkKeyCompute`)**: `306.7 ns/op` | `160 B/op` | `3 allocs/op`
- **Header Parsing (`BenchmarkParseHeaders`)**: `1.81 µs/op` | `128 B/op` | `2 allocs/op`
- **LRU Store Access (`BenchmarkStoreGetHit`)**: `11.36 µs/op` (complete detached deep-copy safety)
- **Native Header Fuzzing (`FuzzParseHeaders`)**: 122,383 executions across 24 concurrent workers with 0 panics / 0 crashes.
- **Race Audit**: 0 data races detected across all packages under `go test -race`.
