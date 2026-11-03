"""Cross-language parity: chronobrief/fingerprint.py (reference) vs gateway/sdk/freshness.ts.

The TS SDK promises byte-for-byte identical canonical URLs and set_hash. This test runs BOTH
implementations on the same inputs and compares their outputs directly; no expected values are
typed in. Python is the reference because it is what the live app sends to the gateway.

Node runs the TS file with type stripping. It is run with TZ=Asia/Kolkata on purpose, so any
local-time date parsing in the SDK shows up as a mismatch. Skipped only when node is missing.
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest

from chronobrief.fingerprint import canonical_url, fingerprint, sources_from_tavily

ROOT = Path(__file__).resolve().parent.parent
SDK = ROOT / "gateway" / "sdk" / "freshness.ts"
NOW = datetime(2026, 11, 2, 12, 0, tzinfo=UTC)

URLS = [
    "https://www.Example.com/News/Story/",
    "https://example.com/news?q=hello world",
    "https://example.com/news?b=2&a=1&utm_source=x&fbclid=y#frag",
    "https://example.com/news?a=&b=1",
    "https://example.com/news?k=2&k=1",
    "https://example.com/news?B=1&a=2",
    "https://example.com/news?q=it's(1)!*~",
    "https://example.com/caf%C3%A9/a%20b",
    "https://example.com/news?q=caf%C3%A9",
    "HTTP://EXAMPLE.COM/Path",
    "https://example.com:8443/x",
    "example.com/x",
    "example.com:8080/x",
    "https://user:pw@Example.com/x",
    "https://[::1]:80/x",
    "https://a.com/x?a=1;b=2",
    "https://a.com/x?q=%E0%A4",
    "https://a.com/x?q=a+b",
    "https://a.com/x?=v&k",
    "https://a.com/x?q=%zz%41",
    "https://a.com/x?q=%F0%9F%93%88&r=\u00e9",
    "mailto:Bob@x.com",
    "  https://a.com/x/  ",
]

CASES: dict[str, list[dict[str, Any]]] = {
    "rfc2822_and_iso": [
        {"url": "https://a.com/1", "title": "One", "published_date": "Mon, 26 Oct 2026 14:00:00 GMT"},
        {"url": "https://a.com/2", "title": "Two", "published_date": "2026-10-30T09:15:00Z"},
    ],
    "bare_date": [
        {"url": "https://a.com/1", "title": "Bare", "published_date": "2026-10-30"},
    ],
    "naive_datetime_no_zone": [
        {"url": "https://a.com/1", "title": "Naive", "published_date": "2026-10-30T10:00:00"},
    ],
    "offset_datetime": [
        {"url": "https://a.com/1", "title": "Offset", "published_date": "2026-10-30T10:00:00+05:30"},
    ],
    "fractional_seconds_tie": [
        {"url": "https://a.com/1", "title": "A", "published_date": "2026-10-30T10:00:00.400Z"},
        {"url": "https://a.com/2", "title": "B", "published_date": "2026-10-30T10:00:00.900Z"},
    ],
    "undated_future_and_garbage": [
        {"url": "https://a.com/1", "title": "No date", "published_date": None},
        {"url": "https://a.com/2", "title": "Future", "published_date": "2026-11-20T00:00:00Z"},
        {"url": "https://a.com/3", "title": "Garbage", "published_date": "yesterday-ish"},
        {"url": "https://a.com/4", "title": "Dated", "published_date": "2026-10-01T00:00:00Z"},
    ],
    "duplicates_keep_newest": [
        {"url": "https://www.a.com/x/?utm_source=t", "title": "Old copy", "published_date": "2026-10-01T00:00:00Z"},
        {"url": "https://a.com/x", "title": "New copy", "published_date": "2026-10-05T00:00:00Z"},
    ],
    "title_whitespace": [
        {"url": "https://a.com/1", "title": "  Spaced \t out\n title  ", "published_date": "2026-10-30T00:00:00Z"},
    ],
    "sort_order_case": [
        {"url": "https://a.com/Zeta", "title": "Z", "published_date": "2026-10-30T00:00:00Z"},
        {"url": "https://a.com/alpha", "title": "a", "published_date": "2026-10-30T00:00:00Z"},
        {"url": "https://a.com/_under", "title": "u", "published_date": "2026-10-30T00:00:00Z"},
        {"url": "https://a.com/Beta-2", "title": "B", "published_date": "2026-10-30T00:00:00Z"},
    ],
    "rfc_variants": [
        {"url": "https://a.com/1", "title": "no zone", "published_date": "Mon, 26 Oct 2026 14:00:00"},
        {"url": "https://a.com/2", "title": "offset", "published_date": "26 Oct 2026 14:00 +0530"},
        {"url": "https://a.com/3", "title": "minus zero", "published_date": "Mon, 26 Oct 2026 14:00:00 -0000"},
        {"url": "https://a.com/4", "title": "EST", "published_date": "Mon, 26 Oct 2026 09:00:00 EST"},
    ],
    "iso_variants": [
        {"url": "https://a.com/1", "title": "space sep", "published_date": "2026-10-30 10:00:00"},
        {"url": "https://a.com/2", "title": "micro", "published_date": "2026-10-30T10:00:00.123456+01:00"},
        {"url": "https://a.com/3", "title": "hour only", "published_date": "2026-10-29T07"},
        {"url": "https://a.com/4", "title": "bad month", "published_date": "2026-13-01"},
        {"url": "https://a.com/5", "title": "not a date", "published_date": "Oct 30 2026"},
    ],
    "unicode_whitespace_title": [
        {"url": "https://a.com/1", "title": "a\u00a0b\u2003c\x1fd", "published_date": "2026-10-30T00:00:00Z"},
    ],
    "query_space_in_source": [
        {"url": "https://example.com/news?q=hello world", "title": "Q", "published_date": "2026-10-30T00:00:00Z"},
    ],
}

RUNNER = """
import { canonicalUrl, computeFingerprint } from %(sdk)s;
const input = JSON.parse(process.argv[2]);
const now = new Date(input.now);
const urls = input.urls.map((u) => canonicalUrl(u));
const cases = {};
for (const [name, rows] of Object.entries(input.cases)) {
  const fp = computeFingerprint(
    rows.map((r) => ({ url: r.url, title: r.title, publishedDate: r.published_date, content: "" })),
    now,
  );
  cases[name] = {
    newest: fp.newest ? fp.newest.toISOString().replace(/\\.\\d{3}Z$/, "Z") : null,
    count_at_newest: fp.countAtNewest,
    dated: fp.dated,
    undated: fp.undated,
    set_hash: fp.setHash,
  };
}
console.log(JSON.stringify({ urls, cases }));
"""


def _python_side() -> dict[str, Any]:
    cases: dict[str, dict[str, Any]] = {}
    for name, rows in CASES.items():
        fp = fingerprint(sources_from_tavily(rows, NOW))
        cases[name] = {
            "newest": fp.newest.strftime("%Y-%m-%dT%H:%M:%SZ") if fp.newest else None,
            "count_at_newest": fp.count_at_newest,
            "dated": fp.dated,
            "undated": fp.undated,
            "set_hash": fp.set_hash,
        }
    return {"urls": [canonical_url(u) for u in URLS], "cases": cases}


@pytest.fixture(scope="module")
def ts_side(tmp_path_factory: pytest.TempPathFactory) -> dict[str, Any]:
    node = shutil.which("node")
    if node is None:
        pytest.skip("node not installed: TS SDK parity not checked")
    version = subprocess.run([node, "--version"], capture_output=True, text=True, check=False).stdout
    m = re.match(r"v(\d+)\.(\d+)", version.strip())
    if m is None or (int(m.group(1)), int(m.group(2))) < (22, 6):
        pytest.skip(f"node {version.strip()} cannot strip TS types (needs >= 22.6)")
    runner = tmp_path_factory.mktemp("parity") / "runner.mts"
    runner.write_text(RUNNER % {"sdk": json.dumps(SDK.as_uri())}, encoding="utf-8")
    payload = json.dumps({"now": NOW.isoformat(), "urls": URLS, "cases": CASES})
    proc = subprocess.run(
        [node, "--experimental-strip-types", "--no-warnings", str(runner), payload],
        capture_output=True,
        text=True,
        timeout=60,
        env={**os.environ, "TZ": "Asia/Kolkata"},
        check=False,
    )
    assert proc.returncode == 0, f"node failed:\n{proc.stderr}"
    result: dict[str, Any] = json.loads(proc.stdout)
    return result


@pytest.mark.parametrize("i", range(len(URLS)), ids=URLS)
def test_canonical_url_parity(ts_side: dict[str, Any], i: int) -> None:
    py = canonical_url(URLS[i])
    assert ts_side["urls"][i] == py, f"{URLS[i]!r}: python={py!r} ts={ts_side['urls'][i]!r}"


@pytest.mark.parametrize("name", list(CASES))
def test_fingerprint_parity(ts_side: dict[str, Any], name: str) -> None:
    py = _python_side()["cases"][name]
    assert ts_side["cases"][name] == py, f"{name}: python={py} ts={ts_side['cases'][name]}"
