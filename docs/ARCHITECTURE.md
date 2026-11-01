# Architecture

```mermaid
flowchart LR
  U[User: person or company] --> S[Streamlit app]
  S --> A[Agent: Tavily 30-day search, normalise, fingerprint]
  A -->|OpenAI-compatible request + x-chrono-* headers| B[Bifrost gateway]
  B --> P[freshcache plugin: serve or recompute]
  P -->|HIT| B
  P -->|MISS or REFUSE| M[(Model provider)]
  M --> B
  B --> A --> V[Citation validator] --> S
  P -.-> L[(audit JSONL)]
  L -.-> X[verify_audit.py] -.-> S
```

## Header contract
Request, agent to plugin. All are metadata, not authentication.
| Header | Meaning |
|---|---|
| x-chrono-subject | normalised target, max 200 chars |
| x-chrono-intent | `brief` |
| x-chrono-window-days | 30 |
| x-chrono-prompt-version | e.g. brief-v1 |
| x-chrono-newest | newest dated source, RFC3339 UTC, empty if none |
| x-chrono-count-at-newest | dated sources sharing that timestamp |
| x-chrono-dated / x-chrono-undated | counts |
| x-chrono-set-hash | sha256 over sorted (canonical URL, timestamp, title) |
| x-chrono-cache-mode | `normal` or `refresh` (skip lookup, store result) |

Response, plugin to agent: `x-chrono-cache` (HIT, MISS_COLD, REFUSE_*), `x-chrono-cache-key` (first 12 hex), `x-chrono-entry-newest`, `x-chrono-saved-tokens`, `x-chrono-saved-cost-usd`.

## Cache Key & Tenant Isolation (ADR-006)
The cache key is `sha256(len_prefixed(tenant_id, model, subject, intent, window_days, prompt_version))`.
`tenant_id` is extracted inside the Go plugin strictly from the authenticated Bifrost virtual key (`x-bf-vk` / Bifrost virtual key context) and NEVER from a client-supplied `x-chrono-*` header, preventing cross-team cache leakage.
**Fail Closed (No "default" tenant)**: If a request has no Bifrost virtual key (missing or empty), the plugin MUST NOT cache—it refuses lookup and does not store (`REFUSE_NO_TENANT` / fail closed). Never fall back to a shared `"default"` tenant, which would risk cross-caller leakage.
Bumping `prompt_version` invalidates all cached entries deterministically.

## Decision order (internal/decide)
missing tenant / unauthenticated -> refuse (fail closed); no evidence -> refuse; future-dated -> refuse; no entry -> cold miss; expired -> refuse; newer source -> refuse; undated count changed -> refuse; exact policy and set differs -> refuse; else hit.

## Policy Comparison Benchmark & LLM-Only Savings
- **Canonical Pitch Line**: `"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."`
- **Cache Policy**: `exact_set` (`ExactSet: true`) is our policy. Because live Tavily search results vary slightly call-to-call even without new news, cache `HIT` demonstrations must run in replay mode (`CHRONO_TAVILY_MODE=replay`) for deterministic repeatability.
- **Savings = LLM cost and tokens only**: Tavily search runs on every request to gather live source timestamps and titles; cache hits save 100% of LLM generation cost and latency.
- **Comparison Benchmark (Expected Behaviour until measured against audit log)**:
  - *5-min TTL (Expected)*: High false-miss rate (recomputes needlessly when teammates prep >5 mins apart with unchanged news).
  - *24h TTL (Expected)*: High stale-serve rate (serves yesterday's brief after breaking news or headline corrections).
  - *ChronoBrief (Expected)*: 0% stale serves while retaining full 24h cache hits when evidence is unchanged.


## Audit record (one JSON line per lookup)
ts, key, subject, model, prompt_version, mode, decision, incoming_newest, entry_newest, incoming_count, entry_count, incoming_set_hash8, entry_set_hash8, latency_us, saved_tokens, saved_cost_usd.

## Metrics (computed by scripts/verify_audit.py, never by the plugin)
Hit rate; tokens and cost saved (from the original computation recorded at store time); stale serves (must be 0); stale answers prevented (REFUSE_NEWER_SOURCE count, i.e. what a plain TTL or semantic cache would have served wrongly).

## Threat model, honestly
- `tenant_id` is derived strictly from the authenticated Bifrost virtual key context (`x-bf-vk`), never from client headers. Requests lacking a virtual key fail closed (`REFUSE_NO_TENANT`), guaranteeing cross-tenant cache isolation.
- Freshness headers are client-supplied metadata. A caller who lies about freshness can cause a stale serve. Acceptable here because the only caller is our app; documented, not hidden. Hardening option: shared-secret header checked by the plugin.
- Source text is untrusted. It is fenced as data in the prompt and never treated as instructions.
- A brief that fails citation validation can already be cached by the time we validate it. The client heals this: a HIT that fails validation triggers an immediate `refresh` request.

## Non-goals
No accounts, no database, no scheduled monitoring, no semantic similarity matching. The cache key is exact on purpose.
