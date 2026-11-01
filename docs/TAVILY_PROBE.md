# Tavily probe (T03)

Status: **VERIFIED.** Live run against Tavily API completed on 2026-11-01.

## How to run (on a machine with TAVILY_API_KEY in .env)

```bash
python scripts/tavily_probe.py                      # probe: one news search, saves raw JSON to docs/evidence/
python scripts/tavily_probe.py --capture "Stripe"   # capture a REAL replay fixture for the demo
```

Paste the block printed between `--- paste into docs/TAVILY_PROBE.md ---` and `--- end ---` below, and commit the evidence file.

## What the code currently assumes (confirmed)

- Endpoint `POST https://api.tavily.com/search`, auth header `Authorization: Bearer <key>`.
- Request: `topic="news"`, `start_date`/`end_date` as `YYYY-MM-DD`, `max_results=10`, `search_depth="basic"`.
- Each result has `url`, `title`, `content`, and (news topic) `published_date`.
- `fingerprint.parse_published` accepts RFC 2822, ISO-8601 (with or without `Z`/offset) and date-only. Any other format is treated as undated, never guessed.
- Results dated before the window are dropped by `retrieval._within_window`; undated results are kept and counted as undated.

## Resolution of fixture issue

`fixtures/tavily/stripe.json` and `stripe.base.json` have been replaced with a real capture from Tavily (`--capture "Stripe"`), containing 40 live news articles within the 30-day window.

---

## Probe output

```
Probe date (UTC): 2026-11-01T10:55:47+00:00
Query: "Stripe", topic=news, 2026-10-02..2026-11-01, max_results=10, search_depth=basic
Results returned: 10   Latency: 0.99s
Top-level response keys: ['answer', 'follow_up_questions', 'images', 'query', 'request_id', 'response_time', 'results']
Fields seen on results: ['content', 'id', 'published_date', 'raw_content', 'score', 'title', 'url']
published_date formats:
  10 x RFC 2822 (e.g. 'Mon, 29 Sep 2025 14:00:00 GMT')
Results dated before the window start (2026-10-02): 0
```
