# Clip B: REPLAY mode, subject Stripe

- Recorded: 2026-11-08T10:35:19Z (UTC wall clock at video start)
- Commit of the code that ran: `82ce983bec7adb260e48d10d06cbb2e9787d2bb6`
- Search mode: `CHRONO_TAVILY_MODE=replay`  (recorded Tavily search from fixtures/; the injected article is badged SIMULATED)
- Model behind Bifrost: `gemini-3.1-flash-lite`
- Gateway: Bifrost + freshcache plugin from `docker compose`
- Browser: Playwright Chromium, viewport 1920x1080
- Decisions shown on screen, in order: MISS -> HIT -> REFUSED STALE
- File: `demo/ui-recording/clip-B-v2.mp4`: H.264, 1920x1080, 30/1 fps, 97.10 s, 36.74 MB; the intermediate WebM was not kept
- Capture length 97.10 s, MP4 length 97.10 s: no cuts, no speed change
- Before the clip (off camera): audit log had 0 line(s); scoreboard read: "No cache decisions recorded yet. The gateway writes them to data\\audit\\freshcache\.jsonl."
- Scoreboard on screen matches this clip's audit lines: **YES** (table below)

## Timeline (seconds from the first frame of this clip)

Times come from the recording script's clock, started the moment the browser page (and so the video) was created; expect them to be within about 0.3 s of the video. Between actions the script holds about 2 s on each result (3 s on the scoreboard) and scrolls smoothly.

| t (s) | action | detail |
|---:|---|---|
| 0.00 | browser page opened (video starts) |  |
| 1.10 | dashboard loaded | search mode REPLAY |
| 3.86 | typed subject | Stripe |
| 4.90 | click Generate brief | run 1 from a cold cache (expect MISS) |
| 9.88 | result banner | 🆕 MISS: first brief for this subject, freshly generated and stored. |
| 12.93 | click Generate brief | run 2, same recorded search (expect HIT) |
| 15.40 | result banner | ✅ HIT: served from cache. Newest source in this request: 31 Oct 2026, 17:11 UTC. Newest source behind the cached brief: 31 Oct 2026, 17:11 UTC. Saved about 5,491 tokens ($0.0021) |
| 28.13 | open expander | Demo controls |
| 29.18 | click Simulate breaking news |  |
| 31.55 | open expander | Demo controls |
| 33.08 | simulated article added | Added: [SIMULATED] Stripe announces major strategic product expansion and executive appointment (2026-11-08T10:35:48Z). Now press Generate brief. |
| 44.38 | back to the form |  |
| 45.41 | click Generate brief | run 3 after breaking news (expect REFUSE) |
| 50.39 | result banner | 🔄 REFUSED STALE: a newer source arrived (newest now 08 Nov 2026, 10:35 UTC), so the stored brief was not reused and a fresh one was generated. |
| 52.40 | SIMULATED badges visible in the brief and sources | 1 |
| 69.68 | showing SIMULATED badge | [SIMULATED] Stripe announces major strategic product expansion and executive appointment — example.com, 08 Nov 2026 SIMULATED (demo only) |
| 76.03 | open expander | Cache scoreboard |
| 77.07 | scoreboard numbers | Reuse (hit) rate: 33%; AI tokens saved: 5,491; AI cost saved (USD): $0.0021; Stale briefs served: 0; Stale briefs prevented: 1; Median latency HIT / MISS: 0 / 2545 ms |
| 82.18 | scoreboard verdict | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1, REFUSE_NEWER_SOURCE 1 |
| 82.77 | scoreboard recent decisions table, hold | 3 s |
| 87.14 | open expander | Demo controls |
| 89.77 | click Reset demo |  |
| 92.16 | open expander | Demo controls |
| 93.59 | reset confirmed | Recorded search restored. Simulated articles removed. |
| 97.10 | end of clip |  |

## Scoreboard on screen vs this clip's audit lines

Expected values are computed by `scripts/verify_audit.py --json` over this clip's audit lines only, formatted the way `app/streamlit_app.py` formats them.

| scoreboard field | on screen | expected from this clip's lines | match |
|---|---|---|---|
| Reuse (hit) rate | 33% | 33% | yes |
| AI tokens saved | 5,491 | 5,491 | yes |
| AI cost saved (USD) | $0.0021 | $0.0021 | yes |
| Stale briefs served | 0 | 0 | yes |
| Stale briefs prevented | 1 | 1 | yes |
| Independent check | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1, REFUSE_NEWER_SOURCE 1 | Independent check (scripts/verify_audit.py): PASS · decisions: HIT 1, MISS_COLD 1, REFUSE_NEWER_SOURCE 1 | yes |

## Gateway audit lines written during this clip (3)

From `data/audit/freshcache.jsonl` (lines 1..3). `tenant_id` is a hash of the virtual key, never the key. Also saved raw next to this file.

```json
{"timestamp":"2026-11-08T10:35:29Z","tenant_id":"64b56b721b34","cache_key":"d7fe6da04bfdc70aa8b84b13b9754b0d64080fd42044bd0df92e921d74970754","decision":"MISS_COLD","request_newest":"2026-10-31T17:11:00Z","incoming_newest":"2026-10-31T17:11:00Z","request_count_at_newest":1,"incoming_count":1,"request_set_hash":"9ea4a2273a3b247a83e038d69029b553e70376e91e8f7d0a83814bf529c469f1","prompt_tokens":4926,"completion_tokens":565,"total_tokens":5491,"saved_tokens":0,"cost_usd":0.002079,"saved_cost_usd":0,"latency_ms":2545,"shadow_ttl_5m":"MISS_COLD","shadow_ttl_1h":"MISS_COLD","shadow_ttl_24h":"MISS_COLD","would_serve_stale_24h":false,"plugin_overhead_us":91}
{"timestamp":"2026-11-08T10:35:34Z","tenant_id":"64b56b721b34","cache_key":"d7fe6da04bfdc70aa8b84b13b9754b0d64080fd42044bd0df92e921d74970754","decision":"HIT","request_newest":"2026-10-31T17:11:00Z","incoming_newest":"2026-10-31T17:11:00Z","request_count_at_newest":1,"incoming_count":1,"request_set_hash":"9ea4a2273a3b247a83e038d69029b553e70376e91e8f7d0a83814bf529c469f1","cached_newest":"2026-10-31T17:11:00Z","entry_newest":"2026-10-31T17:11:00Z","cached_count_at_newest":1,"entry_count":1,"cached_set_hash":"9ea4a2273a3b247a83e038d69029b553e70376e91e8f7d0a83814bf529c469f1","prompt_tokens":4926,"completion_tokens":565,"total_tokens":5491,"saved_tokens":5491,"cost_usd":0.002079,"saved_cost_usd":0.002079,"latency_ms":0,"shadow_ttl_5m":"HIT_FRESH","shadow_ttl_1h":"HIT_FRESH","shadow_ttl_24h":"HIT_FRESH","would_serve_stale_24h":false,"plugin_overhead_us":170}
{"timestamp":"2026-11-08T10:36:09Z","tenant_id":"64b56b721b34","cache_key":"d7fe6da04bfdc70aa8b84b13b9754b0d64080fd42044bd0df92e921d74970754","decision":"REFUSE_NEWER_SOURCE","request_newest":"2026-11-08T10:35:48Z","incoming_newest":"2026-11-08T10:35:48Z","request_count_at_newest":1,"incoming_count":1,"request_set_hash":"273936963544262d16c258243a5e947d3b8a3dffb2f0a982ce2ad7c1e178a295","prompt_tokens":4865,"completion_tokens":555,"total_tokens":5420,"saved_tokens":0,"cost_usd":0.00204875,"saved_cost_usd":0,"latency_ms":2367,"shadow_ttl_5m":"HIT_STALE","shadow_ttl_1h":"HIT_STALE","shadow_ttl_24h":"HIT_STALE","would_serve_stale_24h":true,"plugin_overhead_us":251}
```

## scripts/verify_audit.py on this clip's lines

```
lookups 3  hits 1  hit rate 33.3%
stale serves 0  (stale answers prevented: 1)
saved (LLM only): 5491 tokens  $0.0021
decisions {'MISS_COLD': 1, 'HIT': 1, 'REFUSE_NEWER_SOURCE': 1}
benchmark (expected until measured against audit log): 5-min TTL (high false miss) vs 24h TTL (high stale serve) vs ChronoBrief (0% stale serve)
pitch: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."
(exit code 0)
```

## scripts/verify_audit.py on the whole audit log at the end of this clip

```
lookups 3  hits 1  hit rate 33.3%
stale serves 0  (stale answers prevented: 1)
saved (LLM only): 5491 tokens  $0.0021
decisions {'MISS_COLD': 1, 'HIT': 1, 'REFUSE_NEWER_SOURCE': 1}
benchmark (expected until measured against audit log): 5-min TTL (high false miss) vs 24h TTL (high stale serve) vs ChronoBrief (0% stale serve)
pitch: "Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."
(exit code 0)
```

## Conversion

```
ffmpeg -y -hide_banner -loglevel error -i page@d95e185be59a6f0996cef3c592fd3c9a.webm -c:v libx264 -preset slow -crf 18 -pix_fmt yuv420p -fps_mode cfr -r 30 -movflags +faststart -an clip-B-v2.mp4
```

## Notes

- Recorded from a cold gateway: after Clip A v2 the gateway was stopped, its audit log and snapshot moved aside (kept locally, not committed), and restarted with 0 cache entries, so this clip's scoreboard counts only this clip.

## Reviewer notes (added by hand after recording, from watching clip-B-v2.mp4)

- SIMULATED is visible twice after the REFUSE: the caption "ℹ 1 SIMULATED article(s) included for the demo." right under the REFUSE banner (about 50-52 s), and source 5 in the source list, badged "SIMULATED (demo only)" (69.68 s). In this run the regenerated brief did not cite the simulated article in any claim (in clip B v1 it did), so there is no claim-level badge; the script counts only the visible badge in the brief and sources, hence "1".
- After Reset demo, fixtures/tavily/stripe.json matched the committed file again (git status clean).
