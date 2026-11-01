# Tavily probe (T03)

Status: **PENDING LIVE RUN.** Nothing below the line is verified until someone runs the probe with a real key and pastes the output.

## How to run (on a machine with TAVILY_API_KEY in .env)

```bash
python scripts/tavily_probe.py                      # probe: one news search, saves raw JSON to docs/evidence/
python scripts/tavily_probe.py --capture "Stripe"   # capture a REAL replay fixture for the demo
```

Paste the block printed between `--- paste into docs/TAVILY_PROBE.md ---` and `--- end ---` below, and commit the evidence file.

## What the code currently assumes (to be confirmed or corrected)

- Endpoint `POST https://api.tavily.com/search`, auth header `Authorization: Bearer <key>`.
- Request: `topic="news"`, `start_date`/`end_date` as `YYYY-MM-DD`, `max_results=10`, `search_depth="basic"`.
- Each result has `url`, `title`, `content`, and (news topic) `published_date`.
- `fingerprint.parse_published` accepts RFC 2822, ISO-8601 (with or without `Z`/offset) and date-only. Any other format is treated as undated, never guessed.
- Results dated before the window are dropped by `retrieval._within_window`; undated results are kept and counted as undated.

If the probe shows a different date format, update `parse_published` and its tests in the same commit.

## Known issue with the current fixtures

`fixtures/tavily/stripe.json` and `stripe.base.json` were written by hand: their articles are invented but carry real outlet domains (reuters.com, techcrunch.com, bloomberg.com) and `"simulated": false`. Showing invented news attributed to real publishers breaks invariant 5 and is exactly the misattribution publishers are suing AI companies over. Replace them with a real capture (`--capture "Stripe"`) before any demo; keep hand-made articles only via `scripts/inject_article.py`, which badges them SIMULATED.

---

## Probe output

_(paste here)_
