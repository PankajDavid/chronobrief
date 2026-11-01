package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	keyReqID                          = "freshcache:req_id"
)

type reqOutcome struct {
	decision  decide.Decision
	cacheKey  string
	served    bool
	hitEntry  *store.StoredEntry
	startTime time.Time
	createdAt time.Time
}

var (
	pluginMu    sync.RWMutex
	cacheStore  *store.Store
	auditLogger *audit.Logger
	cachePolicy decide.Policy
	isInitDone  bool

	reqCounter  uint64
	reqOutcomes sync.Map // map[string]*reqOutcome
)

const (
	reqOutcomeTTL  = 2 * time.Minute
	maxReqOutcomes = 5000
)

// pruneOldReqOutcomes removes reqOutcome entries older than reqOutcomeTTL,
// and caps the map size at maxReqOutcomes to prevent unbounded memory growth (F4).
func pruneOldReqOutcomes(now time.Time) {
	cutoff := now.Add(-reqOutcomeTTL)
	count := 0
	reqOutcomes.Range(func(k, v any) bool {
		count++
		if o, ok := v.(*reqOutcome); ok {
			if o.createdAt.Before(cutoff) {
				reqOutcomes.Delete(k)
				count--
			}
		}
		return true
	})
	if count > maxReqOutcomes {
		excess := count - maxReqOutcomes
		reqOutcomes.Range(func(k, v any) bool {
			if excess <= 0 {
				return false
			}
			reqOutcomes.Delete(k)
			excess--
			return true
		})
	}
}

func initPlugin(config any) error {
	pluginMu.Lock()
	defer pluginMu.Unlock()

	auditPath := "/var/log/bifrost/freshcache.jsonl"
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
	now := time.Now()
	ctx.SetValue(keyParsedHeaders, parsed)
	ctx.SetValue(keyStartTime, now)

	// Always generate request ID server-side; ignore client headers (F4)
	id := atomic.AddUint64(&reqCounter, 1)
	reqID := fmt.Sprintf("fc-%d-%d", now.UnixNano(), id)
	if req != nil {
		if req.Headers == nil {
			req.Headers = make(map[string]string)
		}
		req.Headers["x-freshcache-id"] = reqID
	}
	ctx.SetValue(keyReqID, reqID)

	// Prune old entries to prevent unbounded memory growth (F4)
	pruneOldReqOutcomes(now)

	initialOutcome := &reqOutcome{
		startTime: now,
		createdAt: now,
	}
	if parsed.TenantID == "" {
		ctx.SetValue(keyDecision, decide.RefuseNoTenant)
		initialOutcome.decision = decide.RefuseNoTenant
	}
	reqOutcomes.Store(reqID, initialOutcome)

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

	reqID, _ := ctx.Value(keyReqID).(string)
	var outcome *reqOutcome
	if reqID != "" {
		if v, ok := reqOutcomes.Load(reqID); ok {
			outcome = v.(*reqOutcome)
		}
	}
	if outcome == nil {
		cutoff := time.Now().Add(-10 * time.Second)
		count := 0
		reqOutcomes.Range(func(k, v any) bool {
			o := v.(*reqOutcome)
			if o.createdAt.After(cutoff) {
				outcome = o
				count++
			}
			return true
		})
		if count != 1 {
			outcome = &reqOutcome{createdAt: time.Now(), startTime: time.Now()}
			if reqID != "" {
				reqOutcomes.Store(reqID, outcome)
			}
		}
	}

	// 1. Streaming bypass
	if req != nil && (req.RequestType == schemas.ChatCompletionStreamRequest || strings.Contains(strings.ToLower(string(req.RequestType)), "stream")) {
		ctx.SetValue(keyDecision, decide.BypassStream)
		outcome.decision = decide.BypassStream
		return req, nil, nil
	}

	// 2. Missing Virtual Key -> Fail Closed (ADR-006)
	if parsed.TenantID == "" {
		ctx.SetValue(keyDecision, decide.RefuseNoTenant)
		outcome.decision = decide.RefuseNoTenant
		return req, nil, nil
	}

	// 3. Compute Cache Key (needed for lookup AND for storing refresh results in PostLLMHook)
	_, model, _ := req.GetRequestFields()
	if model == "" && req.ChatRequest != nil {
		model = req.ChatRequest.Model
	}
	cacheK := key.Compute(parsed.TenantID, parsed.Subject, parsed.Intent, parsed.WindowDays, parsed.PromptVersion, model)
	ctx.SetValue(keyCacheKey, cacheK)
	outcome.cacheKey = cacheK

	// 4. Client bypass (refresh): skip lookup, proceed to model, store result in PostLLMHook
	if parsed.Refresh {
		ctx.SetValue(keyDecision, decide.BypassRefresh)
		outcome.decision = decide.BypassRefresh
		return req, nil, nil
	}
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
	outcome.decision = decision

	// 6. Handle HIT
	if decision.Serves() && storedEntry != nil && storedEntry.Response != nil {
		ctx.SetValue(keyServed, true)
		ctx.SetValue(keyHitEntry, storedEntry)
		outcome.served = true
		outcome.hitEntry = storedEntry
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
		reqNewest := ""
		if !parsed.Fingerprint.Newest.IsZero() {
			reqNewest = parsed.Fingerprint.Newest.Format(time.RFC3339)
		}
		rec := audit.Record{
			Timestamp:            time.Now().UTC().Format(time.RFC3339),
			TenantID:             parsed.TenantID,
			CacheKey:             cacheK,
			Decision:             string(dec),
			RequestNewest:        reqNewest,
			IncomingNewest:       reqNewest,
			RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
			IncomingCount:        parsed.Fingerprint.CountAtNewest,
			RequestSetHash:       parsed.Fingerprint.SetHash,
			LatencyMS:            latencyMS,
		}
		if hitE != nil {
			cachedNewest := ""
			if !hitE.Fingerprint.Newest.IsZero() {
				cachedNewest = hitE.Fingerprint.Newest.Format(time.RFC3339)
			}
			rec.CachedNewest = cachedNewest
			rec.EntryNewest = cachedNewest
			rec.CachedCountAtNewest = hitE.Fingerprint.CountAtNewest
			rec.EntryCount = hitE.Fingerprint.CountAtNewest
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
		reqNewest := ""
		if !parsed.Fingerprint.Newest.IsZero() {
			reqNewest = parsed.Fingerprint.Newest.Format(time.RFC3339)
		}
		rec := audit.Record{
			Timestamp:            time.Now().UTC().Format(time.RFC3339),
			TenantID:             parsed.TenantID,
			CacheKey:             cacheK,
			Decision:             string(dec),
			RequestNewest:        reqNewest,
			IncomingNewest:       reqNewest,
			RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
			IncomingCount:        parsed.Fingerprint.CountAtNewest,
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
	reqNewest := ""
	if !parsed.Fingerprint.Newest.IsZero() {
		reqNewest = parsed.Fingerprint.Newest.Format(time.RFC3339)
	}
	rec := audit.Record{
		Timestamp:            now.Format(time.RFC3339),
		TenantID:             parsed.TenantID,
		CacheKey:             cacheK,
		Decision:             string(dec),
		RequestNewest:        reqNewest,
		IncomingNewest:       reqNewest,
		RequestCountAtNewest: parsed.Fingerprint.CountAtNewest,
		IncomingCount:        parsed.Fingerprint.CountAtNewest,
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

	reqID, _ := ctx.Value(keyReqID).(string)

	var outcome *reqOutcome
	if reqID != "" {
		if v, ok := reqOutcomes.LoadAndDelete(reqID); ok {
			outcome = v.(*reqOutcome)
		}
	}
	if outcome == nil {
		var matchedKey any
		cutoff := time.Now().Add(-10 * time.Second)
		count := 0
		reqOutcomes.Range(func(k, v any) bool {
			o := v.(*reqOutcome)
			if o.createdAt.After(cutoff) {
				outcome = o
				matchedKey = k
				count++
			}
			return true
		})
		if count == 1 && matchedKey != nil {
			reqOutcomes.Delete(matchedKey)
		} else if count > 1 {
			outcome = nil
		}
	}

	dec := decide.MissCold
	var cacheK string
	var hitE *store.StoredEntry
	served := false

	if outcome != nil {
		if outcome.decision != "" {
			dec = outcome.decision
		}
		cacheK = outcome.cacheKey
		hitE = outcome.hitEntry
		served = outcome.served
	} else {
		if d, ok := ctx.Value(keyDecision).(decide.Decision); ok && d != "" {
			dec = d
		}
		if k, ok := ctx.Value(keyCacheKey).(string); ok {
			cacheK = k
		}
		if s, ok := ctx.Value(keyServed).(bool); ok {
			served = s
		}
		if h, ok := ctx.Value(keyHitEntry).(*store.StoredEntry); ok {
			hitE = h
		}
	}

	resp.Headers["x-chrono-cache"] = string(dec)
	if cacheK != "" {
		resp.Headers["x-chrono-cache-key"] = cacheK
	}
	resp.Headers["x-chrono-served-at"] = time.Now().UTC().Format(time.RFC3339)

	if served && hitE != nil {
		resp.Headers["x-chrono-cached-newest"] = hitE.Fingerprint.Newest.Format(time.RFC3339)
		resp.Headers["x-chrono-cached-count"] = strconv.Itoa(hitE.Fingerprint.CountAtNewest)
		resp.Headers["x-chrono-saved-tokens"] = strconv.Itoa(hitE.TotalTokens)
		resp.Headers["x-chrono-saved-cost-usd"] = fmt.Sprintf("%.6f", hitE.CostUSD)
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
