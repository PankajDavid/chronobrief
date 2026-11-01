from datetime import UTC, datetime, timedelta

from chronobrief.fingerprint import (
    canonical_url,
    fingerprint,
    parse_published,
    sources_from_tavily,
)

NOW = datetime(2026, 11, 1, 10, 0, tzinfo=UTC)


def test_canonical_url_collapses_tracking_and_www():
    a = canonical_url("https://www.Example.com/News/Story/?utm_source=x&id=7#top")
    b = canonical_url("https://example.com/News/Story?id=7")
    assert a == b


def test_parse_published_formats():
    assert parse_published("Mon, 29 Sep 2025 14:00:00 GMT") == datetime(2025, 9, 29, 14, 0, tzinfo=UTC)
    assert parse_published("2026-10-30T08:15:00Z") == datetime(2026, 10, 30, 8, 15, tzinfo=UTC)
    assert parse_published("2026-10-30") == datetime(2026, 10, 30, tzinfo=UTC)
    assert parse_published("last tuesday") is None
    assert parse_published(None) is None
    assert parse_published("") is None


def test_future_dated_source_is_treated_as_undated():
    results = [
        {"url": "https://a.com/1", "published_date": "2026-10-30"},
        {"url": "https://a.com/2", "published_date": (NOW + timedelta(days=30)).strftime("%Y-%m-%d")},
    ]
    fp = fingerprint(sources_from_tavily(results, NOW))
    assert fp.dated == 1 and fp.undated == 1
    assert fp.newest == datetime(2026, 10, 30, tzinfo=UTC)


def test_duplicates_collapse_and_hash_is_order_independent():
    r1 = [{"url": "https://a.com/1?utm_x=1", "published_date": "2026-10-30"}, {"url": "https://b.com/2", "published_date": "2026-10-29"}]
    r2 = list(reversed(r1)) + [{"url": "https://www.a.com/1", "published_date": "2026-10-30"}]
    assert fingerprint(sources_from_tavily(r1, NOW)) == fingerprint(sources_from_tavily(r2, NOW))


def test_new_article_changes_fingerprint_in_the_right_direction():
    before = fingerprint(sources_from_tavily([{"url": "https://a.com/1", "published_date": "2026-10-30"}], NOW))
    after = fingerprint(
        sources_from_tavily(
            [{"url": "https://a.com/1", "published_date": "2026-10-30"}, {"url": "https://c.com/9", "published_date": "2026-11-01"}],
            NOW,
        )
    )
    assert after.newest > before.newest
    assert after.set_hash != before.set_hash


def test_same_timestamp_second_article_raises_count():
    one = fingerprint(sources_from_tavily([{"url": "https://a.com/1", "published_date": "2026-10-30"}], NOW))
    two = fingerprint(
        sources_from_tavily(
            [{"url": "https://a.com/1", "published_date": "2026-10-30"}, {"url": "https://b.com/2", "published_date": "2026-10-30"}], NOW
        )
    )
    assert two.newest == one.newest and two.count_at_newest == one.count_at_newest + 1


def test_no_dated_sources_yields_no_evidence_headers():
    fp = fingerprint(sources_from_tavily([{"url": "https://a.com/1"}], NOW))
    h = fp.to_headers()
    assert h["x-chrono-newest"] == "" and h["x-chrono-dated"] == "0" and h["x-chrono-undated"] == "1"


def test_corrected_article_title_changes_set_hash():
    r1 = [{"url": "https://a.com/1", "published_date": "2026-10-30", "title": "Original Headline"}]
    r2 = [{"url": "https://a.com/1", "published_date": "2026-10-30", "title": "Corrected Headline: Update"}]
    fp1 = fingerprint(sources_from_tavily(r1, NOW))
    fp2 = fingerprint(sources_from_tavily(r2, NOW))
    assert fp1.newest == fp2.newest
    assert fp1.set_hash != fp2.set_hash

