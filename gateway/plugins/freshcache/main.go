package main

import (
	"encoding/json"
	"fmt"
	"sort"
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
	decision       decide.Decision
	cacheKey       string
	served         bool
	hitEntry       *store.StoredEntry
	startTime      time.Time
	createdAt      time.Time
	hookOverheadUS int64
	shadowOutcomes decide.ShadowOutcomes
	guardAction    headers.GuardAction
	entryNewest    string
	entryCount     int
}

var (
	pluginMu    sync.RWMutex
	cacheStore  *store.Store
	auditLogger *audit.Logger
	cachePolicy decide.Policy
	isInitDone  bool

	pluginStartTime       time.Time
	snapshotPath          string
	snapshotEntriesLoaded int
	snapshotErrors        uint64

	reqCounter  uint64
	reqOutcomes sync.Map // map[string]*reqOutcome

	lookupsTotal        uint64
	hitsTotal           uint64
	stalePreventedTotal uint64
	courtTTL24hStale    uint64
	courtTTL1hStale     uint64
	courtTTL5mMisses    uint64

	totalOverheadUS uint64
	overheadCount   uint64
	maxOverheadUS   int64
)

const (
	reqOutcomeTTL  = 2 * time.Minute
	maxReqOutcomes = 5000
)

// pruneOldReqOutcomes removes reqOutcome entries older than reqOutcomeTTL,
// and caps the map size at maxReqOutcomes to prevent unbounded memory growth (F4).
// When over maxReqOutcomes, the oldest entries are evicted first.
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
		type outcomeItem struct {
			key       any
			createdAt time.Time
		}
		items := make([]outcomeItem, 0, count)
		reqOutcomes.Range(func(k, v any) bool {
			if o, ok := v.(*reqOutcome); ok {
				items = append(items, outcomeItem{key: k, createdAt: o.createdAt})
			}
			return true
		})
		if len(items) > maxReqOutcomes {
			sort.Slice(items, func(i, j int) bool {
				return items[i].createdAt.Before(items[j].createdAt)
			})
			toRemove := len(items) - maxReqOutcomes
			for i := 0; i < toRemove; i++ {
				reqOutcomes.Delete(items[i].key)
			}
		}
	}
}

type metricsPayload struct {
	Status                    string  `json:"status"`
	UptimeSeconds             int64   `json:"uptime_seconds"`
	LookupsTotal              uint64  `json:"lookups_total"`
	HitsTotal                 uint64  `json:"hits_total"`
	StalePreventedTotal       uint64  `json:"stale_prevented_total"`
	CourtStalePreventedTTL24h uint64  `json:"court_stale_prevented_ttl_24h"`
	CourtStalePreventedTTL1h  uint64  `json:"court_stale_prevented_ttl_1h"`
	CourtTTL5mFalseMisses     uint64  `json:"court_ttl_5m_false_misses"`
	OverheadUSAvg             float64 `json:"overhead_us_avg"`
	OverheadUSMax             int64   `json:"overhead_us_max"`
	CacheEntries              int     `json:"cache_entries"`
	CacheBytes                int64   `json:"cache_bytes"`
	CacheMaxBytes             int64   `json:"cache_max_bytes"`
	CacheEvictions            int64   `json:"cache_evictions"`
	SnapshotPath              string  `json:"snapshot_path"`
	SnapshotEntriesLoaded     int     `json:"snapshot_entries_loaded"`
	SnapshotErrors            uint64  `json:"snapshot_errors"`
}

func getMetricsJSON() []byte {
	pluginMu.RLock()
	st := cacheStore
	sp := snapshotPath
	sl := snapshotEntriesLoaded
	pluginMu.RUnlock()

	entries := 0
	bytes := int64(0)
	maxBytes := int64(0)
	evictions := int64(0)
	if st != nil {
		entries = st.Len()
		bytes = st.CurrentBytes()
		maxBytes = st.MaxBytes()
		evictions = st.Evictions()
	}

	totOverhead := atomic.LoadUint64(&totalOverheadUS)
	cnt := atomic.LoadUint64(&overheadCount)
	avgOverhead := 0.0
	if cnt > 0 {
		avgOverhead = float64(totOverhead) / float64(cnt)
	}

	uptime := int64(0)
	if !pluginStartTime.IsZero() {
		uptime = int64(time.Since(pluginStartTime).Seconds())
	}

	payload := metricsPayload{
		Status:                    "healthy",
		UptimeSeconds:             uptime,
		LookupsTotal:              atomic.LoadUint64(&lookupsTotal),
		HitsTotal:                 atomic.LoadUint64(&hitsTotal),
		StalePreventedTotal:       atomic.LoadUint64(&stalePreventedTotal),
		CourtStalePreventedTTL24h: atomic.LoadUint64(&courtTTL24hStale),
		CourtStalePreventedTTL1h:  atomic.LoadUint64(&courtTTL1hStale),
		CourtTTL5mFalseMisses:     atomic.LoadUint64(&courtTTL5mMisses),
		OverheadUSAvg:             avgOverhead,
		OverheadUSMax:             atomic.LoadInt64(&maxOverheadUS),
		CacheEntries:              entries,
		CacheBytes:                bytes,
		CacheMaxBytes:             maxBytes,
		CacheEvictions:            evictions,
		SnapshotPath:              sp,
		SnapshotEntriesLoaded:     sl,
		SnapshotErrors:            atomic.LoadUint64(&snapshotErrors),
	}

	data, _ := json.MarshalIndent(payload, "", "  ")
	return data
}

func recordOverhead(overheadUS int64) {
	if overheadUS <= 0 {
		return
	}
	atomic.AddUint64(&totalOverheadUS, uint64(overheadUS))
	atomic.AddUint64(&overheadCount, 1)
	for {
		currMax := atomic.LoadInt64(&maxOverheadUS)
		if overheadUS <= currMax || atomic.CompareAndSwapInt64(&maxOverheadUS, currMax, overheadUS) {
			break
		}
	}
}

func initPlugin(config any) error {
	pluginMu.Lock()
	defer pluginMu.Unlock()

	auditPath := "/var/log/bifrost/freshcache.jsonl"
	snapPath := "/var/log/bifrost/freshcache-snapshot.json"
	maxEntries := 1000
	maxBytes := int64(64 * 1024 * 1024) // 64 MB
	ttl := 24 * time.Hour
	futureSkew := 24 * time.Hour
	exactSet := true

	if cfgMap, ok := config.(map[string]any); ok && cfgMap != nil {
		if ap, ok := cfgMap["audit_path"].(string); ok && ap != "" {
			auditPath = ap
		}
		if sp, ok := cfgMap["snapshot_path"].(string); ok && sp != "" {
			snapPath = sp
		}
		if me, ok := cfgMap["max_entries"].(float64); ok && me > 0 {
			maxEntries = int(me)
		}
		if mb, ok := cfgMap["max_bytes"].(float64); ok && mb > 0 {
			maxBytes = int64(mb)
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
	if cacheStore != nil {
		if err := cacheStore.FlushSnapshot(""); err != nil {
			fmt.Printf("[ERROR] freshcache FlushSnapshot on re-init failed: %v\n", err)
		}
	}
	cacheStore = store.New(maxEntries, maxBytes)
	cacheStore.SetTTL(ttl)
	cacheStore.SetSnapshotErrorHandler(func(path string, err error) {
		atomic.AddUint64(&snapshotErrors, 1)
		fmt.Printf("[ERROR] freshcache SaveSnapshot(%s) failed: %v\n", path, err)
	})
	cachePolicy = decide.Policy{
		TTL:        ttl,
		FutureSkew: futureSkew,
		ExactSet:   exactSet,
	}

	snapshotPath = snapPath
	snapshotEntriesLoaded = 0
	if snapPath != "" {
		if loaded, err := cacheStore.LoadSnapshot(snapPath, time.Now().UTC(), ttl); err == nil {
			snapshotEntriesLoaded = loaded
			fmt.Printf("[INFO] freshcache loaded %d entries from snapshot %s\n", loaded, snapPath)
		}
	}

	pluginStartTime = time.Now().UTC()
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

// HTTPTransportPreHook extracts headers, routes metrics endpoints, and applies native cache guarding.
func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache HTTPTransportPreHook recovered from panic: %v\n", r)
		}
	}()

	hookStart := time.Now()

	// Short-circuit Stale-Cache Court / telemetry metrics endpoint
	if req != nil {
		trimmedPath := strings.TrimSuffix(req.Path, "/")
		if trimmedPath == "/x-chrono/metrics" ||
			req.Headers["x-chrono-admin"] == "metrics" ||
			req.CaseInsensitiveHeaderLookup("x-chrono-admin") == "metrics" {
			data := getMetricsJSON()
			return &schemas.HTTPResponse{
				StatusCode: 200,
				Headers: map[string]string{
					"content-type":  "application/json",
					"cache-control": "no-store",
				},
				Body: data,
			}, nil
		}
	}

	parsed := headers.Parse(req)
	now := time.Now().UTC()
	if ctx != nil {
		ctx.SetValue(keyParsedHeaders, parsed)
		ctx.SetValue(keyStartTime, hookStart)
	}

	// Apply Native Bifrost Cache Guard (Stage 3)
	headers.ApplyNativeCacheGuard(req, &parsed, now)

	// Always generate request ID server-side; ignore client headers (F4)
	id := atomic.AddUint64(&reqCounter, 1)
	reqID := fmt.Sprintf("fc-%d-%d", now.UnixNano(), id)
	if req != nil {
		if req.Headers == nil {
			req.Headers = make(map[string]string)
		}
		req.Headers["x-freshcache-id"] = reqID
		req.Headers["x-internal-freshcache-id"] = reqID
	}
	if ctx != nil {
		ctx.SetValue(keyReqID, reqID)
	}

	// Prune old entries to prevent unbounded memory growth (F4)
	pruneOldReqOutcomes(now)

	initialOutcome := &reqOutcome{
		startTime:      hookStart,
		createdAt:      hookStart,
		guardAction:    parsed.GuardAction,
		hookOverheadUS: time.Since(hookStart).Microseconds(),
	}
	if parsed.TenantID == "" {
		if ctx != nil {
			ctx.SetValue(keyDecision, decide.RefuseNoTenant)
		}
		initialOutcome.decision = decide.RefuseNoTenant
	}
	reqOutcomes.Store(reqID, initialOutcome)

	return nil, nil
}

// PreLLMHook evaluates the freshness decision and computes counterfactual shadow TTLs.
func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (bReq *schemas.BifrostRequest, shortCircuit *schemas.LLMPluginShortCircuit, err error) {
	bReq = req
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache PreLLMHook recovered from panic: %v\n", r)
		}
	}()

	hookStart := time.Now()
	atomic.AddUint64(&lookupsTotal, 1)

	var parsed headers.RequestHeaders
	var reqID string
	if ctx != nil {
		if p, ok := ctx.Value(keyParsedHeaders).(headers.RequestHeaders); ok {
			parsed = p
		}
		if id, ok := ctx.Value(keyReqID).(string); ok {
			reqID = id
		}
	}

	var outcome *reqOutcome
	if reqID != "" {
		if v, ok := reqOutcomes.Load(reqID); ok {
			outcome = v.(*reqOutcome)
		}
	}
	if outcome == nil {
		outcome = &reqOutcome{createdAt: time.Now(), startTime: time.Now()}
		if reqID != "" {
			reqOutcomes.Store(reqID, outcome)
		}
	}

	// 1. Streaming bypass
	if req != nil && (req.RequestType == schemas.ChatCompletionStreamRequest || strings.Contains(strings.ToLower(string(req.RequestType)), "stream")) {
		if ctx != nil {
			ctx.SetValue(keyDecision, decide.BypassStream)
		}
		outcome.decision = decide.BypassStream
		outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
		return req, nil, nil
	}

	// 2. Missing Virtual Key -> Fail Closed (ADR-006)
	if parsed.TenantID == "" {
		if ctx != nil {
			ctx.SetValue(keyDecision, decide.RefuseNoTenant)
		}
		outcome.decision = decide.RefuseNoTenant
		outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
		return req, nil, nil
	}

	// 3. Compute Cache Key (needed for lookup AND for storing refresh results in PostLLMHook)
	var model string
	if req != nil {
		_, model, _ = req.GetRequestFields()
		if model == "" && req.ChatRequest != nil {
			model = req.ChatRequest.Model
		}
	}
	cacheK := key.Compute(parsed.TenantID, parsed.Subject, parsed.Intent, parsed.WindowDays, parsed.PromptVersion, model)
	if ctx != nil {
		ctx.SetValue(keyCacheKey, cacheK)
	}
	outcome.cacheKey = cacheK

	// 4. Client bypass (refresh): skip lookup, proceed to model, store result in PostLLMHook
	if parsed.Refresh {
		if ctx != nil {
			ctx.SetValue(keyDecision, decide.BypassRefresh)
		}
		outcome.decision = decide.BypassRefresh
		outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
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
			FP:          storedEntry.Fingerprint,
			CreatedAt:   storedEntry.CreatedAt,
			FirstSeenAt: storedEntry.FirstSeenAt,
			FirstSeenFP: storedEntry.FirstSeenFP,
		}
		if !storedEntry.Fingerprint.Newest.IsZero() {
			outcome.entryNewest = storedEntry.Fingerprint.Newest.Format(time.RFC3339)
			outcome.entryCount = storedEntry.Fingerprint.CountAtNewest
		}
	}

	now := time.Now().UTC()
	decision := decide.Decide(now, parsed.Fingerprint, decideEntry, pol)
	if ctx != nil {
		ctx.SetValue(keyDecision, decision)
	}
	outcome.decision = decision

	// Compute Counterfactual TTL Shadow Ledger (Stage 1)
	shadows := decide.ComputeShadowOutcomes(now, parsed.Fingerprint, decideEntry)
	outcome.shadowOutcomes = shadows

	// Update Stale-Cache Court & Telemetry counters
	if decision.Serves() {
		atomic.AddUint64(&hitsTotal, 1)
		if shadows.TTL5m == "MISS_EXPIRED" {
			atomic.AddUint64(&courtTTL5mMisses, 1)
		}
	} else if strings.HasPrefix(string(decision), "REFUSE_") {
		atomic.AddUint64(&stalePreventedTotal, 1)
		if shadows.TTL24h == "HIT_STALE" {
			atomic.AddUint64(&courtTTL24hStale, 1)
		}
		if shadows.TTL1h == "HIT_STALE" {
			atomic.AddUint64(&courtTTL1hStale, 1)
		}
	}

	// 6. Handle HIT
	if decision.Serves() && storedEntry != nil && storedEntry.Response != nil {
		if ctx != nil {
			ctx.SetValue(keyServed, true)
			ctx.SetValue(keyHitEntry, storedEntry)
		}
		outcome.served = true
		outcome.hitEntry = storedEntry
		outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
		return req, &schemas.LLMPluginShortCircuit{Response: storedEntry.Response}, nil
	}

	outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
	return req, nil, nil
}

// PostLLMHook records audit logs and stores computed responses on MISS/REFUSE.
func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (bResp *schemas.BifrostResponse, bErr *schemas.BifrostError, err error) {
	bResp = resp
	bErr = bifrostErr
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache PostLLMHook recovered from panic: %v\n", r)
		}
	}()

	hookStart := time.Now()

	var parsed headers.RequestHeaders
	var cacheK string
	var dec decide.Decision
	var served bool
	var startT time.Time
	var reqID string

	if ctx != nil {
		parsed, _ = ctx.Value(keyParsedHeaders).(headers.RequestHeaders)
		cacheK, _ = ctx.Value(keyCacheKey).(string)
		dec, _ = ctx.Value(keyDecision).(decide.Decision)
		served, _ = ctx.Value(keyServed).(bool)
		startT, _ = ctx.Value(keyStartTime).(time.Time)
		reqID, _ = ctx.Value(keyReqID).(string)
	}

	var outcome *reqOutcome
	if reqID != "" {
		if v, ok := reqOutcomes.Load(reqID); ok {
			outcome = v.(*reqOutcome)
		}
	}

	// Latency accounting:
	// latencyMS measures the elapsed wall-clock time in milliseconds from PreLLMHook
	// to PostLLMHook, which includes upstream LLM generation on a MISS or cache lookup on a HIT.
	latencyMS := int64(0)
	if !startT.IsZero() {
		latencyMS = time.Since(startT).Milliseconds()
	}

	pluginMu.RLock()
	st := cacheStore
	al := auditLogger
	pol := cachePolicy
	snapPath := snapshotPath
	pluginMu.RUnlock()

	var shadows decide.ShadowOutcomes
	hookOverhead := int64(0)
	if outcome != nil {
		shadows = outcome.shadowOutcomes
		hookOverhead = outcome.hookOverheadUS
	}

	// 1. If served from cache (HIT)
	if served {
		var hitE *store.StoredEntry
		if ctx != nil {
			hitE, _ = ctx.Value(keyHitEntry).(*store.StoredEntry)
		}
		if hitE == nil && outcome != nil {
			hitE = outcome.hitEntry
		}

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
			ShadowTTL5m:          shadows.TTL5m,
			ShadowTTL1h:          shadows.TTL1h,
			ShadowTTL24h:         shadows.TTL24h,
			WouldServeStale24h:   shadows.WouldServeStale24h,
			PluginOverheadUS:     hookOverhead,
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
			if err := al.Log(rec); err != nil {
				fmt.Printf("[ERROR] freshcache audit write failed: %v\n", err)
			}
		}
		if outcome != nil {
			outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
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
			ShadowTTL5m:          shadows.TTL5m,
			ShadowTTL1h:          shadows.TTL1h,
			ShadowTTL24h:         shadows.TTL24h,
			WouldServeStale24h:   shadows.WouldServeStale24h,
			PluginOverheadUS:     hookOverhead,
		}
		if al != nil {
			if err := al.Log(rec); err != nil {
				fmt.Printf("[ERROR] freshcache audit write failed: %v\n", err)
			}
		}
		if outcome != nil {
			outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
		}
		return resp, bifrostErr, nil
	}

	// 3. Synchronous cost calculation & token accounting
	cost := 0.0
	if ctx != nil {
		cost = ctx.CalculateCost(resp)
	}
	pt, ct, tt := 0, 0, 0
	if resp.ChatResponse.Usage != nil {
		pt = resp.ChatResponse.Usage.PromptTokens
		ct = resp.ChatResponse.Usage.CompletionTokens
		tt = resp.ChatResponse.Usage.TotalTokens
	}

	// 4. Store newly generated response if eligible & save atomic snapshot
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
			if snapPath != "" {
				st.ScheduleSnapshot(snapPath)
			}
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
		ShadowTTL5m:          shadows.TTL5m,
		ShadowTTL1h:          shadows.TTL1h,
		ShadowTTL24h:         shadows.TTL24h,
		WouldServeStale24h:   shadows.WouldServeStale24h,
		PluginOverheadUS:     hookOverhead,
	}
	if al != nil {
		if err := al.Log(rec); err != nil {
			fmt.Printf("[ERROR] freshcache audit write failed: %v\n", err)
		}
	}

	if outcome != nil {
		outcome.hookOverheadUS += time.Since(hookStart).Microseconds()
	}
	return resp, bifrostErr, nil
}

// HTTPTransportPostHook sets client-facing response headers (e.g. x-chrono-cache, shadow TTLs, overhead).
func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache HTTPTransportPostHook recovered from panic: %v\n", r)
		}
	}()

	hookStart := time.Now()

	if resp == nil {
		return nil
	}
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}

	var reqID string
	if ctx != nil {
		reqID, _ = ctx.Value(keyReqID).(string)
	}
	if reqID == "" && req != nil && req.Headers != nil {
		reqID = req.Headers["x-internal-freshcache-id"]
	}

	var outcome *reqOutcome
	if reqID != "" {
		if v, ok := reqOutcomes.LoadAndDelete(reqID); ok {
			outcome = v.(*reqOutcome)
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
	} else if ctx != nil {
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
	resp.Headers["x-bf-freshness-cache"] = string(dec)
	if cacheK != "" {
		resp.Headers["x-chrono-cache-key"] = cacheK
	}
	resp.Headers["x-chrono-served-at"] = time.Now().UTC().Format(time.RFC3339)

	if outcome != nil {
		if outcome.shadowOutcomes.TTL5m != "" {
			resp.Headers["x-chrono-shadow-ttl-5m"] = outcome.shadowOutcomes.TTL5m
			resp.Headers["x-chrono-shadow-ttl-1h"] = outcome.shadowOutcomes.TTL1h
			resp.Headers["x-chrono-shadow-ttl-24h"] = outcome.shadowOutcomes.TTL24h
		}
		if outcome.guardAction != "" {
			resp.Headers["x-bf-freshness-guard"] = string(outcome.guardAction)
		}
		// Plugin overhead calculation:
		// x-chrono-plugin-overhead-us measures plugin-only hook execution time in microseconds
		// (HTTPTransportPreHook + PreLLMHook + PostLLMHook + HTTPTransportPostHook),
		// strictly excluding upstream LLM generation or network latency.
		overhead := outcome.hookOverheadUS + time.Since(hookStart).Microseconds()
		recordOverhead(overhead)
		resp.Headers["x-chrono-plugin-overhead-us"] = strconv.FormatInt(overhead, 10)
	}

	if served && hitE != nil {
		newestStr := hitE.Fingerprint.Newest.Format(time.RFC3339)
		resp.Headers["x-chrono-cached-newest"] = newestStr
		resp.Headers["x-chrono-entry-newest"] = newestStr
		resp.Headers["x-chrono-cached-count"] = strconv.Itoa(hitE.Fingerprint.CountAtNewest)
		resp.Headers["x-chrono-entry-count"] = strconv.Itoa(hitE.Fingerprint.CountAtNewest)
		resp.Headers["x-chrono-saved-tokens"] = strconv.Itoa(hitE.TotalTokens)
		resp.Headers["x-chrono-saved-cost-usd"] = fmt.Sprintf("%.6f", hitE.CostUSD)
	} else if outcome != nil && outcome.entryNewest != "" {
		resp.Headers["x-chrono-cached-newest"] = outcome.entryNewest
		resp.Headers["x-chrono-entry-newest"] = outcome.entryNewest
		resp.Headers["x-chrono-cached-count"] = strconv.Itoa(outcome.entryCount)
		resp.Headers["x-chrono-entry-count"] = strconv.Itoa(outcome.entryCount)
	}

	return nil
}

// Cleanup flushes audit logs and saves disk snapshot on gateway shutdown.
func Cleanup() error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("freshcache Cleanup recovered from panic: %v\n", r)
		}
	}()

	pluginMu.Lock()
	defer pluginMu.Unlock()

	if cacheStore != nil {
		if snapshotPath != "" {
			if err := cacheStore.FlushSnapshot(snapshotPath); err != nil {
				atomic.AddUint64(&snapshotErrors, 1)
				fmt.Printf("[ERROR] freshcache SaveSnapshot(%s) failed: %v\n", snapshotPath, err)
			}
		} else {
			_ = cacheStore.FlushSnapshot("")
		}
	}

	if auditLogger != nil {
		_ = auditLogger.Close()
		auditLogger = nil
	}
	return nil
}
