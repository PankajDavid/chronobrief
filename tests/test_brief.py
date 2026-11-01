from datetime import UTC, datetime, timedelta

from chronobrief.brief import Brief, Claim, Icebreaker, assign_ids, validate_brief
from chronobrief.fingerprint import Source

NOW = datetime(2026, 11, 1, 10, 0, tzinfo=UTC)


def srcs():
    return assign_ids(
        [
            Source(url="https://a.com/1", title="Acme hires new CFO", published=NOW - timedelta(days=3), snippet="Acme named Jane Doe CFO. Revenue was 4.2 billion."),
            Source(url="https://b.com/2", title="Old news", published=NOW - timedelta(days=90), snippet="Something old"),
            Source(url="https://c.com/3", title="Undated piece", published=None, snippet="Acme opened 12 offices"),
        ]
    )


def test_unsupported_and_unknown_claims_are_dropped_not_rewritten():
    b = Brief(
        subject="Acme",
        claims=[
            Claim(id="c1", section="hires", text="Acme named a new CFO", source_ids=["S1"]),
            Claim(id="c2", section="news", text="Acme is expanding", source_ids=[]),
            Claim(id="c3", section="news", text="Acme won an award", source_ids=["S9"]),
        ],
    )
    out, removed = validate_brief(b, srcs(), NOW)
    assert [c.id for c in out.claims] == ["c1"]
    assert {r.ref for r in removed} == {"c2", "c3"}


def test_numbers_must_appear_in_cited_text():
    b = Brief(
        subject="Acme",
        claims=[
            Claim(id="c1", section="news", text="Revenue was 4.2 billion", source_ids=["S1"]),
            Claim(id="c2", section="news", text="Revenue was 9.9 billion", source_ids=["S1"]),
        ],
    )
    out, removed = validate_brief(b, srcs(), NOW)
    assert [c.id for c in out.claims] == ["c1"]
    assert "9.9" in removed[0].reason


def test_claim_citing_only_out_of_window_source_is_dropped():
    b = Brief(subject="Acme", claims=[Claim(id="c1", section="news", text="Something old happened", source_ids=["S2"])])
    out, removed = validate_brief(b, srcs(), NOW)
    assert out.claims == [] and "outside the window" in removed[0].reason


def test_undated_only_claim_kept_but_flagged():
    b = Brief(subject="Acme", claims=[Claim(id="c1", section="news", text="Acme opened 12 offices", source_ids=["S3"])])
    out, _ = validate_brief(b, srcs(), NOW)
    assert out.claims[0].date_unverified is True


def test_icebreaker_needs_a_surviving_claim():
    b = Brief(
        subject="Acme",
        claims=[Claim(id="c1", section="hires", text="Acme named a new CFO", source_ids=["S1"]), Claim(id="c2", section="news", text="x", source_ids=[])],
        icebreakers=[Icebreaker(text="What will the new CFO change?", motivated_by="c1"), Icebreaker(text="Why expand?", motivated_by="c2")],
    )
    out, removed = validate_brief(b, srcs(), NOW)
    assert len(out.icebreakers) == 1 and any(r.ref == "icebreaker 2" for r in removed)


def test_model_cannot_set_date_unverified_to_hide_it():
    b = Brief(subject="Acme", claims=[Claim(id="c1", section="hires", text="Acme named a new CFO", source_ids=["S1"], date_unverified=True)])
    out, _ = validate_brief(b, srcs(), NOW)
    assert out.claims[0].date_unverified is False


def test_meeting_time_and_freshness_age_days_computed_deterministically():
    b = Brief(
        subject="Acme",
        meeting_time="2026-11-02T15:00:00Z",
        claims=[Claim(id="c1", section="hires", text="Acme named a new CFO", source_ids=["S1"])],
        icebreakers=[Icebreaker(text="How does the new CFO view Q4?", motivated_by="c1")],
    )
    out, removed = validate_brief(b, srcs(), NOW)
    assert not removed
    assert out.meeting_time == "2026-11-02T15:00:00Z"
    assert out.claims[0].freshness_age_days == 3
    assert len(out.open_questions) == 1
