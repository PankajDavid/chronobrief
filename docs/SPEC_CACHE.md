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

## 2. Request Contract (`x-chrono-*` Headers)

Incoming requests to Bifrost's `/v1/chat/completions` carry the search evidence in HTTP request headers:

| Header | Type | Description | Example / Range |
|---|---|---|---|
| `x-bf-vk` | string | Authenticated Bifrost Virtual Key (enforces tenant isolation) | `vk-team-alpha-secret` |
| `x-chrono-subject` | string | Subject/company/topic of the brief | `Anthropic` (max 256 chars) |
| `x-chrono-intent` | string | Intent/goal of the brief | `pre-meeting` (max 128 chars) |
| `x-chrono-window-days` | integer | Retrieval lookback window in days | `30` (1–365) |
| `x-chrono-prompt-version` | string | Schema & prompt template version | `v1` (max 64 chars) |
| `x-chrono-newest` | RFC3339 | Newest dated source timestamp (UTC) | `2026-10-31T20:15:00Z` |
| `x-chrono-count-at-newest` | integer | Number of sources sharing the newest timestamp | `2` (>= 1) |
| `x-chrono-dated` | integer | Count of sources with valid published date | `5` (>= 0) |
| `x-chrono-undated` | integer | Count of sources without published date | `0` (>= 0) |
| `x-chrono-set-hash` | hex sha256 | SHA-256 over canonical sorted `(url, timestamp, title)` | `e3b0c44298fc1c149afb...` (64 hex chars) |
| `x-chrono-cache-mode` | string | `live` or `replay` | `replay` |
| `x-chrono-refresh` | boolean | Optional bypass (`true` forces recomputation and store refresh) | `true` |

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
   - Read & strictly parse incoming x-chrono-* headers
   - Extract tenant_id from x-bf-vk
   - If tenant_id missing: mark REFUSE_NO_TENANT on context
   - Validate timestamp format & fingerprint bounds
   - Store parsed Fingerprint & CacheKey on BifrostContext
       │
       ▼
2. PreLLMHook (client.go -> plugin runner)
   - If streaming: bypass with BYPASS_STREAM
   - If x-chrono-refresh: true: bypass lookup with BYPASS_REFRESH
   - If REFUSE_NO_TENANT: skip lookup, proceed to LLM
   - Call decide.Decide(now, reqFP, storedEntry, policy)
   - On HIT:
     * Deep-copy stored BifrostResponse
     * Return (req, &schemas.LLMPluginShortCircuit{Response: &copy}, nil)
     * Set "chrono-served" = true on context
   - On MISS/REFUSE:
     * Return (req, nil, nil) to invoke Gemini upstream
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
   - Append 1 JSONL audit log record (non-blocking, append failure never fails request)
       │
       ▼
4. HTTPTransportPostHook (middlewares.go -> response)
   - Attach response headers:
     * x-chrono-cache: HIT | MISS_COLD | REFUSE_NEWER_SOURCE | REFUSE_SET_CHANGED | REFUSE_NO_TENANT | ...
     * x-chrono-cache-key: <sha256>
     * x-chrono-cached-newest: <RFC3339>
     * x-chrono-saved-tokens: <int> (on HIT)
     * x-chrono-saved-cost-usd: <float> (on HIT)
     * x-chrono-served-at: <RFC3339>
       │
       ▼
5. Cleanup
   - Flush and close audit file descriptor on gateway shutdown
```

---

## 6. Audit Log Schema (JSONL)

Every request processed by the plugin appends a single JSON line to `/var/log/bifrost/freshcache-audit.jsonl`:

```json
{
  "timestamp": "2026-11-01T14:30:00Z",
  "tenant_id": "vk-team-alpha-prod",
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
  "latency_ms": 12
}
```

---

## 7. Expected 3-Way Policy Comparison

*(Explicitly labeled as expected behaviour until measured against the audit log)*

| Metric | 5-Minute TTL Cache | 24-Hour TTL Cache | ChronoBrief Freshness Cache |
|---|---|---|---|
| **Stale-Serve Rate** | Moderate (serves stale if news breaks within 5m) | High (serves stale for up to 24h after news breaks) | **0% (Guaranteed zero stale serves)** |
| **Cache Hit Ratio (Unchanged News)** | Low (expires every 5m even when facts don't move) | High (caches for 24h) | **High (Hits whenever source set is unchanged)** |
| **Token & API Cost Savings** | Low (~10-20%) | High (~80%), but corrupts news accuracy | **Optimal (~60-80% without serving stale data)** |
| **Freshness Verification** | Blind to news dates | Blind to news dates | **Deterministic cryptographic source verification** |
