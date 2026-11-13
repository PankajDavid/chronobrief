# Clip A: LIVE mode, subject Cloudflare

- Recorded: 2026-11-08T10:32:30Z (UTC wall clock at video start)
- Commit of the code that ran: `82ce983bec7adb260e48d10d06cbb2e9787d2bb6`
- Search mode: `CHRONO_TAVILY_MODE=live`  (real Tavily search, real articles)
- Model behind Bifrost: `gemini-3.1-flash-lite`
- Gateway: Bifrost + freshcache plugin from `docker compose`
- Browser: Playwright Chromium, viewport 1920x1080
- Decisions shown on screen, in order: MISS -> HIT
- File: `demo/ui-recording/clip-A-v2.mp4`: H.264, 1920x1080, 30/1 fps, 86.93 s, 29.63 MB; the intermediate WebM was not kept
- Capture length 86.93 s, MP4 length 86.93 s: no cuts, no speed change
- Before the clip (off camera): audit log had 0 line(s); scoreboard read: "No cache decisions recorded yet. The gateway writes them to data\\audit\\freshcache\.jsonl."
- Scoreboard on screen matches this clip's audit lines: **YES** (table below)

## Timeline (seconds from the first frame of this clip)

Times come from the recording script's clock, started the moment the browser page (and so the video) was created; expect them to be within about 0.3 s of the video. Between actions the script holds about 2 s on each result (3 s on the scoreboard) and scrolls smoothly.

| t (s) | action | detail |
|---:|---|---|
| 0.00 | browser page opened (video starts) |  |
| 1.08 | dashboard loaded | search mode LIVE |
| 4.30 | typed subject | Cloudflare |
| 5.33 | click Generate brief | run 1 (expect MISS) |
| 23.38 | result banner | 🆕 MISS: first brief for this subject, freshly generated and stored. |
| 31.26 | reading section | News |
| 37.42 | reading section | Executive moves |
| 40.44 | reading section | Product launches |
| 43.76 | reading section | Questions to open with |
| 49.36 | reading section | Sources |
| 53.90 | end of source list |  |
| 61.49 | back to the form |  |
| 62.54 | click Generate brief | run 2, same subject (expect HIT) |
| 66.90 | result banner | ✅ HIT: served from cache. Newest source in this request: 06 Nov 2026, 10:00 UTC. Newest source behind the cached brief: 06 Nov 2026, 10:00 UTC. Saved about 3,588 tokens ($0.0016) |
| 76.26 | open expander | Cache scoreboard |
| 77.30 | scoreboard numbers | Reuse (hit) rate: 50%; AI tokens saved: 3,588; AI cost saved (USD): $0.0016; Stale briefs served: 0; Stale briefs prevented: 0; Median latency HIT / MISS: 0 / 2158 ms |
| 81.88 | scoreboard verdict | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1 |
| 82.45 | scoreboard recent decisions table, hold | 3 s |
| 86.97 | end of clip |  |

## Scoreboard on screen vs this clip's audit lines

Expected values are computed by `scripts/verify_audit.py --json` over this clip's audit lines only, formatted the way `app/streamlit_app.py` formats them.

| scoreboard field | on screen | expected from this clip's lines | match |
|---|---|---|---|
| Reuse (hit) rate | 50% | 50% | yes |
| AI tokens saved | 3,588 | 3,588 | yes |
| AI cost saved (USD) | $0.0016 | $0.0016 | yes |
| Stale briefs served | 0 | 0 | yes |
| Stale briefs prevented | 0 | 0 | yes |
| Independent check | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1 | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1 | yes |

## Gateway audit lines written during this clip (2)

From `data/audit/freshcache.jsonl` (lines 1..2). `tenant_id` is a hash of the virtual key, never the key. Also saved raw next to this file.

```json
{"timestamp":"2026-11-08T10:32:53Z","tenant_id":"64b56b721b34","cache_key":"8b5221df12854723d803bedc0d26b49f0c421a36dcdaa84088b1e6e0365f116e","decision":"MISS_COLD","request_newest":"2026-11-06T10:00:00Z","incoming_newest":"2026-11-06T10:00:00Z","request_count_at_newest":1,"incoming_count":1,"request_set_hash":"65522d9fefdd2790b2290124d72a48f463fae9379678b43268fab97846f5c260","prompt_tokens":3053,"completion_tokens":535,"total_tokens":3588,"saved_tokens":0,"cost_usd":0.0015657499999999999,"saved_cost_usd":0,"latency_ms":2158,"shadow_ttl_5m":"MISS_COLD","shadow_ttl_1h":"MISS_COLD","shadow_ttl_24h":"MISS_COLD","would_serve_stale_24h":false,"plugin_overhead_us":79}
{"timestamp":"2026-11-08T10:33:37Z","tenant_id":"64b56b721b34","cache_key":"8b5221df12854723d803bedc0d26b49f0c421a36dcdaa84088b1e6e0365f116e","decision":"HIT","request_newest":"2026-11-06T10:00:00Z","incoming_newest":"2026-11-06T10:00:00Z","request_count_at_newest":1,"incoming_count":1,"request_set_hash":"65522d9fefdd2790b2290124d72a48f463fae9379678b43268fab97846f5c260","cached_newest":"2026-11-06T10:00:00Z","entry_newest":"2026-11-06T10:00:00Z","cached_count_at_newest":1,"entry_count":1,"cached_set_hash":"65522d9fefdd2790b2290124d72a48f463fae9379678b43268fab97846f5c260","prompt_tokens":3053,"completion_tokens":535,"total_tokens":3588,"saved_tokens":3588,"cost_usd":0.0015657499999999999,"saved_cost_usd":0.0015657499999999999,"latency_ms":0,"shadow_ttl_5m":"HIT_FRESH","shadow_ttl_1h":"HIT_FRESH","shadow_ttl_24h":"HIT_FRESH","would_serve_stale_24h":false,"plugin_overhead_us":317}
```

## scripts/verify_audit.py on this clip's lines

```
lookups 2  hits 1  hit rate 50.0%
stale serves 0  (stale answers prevented: 0)
saved (LLM only): 3588 tokens  $0.0016
decisions {'MISS_COLD': 1, 'HIT': 1}
benchmark (expected until measured against audit log): 5-min TTL (high false miss) vs 24h TTL (high stale serve) vs ChronoBrief (0% stale serve)
pitch: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."
(exit code 0)
```

## scripts/verify_audit.py on the whole audit log at the end of this clip

```
lookups 2  hits 1  hit rate 50.0%
stale serves 0  (stale answers prevented: 0)
saved (LLM only): 3588 tokens  $0.0016
decisions {'MISS_COLD': 1, 'HIT': 1}
benchmark (expected until measured against audit log): 5-min TTL (high false miss) vs 24h TTL (high stale serve) vs ChronoBrief (0% stale serve)
pitch: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."
(exit code 0)
```

## Conversion

```
ffmpeg -y -hide_banner -loglevel error -i page@1ac80cbe37ded9ac0299acb5ff8e3461.webm -c:v libx264 -preset slow -crf 18 -pix_fmt yuv420p -fps_mode cfr -r 30 -movflags +faststart -an clip-A-v2.mp4
```

## Notes

- Take 2. Take 1 (Nvidia, 2026-11-08 ~10:28-10:30Z on the same cold gateway) showed MISS_COLD then REFUSE_SET_CHANGED: live Tavily dropped the newest Nvidia article (2026-11-07T21:17:08Z) between the two searches, so the plugin correctly refused to reuse the brief. That take is kept locally, not committed. The gateway was stopped, its audit log and snapshot moved aside, and restarted cold before this take; Cloudflare had not been used before.
