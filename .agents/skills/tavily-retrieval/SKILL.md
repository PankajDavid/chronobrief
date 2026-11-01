---
name: tavily-retrieval
description: How the agent retrieves the last 30 days of sources from Tavily, normalises them, and supports an offline replay mode. Use when writing or changing retrieval code, fixtures, query plans, or anything that reads published dates.
---

# Tavily retrieval

Tavily search lives in the agent (Python), never in the plugin. Retrieval produces the sources, their dates, and therefore the fingerprint, so its correctness decides whether the cache can be trusted.

## Facts to verify with a probe before writing code (T03)
Tavily's documentation, at the time of writing, says: `topic: "news"` returns a `published_date` per result and `published_date` is only available for the news topic; date filtering is available through `time_range` (day, week, month, year) and through `start_date` and `end_date` in `YYYY-MM-DD`; for news, `days` also exists. Parameter names and defaults change, so run the probe and write what you actually see.

Probe procedure:
1. Write `scripts/tavily_probe.py` that issues one search for a well-known company with `topic="news"`, `start_date = today - 30 days`, `end_date = today`, `max_results=10`, and saves the raw JSON to `docs/evidence/tavily-probe-<date>.json`. Read the key from the environment; never print it.
2. Record in `docs/TAVILY_PROBE.md`: which fields every result has, whether `published_date` is present on every result, its exact string format (RFC 2822, ISO, date-only), whether results outside the window appear, how many credits the call cost, and latency.
3. Update `fingerprint.parse_published` and its tests if the real format is not already covered.

## Query plan per target
Four searches, all `topic="news"`, same date window, each `max_results` 8 to 10:
1. `"<target>"` general news.
2. `"<target>" appoints OR hires OR joins executive OR CEO OR CFO OR CTO`.
3. `"<target>" launches OR announces OR releases product OR feature`.
4. `"<target>" partnership OR acquisition OR funding OR strategy`.
Merge all results, then run `sources_from_tavily` (canonicalise URLs, parse dates, drop future dates, dedupe). Cap the merged set (for example 25 newest) before building the prompt so token cost stays bounded; the fingerprint is computed on the capped set that the model actually sees.

## Modes
`CHRONO_TAVILY_MODE=live` calls the API. `replay` reads `fixtures/tavily/<slug>.json`, where `<slug>` is the lower-case subject with non-alphanumerics replaced by `-`. A fixture file is the raw merged Tavily result list plus `"captured_at"`. Capture fixtures by running live once for each demo subject and saving the output; commit them. Replay must exercise the same normalisation code path as live; only the HTTP call differs.

## Failure behaviour
- Timeout 15 s per call, two retries with backoff on 429 and 5xx, none on other 4xx. Missing or invalid key raises a typed `RetrievalConfigError` that the UI shows as a setup message, never a stack trace.
- Zero results across all four queries: return an empty source list. The app shows "No coverage found in the last 30 days" and does not call the model.
- Ambiguous names (a person who shares a name with a company): show the top distinct publishers' headlines in the source list so the user can see which entity was matched; do not guess.
- Partial failure (one of four queries fails): continue with the rest and show a visible notice that coverage may be incomplete. A fingerprint built from partial data must not be cacheable as complete: add `x-chrono-cache-mode: refresh`-style bypass by sending no fingerprint (empty headers), which the plugin refuses to serve or store.

## Cost control
Four searches per brief. Use `search_depth="basic"` by default. In replay mode zero credits are spent. The demo script must not issue more than a handful of live calls.
