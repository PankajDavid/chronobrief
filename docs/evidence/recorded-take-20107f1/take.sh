# BEGIN TAKE
set -u
cd "$(git rev-parse --show-toplevel)" && source ~/.chronobrief-demo/demo.env
act()  { printf '\n\033[1;33m==== %s ====\033[0m\n' "$*"; sleep 2; }
run()  { printf '\033[1;32m$\033[0m %s\n' "$1"; eval "$1"; sleep 3; }

act "Act 0 - Which code is running, and a cold gateway"
run 'git rev-parse --short HEAD'
run 'curl -s -X POST -H "x-chrono-admin: metrics" -H "x-bf-vk: $CHRONO_VK_ALPHA" $BIFROST_URL/v1/chat/completions | head -6'

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
act "Act 5b - One normal uncached LLM call through the gateway (no freshness headers, so never cached)"
run 'curl -s -o /dev/null -D - -w "uncached LLM call total: %{time_total}s\n" -H "Authorization: Bearer $CHRONO_VK_ALPHA" -H "x-bf-vk: $CHRONO_VK_ALPHA" -H "Content-Type: application/json" -d "{\"model\":\"gemini/gemini-3.1-flash-lite\",\"messages\":[{\"role\":\"user\",\"content\":\"In three bullet points, what does a payments company like Stripe do?\"}]}" $BIFROST_URL/v1/chat/completions | grep -iE "^HTTP|x-chrono-cache:|uncached LLM"'

act "End of take"
# END TAKE
