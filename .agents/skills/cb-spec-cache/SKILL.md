---
name: cb-spec-cache
description: Manual command. Run only when the user types /cb-spec-cache. Freezes the concrete cache specification against the verified Bifrost API so plugin code can be written without guessing.
---

# /cb-spec-cache

Preconditions: `docs/BIFROST_RECON.md` and `docs/TAVILY_PROBE.md` exist. If not, stop and say run `/cb-recon`.

1. Read the `freshness-cache-spec` skill, `docs/ARCHITECTURE.md`, `docs/BIFROST_RECON.md`, `internal/decide/decide.go` and its tests.
2. Write `docs/SPEC_CACHE.md`, concrete and short, containing:
   - the final header names and the exact parsing rules (RFC3339 UTC, integer bounds, maximum lengths);
   - for each hook: the exact verified signature and what it reads and writes on the context, using the types named in the recon;
   - how the short-circuit response is constructed, with the recon's file reference;
   - how token usage and cost are read when storing; if cost calculation is unavailable at the pinned version, say the cost fields are recorded as 0 and flagged `cost_unavailable`, and list that under limits;
   - the plugin's `config.json` block with every option (ttl, max entries, audit path, set policy, future skew) and its default, in the form the recon verified;
   - how the built-in semantic cache is kept out of the way;
   - the audit record fields and file rotation policy (none, the file is append-only and bounded by the demo);
   - a deviations section: anything that differs from the skill, with the reason.
3. Re-run `make go-test`. It must still pass; if the verified API forces a change to `decide`, change code and tests together and say why.
4. Verify the Python fingerprint headers match the spec field for field.
5. Present the spec and stop for approval. Next: `/cb-build-plugin`.
