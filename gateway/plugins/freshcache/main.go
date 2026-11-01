package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chronobrief/freshcache/internal/audit"
	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/chronobrief/freshcache/internal/headers"
	"github.com/chronobrief/freshcache/internal/key"
	"github.com/chronobrief/freshcache/internal/store"
	"github.com/maximhq/bifrost/core/schemas"
)

type pluginContextKey string

const (
	keyParsedHeaders pluginContextKey = "freshcache:headers"
	keyCacheKey      pluginContextKey = "freshcache:key"
	keyDecision      pluginContextKey = "freshcache:decision"
	keyServed        pluginContextKey = "freshcache:served"
	keyStartTime     pluginContextKey = "freshcache:start_time"
	keyHitEntry      pluginContextKey = "freshcache:hit_entry"
)

var (
	pluginMu     sync.RWMutex
	cacheStore   *store.Store
	auditLogger  *audit.Logger
	cachePolicy  decide.Policy
	isInitDone   bool
)

func initPlugin(config any) error {
	pluginMu.Lock()
	defer pluginMu.Unlock()

	auditPath := "/var/log/bifrost/freshcache-audit.jsonl"
	maxEntries := 1000
	ttl := 24 * time.Hour
	futureSkew := 24 * time.Hour
	exactSet := true

	if cfgMap, ok := config.(map[string]any); ok && cfgMap != nil {
		if ap, ok := cfgMap["audit_path"].(string); ok && ap != "" {
			auditPath = ap
		}
		if me, ok := cfgMap["max_entries"].(float64); ok && me > 0 {
			maxEntries = int(me)
		}
		if ttlSec, ok := cfgMap["ttl_seconds"].(float64); ok && ttlSec > 0 {
			ttl = time.Duration(ttlSec) * time.Second
		}
		if skewSec, ok := cfgMap["future_skew_seconds"].(float64); ok && skewSec > 0 {
			futureSkew = time.Duration(skewSec) * time.Second
		}
		if es, ok := cfgMap["exact_set"].(bool); ok {
			exactSet = es
		}
	}

	logger, err := audit.New(auditPath)
	if err != nil {
		// Log warning or fallback, do not crash gateway
		logger, _ = audit.New("")
	}
	auditLogger = logger
	cacheStore = store.New(maxEntries)
	cachePolicy = decide.Policy{
		TTL:        ttl,
		FutureSkew: futureSkew,
		ExactSet:   exactSet,
	}
	isInitDone = true
	return nil
}

// Init is called once by Bifrost when the plugin is loaded.
func Init(config any) error {
	return initPlugin(config)
}

// GetName returns the unique system identifier for this plugin.
func GetName() string {
	return "freshcache"
}

// HTTPTransportPreHook extracts and strictly parses the x-chrono-* headers and x-bf-vk tenant ID.
func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache HTTPTransportPreHook recovered from panic: %v\n", r)
		}
	}()

	parsed := headers.Parse(req)
	ctx.SetValue(keyParsedHeaders, parsed)
	ctx.SetValue(keyStartTime, time.Now())
	if parsed.TenantID == "" {
		ctx.SetValue(keyDecision, decide.RefuseNoTenant)
	}

	return nil, nil
}

// PreLLMHook evaluates the freshness decision. On HIT, it returns a deep copy short-circuit.
func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache PreLLMHook recovered from panic: %v\n", r)
		}
	}()

	parsed, ok := ctx.Value(keyParsedHeaders).(headers.RequestHeaders)
	if !ok {
		parsed = headers.RequestHeaders{}
	}

	// 1. Streaming bypass
	if req != nil && (req.RequestType == schemas.ChatCompletionStreamRequest || strings.Contains(strings.ToLower(string(req.RequestType)), "stream")) {
		ctx.SetValue(keyDecision, decide.BypassStream)
		return req, nil, nil
	}

	// 2. Missing Virtual Key -> Fail Closed (ADR-006)
	if parsed.TenantID == "" {
		ctx.SetValue(keyDecision, decide.RefuseNoTenant)
		return req, nil, nil
	}

	// 3. Client bypass (refresh)
	if parsed.Refresh {
		ctx.SetValue(keyDecision, decide.BypassRefresh)
		return req, nil, nil
	}

	// 4. Compute Cache Key
	_, model, _ := req.GetRequestFields()
	if model == "" && req.ChatRequest != nil {
		model = req.ChatRequest.Model
	}
	cacheK := key.Compute(parsed.TenantID, parsed.Subject, parsed.Intent, parsed.WindowDays, parsed.PromptVersion, model)
	ctx.SetValue(keyCacheKey, cacheK)

	// 5. Lookup Entry in Store
	pluginMu.RLock()
	st := cacheStore
	pol := cachePolicy
	pluginMu.RUnlock()

	var storedEntry *store.StoredEntry
	if st != nil {
		if e, found := st.Get(cacheK); found {
			storedEntry = e
		}
	}

	var decideEntry *decide.Entry
	if storedEntry != nil {
		decideEntry = &decide.Entry{
			FP:        storedEntry.Fingerprint,
			CreatedAt: storedEntry.CreatedAt,
		}
	}

	now := time.Now().UTC()
	decision := decide.Decide(now, parsed.Fingerprint, decideEntry, pol)
	ctx.SetValue(keyDecision, decision)

	// 6. Handle HIT
	if decision.Serves() && storedEntry != nil && storedEntry.Response != nil {
		ctx.SetValue(keyServed, true)
		ctx.SetValue(keyHitEntry, storedEntry)
		return req, &schemas.LLMPluginShortCircuit{Response: storedEntry.Response}, nil
	}

	return req, nil, nil
}

// PostLLMHook records audit logs and stores computed responses on MISS/REFUSE.
func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache PostLLMHook recovered from panic: %v\n", r)
		}
	}()

	parsed, _ := ctx.Value(keyParsedHeaders).(headers.RequestHeaders)
	cacheK, _ := ctx.Value(keyCacheKey).(string)
	dec, _ := ctx.Value(keyDecision).(decide.Decision)
	served, _ := ctx.Value(keyServed).(bool)
	startT, _ := ctx.Value(keyStartTime).(time.Time)

	latencyMS := int64(0)
	if !startT.IsZero() {
		latencyMS = time.Since(startT).Milliseconds()
	}

	pluginMu.RLock()
	st := cacheStore
	al := auditLogger
	pol := cachePolicy
	pluginMu.RUnlock()

	// 1. If served from cache (HIT)
	if served {
		hitE, _ := ctx.Value(keyHitEntry).(*store.StoredEntry)
		rec := audit.Record{
			Timestamp:            time.Now().UTC().Format(time.RFC3339),
			TenantID:             parsed.TenantID,
			CacheKey:             cacheK,
			Decision:             string(dec),
			RequestNewest:        parsed.Fingerprint.Newest.Format(time.RFC3339),
			RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
			RequestSetHash:       parsed.Fingerprint.SetHash,
			LatencyMS:            latencyMS,
		}
		if hitE != nil {
			rec.CachedNewest = hitE.Fingerprint.Newest.Format(time.RFC3339)
			rec.CachedCountAtNewest = hitE.Fingerprint.CountAtNewest
			rec.CachedSetHash = hitE.Fingerprint.SetHash
			rec.PromptTokens = hitE.PromptTokens
			rec.CompletionTokens = hitE.CompletionTokens
			rec.TotalTokens = hitE.TotalTokens
			rec.SavedTokens = hitE.TotalTokens
			rec.CostUSD = hitE.CostUSD
			rec.SavedCostUSD = hitE.CostUSD
		}
		if al != nil {
			al.Log(rec)
		}
		return resp, bifrostErr, nil
	}

	// 2. If response failed or empty, do not store
	if bifrostErr != nil || resp == nil || resp.ChatResponse == nil {
		rec := audit.Record{
			Timestamp:            time.Now().UTC().Format(time.RFC3339),
			TenantID:             parsed.TenantID,
			CacheKey:             cacheK,
			Decision:             string(dec),
			RequestNewest:        parsed.Fingerprint.Newest.Format(time.RFC3339),
			RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
			RequestSetHash:       parsed.Fingerprint.SetHash,
			LatencyMS:            latencyMS,
		}
		if al != nil {
			al.Log(rec)
		}
		return resp, bifrostErr, nil
	}

	// 3. Synchronous cost calculation & token accounting
	cost := ctx.CalculateCost(resp)
	pt, ct, tt := 0, 0, 0
	if resp.ChatResponse.Usage != nil {
		pt = resp.ChatResponse.Usage.PromptTokens
		ct = resp.ChatResponse.Usage.CompletionTokens
		tt = resp.ChatResponse.Usage.TotalTokens
	}

	// 4. Store newly generated response if eligible
	now := time.Now().UTC()
	if parsed.TenantID != "" && cacheK != "" && decide.Storable(now, parsed.Fingerprint, pol) {
		newEntry := &store.StoredEntry{
			Response:         resp,
			Fingerprint:      parsed.Fingerprint,
			CreatedAt:        now,
			CostUSD:          cost,
			PromptTokens:     pt,
			CompletionTokens: ct,
			TotalTokens:      tt,
		}
		if st != nil {
			st.Put(cacheK, newEntry)
		}
	}

	// 5. Append audit log
	rec := audit.Record{
		Timestamp:            now.Format(time.RFC3339),
		TenantID:             parsed.TenantID,
		CacheKey:             cacheK,
		Decision:             string(dec),
		RequestNewest:        parsed.Fingerprint.Newest.Format(time.RFC3339),
		RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
		RequestSetHash:       parsed.Fingerprint.SetHash,
		PromptTokens:         pt,
		CompletionTokens:     ct,
		TotalTokens:          tt,
		CostUSD:              cost,
		LatencyMS:            latencyMS,
	}
	if al != nil {
		al.Log(rec)
	}

	return resp, bifrostErr, nil
}

// HTTPTransportPostHook sets client-facing response headers (e.g. x-chrono-cache).
func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache HTTPTransportPostHook recovered from panic: %v\n", r)
		}
	}()

	if resp == nil {
		return nil
	}
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}

	dec, ok := ctx.Value(keyDecision).(decide.Decision)
	if !ok || dec == "" {
		dec = decide.MissCold
	}
	resp.Headers["x-chrono-cache"] = string(dec)

	if cacheK, ok := ctx.Value(keyCacheKey).(string); ok && cacheK != "" {
		resp.Headers["x-chrono-cache-key"] = cacheK
	}
	resp.Headers["x-chrono-served-at"] = time.Now().UTC().Format(time.RFC3339)

	if served, _ := ctx.Value(keyServed).(bool); served {
		if hitE, ok := ctx.Value(keyHitEntry).(*store.StoredEntry); ok && hitE != nil {
			resp.Headers["x-chrono-cached-newest"] = hitE.Fingerprint.Newest.Format(time.RFC3339)
			resp.Headers["x-chrono-cached-count"] = strconv.Itoa(hitE.Fingerprint.CountAtNewest)
			resp.Headers["x-chrono-saved-tokens"] = strconv.Itoa(hitE.TotalTokens)
			resp.Headers["x-chrono-saved-cost-usd"] = fmt.Sprintf("%.6f", hitE.CostUSD)
		}
	}

	return nil
}

// Cleanup flushes and closes open file descriptors on gateway shutdown.
func Cleanup() error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache Cleanup recovered from panic: %v\n", r)
		}
	}()

	pluginMu.Lock()
	defer pluginMu.Unlock()

	if auditLogger != nil {
		_ = auditLogger.Close()
		auditLogger = nil
	}
	return nil
}
