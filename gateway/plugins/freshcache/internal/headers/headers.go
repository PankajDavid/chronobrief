package headers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

// GuardAction represents native cache guard states.
type GuardAction string

const (
	GuardActionNone           GuardAction = ""
	GuardActionBypassedUnsafe GuardAction = "BYPASSED_UNSAFE"
	GuardActionStampedSafe    GuardAction = "STAMPED_SAFE"
)

// RequestHeaders contains the parsed and bounded headers from an inference request.
type RequestHeaders struct {
	TenantID       string // sha256(vk)[:12] - safe hashed tenant identifier (never raw secret)
	RawTenant      string // raw virtual key from x-bf-vk
	Subject        string
	Intent         string
	WindowDays     int
	PromptVersion  string
	Fingerprint    decide.Fingerprint
	CacheMode      string
	Refresh        bool
	NativeCacheKey string      // incoming x-bf-cache-key from caller
	GuardAction    GuardAction // BYPASSED_UNSAFE or STAMPED_SAFE
}

// HashTenant computes sha256(vk)[:12] for safe logging and tenant isolation
// without exposing the raw secret virtual key in logs, memory, or cache keys.
func HashTenant(vk string) string {
	if vk == "" {
		return ""
	}
	h := sha256.Sum256([]byte(vk))
	return hex.EncodeToString(h[:])[:12]
}

func getHeader(req *schemas.HTTPRequest, key string) string {
	if req == nil {
		return ""
	}
	if val := req.CaseInsensitiveHeaderLookup(key); val != "" {
		return strings.TrimSpace(val)
	}
	if req.Headers != nil {
		for k, v := range req.Headers {
			if strings.EqualFold(k, key) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// getHeaderWithAlias returns value of primary key or alias fallback.
func getHeaderWithAlias(req *schemas.HTTPRequest, primary, alias string) string {
	if v := getHeader(req, primary); v != "" {
		return v
	}
	return getHeader(req, alias)
}

// Parse extracts and validates x-chrono-* (and fallback x-bf-freshness-*) headers and tenant ID.
func Parse(req *schemas.HTTPRequest) RequestHeaders {
	h := RequestHeaders{
		WindowDays: 30, // Default window
	}
	if req == nil {
		return h
	}

	// 1. Tenant ID strictly from x-bf-vk (ADR-006)
	tenant := getHeader(req, "x-bf-vk")
	if len(tenant) > 256 {
		tenant = tenant[:256]
	}
	h.RawTenant = tenant
	h.TenantID = HashTenant(tenant)

	// 2. Request metadata (with x-bf-freshness-* aliases)
	sub := getHeaderWithAlias(req, "x-chrono-subject", "x-bf-freshness-subject")
	if len(sub) > 256 {
		sub = sub[:256]
	}
	h.Subject = sub

	intent := getHeaderWithAlias(req, "x-chrono-intent", "x-bf-freshness-intent")
	if len(intent) > 128 {
		intent = intent[:128]
	}
	h.Intent = intent

	if wdStr := getHeaderWithAlias(req, "x-chrono-window-days", "x-bf-freshness-window-days"); wdStr != "" {
		if wd, err := strconv.Atoi(wdStr); err == nil && wd > 0 && wd <= 365 {
			h.WindowDays = wd
		}
	}

	pv := getHeaderWithAlias(req, "x-chrono-prompt-version", "x-bf-freshness-prompt-version")
	if len(pv) > 64 {
		pv = pv[:64]
	}
	h.PromptVersion = pv

	// 3. Fingerprint (with x-bf-freshness-* aliases)
	fp := decide.Fingerprint{}
	if newestStr := getHeaderWithAlias(req, "x-chrono-newest", "x-bf-freshness-newest"); newestStr != "" {
		if parsedTime, err := time.Parse(time.RFC3339, newestStr); err == nil {
			fp.Newest = parsedTime.UTC()
		}
	}

	if cntStr := getHeaderWithAlias(req, "x-chrono-count-at-newest", "x-bf-freshness-count-at-newest"); cntStr != "" {
		if cnt, err := strconv.Atoi(cntStr); err == nil && cnt > 0 {
			fp.CountAtNewest = cnt
		}
	}

	if datedStr := getHeaderWithAlias(req, "x-chrono-dated", "x-bf-freshness-dated"); datedStr != "" {
		if dated, err := strconv.Atoi(datedStr); err == nil && dated >= 0 {
			fp.Dated = dated
		}
	}

	if undatedStr := getHeaderWithAlias(req, "x-chrono-undated", "x-bf-freshness-undated"); undatedStr != "" {
		if undated, err := strconv.Atoi(undatedStr); err == nil && undated >= 0 {
			fp.Undated = undated
		}
	}

	setHash := getHeaderWithAlias(req, "x-chrono-set-hash", "x-bf-freshness-set-hash")
	if len(setHash) == 64 {
		fp.SetHash = strings.ToLower(setHash)
	}
	h.Fingerprint = fp

	// 4. Cache Mode & Refresh bypass
	mode := strings.ToLower(getHeaderWithAlias(req, "x-chrono-cache-mode", "x-bf-freshness-mode"))
	if mode == "replay" || mode == "live" {
		h.CacheMode = mode
	} else {
		h.CacheMode = "live"
	}

	refreshStr := strings.ToLower(getHeaderWithAlias(req, "x-chrono-refresh", "x-bf-freshness-refresh"))
	cacheControl := strings.ToLower(getHeader(req, "cache-control"))
	if mode == "refresh" || refreshStr == "true" || strings.Contains(cacheControl, "no-cache") {
		h.Refresh = true
	}

	// 5. Native Bifrost Cache Key
	h.NativeCacheKey = getHeader(req, "x-bf-cache-key")

	return h
}

// ApplyNativeCacheGuard checks if x-bf-cache-key is present alongside freshness headers.
// If fingerprint is invalid or future-dated, it strips x-bf-cache-key so native semantic cache
// is not polluted, setting guard to BYPASSED_UNSAFE.
// If valid, it stamps x-bf-cache-key with ":fc:<newest_rfc3339>:<sethash8>" and sets STAMPED_SAFE.
func ApplyNativeCacheGuard(req *schemas.HTTPRequest, h *RequestHeaders, now time.Time) {
	if req == nil || h.NativeCacheKey == "" {
		return
	}
	if !h.Fingerprint.Valid() || h.Fingerprint.Newest.After(now.Add(24*time.Hour)) {
		h.GuardAction = "BYPASSED_UNSAFE"
		if req.Headers != nil {
			delete(req.Headers, "x-bf-cache-key")
			for k := range req.Headers {
				if strings.EqualFold(k, "x-bf-cache-key") {
					delete(req.Headers, k)
				}
			}
		}
		return
	}

	h.GuardAction = "STAMPED_SAFE"
	hash8 := h.Fingerprint.SetHash
	if len(hash8) > 8 {
		hash8 = hash8[:8]
	}
	stamped := fmt.Sprintf("%s:fc:%s:%s", h.NativeCacheKey, h.Fingerprint.Newest.Format(time.RFC3339), hash8)
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["x-bf-cache-key"] = stamped
	h.NativeCacheKey = stamped
}
