---
trigger: glob
globs: "**/*.go, gateway/**"
description: Rules for the Bifrost freshness-cache Go plugin
---

# Go plugin rules

Spec: `.agents/skills/freshness-cache-spec/SKILL.md`. Verified API facts: `docs/BIFROST_RECON.md`.

- Package `main` with exported hook functions, built with `-buildmode=plugin`. A mistyped hook name silently disables the hook, so every build must pass the symbol check (`scripts/check_plugin_symbols.sh`) listing Init, GetName, HTTPTransportPreHook, PreLLMHook, PostLLMHook, HTTPTransportPostHook, Cleanup.
- Decision logic lives only in `internal/decide` (pure, tested, no Bifrost imports). Hooks translate Bifrost types to `decide` types and back. No cache rules inside hook bodies.
- Forbidden imports in the plugin: `net`, `net/http`, `os/exec`, `crypto/tls`, `plugin`. `scripts/check_invariants.py` fails the build if one appears.
- Hooks must never panic the gateway. Recover, log, and fall through to "recompute". A cache failure is a miss, never an error to the caller.
- Shared state: one `sync.RWMutex` around the store; bounded size with eviction; copy values in and out. Never return a pointer into the store.
- Do not mutate a cached response in place. Deep-copy before returning it as a short circuit, then tag the copy.
- PostLLMHook also runs for short-circuited responses. Mark served-from-cache on the context in PreLLMHook and skip storing in PostLLMHook when set.
- Treat every header as untrusted input: bound lengths, parse strictly (RFC3339 UTC), reject on failure with `REFUSE_NO_FINGERPRINT`.
- Call cost helpers synchronously inside the hook, not in a goroutine.
- Use `ctx.Log` for scoped logs. The audit file is separate, append-only JSONL, one `Write` per record under a mutex.
- Toolchain: the plugin and gateway use the same Go version and the same `github.com/maximhq/bifrost/core` version, pinned in `go.mod` and in the Dockerfile. Windows is unsupported for plugins; build in Docker.
- Tests: table-driven for decisions; one randomised invariant test (no stale serve) kept at 300k+ iterations; one integration test that drives MISS, HIT, REFUSE through the real gateway.
