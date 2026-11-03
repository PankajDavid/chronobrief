package decide

import (
	"math/rand"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

func fp(newest time.Time, countAtNewest, dated, undated int, hash string) Fingerprint {
	return Fingerprint{Newest: newest, CountAtNewest: countAtNewest, Dated: dated, Undated: undated, SetHash: hash}
}

var pol = Policy{TTL: 24 * time.Hour, FutureSkew: 24 * time.Hour}

func TestDecideTable(t *testing.T) {
	base := fp(t0.Add(-48*time.Hour), 1, 5, 0, "h1")
	entry := &Entry{FP: base, CreatedAt: t0.Add(-1 * time.Hour)}

	cases := []struct {
		name  string
		in    Fingerprint
		entry *Entry
		pol   Policy
		want  Decision
	}{
		{"cold miss", base, nil, pol, MissCold},
		{"identical fingerprint hits", base, entry, pol, Hit},
		{"newer source refuses", fp(t0.Add(-2*time.Hour), 1, 6, 0, "h2"), entry, pol, RefuseNewerSource},
		{"same timestamp extra article refuses", fp(base.Newest, 2, 6, 0, "h2"), entry, pol, RefuseNewerSource},
		{"older incoming newest still hits (entry already knows newer)", fp(base.Newest.Add(-24*time.Hour), 1, 4, 0, "h3"), entry, pol, Hit},
		{"undated count changed refuses", fp(base.Newest, 1, 5, 1, "h1"), entry, pol, RefuseSetChanged},
		{"dated count changed refuses (ExactSet: false)", fp(base.Newest, 1, 6, 0, "h1"), entry, pol, RefuseSetChanged},
		{"exact policy: different set same newest refuses", fp(base.Newest, 1, 5, 0, "hX"), entry, Policy{TTL: pol.TTL, FutureSkew: pol.FutureSkew, ExactSet: true}, RefuseSetChanged},
		{"newest_only policy: different set same newest hits", fp(base.Newest, 1, 5, 0, "hX"), entry, pol, Hit},
		{"no dated sources refuses", fp(time.Time{}, 0, 0, 3, ""), entry, pol, RefuseNoFingerprint},
		{"missing hash refuses", fp(base.Newest, 1, 5, 0, ""), entry, pol, RefuseNoFingerprint},
		{"future dated refuses", fp(t0.Add(72*time.Hour), 1, 5, 0, "h9"), entry, pol, RefuseFutureDate},
		{"expired entry refuses", base, &Entry{FP: base, CreatedAt: t0.Add(-25 * time.Hour)}, pol, RefuseExpired},
		{"ttl zero means no ttl", base, &Entry{FP: base, CreatedAt: t0.Add(-400 * time.Hour)}, Policy{FutureSkew: pol.FutureSkew}, Hit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(t0, c.in, c.entry, c.pol); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestStorable(t *testing.T) {
	if Storable(t0, fp(t0.Add(72*time.Hour), 1, 1, 0, "h"), pol) {
		t.Fatal("future-dated fingerprint must not be stored")
	}
	if Storable(t0, fp(time.Time{}, 0, 0, 2, ""), pol) {
		t.Fatal("evidence-free fingerprint must not be stored")
	}
	if !Storable(t0, fp(t0.Add(-time.Hour), 1, 1, 0, "h"), pol) {
		t.Fatal("normal fingerprint must be storable")
	}
}

func TestDecideWithTenant(t *testing.T) {
	base := fp(t0.Add(-48*time.Hour), 1, 5, 0, "h1")
	entry := &Entry{FP: base, CreatedAt: t0.Add(-1 * time.Hour)}

	// Empty tenant must fail closed
	if got := DecideWithTenant(t0, "", base, entry, pol); got != RefuseNoTenant {
		t.Fatalf("expected REFUSE_NO_TENANT for empty tenant, got %s", got)
	}

	// Valid tenant evaluates normally
	if got := DecideWithTenant(t0, "team-alpha", base, entry, pol); got != Hit {
		t.Fatalf("expected HIT for valid tenant and identical fp, got %s", got)
	}
}

func TestStorableWithTenant(t *testing.T) {
	validFP := fp(t0.Add(-time.Hour), 1, 1, 0, "h")
	if StorableWithTenant(t0, "", validFP, pol) {
		t.Fatal("empty tenant must fail closed and never be storable")
	}
	if !StorableWithTenant(t0, "team-alpha", validFP, pol) {
		t.Fatal("valid tenant with valid fp must be storable")
	}
}

type ttlCacheSimulator struct {
	ttl      time.Duration
	storedAt time.Time
	fp       Fingerprint
	occupied bool
}

func newTTLCacheSimulator(ttl time.Duration) *ttlCacheSimulator {
	return &ttlCacheSimulator{ttl: ttl}
}

func (s *ttlCacheSimulator) Lookup(now time.Time, in Fingerprint) string {
	if !s.occupied {
		s.storedAt = now
		s.fp = in
		s.occupied = true
		return "MISS_COLD"
	}
	if s.ttl > 0 && now.Sub(s.storedAt) > s.ttl {
		s.storedAt = now
		s.fp = in
		return "MISS_EXPIRED"
	}
	dec := Decide(now, in, &Entry{FP: s.fp, CreatedAt: s.storedAt}, Policy{
		TTL:        s.ttl,
		FutureSkew: 24 * time.Hour,
		ExactSet:   true,
	})
	if dec == Hit {
		return "HIT_FRESH"
	}
	return "HIT_STALE"
}

func TestShadowSlotTransitions(t *testing.T) {
	base := fp(t0.Add(-48*time.Hour), 1, 5, 0, "h1")
	newer := fp(t0.Add(-2*time.Hour), 1, 6, 0, "h2")

	// 1. Empty slot -> MISS_COLD
	out, slot := AdvanceShadowSlot(t0, base, ShadowSlot{}, 5*time.Minute)
	if out != "MISS_COLD" {
		t.Fatalf("expected MISS_COLD, got %s", out)
	}
	if !slot.StoredAt.Equal(t0) || slot.FP.SetHash != "h1" {
		t.Fatalf("slot not initialized properly: %+v", slot)
	}

	// 2. Lookup within TTL with identical FP -> HIT_FRESH (slot unchanged)
	t2m := t0.Add(2 * time.Minute)
	out2, slot2 := AdvanceShadowSlot(t2m, base, slot, 5*time.Minute)
	if out2 != "HIT_FRESH" {
		t.Fatalf("expected HIT_FRESH, got %s", out2)
	}
	if !slot2.StoredAt.Equal(t0) {
		t.Fatalf("slot StoredAt mutated: %v", slot2.StoredAt)
	}

	// 3. Lookup within TTL with newer FP -> HIT_STALE (slot unchanged)
	out3, slot3 := AdvanceShadowSlot(t2m, newer, slot, 5*time.Minute)
	if out3 != "HIT_STALE" {
		t.Fatalf("expected HIT_STALE, got %s", out3)
	}
	if !slot3.StoredAt.Equal(t0) {
		t.Fatalf("slot StoredAt mutated: %v", slot3.StoredAt)
	}

	// 4. Lookup past TTL -> MISS_EXPIRED (slot reset)
	t10m := t0.Add(10 * time.Minute)
	out4, slot4 := AdvanceShadowSlot(t10m, newer, slot, 5*time.Minute)
	if out4 != "MISS_EXPIRED" {
		t.Fatalf("expected MISS_EXPIRED, got %s", out4)
	}
	if !slot4.StoredAt.Equal(t10m) || slot4.FP.SetHash != "h2" {
		t.Fatalf("slot not updated on expiry: %+v", slot4)
	}
}

func TestShadowParitySequenceA(t *testing.T) {
	baseFP := fp(t0.Add(-48*time.Hour), 1, 5, 0, "h1")
	sim5m := newTTLCacheSimulator(5 * time.Minute)
	sim1h := newTTLCacheSimulator(1 * time.Hour)
	sim24h := newTTLCacheSimulator(24 * time.Hour)

	var state ShadowState
	var outcomes5m []string

	offsets := []time.Duration{
		0,
		6 * time.Minute,
		8 * time.Minute,
		10 * time.Minute,
		30 * time.Minute,
		32 * time.Minute,
	}

	for i, offset := range offsets {
		now := t0.Add(offset)
		outcomes, nextState := AdvanceShadowState(now, baseFP, state)
		state = nextState

		want5m := sim5m.Lookup(now, baseFP)
		want1h := sim1h.Lookup(now, baseFP)
		want24h := sim24h.Lookup(now, baseFP)

		if outcomes.TTL5m != want5m {
			t.Fatalf("step %d (+%v): 5m got %s, want %s", i, offset, outcomes.TTL5m, want5m)
		}
		if outcomes.TTL1h != want1h {
			t.Fatalf("step %d (+%v): 1h got %s, want %s", i, offset, outcomes.TTL1h, want1h)
		}
		if outcomes.TTL24h != want24h {
			t.Fatalf("step %d (+%v): 24h got %s, want %s", i, offset, outcomes.TTL24h, want24h)
		}
		if outcomes.WouldServeStale24h != (want24h == "HIT_STALE") {
			t.Fatalf("step %d (+%v): WouldServeStale24h got %v, want %v", i, offset, outcomes.WouldServeStale24h, want24h == "HIT_STALE")
		}

		outcomes5m = append(outcomes5m, outcomes.TTL5m)
	}

	expected5m := []string{"MISS_COLD", "MISS_EXPIRED", "HIT_FRESH", "HIT_FRESH", "MISS_EXPIRED", "HIT_FRESH"}
	if len(outcomes5m) != len(expected5m) {
		t.Fatalf("expected %d 5m outcomes, got %d", len(expected5m), len(outcomes5m))
	}
	for i, exp := range expected5m {
		if outcomes5m[i] != exp {
			t.Fatalf("step %d: expected 5m outcome %s, got %s", i, exp, outcomes5m[i])
		}
	}
}

func BenchmarkDecide(b *testing.B) {
	base := fp(t0.Add(-48*time.Hour), 1, 5, 0, "h1")
	entry := &Entry{FP: base, CreatedAt: t0.Add(-1 * time.Hour)}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Decide(t0, base, entry, pol)
	}
}

// The headline invariant: whenever Decide serves from cache, the request is NOT newer than
// the entry. Checked over a few hundred thousand random fingerprint pairs.
func TestNoStaleServeInvariant(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	randFP := func() Fingerprint {
		if r.Intn(10) == 0 {
			return Fingerprint{} // evidence-free
		}
		n := t0.Add(-time.Duration(r.Intn(24*40)) * time.Hour)
		return Fingerprint{
			Newest:        n,
			CountAtNewest: 1 + r.Intn(3),
			Dated:         1 + r.Intn(8),
			Undated:       r.Intn(3),
			SetHash:       string(rune('a' + r.Intn(4))),
		}
	}
	for i := 0; i < 300000; i++ {
		in, ef := randFP(), randFP()
		e := &Entry{FP: ef, CreatedAt: t0.Add(-time.Duration(r.Intn(48)) * time.Hour)}
		p := Policy{TTL: 24 * time.Hour, FutureSkew: 24 * time.Hour, ExactSet: r.Intn(2) == 0}
		if Decide(t0, in, e, p).Serves() {
			if in.Newest.After(e.FP.Newest) || (in.Newest.Equal(e.FP.Newest) && in.CountAtNewest > e.FP.CountAtNewest) {
				t.Fatalf("STALE SERVE: in=%+v entry=%+v", in, e.FP)
			}
			if !in.Valid() {
				t.Fatalf("served without evidence: %+v", in)
			}
		}
	}
}
