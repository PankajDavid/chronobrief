# Bifrost Plugin Recon (Pre-Seeded Reference & Live Verification Checklist)

> **CRITICAL RECON NOTICE**:
> Upstream tags `transports/v2.4.0` and `core v1.14.1` exist on `maximhq/bifrost`, but the exact `go` directive and toolchain version are **UNVERIFIED** until `/cb-recon` (`T02`) clones `maximhq/bifrost` at `transports/v2.4.0` and reads its `go.mod` verbatim.
> The plugin and Docker build MUST match Bifrost's `go.mod` toolchain version exactly.

## 1. Verified Hook Signatures (Bifrost v1.6+ / v2.0 HTTP Transport)
In Bifrost's native Go plugin architecture (`package main`, built with `CGO_ENABLED=1 go build -buildmode=plugin`), symbols are looked up in the `.so` shared object:

- `func Init(config any) error` — Called once when Bifrost loads the plugin with the configuration map from `config.json`.
- `func GetName() string` — Mandatory. Returns the plugin identifier (e.g., `"freshcache"`).
- `func HTTPTransportPreAuthHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error)` — Optional in `.so` plugins (v2.0+ runs before auth; skipped if not exported).
- `func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error)` — Runs on HTTP requests after authentication. Read incoming headers via `req.CaseInsensitiveHeaderLookup("x-chrono-newest")` and store parsed struct values on `ctx` via `ctx.SetValue(schemas.BifrostContextKey("chrono-fp"), fp)`. Return `(nil, nil)` to continue.
- `func PreRequestHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) error` — Optional routing phase hook (v1.6.x+).
- `func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error)` — Per-attempt hook before the provider call. Can short-circuit on cache `HIT` by returning `(req, &schemas.LLMPluginShortCircuit{Response: cachedRespCopy}, nil)` and setting `ctx.SetValue(schemas.BifrostContextKey("chrono-served"), true)`.
- `func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error)` — Runs after provider call AND after `PreLLMHook` short-circuits (in reverse registration order). Check if `"chrono-served"` is set on `ctx`; if not, and `bifrostErr == nil` and response is valid, compute cost synchronously via `cost := ctx.CalculateCost(resp)` (v1.6.7+) and store in cache.
- `func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error` — Called for non-streaming HTTP responses. Set response headers on `resp.Headers["x-chrono-cache"] = string(decision)`, etc.
- `func Cleanup() error` — Mandatory. Flushes/closes audit file handle on shutdown.

## 2. Critical Build & Runtime Gotchas
1. **Dynamic Binary Requirement**: Stock prebuilt Bifrost binaries may be statically linked. To load `.so` plugins reliably, build `bifrost-http` AND `freshcache.so` inside the **same multi-stage Docker container** with `CGO_ENABLED=1` and the exact same `go.mod` `github.com/maximhq/bifrost/core` version.
2. **PostLLMHook Short-Circuit Guarantee**: In Bifrost, all executed `PreLLMHook`s receive corresponding `PostLLMHook` calls even when short-circuited. Always mark cache hits on `ctx` in `PreLLMHook` so `PostLLMHook` does not re-store the cached copy or double-log.
3. **Synchronous Cost Calculation**: `ctx.CalculateCost(resp)` (or `ctx.CalculateCostBreakdown(resp)` in v1.10.2+) must be called synchronously inside `PostLLMHook` before any async goroutine, because pooled context/response objects are reset after request completion.

## 3. Recon Target Pins & Architecture Confirmation
- **Target Upstream Tag**: `maximhq/bifrost` tag `transports/v2.4.0` (to be verified in T02)
- **Target Core Version**: `github.com/maximhq/bifrost/core v1.14.1` (to be verified in T02)
- **Go Toolchain**: UNVERIFIED — will be determined by reading `go.mod` of `maximhq/bifrost` at `transports/v2.4.0` during T02
- **Hook Lifecycle & Context Propagation**:
  - `HTTPTransportPreHook` (parses incoming `x-chrono-*` headers, injects parsed fingerprint into `ctx.SetValue`)
  - `PreLLMHook` (evaluates `decide.Decide`; on `HIT`, short-circuits via `&schemas.LLMPluginShortCircuit{Response: cached}`)
  - `PostLLMHook` (on miss, extracts `resp.ChatResponse.Usage`, runs synchronous `ctx.CalculateCost(resp)`, updates store)
  - `HTTPTransportPostHook` (attaches response headers `x-chrono-cache`, `x-chrono-saved-tokens`, etc.)
- **Cost Calculation**: Synchronous `ctx.CalculateCost(resp)` within `PostLLMHook` ensures memory-safe accounting before pooled object reuse.
- **Usage Metrics**: Extracted from `resp.ChatResponse.Usage` (`PromptTokens`, `CompletionTokens`, `TotalTokens`).
- **Configured Runtime Model**: `CHRONO_MODEL=gemini-2.5-flash`

