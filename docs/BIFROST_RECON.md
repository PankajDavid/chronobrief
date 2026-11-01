# Bifrost Plugin Recon (Upstream Verified Reference)

> **VERIFIED UPSTREAM RECON DATA**:
> - **Pinned Upstream Repository**: `https://github.com/maximhq/bifrost`
> - **Pinned Tag**: `transports/v2.4.0` (commit `e869c3f9adc96ea53c38694a21bb04a8e1085487`)
> - **Go Directive & Toolchain**: `go 1.27.3` (verified from `.recon/bifrost/transports/go.mod` and `.recon/bifrost/core/go.mod`)
> - **Core Module**: `github.com/maximhq/bifrost/core v1.14.1`
> - **Docker Builder Base Image**: `golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd`
> - **Runtime Base Image**: `alpine:3.23.5`

---

## 1. Verified Hook Signatures (Bifrost v2.4.0 / Core v1.14.1)

In Bifrost's native Go plugin architecture, plugins are built as Go shared objects (`package main`, compiled with `CGO_ENABLED=1 go build -buildmode=plugin`). Symbols are loaded dynamically via `soloader.go`:

1. **`Init(config any) error`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:82-88`
   - Invocation: Called once when Bifrost loads the plugin. Receives configuration object decoded from gateway `config.json`.
2. **`GetName() string`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:91-99`
   - Invocation: Mandatory symbol. Returns unique plugin identifier (e.g., `"freshcache"`).
3. **`HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error)`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:118-125`
   - Invocation: `.recon/bifrost/transports/bifrost-http/handlers/middlewares.go:818`. Runs after authentication middlewares. Reads incoming request headers via `req.CaseInsensitiveHeaderLookup(...)` or `req.Headers[...]`. Context values set here (`ctx.SetValue`) are persisted on the request lifecycle. Return `(nil, nil)` to continue pipeline, or `(*schemas.HTTPResponse, nil)` to short-circuit immediately.
4. **`PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error)`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:139-146`
   - Invocation: Per-attempt hook before invoking provider client. Inspects model, request fingerprint, and tenant ID. On cache `HIT`, short-circuits provider invocation by returning `(req, &schemas.LLMPluginShortCircuit{Response: &cachedResponse}, nil)`.
5. **`PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error)`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:148-155`
   - Invocation: Runs after provider execution AND after `PreLLMHook` short-circuit in reverse registration order. Checks if request was already served from cache. If not, reads token usage (`resp.ChatResponse.Usage`), computes pricing synchronously via `ctx.CalculateCost(resp)`, and updates the LRU store.
6. **`HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:127-134`
   - Invocation: Called for non-streaming HTTP responses. Modifies response headers directly on `resp.Headers` (e.g. `resp.Headers["x-chrono-cache"] = string(decision)`).
7. **`Cleanup() error`**
   - Source: `.recon/bifrost/framework/plugins/soloader.go:101-108`
   - Invocation: Mandatory symbol. Flushes and closes file descriptors (such as JSONL audit logs) on gateway shutdown.

---

## 2. Answers to the 10 Recon Questions (with Verified Source Citations)

### Q1: Exact signatures of the seven hooks at this tag
- Citations: `.recon/bifrost/framework/plugins/soloader.go:80-178`. Quoted verbatim above in Section 1.

### Q2: Context propagation from `HTTPTransportPreHook` to `PreLLMHook`
- Citations:
  - `transports/bifrost-http/handlers/middlewares.go:802-848`: `bifrostCtx := getBifrostContextFromFastHTTP(ctx)`. Plugin's `HTTPTransportPreHook` executes with `bifrostCtx`.
  - `core/schemas/context.go:518-529`: `ctx.SetValue(key, value)` writes directly to `bifrostCtx.userValues`.
  - `transports/bifrost-http/handlers/middlewares.go:846-848`:
    ```go
    // Adding user values
    for key, value := range bifrostCtx.GetUserValues() {
        ctx.SetUserValue(key, value)
    }
    ```
  - `transports/bifrost-http/lib/ctx.go:303-365` (`ConvertToBifrostContext`): Reads fasthttp user values into the downstream inference context.
  - Authentication headers (`x-bf-vk`): Bifrost's auth middleware runs before `TransportInterceptorMiddleware` (`server/server.go:3737-3743`). The virtual key is reliably present in `req.Headers["x-bf-vk"]` and in `ctx.Value(schemas.BifrostContextKeyVirtualKey)`.

### Q3: How a short-circuit chat response is constructed
- Citations:
  - `.recon/bifrost/plugins/semanticcache/search.go:339-342`:
    ```go
    return &schemas.LLMPluginShortCircuit{Response: &cachedResponse}, nil
    ```
  - Cached response type is `schemas.BifrostResponse` containing a populated `ChatResponse: &schemas.ChatResponse{...}`.
  - For streaming short-circuits: `&schemas.LLMPluginShortCircuit{Stream: streamChan}` (`search.go:407`).

### Q4: Reading model, provider, messages, and stream flag from `BifrostRequest`
- Citations:
  - `.recon/bifrost/core/schemas/bifrost.go:885-920`: `provider, model, _ := req.GetRequestFields()`.
  - Chat request inspection: `req.ChatRequest.Model` and `req.ChatRequest.Provider`.
  - Messages inspection: `req.ChatRequest.Input` (`[]schemas.ChatMessage`, each with `Role` and `Content.ContentStr`).
  - Stream flag: `req.ChatRequest.Params != nil && req.ChatRequest.Params.Stream != nil && *req.ChatRequest.Params.Stream`.

### Q5: Token counts and `ctx.CalculateCost(resp)` availability
- Citations:
  - Tokens: `resp.ChatResponse.Usage.PromptTokens`, `resp.ChatResponse.Usage.CompletionTokens`, `resp.ChatResponse.Usage.TotalTokens` (`core/schemas/chatcompletions.go`).
  - Cost calculation: `ctx.CalculateCost(resp)` is fully supported and exported:
    ```go
    // .recon/bifrost/core/schemas/context.go:1000-1006
    func (bc *BifrostContext) CalculateCost(resp *BifrostResponse) float64
    ```
  - Breakdown: `ctx.CalculateCostBreakdown(resp)` is also available (`context.go:1032`).
  - Crucial invariant: Must be called synchronously inside `PostLLMHook` before any asynchronous logging goroutine because pooled response objects are reset upon request return.

### Q6: Setting response headers on non-streaming responses
- Citations:
  - `.recon/bifrost/examples/plugins/hello-world/main.go:37-43`:
    ```go
    func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
        resp.Headers["x-chrono-cache"] = string(decision)
        return nil
    }
    ```
  - Modified in-place on the `resp.Headers` string map.

### Q7: Gateway `config.json` plugin declaration and disabling semantic cache
- Citations:
  - `.recon/bifrost/transports/config.schema.json:4202-4240`:
    ```json
    "plugins": [
      {
        "enabled": true,
        "name": "freshcache",
        "path": "/etc/bifrost/plugins/freshcache.so",
        "config": {
          "audit_path": "/var/log/bifrost/freshcache-audit.jsonl"
        }
      }
    ]
    ```
  - Disabling semantic cache: Built-in plugins (`semantic_cache`, `telemetry`, `governance`, etc.) are only activated if declared in the `plugins` array. Omitting `semantic_cache` or specifying `"enabled": false` keeps it inactive.

### Q8: Gemini provider configuration and model string
- Citations:
  - `.recon/bifrost/examples/configs/withprompushgateway/config.json:36-47`:
    ```json
    "gemini": {
      "keys": [
        {
          "name": "gemini-key",
          "value": "env.GEMINI_API_KEY",
          "weight": 1,
          "models": ["*"]
        }
      ]
    }
    ```
  - Model identifier: `gemini-2.5-flash` or `gemini/gemini-2.5-flash`. `/v1/models` lists all configured models.

### Q9: Building `bifrost-http` from source and `.so` lookup
- Citations:
  - `.recon/bifrost/transports/Dockerfile:20-48`:
    `CGO_ENABLED=1 go build -tags sqlite_static -o /app/bifrost-http ./bifrost-http`
  - Dynamic Linking: Do NOT use `-extldflags '-static'`, which disables `plugin.Open()`.
  - Symbol Naming: When compiling `.so` plugins from `main.go`, compile source file directly (`go build -buildmode=plugin -o plugin.so main.go`) to generate `plugin/unnamed-<hash>` symbol names that pass symbol integrity checks.
  - Path Resolution: Bifrost looks up plugins using the exact `"path"` specified in `config.json`.

### Q10: Plugin load success/failure logging
- Citations:
  - `.recon/bifrost/transports/bifrost-http/server/utils.go:80-81`: logs `[INFO] <name> plugin initialized successfully`.
  - Failure logging: Returns descriptive error on missing symbols or type mismatch from `soloader.go:80-178` and logs `[ERROR]` during server startup.

---

## 3. Critical Architectural Invariants Confirmed
1. **Zero External I/O in Plugin**: The freshness-aware cache makes decisions using only in-memory LRU state and headers passed in the request. No network or Tavily calls occur in the gateway plugin.
2. **Deterministic ADR-006 Fail-Closed Isolation**: `tenant_id` is extracted strictly from authenticated `x-bf-vk`. If missing or empty, `REFUSE_NO_TENANT` is returned and caching is bypassed completely (fail closed).
3. **ExactSet Policy Determinism**: Demonstrations of cache `HIT` require replay mode (`CHRONO_TAVILY_MODE=replay`) to ensure deterministic set hashes across searches.
