# Cloud demo: one continuous take

Everything runs in one cloud session against the native gateway built by
`scripts/cloud-demo-setup.sh`. No clips from other machines. Spoken lines are in
`docs/DEMO_SCRIPT.md`; this file is only the commands, in order.

## 0. Off camera (gate: do not record unless step 0.3 prints PREFLIGHT PASS)

```bash
bash scripts/cloud-demo-setup.sh            # 0.1 build gateway + plugin, venv, asciinema, agg (~5 min cold)
source ~/.chronobrief-demo/demo.env         # 0.2 env for everything below
bash scripts/cloud-demo-setup.sh preflight  # 0.3 gateway health + real pricing + the live act Tavily retrieval (4 calls)
bash scripts/cloud-demo-setup.sh reset      # 0.4 empty cache + audit log, stripe fixture restored
```

Preflight must show `PASS gateway health`, no placeholder-pricing failure, and
`PASS live Tavily`. Placeholder pricing (`CB_ALLOW_PLACEHOLDER_PRICING=1`) makes every `$`
figure fake: never record with it.

## 1. Record the take

The take is the block between `# BEGIN TAKE` and `# END TAKE` below, run as one script under
one asciinema session. Commands are echoed with their variables unexpanded, so no key ever
appears on screen.

```bash
sed -n '/^# BEGIN TAKE/,/^# END TAKE/p' scripts/cloud-demo-record.md > ~/.chronobrief-demo/take.sh
asciinema rec --overwrite --cols 100 --rows 26 -c "bash ~/.chronobrief-demo/take.sh" ~/.chronobrief-demo/demo.cast
```

```bash
# BEGIN TAKE
set -u
cd "$(git rev-parse --show-toplevel)" && source ~/.chronobrief-demo/demo.env
act()  { printf '\n\033[1;33m==== %s ====\033[0m\n' "$*"; sleep 2; }
run()  { printf '\033[1;32m$\033[0m %s\n' "$1"; eval "$1"; sleep 3; }

act "Act 1 - The thesis: TTL frontier (250 synthetic lookups, seed 20261102)"
run '(cd gateway/plugins/freshcache && go test -count=1 -run TestFrontier -v ./internal/frontier/ | grep -E "^(--- |ok)")'
run "sed -n '/^| Strategy/,/^| \*\*freshcache/p' docs/evidence/ttl-frontier-report.md"

act "Act 2 - Cold miss -> warm hit (Stripe, replay fixtures) and Act 3 - breaking news -> refuse"
run 'python scripts/e2e_cache.py --subject Stripe'

act "Act 2b - Same pipeline on LIVE Tavily retrieval (Team Beta tenant, its own cache entry)"
run 'CHRONO_VK=$CHRONO_VK_BETA CHRONO_TAVILY_MODE=live python -m chronobrief Stripe | head -40'
run 'CHRONO_VK=$CHRONO_VK_BETA CHRONO_TAVILY_MODE=live python -m chronobrief Stripe | grep -E "^Cache:"'

act "Act 4 - Operator readiness: live metrics, restart with snapshot restore, SEC 8-K guard"
run 'curl -s -X POST -H "x-chrono-admin: metrics" -H "x-bf-vk: $CHRONO_VK_ALPHA" $BIFROST_URL/v1/chat/completions | head -14'
run 'bash scripts/cloud-demo-setup.sh restart'
run 'python gateway/examples/sec_8k_guard_demo.py'

act "Act 5 - Proof suite: negative tests, independent audit oracle, live hook overhead"
run 'python gateway/test_negative.py | grep -E "^(Result|OVERALL)"'
run 'python scripts/verify_audit.py data/audit/freshcache.jsonl'
run 'python gateway/measure_live_overhead.py | tail -4'
act "End of take"
# END TAKE
```

Act 2b note: the second live call prints `HIT` only if Tavily returns the same sources both
times. If a source moved in between, it prints `REFUSE_SET_CHANGED`. That is the plugin working
correctly: say so on camera, never re-take until it says HIT.

## 2. Render to 1080p MP4 (off camera)

```bash
source ~/.chronobrief-demo/demo.env && cd ~/.chronobrief-demo   # agg lives on this PATH
agg --font-family "DejaVu Sans Mono" --font-size 28 --idle-time-limit 4 demo.cast demo.gif
ffmpeg -y -i demo.gif -vf "scale=1920:1080:force_original_aspect_ratio=decrease:flags=lanczos,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=0x121314,format=yuv420p" \
  -c:v libx264 -crf 18 -movflags +faststart demo-1080p.mp4
```

At 28 pt, 100x26 cells render at about 1720x1070, so the text is scaled roughly 1:1 into
1920x1080.

## 3. After the take (off camera): evidence files

The take rewrites tracked evidence: `docs/evidence/live-overhead-25runs.txt`,
`docs/evidence/gateway-negative-tests.txt`, and a new `docs/evidence/e2e-cache-<stamp>.txt`.
Cloud numbers differ from the committed laptop/Docker numbers (HIT median ~0.24 ms native here
vs 0.95 ms committed), and `make evidence` checks that docs quote the committed file. Keep
copies, then restore:

```bash
mkdir -p ~/.chronobrief-demo/evidence && cp docs/evidence/live-overhead-25runs.txt docs/evidence/gateway-negative-tests.txt docs/evidence/e2e-cache-*.txt ~/.chronobrief-demo/evidence/
git checkout -- docs/evidence/ && git clean -n docs/evidence/   # review, then git clean -f docs/evidence/ for the new e2e file
```

Committing cloud evidence is a separate decision: if you do, update every doc that quotes the
overhead numbers in the same commit so `make check` stays green.
