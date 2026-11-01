---
name: cb-build-plugin
description: Manual command. Run only when the user types /cb-build-plugin. Implements the freshcache Go plugin test-first on top of the verified decision logic, then proves the .so builds and exports every hook.
---

# /cb-build-plugin

Preconditions: `docs/SPEC_CACHE.md` approved. Rules in `.agents/rules/20-go-plugin.md` apply to every file you touch. Work in `gateway/plugins/freshcache/`.

Each step ends with a command whose output you quote. Do not start the next step on red.

1. **Baseline.** `make go-test` is green.
2. **Module pins.** Set the `go` directive and the `github.com/maximhq/bifrost/core` requirement in `go.mod` to the versions in `docs/BIFROST_RECON.md`. `internal/decide` must still compile without Bifrost imports.
3. **Headers** (`headers.go`, package main or an internal package without Bifrost types): parse the `x-chrono-*` headers into `decide.Fingerprint` plus key inputs. Table tests: valid, all empty, missing one, oversized, bad RFC3339, negative counts, non-numeric counts, mode values. Invalid input yields an empty fingerprint, never an error.
4. **Key** (`key.go`): `sha256(model|subject|intent|window|prompt_version)` with an unambiguous separator and length-prefixing so `a|b` cannot collide with `a` + `|b`. Tests: stable, changes with each field, no collision on separator injection.
5. **Store** (`store.go`): map plus LRU order, `sync.RWMutex`, `MaxEntries`, copy-in and copy-out of the response bytes or struct as the recon dictates. Tests with `-race`: concurrent get and put, eviction order, returned value mutation does not change the stored one.
6. **Audit** (`audit.go`): append-only JSONL, one mutex, one write per record, fields from `docs/ARCHITECTURE.md`. Tests: 50 concurrent writers produce 50 valid lines, no interleaving; unwritable path does not panic and is reported once in logs.
7. **Hooks** (`main.go`): keep thin. Each hook converts Bifrost types to plain values, calls tested functions, converts back. Recover from panics and fall through to recompute. Implement exactly the flow in the `freshness-cache-spec` skill: stream bypass, refresh mode, served-from-cache flag so PostLLMHook never re-stores, not-storing on error or empty content, response headers.
8. **Guards.** `go vet ./...`, `go test -race ./internal/... ` plus any package without Bifrost imports, `make invariants` (purity check), and `gofmt -l .` must print nothing.
9. **Build.** In Docker (see `plugin-build-and-load`), build the plugin with the gateway and run `scripts/check_plugin_symbols.sh` on the `.so`. Paste its output. Seven `ok` lines are required.

Stop conditions: step 9 fails to load after 90 minutes total on load problems; or the verified API cannot support a flow in the spec. In either case stop, record it in `docs/DECISIONS.md`, and offer the ADR-002 fallback.

Done when: all nine outputs are quoted and `git log` shows one commit per step.
