---
trigger: glob
globs: "chronobrief/**/*.py, app/**/*.py, tests/**/*.py"
description: Python and Streamlit rules for the agent and dashboard
---

# Python rules

- Python 3.11+, full type hints, `mypy --strict` clean on `chronobrief/`. Pydantic v2 models for every structure that crosses a boundary (Tavily result, claim, brief, gateway response).
- One module owns each concern: `fingerprint.py` (dates, canonical URLs, hashes), `retrieval.py` (Tavily and replay), `prompt.py` (prompt text and version), `brief.py` (schema and validator), `gateway.py` (HTTP to Bifrost), `metrics.py` (reads the audit oracle). The Streamlit file only wires them and renders.
- All dates are timezone-aware UTC. Parse in exactly one place (`fingerprint.parse_published`). No `datetime.now()` outside a function that takes `now` as a parameter, so tests can fix the clock.
- Network calls: explicit timeouts, bounded retries with backoff for 429 and 5xx, no retry on 4xx. Raise typed errors; the UI turns them into plain messages with a next step.
- NEVER cache brief generation in Streamlit (`st.cache_data`, `st.cache_resource`, `lru_cache`). It would produce hits the gateway never made and invalidate the demo. `make invariants` fails on it; a justified exception needs `# cache-ok: reason` on that line.
- Do not log API keys, full prompts with source text, or raw headers. Log request IDs, decisions, counts and durations.
- Input handling: strip, collapse whitespace, cap the target at 200 characters, reject empty. The same normalised string feeds the cache key everywhere.
- `chronobrief/fingerprint.py` and `gateway/plugins/freshcache/internal/decide/decide.go` are a pair. Change one, change the other and both test files in the same commit.
