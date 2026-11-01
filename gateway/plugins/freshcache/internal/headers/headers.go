package headers

import (
	"strconv"
	"strings"
	"time"

	"github.com/chronobrief/freshcache/internal/decide"
	"github.com/maximhq/bifrost/core/schemas"
)

// RequestHeaders contains the parsed and bounded headers from an inference request.
type RequestHeaders struct {
	TenantID      string
	Subject       string
	Intent        string
	WindowDays    int
	PromptVersion string
	Fingerprint   decide.Fingerprint
	CacheMode     string
	Refresh       bool
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

// Parse extracts and validates x-chrono-* headers and x-bf-vk tenant ID.
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
	h.TenantID = tenant

	// 2. Request metadata
	sub := getHeader(req, "x-chrono-subject")
	if len(sub) > 256 {
		sub = sub[:256]
	}
	h.Subject = sub

	intent := getHeader(req, "x-chrono-intent")
	if len(intent) > 128 {
		intent = intent[:128]
	}
	h.Intent = intent

	if wdStr := getHeader(req, "x-chrono-window-days"); wdStr != "" {
		if wd, err := strconv.Atoi(wdStr); err == nil && wd > 0 && wd <= 365 {
			h.WindowDays = wd
		}
	}

	pv := getHeader(req, "x-chrono-prompt-version")
	if len(pv) > 64 {
		pv = pv[:64]
	}
	h.PromptVersion = pv

	// 3. Fingerprint
	fp := decide.Fingerprint{}
	if newestStr := getHeader(req, "x-chrono-newest"); newestStr != "" {
		if parsedTime, err := time.Parse(time.RFC3339, newestStr); err == nil {
			fp.Newest = parsedTime.UTC()
		}
	}

	if cntStr := getHeader(req, "x-chrono-count-at-newest"); cntStr != "" {
		if cnt, err := strconv.Atoi(cntStr); err == nil && cnt > 0 {
			fp.CountAtNewest = cnt
		}
	}

	if datedStr := getHeader(req, "x-chrono-dated"); datedStr != "" {
		if dated, err := strconv.Atoi(datedStr); err == nil && dated >= 0 {
			fp.Dated = dated
		}
	}

	if undatedStr := getHeader(req, "x-chrono-undated"); undatedStr != "" {
		if undated, err := strconv.Atoi(undatedStr); err == nil && undated >= 0 {
			fp.Undated = undated
		}
	}

	setHash := getHeader(req, "x-chrono-set-hash")
	if len(setHash) == 64 {
		fp.SetHash = strings.ToLower(setHash)
	}
	h.Fingerprint = fp

	// 4. Cache Mode & Refresh bypass
	mode := strings.ToLower(getHeader(req, "x-chrono-cache-mode"))
	if mode == "replay" || mode == "live" {
		h.CacheMode = mode
	} else {
		h.CacheMode = "live"
	}

	refreshStr := strings.ToLower(getHeader(req, "x-chrono-refresh"))
	cacheControl := strings.ToLower(getHeader(req, "cache-control"))
	if refreshStr == "true" || strings.Contains(cacheControl, "no-cache") {
		h.Refresh = true
	}

	return h
}
