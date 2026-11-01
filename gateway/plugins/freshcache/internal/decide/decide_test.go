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
