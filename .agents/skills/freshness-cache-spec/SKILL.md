---
name: freshness-cache-spec
description: The exact behaviour of the freshness-aware cache plugin: fingerprint, cache key, decision order, store rules, headers, audit record, edge cases and test matrix. Use when implementing, reviewing, or testing the plugin or the client fingerprint code.
---

# Freshness cache specification

The decision logic is already implemented and tested in `gateway/plugins/freshcache/internal/decide/` (Go) and `chronobrief/fingerprint.py` (Python). This skill explains why, and what the hooks around them must do. Header names and audit fields are in `docs/ARCHITECTURE.md`.

## The idea
A normal semantic cache returns the answer it last saw. For news, "the last answer" is exactly the wrong thing once a new article exists. So each stored answer remembers how fresh its sources were, and a new request brings the freshness of its own sources. The cache serves only when the request has nothing newer than what the stored answer already knew.

## Definitions
- **Fingerprint** of a source set: newest dated source (UTC), how many dated sources share that timestamp, counts of dated and undated sources, and a hash of the sorted (canonical URL, timestamp) lines. Computed by the agent from Tavily's `published_date`, never by the model.
- **Cache key**: sha256 of `model | subject | intent | window_days | prompt_version`. Sources are NOT in the key (ADR-004).
- **Entry**: response copy, fingerprint, created-at, original prompt and completion tokens, original cost.

## Hook flow
1. `HTTPTransportPreHook`: read and strictly parse the `x-chrono-*` headers, bound every length, build the request fingerprint and key inputs, put them on the context. Missing or malformed fingerprint headers produce an "empty" fingerprint, which the decision treats as no evidence (refuse). Never block the request.
2. `PreLLMHook`: if streaming, bypass and log `BYPASS_STREAM`. If mode is `refresh`, skip lookup. Otherwise call `decide.Decide`. On HIT return a deep copy of the stored response as the short circuit and set the served-from-cache flag on the context. On anything else return the request unchanged.
3. `PostLLMHook`: if served-from-cache, do nothing. Else, if the response is a success, non-empty, finished normally, and `decide.Storable` is true, store it with the request fingerprint, token usage and cost computed synchronously.
4. `HTTPTransportPostHook`: set `x-chrono-cache`, `x-chrono-cache-key`, `x-chrono-entry-newest`, `x-chrono-saved-tokens`, `x-chrono-saved-cost-usd` from context values.
5. Every lookup, including bypasses and refreshes, appends one audit JSON line. Append failure never fails the request.

## Edge cases and required outcomes
| Case | Outcome |
|---|---|
| Same subject, same sources | HIT |
| New article newer than entry | REFUSE_NEWER_SOURCE, recompute, store new entry |
| New article, same timestamp as newest (date-only granularity) | REFUSE_NEWER_SOURCE via count_at_newest |
| Search returned fewer or older sources than last time | HIT (entry already knows newer) |
| Source without a parseable date appears or disappears | REFUSE_SET_CHANGED |
| Source dated in the future | agent drops the date; if one slips through, REFUSE_FUTURE_DATE and never stored |
| No dated sources at all | REFUSE_NO_FINGERPRINT, not stored |
| Entry older than TTL | REFUSE_EXPIRED |
| Prompt version or model changed | different key, MISS_COLD |
| Streaming request | bypass, not stored |
| Model returned an error or empty content | not stored |
| Two concurrent identical requests | both may compute; last write wins; never serve a half-written entry |
| Cached brief later fails citation validation | client sends `refresh`; result replaces the entry |

## Test matrix
Go: the table test and the 300k-iteration invariant test in `decide_test.go` (already green). Add hook-level tests with fake contexts for: header parsing (valid, empty, oversized, malformed), HIT returns a copy not the stored pointer, PostLLMHook skips storing when served from cache, bypass on stream.
Integration (real gateway): MISS then HIT then REFUSE, driven by `scripts/e2e_cache.py` (build it in `/cb-wire-gateway`), followed by `scripts/verify_audit.py` reporting zero stale serves.

## Pitfalls
- PostLLMHook runs for short-circuited responses too; forgetting the flag re-stores the cached copy and resets its age.
- Returning the stored pointer lets later code mutate the cache.
- Treating `published_date` absence as "old" is wrong; absence is "unknown", and unknown must never count as fresh.
- A future-dated entry would hide all later real news. The guard exists in both `Decide` and `Storable`; keep both.
