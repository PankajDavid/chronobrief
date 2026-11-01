# Tavily News API Probe & Payload Contract

Verified baseline for Tavily search queries executed by the ChronoBrief agent.

## Request Contract
```json
{
  "query": "<normalized subject> news",
  "topic": "news",
  "days": 30,
  "max_results": 10,
  "include_answer": false,
  "include_raw_content": false
}
```

## Response Schema & Fields
Tavily returns a list of results in `results`:
- `url` (string): Target source article URL. Canonicalized by `canonical_url` (strip `www.`, tracking query parameters, lowercase scheme and host).
- `title` (string): Headline/title of the article. Whitespace-normalized and included in the source hash.
- `published_date` (string): Published timestamp. Expected formats include:
  - ISO-8601 UTC string (`YYYY-MM-DDTHH:MM:SSZ` or with timezone offset)
  - RFC 2822 format (e.g. `Mon, 29 Sep 2025 14:00:00 GMT`)
  - Date-only ISO string (`YYYY-MM-DD`, treated as 00:00:00 UTC)
- `content` (string): Article snippet or extract, used as evidence context for prompt synthesis.

## Timestamp & Freshness Rules
1. All dates are parsed into aware UTC `datetime` objects.
2. If `published_date` is unparseable or missing, the source is treated as `undated` and never contributes to `newest`.
3. Sources dated beyond `now + FUTURE_SKEW` (24h) are rejected as future-dated/untrustworthy and treated as undated.
4. Fingerprint set hash includes sorted lines of `url|published_utc|title`.
