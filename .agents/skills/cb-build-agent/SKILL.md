---
name: cb-build-agent
description: Manual command. Run only when the user types /cb-build-agent. Builds the Python agent: retrieval with replay mode, prompt, gateway client, citation validation and the pipeline function, test-first with fakes.
---

# /cb-build-agent

Preconditions: `docs/TAVILY_PROBE.md` exists; `chronobrief/fingerprint.py` and `chronobrief/brief.py` are green. Rules: `.agents/rules/30-python-app.md`, `40-grounding-and-guardrails.md`. Skills: `tavily-retrieval`, `brief-grounding`.

1. **Retrieval** (`chronobrief/retrieval.py`): `search(subject, *, mode, now, client) -> RetrievalResult` with the four-query plan, merged and normalised through `sources_from_tavily`, capped, with `partial: bool` and `simulated` carried through. Replay mode reads `fixtures/tavily/<slug>.json`. Tests use `httpx.MockTransport` (live path) and temp fixture files (replay path): happy path, zero results, one query failing, timeout and retry, missing key, undated results, duplicate URLs across queries.
2. **Prompt** (`chronobrief/prompt.py`): `PROMPT_VERSION`, builder returning the messages list, fenced sources. Snapshot test plus the injection and version-bump tests from the skill.
3. **Gateway client** (`chronobrief/gateway.py`): OpenAI-compatible chat completion to `BIFROST_URL`, headers from `Fingerprint.to_headers()` plus subject, intent, window, prompt version, cache mode; returns the parsed response text and a `CacheInfo` read from the `x-chrono-*` response headers (decision, key, entry newest, saved tokens and cost). Timeouts and retry policy per the python rule. Tests with a fake gateway: HIT, MISS, REFUSE headers, 500, malformed body, missing cache headers (treated as `UNKNOWN`, not as HIT).
4. **Pipeline** (`chronobrief/pipeline.py`): `generate_brief(subject, *, mode, cache_mode, now) -> BriefResult(brief, removed, cache_info, sources, notices)`. Order: normalise subject, retrieve, if no sources return the empty-state result without calling the model, assign IDs, fingerprint (if the retrieval was partial send empty fingerprint headers), call gateway, parse, validate, and on parse failure or on a HIT that fails validation do the single `refresh` repair described in the skill. Tests with fakes for every branch, including that the model is never called with zero sources.
5. **Metrics reader** (`chronobrief/metrics.py`): runs or imports `scripts/verify_audit.py` logic and returns the summary dict; test against a temp JSONL.
6. **Types.** `mypy chronobrief` clean in strict mode. `ruff check .` clean.
7. `make check` and `pytest --cov=chronobrief`. Quote the final lines and the coverage number.

Done when: a command-line run (`python -m chronobrief "Acme" --mode replay`, add a tiny `__main__.py`) prints a validated brief from a fixture against a fake gateway, and all checks are quoted.
