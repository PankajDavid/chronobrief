# T14 Security & quality audit

Date: 2026-11-01 · Scope: whole repository at `main` 648f39f plus this branch · Protocol: Infinite India IT Services Security Audit Protocol v2.2 baseline.

## Summary

| Check | Result |
|---|---|
| Secrets in git history (22 commits, all branches) | **None found.** No `.env` ever committed; no Gemini/Tavily/virtual-key-shaped strings in any version. Only the retired dummy value `team-alpha` appears in old evidence. |
| Python dependencies (`pip-audit`, 56 locked packages) | No known vulnerabilities |
| Tests | 89 passed, coverage 93% (`chronobrief/`) |
| `ruff`, `mypy --strict`, `check_invariants.py --strict` | Clean |
| Go plugin tests | Not re-run here (Go 1.27.3 toolchain not downloadable in the audit workspace); last green run is in Swarit's T07/T10 evidence. Re-run with `docker run --rm -v "$PWD/gateway/plugins/freshcache:/src" -w /src golang:1.27.3 go test ./...` |

Before making the repo public: commit author emails (two personal Gmail addresses) are visible in history. That is normal for public repos; switch to GitHub `noreply` addresses for future commits if you prefer.

## Findings

| ID | Severity | Where | Finding | Status |
|---|---|---|---|---|
| F1 | **High** | `gateway/docker-entrypoint.sh` | Missing virtual keys silently fell back to built-in values `sk-bf-alpha-dev-fallback` / `sk-bf-beta-dev-fallback`. A missing `.env` produced a running gateway with guessable tenant keys. | **Fixed here**: refuses to start unless both virtual keys (≥32 chars) and the Gemini key are set; rejects placeholders and characters outside `A-Za-z0-9_-` (which also closes `sed` injection). Tested with empty, placeholder, short, valid and `|`-containing values. **Swarit: please rebuild and confirm the container starts with your `.env`.** |
| F2 | Medium | `app` + `chronobrief/pipeline.py` | The meeting-time field went into the prompt unvalidated: free text (a prompt-injection channel) that also changed the prompt without changing the cache key, so a HIT could serve a brief generated for different input. | **Fixed here**: meeting time must parse as ISO-8601 and is normalised to UTC; it is no longer sent to the model (shown on the brief only). Prompt bumped to `brief-v2` so no v1 entry is ever reused. 8 new tests. |
| F3 | Medium | `Dockerfile` (runtime stage) | Container runs as root (rule 60-security: run as non-root). | **For Swarit**: add `RUN addgroup -S bifrost && adduser -S -G bifrost bifrost && chown -R bifrost:bifrost /etc/bifrost /var/log/bifrost` and `USER bifrost` before `ENTRYPOINT`. The entrypoint edits `/etc/bifrost/config.json`, so that directory must stay writable by the user. Needs a Docker rebuild to verify. |
| F4 | Medium (Low while the gateway is private) | `gateway/plugins/freshcache/main.go` lines ~118-145, ~425 | The per-request correlation id is taken from client headers `x-freshcache-id` / `x-request-id` when present. A caller can reuse another request's id and mix up decision headers. Entries are only removed in `HTTPTransportPostHook`, so requests that never reach it (streaming, early errors) stay in `reqOutcomes` forever: unbounded memory growth. | **For Swarit**: always generate the id server-side (ignore client headers), and drop entries older than ~2 minutes on each insert (or cap the map size). Add a unit test for both. |
| F5 | Low | `Dockerfile` | Bifrost is cloned by tag `transports/v2.4.0`; tags can be moved. | **For Swarit**: pin the commit SHA (`git -c advice.detachedHead=false clone ... && git checkout <sha>`). |
| F6 | Low | `.env.example` | Model name (`gemini-2.5-flash`) differed from the one the gateway runs; default search mode `live` spends Tavily credits on a fresh checkout. | **Fixed here**: `gemini-3.1-flash-lite`, `replay` by default, how to generate the virtual keys. |
| F7 | Low | `requirements.lock` | Contained `-e c:\bilfrosttavilychallenge` (a Windows path from one machine); the file failed to install anywhere else and leaked a local path. | **Fixed here**: line removed (the project itself is installed with `pip install -e .`). |
| T1 | Accepted risk (documented) | Plugin trust model | The plugin trusts the `x-chrono-*` freshness headers from its caller. Anyone holding a valid virtual key could send an old "newest" date and be served a stale brief. Mitigations in place: Bifrost requires a virtual key (verified: `virtual_key_required` 401 without one); the gateway listens on `127.0.0.1` only; virtual keys are long random secrets. Hardening if exposed: sign the fingerprint headers with a shared secret. | Document in README threat model (T15) |

## Adversarial pass

| Attempt | Result | Evidence |
|---|---|---|
| Empty subject | Plain error, no calls | `test_empty_input_shows_error` |
| 5,000-character subject | Capped to 200 everywhere (search, prompt, cache header) | `test_huge_subject_is_capped_everywhere` |
| Subject with no coverage | "No coverage found", model never called | `test_no_sources_never_calls_gateway` |
| Tavily key missing / rejected | Setup message, no stack trace | `test_missing_key_is_config_error`, `test_rejected_key_is_config_error` |
| Every search fails | "search unavailable", never read as "no news" | `test_every_query_failing_raises_instead_of_meaning_no_news` |
| Some searches fail | Brief built but not cached (empty fingerprint) | `test_partial_retrieval_sends_empty_fingerprint` |
| Gateway stopped | "Cannot reach the gateway" setup message | `GatewayConfigError` on `ConnectError` |
| Two quick clicks | Second rejected by the 2-second cooldown | `test_cooldown_rejects_double_click` |
| Cached brief failing validation | Regenerated once (only when no claim survives) | `test_cached_hit_failing_validation_is_regenerated`, `test_hit_with_partly_removed_claims_is_served_not_regenerated` |
| Source text with "ignore previous instructions" | Source screened out before ids are assigned; an obedient model's claim is removed | `test_injection_source_is_screened_and_an_obedient_model_cannot_use_it` |
| Source text trying to close the `<source>` fence | Neutralised | `test_source_text_cannot_close_the_fence` |
| Model output with Markdown links/images | Escaped, rendered as text | `test_markdown_from_model_cannot_inject_links` |
| `javascript:` source link | Not rendered as a link | `test_source_line_rejects_non_http_links_and_badges_simulated` |
| Simulated article in live mode | Rejected | `test_live_rejects_simulated_articles`; demo controls hidden in live mode |
| Prompt edited without version bump | Test fails | `test_prompt_digest_is_pinned_to_version` |
| Meeting-time injection | Rejected before any call | `test_bad_meeting_time_rejected_before_any_call` |
| Forged freshness headers | Accepted by design: trust limit T1 | — |

## Invariants (AGENTS.md) → enforcement

1. Plugin makes one decision, no network: `check_invariants.py` plugin-purity check + Go `internal/decide` tests.
2. Stale-serve rate 0: `decide` ordering tests; `verify_audit.py` recomputes from dates; live runs show `stale_serves 0`.
3. Every claim sourced: `validate_brief` + `test_brief.py`, injection tests.
4. Model proposes, code decides: dates, hashes, ages and cache decisions are computed in `fingerprint.py`/`brief.py`/plugin; model-sent `freshness_age_days`/`date_unverified`/`subject` are discarded (`test_parse_handles_code_fences_and_ignores_model_owned_fields`).
5. Simulated only in replay, badged: retrieval guard + UI badge tests.
6. Secrets only in `.env`: history scan above, `check_invariants.py` no-secrets check, F1 fail-closed entrypoint.

## Before the public deployment

- F3 and F4 should be fixed first (F4 matters more once real traffic arrives).
- Rate limiting per IP plus a daily AI cap (GiggerIQ pattern) is planned for the deployment step, not built yet.
