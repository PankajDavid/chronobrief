---
name: bifrost-plugin-recon
description: Establishes verified facts about the Bifrost gateway plugin API at a pinned version before any plugin code is written. Use when starting, changing, or debugging the freshcache plugin, the gateway config, or anything that calls a Bifrost type or hook.
---

# Bifrost plugin recon

Goal: replace assumptions with quoted, versioned facts in `docs/BIFROST_RECON.md`. No plugin code until that file exists and the hello-world load proof (step 5) has passed.

## What was documented when this pack was written (re-verify, do not trust)
- Plugins are native Go shared objects (`go build -buildmode=plugin`), Linux or macOS only, not Windows; no cross-compiling.
- The plugin, the gateway binary and every shared dependency must be built with the same Go toolchain and the same `github.com/maximhq/bifrost/core` version. A mismatch fails at load with "plugin was built with a different version of package".
- Documented exports include `Init(config any) error`, `GetName() string`, `HTTPTransportPreHook`, `PreLLMHook`, `PostLLMHook`, `HTTPTransportPostHook`, `Cleanup() error`. Only `GetName` and `Cleanup` are mandatory for loading, and a typo in any other hook name silently disables it.
- `PreLLMHook` can short-circuit with a synthetic response; `PostLLMHook` runs after a response or after a short-circuit. HTTP transport hooks run only under the HTTP gateway.
- Docs: https://docs.getbifrost.ai/plugins/writing-plugin (index: https://docs.getbifrost.ai/llms.txt). Examples: `examples/plugins/hello-world` and `plugins/semanticcache` in https://github.com/maximhq/bifrost.

## Procedure
1. **Pick and pin the version.** Open the repo's latest release. Record the tag, the Go version its `go.mod` requires, and the `core` module version. Write all three at the top of `docs/BIFROST_RECON.md`.
2. **Clone at the tag** into `.recon/bifrost` (git-ignored): `git clone --depth 1 --branch <tag> https://github.com/maximhq/bifrost .recon/bifrost`.
3. **Answer these questions**, each with a file path and line range or a doc URL, copied not paraphrased:
   1. Exact signatures of the seven hooks at this tag.
   2. Does a value set with `ctx.SetValue` in `HTTPTransportPreHook` reach `PreLLMHook`? Show the code path.
   3. How is a short-circuit chat response built (`LLMPluginShortCircuit`, which field holds the chat response, which `ExtraFields` the caller expects)? Read how `plugins/semanticcache` does it.
   4. How do you read model, provider and messages from a `BifrostRequest` (`GetRequestFields`)?
   5. Where do prompt and completion token counts live on a chat response, and is `ctx.CalculateCost(resp)` available at this tag?
   6. How are response headers set on a non-streaming response from a plugin?
   7. How is a plugin declared in the gateway `config.json` (fields, how `config` reaches `Init`, ordering/placement), and how is the built-in semantic cache disabled or kept out of the way?
   8. How is the Gemini provider configured and what exact model string do you send? Where does `/v1/models` list it?
   9. How is `bifrost-http` built from source (path, build tags, CGO requirement) and where does it look for `.so` files?
   10. Does the gateway log plugin load success and failure, and at what log level?
4. **Record contradictions.** Where the docs and the source disagree, the source at the pinned tag wins; write both.
5. **Hello-world load proof** (this is the gate). Build the upstream hello-world plugin and the gateway in one Docker build (see `plugin-build-and-load`), start it, send one `/v1/chat/completions` request, and paste the log lines showing each hook fired. Save to `docs/evidence/hello-world-load.txt`.
6. **Write the fallback trigger time.** Note the timestamp when step 5 started. If it is not passing 90 minutes later, take the ADR-002 fallback in `docs/DECISIONS.md` and tell the user.

## Output file shape
`docs/BIFROST_RECON.md`: pinned versions, the ten answers with citations, contradictions, load-proof log excerpt, open questions. Mark anything you could not verify as `UNVERIFIED`.
