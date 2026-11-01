# Decision log

Format: ID, date, decision, why, fallback if it fails, trigger for the fallback.

## ADR-001: Freshness travels in HTTP headers, read by HTTPTransportPreHook
Why: the plugin must make a pure decision, so the agent computes the fingerprint and sends it; the transport hook can read headers and pass them to the LLM hook through the request context.
Fallback: encode the fingerprint in request metadata the LLM hook can read directly (confirm in docs/BIFROST_RECON.md).
Trigger: headers are not visible to the plugin after /cb-recon.

## ADR-002: Gateway and plugin are built in the same Docker build
Why: Go plugins (.so) must match the gateway's Go version and core package versions exactly, and cannot be cross-compiled.
Fallback: embed the plugin as a normal Go struct using the Bifrost Go SDK behind a thin OpenAI-compatible HTTP handler we write (loses the stock gateway UI).
Trigger: the .so still fails to load after 90 minutes of version alignment.

## ADR-003: The agent is plain Python, not an agent framework
Why: the pipeline is fixed (search, normalise, fingerprint, prompt, validate). A framework adds moving parts without adding decisions.
Revisit if: the hackathon rules or judging criteria reward a specific agent framework. Check docs/HACKATHON_RULES.md.

## ADR-004: Cache key excludes sources; freshness is compared separately
Why: if sources were in the key, a new article would silently create a new entry and the old one would linger. Comparing fingerprints makes every refusal explainable by two dates.
Key = sha256(model | subject | intent | window_days | prompt_version). Bumping the prompt version invalidates everything on purpose.

## ADR-005: Default set policy is exact_set for the demo (ExactSet: true)
Why: `ExactSet: true` (`exact_set`) is the default policy for the demo because a same-day article swap or a corrected article headline (`set_hash` change) must trigger `REFUSE_SET_CHANGED` and force recomputation. A brief should never rely on stale evidence when headline corrections or source substitutions occur on the same day. Because live Tavily search results vary slightly call-to-call even without new news, cache `HIT` demonstrations must run in replay mode (`CHRONO_TAVILY_MODE=replay`) for deterministic repeatability.
Fallback: switch back to `newest_only` if live Tavily search jitter produces unwanted cache misses without any news movement during live ad-hoc runs.

## ADR-006: Tenant ID extracted strictly from Bifrost Virtual Key (Fail Closed)
Why: Cache isolation between teams/tenants must not be spoofable by clients. The cache key is `sha256(len_prefixed(tenant_id, model, subject, intent, window_days, prompt_version))`. `tenant_id` is extracted inside the Go plugin strictly from the authenticated Bifrost virtual key (`x-bf-vk` / Bifrost virtual key context) and NEVER from a client-supplied `x-chrono-*` header, preventing cross-team cache leakage.
Fail Closed Policy: If a request has no Bifrost virtual key (missing or empty), the plugin MUST NOT cache—it refuses lookup and does not store (`REFUSE_NO_TENANT` / fail closed). Never fall back to a shared `"default"` tenant, which would leak cached briefs across callers.
Fallback: Require valid Bifrost virtual key configuration in all environments where caching is active.
