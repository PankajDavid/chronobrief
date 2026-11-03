# Universal Freshness Cache Contract & Wire Specification

> **Bifrost Plugin Specification (`freshcache.so`)**  
> *"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."*

This document defines the language-agnostic wire protocol, canonical hashing algorithm, and Native Bifrost Cache Guard contract for `freshcache.so`. Any client—whether Python, TypeScript/Node.js, Go, or Java—can interact with the gateway by adhering to these exact specifications.

---

## 1. Universal HTTP Request Headers

When sending chat completion requests to Bifrost's `/v1/chat/completions`, client applications pass retrieved search evidence via HTTP request headers. Freshcache accepts either the canonical Bifrost prefix (`x-bf-freshness-*`) or ChronoBrief's legacy alias (`x-chrono-*`):

| Canonical Header (`x-bf-freshness-*`) | Legacy Alias (`x-chrono-*`) | Type | Description | Example |
|---|---|---|---|---|
| `x-bf-freshness-subject` | `x-chrono-subject` | string | Normalized subject/company identifier (max 256 chars) | `Stripe` |
| `x-bf-freshness-intent` | `x-chrono-intent` | string | Target workflow or intent (max 128 chars) | `pre-meeting` |
| `x-bf-freshness-window-days` | `x-chrono-window-days` | integer | Retrieval lookback window (1–365) | `30` |
| `x-bf-freshness-prompt-version` | `x-chrono-prompt-version` | string | Schema & prompt template version | `brief-v2` |
| `x-bf-freshness-newest` | `x-chrono-newest` | RFC3339 | Newest dated source timestamp (UTC) | `2026-10-31T20:15:00Z` |
| `x-bf-freshness-count-at-newest` | `x-chrono-count-at-newest` | integer | Count of sources published at the newest timestamp | `1` (>= 1) |
| `x-bf-freshness-dated` | `x-chrono-dated` | integer | Total count of sources with valid dates | `5` (>= 0) |
| `x-bf-freshness-undated` | `x-chrono-undated` | integer | Total count of sources lacking dates | `0` (>= 0) |
| `x-bf-freshness-set-hash` | `x-chrono-set-hash` | hex sha256 | SHA-256 over canonical sorted `(url, timestamp, title)` | `5a5583be...` (64 hex chars) |
| `x-bf-freshness-mode` | `x-chrono-cache-mode` | string | Execution mode: `live`, `replay`, or `refresh` | `replay` |
| `x-bf-freshness-refresh` | `x-chrono-refresh` | boolean | Set `true` to force upstream regeneration & cache overwrite | `false` |
| `x-bf-cache-key` | - | string | Built-in Bifrost semantic cache key (guarded by plugin) | `sec-intel:AAPL:quarterly` |

---

## 2. Response Headers Stamped by Freshcache

Every response returned by the gateway is annotated in `HTTPTransportPostHook` with cache provenance:

| Response Header | Values | Description |
|---|---|---|
| `x-bf-freshness-cache` | `HIT`, `MISS_COLD`, `REFUSE_NEWER_SOURCE`, `REFUSE_SET_CHANGED`, `REFUSE_NO_TENANT` | Primary cache outcome (mirrored to `x-chrono-cache`) |
| `x-chrono-cache-key` | 64-char sha256 hex | Unique tenant-isolated collision-resistant cache key |
| `x-chrono-shadow-ttl-5m` | `MISS_COLD`, `MISS_EXPIRED`, `HIT_FRESH`, `HIT_STALE` | Counterfactual outcome of a standard 5m TTL cache |
| `x-chrono-shadow-ttl-1h` | `MISS_COLD`, `MISS_EXPIRED`, `HIT_FRESH`, `HIT_STALE` | Counterfactual outcome of a standard 1h TTL cache |
| `x-chrono-shadow-ttl-24h` | `MISS_COLD`, `MISS_EXPIRED`, `HIT_FRESH`, `HIT_STALE` | Counterfactual outcome of a standard 24h TTL cache |
| `x-chrono-plugin-overhead-us`| integer | Live plugin overhead in microseconds: HIT `~1 ms` (median 1.08 ms, n=25), MISS `~1–2 ms` (median 1.51 ms, mean 1.73 ms, n=25; with audit flush & snapshot write; raw data in `docs/evidence/live-overhead-25runs.txt`); in-memory microbenchmarks `20.17 ns` to `11.36 µs` without I/O |
| `x-bf-freshness-guard` | `STAMPED_SAFE`, `BYPASSED_UNSAFE` | State of native `x-bf-cache-key` guard |
| `x-chrono-saved-tokens` | integer | Tokens saved from LLM generation on `HIT` |
| `x-chrono-saved-cost-usd` | float (6 decimals) | USD cost saved from LLM generation on `HIT` |
| `x-chrono-served-at` | RFC3339 | Gateway timestamp when response was dispatched |

---

## 3. Canonical Fingerprint & SHA-256 `set_hash` Algorithm

To guarantee 100% byte-for-byte reproducibility across languages (Python, TypeScript, Go), every client MUST implement the canonical normalization:

### 3.1 Canonical URL Normalization
1. Strip leading and trailing whitespace.
2. Parse URL scheme, host, path, query.
3. Lowercase scheme (default `https`) and hostname.
4. Strip `www.` prefix from hostname.
5. Filter query parameters to strip marketing/tracking tags:
   - Strip case-insensitively: `utm_.*`, `fbclid`, `gclid`, `mc_cid`, `mc_eid`, `ref`, `ref_src`, `igshid`.
6. Sort remaining query parameters alphabetically by key.
7. Strip trailing slashes from path (`/path/` -> `/path`). Strip fragments.
8. Reconstruct: `<scheme>://<host><path>?<sorted_query>`.

### 3.2 Canonical Entry String
For each source in the retrieved set:
$$\text{line} = \text{canonical\_url} \parallel \text{"|"} \parallel \text{timestamp} \parallel \text{"|"} \parallel \text{normalized\_title}$$
- `timestamp`: RFC3339 UTC string formatted strictly as `YYYY-MM-DDTHH:MM:SSZ` (e.g. `2026-10-31T20:15:00Z`). If undated or unparseable, use `"undated"`.
- `normalized_title`: `title.trim()`, with internal contiguous whitespace collapsed to a single space.

### 3.3 Set Hash Computation
1. Sort all entry lines alphabetically (lexicographical byte order).
2. Join lines with standard newline character (`\n`).
3. Compute SHA-256 digest over UTF-8 bytes and encode as lowercase hex string (64 characters).
4. If sources list is empty, `set_hash` is empty string `""`.

---

## 4. Native Bifrost Cache Guard Mode

> [!NOTE]
> **Implementation Status**: Native `x-bf-cache-key` Guard Mode: header stamping/stripping is implemented and unit-tested inside `freshcache`, while end-to-end interaction with Bifrost's built-in `semanticcache` plugin is designed, not yet verified (as `semanticcache` is disabled in `gateway/config.json`).

Bifrost includes native semantic and KV caching via the `x-bf-cache-key` header. However, native caching is **blind to source freshness**—if an upstream document moves or financial restatements break, a caller sending the same prompt would receive stale hallucinations from Bifrost's internal cache.

When `x-bf-cache-key` is supplied alongside `x-bf-freshness-*`:
1. **Safety Check**: Freshcache verifies that the request fingerprint is valid, dated, and not future-dated beyond 24 hours.
2. **Safe Stamping (`STAMPED_SAFE`)**: Freshcache appends the provenance fingerprint directly to the native cache key:
   $$\text{x-bf-cache-key} \gets \text{original\_key} \mathbin{:} \text{"fc"} \mathbin{:} \text{newest\_rfc3339} \mathbin{:} \text{set\_hash[:8]}$$
   This guarantees that Bifrost's native semantic cache automatically partitions by evidence truth!
3. **Unsafe Stripping (`BYPASSED_UNSAFE`)**: If the fingerprint is missing, unparseable, or future-dated, freshcache **strips** `x-bf-cache-key` from the request before Bifrost's internal handlers run. This prevents unverified or poisoned evidence from polluting Bifrost's global cache.
