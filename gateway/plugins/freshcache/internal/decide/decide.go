// Package decide holds the one decision the freshness cache makes: serve or recompute.
//
// It is deliberately pure: no I/O, no clock reads, no Bifrost types, no network.
// The caller passes "now" in. That keeps every cache decision reproducible from the
// audit log and lets the stale-serve invariant be tested exhaustively.
package decide

import (
	"encoding/json"
	"time"
)

// Fingerprint summarises how fresh the sources behind a request or a stored entry are.
// It is computed by the Python agent (chronobrief/fingerprint.py) and carried in headers.
type Fingerprint struct {
	Newest        time.Time // newest dated source, UTC. Zero when there are no dated sources.
	CountAtNewest int       // how many dated sources share the Newest timestamp
	Dated         int       // sources with a usable published date
	Undated       int       // sources without a usable published date
	SetHash       string    // sha256 over the sorted (canonical URL, timestamp, title) lines
}

// Valid reports whether the fingerprint carries enough evidence to make a freshness claim.
func (f Fingerprint) Valid() bool {
	return f.Dated > 0 && !f.Newest.IsZero() && f.CountAtNewest > 0 && f.SetHash != ""
}

// UnmarshalJSON supports unmarshaling both PascalCase and snake_case JSON field representations.
func (f *Fingerprint) UnmarshalJSON(data []byte) error {
	type rawFP Fingerprint
	var aux struct {
		rawFP
		NewestLower        time.Time `json:"newest"`
		CountAtNewestSnake int       `json:"count_at_newest"`
		DatedLower         int       `json:"dated"`
		UndatedLower       int       `json:"undated"`
		SetHashSnake       string    `json:"set_hash"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*f = Fingerprint(aux.rawFP)
	if f.Newest.IsZero() && !aux.NewestLower.IsZero() {
		f.Newest = aux.NewestLower
	}
	if f.CountAtNewest == 0 && aux.CountAtNewestSnake > 0 {
		f.CountAtNewest = aux.CountAtNewestSnake
	}
	if f.Dated == 0 && aux.DatedLower > 0 {
		f.Dated = aux.DatedLower
	}
	if f.Undated == 0 && aux.UndatedLower > 0 {
		f.Undated = aux.UndatedLower
	}
	if f.SetHash == "" && aux.SetHashSnake != "" {
		f.SetHash = aux.SetHashSnake
	}
	return nil
}

// Entry is what the cache remembers about a stored answer.
type Entry struct {
	FP        Fingerprint
	CreatedAt time.Time
}

// Policy is the knob set the plugin reads from config.
type Policy struct {
	TTL        time.Duration // hard ceiling on entry age, independent of freshness
	FutureSkew time.Duration // sources dated further ahead than this are treated as invalid
	ExactSet   bool          // true: any source-set change forces recompute. false: only "newer" does.
}

// Decision is the outcome. The string values appear in the audit log and response headers.
type Decision string

const (
	Hit                 Decision = "HIT"
	MissCold            Decision = "MISS_COLD"
	RefuseNewerSource   Decision = "REFUSE_NEWER_SOURCE"
	RefuseSetChanged    Decision = "REFUSE_SET_CHANGED"
	RefuseExpired       Decision = "REFUSE_EXPIRED"
	RefuseNoFingerprint Decision = "REFUSE_NO_FINGERPRINT"
	RefuseFutureDate    Decision = "REFUSE_FUTURE_DATE"
	RefuseNoTenant      Decision = "REFUSE_NO_TENANT"
	BypassStream        Decision = "BYPASS_STREAM"
	BypassRefresh       Decision = "BYPASS_REFRESH"
)

// Serves reports whether the decision means "answer from cache".
func (d Decision) Serves() bool { return d == Hit }

// Decide returns the decision for an incoming request fingerprint against an optional entry.
//
// Ordering matters and is part of the contract:
//  1. no usable evidence about the request   -> refuse (never serve without evidence)
//  2. request dated in the future            -> refuse (a poisoned timestamp must not mask real news)
//  3. no entry                               -> cold miss
//  4. entry too old                          -> refuse
//  5. request newer than entry               -> refuse (the story has moved)
//  6. undated count changed                  -> refuse (we cannot prove the new source is not newer)
//  7. exact-set policy and set differs       -> refuse
//  8. otherwise                              -> hit
func Decide(now time.Time, in Fingerprint, e *Entry, p Policy) Decision {
	if !in.Valid() {
		return RefuseNoFingerprint
	}
	if p.FutureSkew > 0 && in.Newest.After(now.Add(p.FutureSkew)) {
		return RefuseFutureDate
	}
	if e == nil {
		return MissCold
	}
	if p.TTL > 0 && now.Sub(e.CreatedAt) > p.TTL {
		return RefuseExpired
	}
	if in.Newest.After(e.FP.Newest) {
		return RefuseNewerSource
	}
	if in.Newest.Equal(e.FP.Newest) {
		if in.CountAtNewest > e.FP.CountAtNewest {
			return RefuseNewerSource
		}
		if in.Dated != e.FP.Dated {
			return RefuseSetChanged
		}
	}
	if in.Undated != e.FP.Undated {
		return RefuseSetChanged
	}
	if p.ExactSet && in.SetHash != e.FP.SetHash {
		return RefuseSetChanged
	}
	return Hit
}

// Storable reports whether an answer computed for this fingerprint may be stored.
// A future-dated or evidence-free fingerprint is never stored: it would later be compared
// against real requests and hide genuinely newer articles.
func Storable(now time.Time, fp Fingerprint, p Policy) bool {
	if !fp.Valid() {
		return false
	}
	if p.FutureSkew > 0 && fp.Newest.After(now.Add(p.FutureSkew)) {
		return false
	}
	return true
}

// DecideWithTenant evaluates freshness with mandatory tenant isolation (ADR-006).
// If tenantID is missing or empty, it fails closed (REFUSE_NO_TENANT) without lookup.
func DecideWithTenant(now time.Time, tenantID string, in Fingerprint, e *Entry, p Policy) Decision {
	if tenantID == "" {
		return RefuseNoTenant
	}
	return Decide(now, in, e, p)
}

// StorableWithTenant reports whether an entry is storable, requiring a non-empty tenant ID.
func StorableWithTenant(now time.Time, tenantID string, fp Fingerprint, p Policy) bool {
	if tenantID == "" {
		return false
	}
	return Storable(now, fp, p)
}

// ShadowOutcomes captures the counterfactual decision a pure time-based TTL cache
// would have made on the exact same request.
type ShadowOutcomes struct {
	TTL5m              string `json:"shadow_ttl_5m"`
	TTL1h              string `json:"shadow_ttl_1h"`
	TTL24h             string `json:"shadow_ttl_24h"`
	WouldServeStale24h bool   `json:"would_serve_stale_24h"`
}

// ShadowSlot tracks provenance for a counterfactual TTL cache slot.
type ShadowSlot struct {
	StoredAt time.Time   `json:"stored_at"`
	FP       Fingerprint `json:"fp"`
}

// ShadowState holds three independent counterfactual TTL slots per cache key.
type ShadowState struct {
	Slot5m  ShadowSlot `json:"slot_5m"`
	Slot1h  ShadowSlot `json:"slot_1h"`
	Slot24h ShadowSlot `json:"slot_24h"`
}

// AdvanceShadowSlot evaluates what a time-only TTL cache would do on this lookup and returns the outcome and next slot state:
// - If slot is empty (slot.StoredAt.IsZero() || !slot.FP.Valid()) -> "MISS_COLD"; updated slot = {StoredAt: now, FP: in}
// - Else if now.Sub(slot.StoredAt) > ttl -> "MISS_EXPIRED"; updated slot = {StoredAt: now, FP: in}
// - Else -> evaluate Decide(now, in, &Entry{FP: slot.FP, CreatedAt: slot.StoredAt}, Policy{TTL: ttl, FutureSkew: 24 * time.Hour, ExactSet: true}).
//   If == Hit, outcome is "HIT_FRESH", else "HIT_STALE"; slot remains unchanged.
func AdvanceShadowSlot(now time.Time, in Fingerprint, slot ShadowSlot, ttl time.Duration) (string, ShadowSlot) {
	if slot.StoredAt.IsZero() || !slot.FP.Valid() {
		return "MISS_COLD", ShadowSlot{StoredAt: now, FP: in}
	}
	if ttl > 0 && now.Sub(slot.StoredAt) > ttl {
		return "MISS_EXPIRED", ShadowSlot{StoredAt: now, FP: in}
	}
	dec := Decide(now, in, &Entry{FP: slot.FP, CreatedAt: slot.StoredAt}, Policy{
		TTL:        ttl,
		FutureSkew: 24 * time.Hour,
		ExactSet:   true,
	})
	if dec == Hit {
		return "HIT_FRESH", slot
	}
	return "HIT_STALE", slot
}

// AdvanceShadowState evaluates counterfactual outcomes across 5m, 1h, and 24h TTLs and advances the shadow state.
func AdvanceShadowState(now time.Time, in Fingerprint, state ShadowState) (ShadowOutcomes, ShadowState) {
	o5m, s5m := AdvanceShadowSlot(now, in, state.Slot5m, 5*time.Minute)
	o1h, s1h := AdvanceShadowSlot(now, in, state.Slot1h, 1*time.Hour)
	o24h, s24h := AdvanceShadowSlot(now, in, state.Slot24h, 24*time.Hour)

	outcomes := ShadowOutcomes{
		TTL5m:              o5m,
		TTL1h:              o1h,
		TTL24h:             o24h,
		WouldServeStale24h: o24h == "HIT_STALE",
	}
	newState := ShadowState{
		Slot5m:  s5m,
		Slot1h:  s1h,
		Slot24h: s24h,
	}
	return outcomes, newState
}
