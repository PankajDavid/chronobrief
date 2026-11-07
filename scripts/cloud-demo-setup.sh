#!/usr/bin/env bash
# ChronoBrief cloud demo: rebuild the native Bifrost + freshcache gateway, the Python env and
# the recording tools in a fresh Claude Code cloud session, from the repo root, in one command.
#
#   bash scripts/cloud-demo-setup.sh            # = setup: build everything (idempotent, ~5 min cold)
#   bash scripts/cloud-demo-setup.sh preflight  # gateway health + the live act Tavily retrieval, PASS/FAIL
#   bash scripts/cloud-demo-setup.sh start|stop|restart|reset|status
#   source ~/.chronobrief-demo/demo.env         # env for every demo command (venv, agg, keys)
#
# Why native and not `docker compose up`: inside the container, Bifrost's HTTP client (fasthttp)
# ignores HTTPS_PROXY and goes straight to Google, so the session proxy never adds the Gemini key,
# and the image's config.json has no proxy_config. Native is the one environment we record in.
#
# The five cloud workarounds, all applied to a GENERATED config outside the repo
# (gateway/config.json is never touched):
#   1. Bifrost pricing / model-parameter data is loaded from local file:// copies of the real
#      getbifrost.ai datasheet (Bifrost validates that URL with direct DNS, which fails here).
#      freshcache's cost_usd / saved_cost_usd comes from this datasheet, so setup refuses to
#      continue without the real one. CB_ALLOW_PLACEHOLDER_PRICING=1 skips that for smoke tests
#      only: every $ figure is then fake and must never be recorded.
#   2. providers.gemini.proxy_config points at the session proxy, so the proxy injects the key.
#   3. Virtual keys are generated locally with the sk-bf- prefix (Bifrost replaces others).
#   4. GEMINI_API_KEY is a placeholder; the session proxy swaps in the real key.
#   5. The audit log and snapshot go to the repo's gitignored data/audit/, the path that
#      gateway/test_negative.py and scripts/verify_audit.py read (same as docker-compose mounts).
#
# No secrets live in this file. Generated virtual keys go to ~/.chronobrief-demo/demo.env (0600).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
H="${CB_DEMO_HOME:-$HOME/.chronobrief-demo}"
PORT="${CB_GATEWAY_PORT:-8080}"
AGG_TAG="v1.5.0"
ASCIINEMA_VERSION="2.4.0"
DATASHEET_URL="https://getbifrost.ai/datasheet"
PARAMS_URL="https://getbifrost.ai/datasheet/model-parameters"
MODEL="gemini-3.1-flash-lite"
CA_BUNDLE="${SSL_CERT_FILE:-/root/.ccr/ca-bundle.crt}"

# Pins come from the repo so this script cannot drift from the Docker build.
BIFROST_COMMIT="$(sed -n 's/^ARG BIFROST_COMMIT=//p' "$ROOT/Dockerfile")"
GO_VERSION="$(awk '$1=="go"{print $2; exit}' "$ROOT/gateway/plugins/freshcache/go.mod")"
export GOTOOLCHAIN="go${GO_VERSION}"

log()  { printf '\033[1;36m[cloud-demo]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[cloud-demo] FAIL:\033[0m %s\n' "$*" >&2; exit 1; }

build_gateway() {
  command -v go >/dev/null || die "go not installed"
  command -v gcc >/dev/null || die "gcc not installed (CGO is required for the plugin)"
  [ -n "$BIFROST_COMMIT" ] || die "could not read BIFROST_COMMIT from Dockerfile"
  mkdir -p "$H/bin"
  if [ ! -d "$H/bifrost/.git" ]; then
    log "cloning maximhq/bifrost"
    git clone -q https://github.com/maximhq/bifrost "$H/bifrost"
  fi
  git -C "$H/bifrost" -c advice.detachedHead=false checkout -q "$BIFROST_COMMIT"
  if [ ! -x "$H/bin/bifrost-http" ] || [ "$(cat "$H/bin/.bifrost-commit" 2>/dev/null)" != "$BIFROST_COMMIT" ]; then
    log "building bifrost-http @ ${BIFROST_COMMIT:0:7} with $GOTOOLCHAIN (2-3 min)"
    (cd "$H/bifrost/transports/bifrost-http" && mkdir -p ui && touch ui/index.html \
      && CGO_ENABLED=1 go build -tags sqlite_static -o "$H/bin/bifrost-http" . > "$H/build.log" 2>&1) \
      || { tail -20 "$H/build.log"; die "bifrost-http build failed"; }
    echo "$BIFROST_COMMIT" > "$H/bin/.bifrost-commit"
  fi
  log "building freshcache.so with the same toolchain"
  (cd "$ROOT/gateway/plugins/freshcache" && CGO_ENABLED=1 go build -buildmode=plugin -o "$H/bin/freshcache.so" main.go)
  bash "$ROOT/scripts/check_plugin_symbols.sh" "$H/bin/freshcache.so" >/dev/null || die "plugin symbol check failed"
  log "plugin symbols: all 7 present"
}

fetch_pricing() {
  mkdir -p "$H/pricing"
  echo '{}' > "$H/pricing/mcp-library.json"
  if curl -fsS -m 30 "$DATASHEET_URL" -o "$H/pricing/datasheet.json.tmp" \
     && python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if any(sys.argv[2] in k for k in d) else 1)' \
          "$H/pricing/datasheet.json.tmp" "$MODEL"; then
    mv "$H/pricing/datasheet.json.tmp" "$H/pricing/datasheet.json"
    rm -f "$H/pricing/PLACEHOLDER"
    log "real Bifrost datasheet saved (contains $MODEL)"
  elif [ "${CB_ALLOW_PLACEHOLDER_PRICING:-0}" = "1" ]; then
    rm -f "$H/pricing/datasheet.json.tmp"
    python3 - "$H/pricing/datasheet.json" "$MODEL" <<'PY'
import json, sys
json.dump({f"gemini/{sys.argv[2]}": {"provider": "gemini", "mode": "chat",
           "input_cost_per_token": 0.0, "output_cost_per_token": 0.0}}, open(sys.argv[1], "w"))
PY
    touch "$H/pricing/PLACEHOLDER"
    log "WARNING: placeholder pricing (all costs \$0). Smoke tests only, NEVER record with this."
  else
    rm -f "$H/pricing/datasheet.json.tmp"
    die "could not fetch $DATASHEET_URL (or it lacks $MODEL). Allow getbifrost.ai, or set CB_ALLOW_PLACEHOLDER_PRICING=1 for a non-recorded smoke test."
  fi
  if ! curl -fsS -m 30 "$PARAMS_URL" -o "$H/pricing/model-parameters.json" 2>/dev/null; then
    echo '{}' > "$H/pricing/model-parameters.json"
    log "model-parameters datasheet unavailable; using empty set (does not affect cost or cache decisions)"
  fi
}

write_config() {
  mkdir -p "$H/app" "$ROOT/data/audit"
  umask 077
  if [ ! -f "$H/demo.env" ]; then
    local a b
    a="sk-bf-$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
    b="sk-bf-$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
    cat > "$H/demo.env" <<EOF
# Generated by scripts/cloud-demo-setup.sh. Local throwaway virtual keys; never commit.
export CHRONO_VK_ALPHA=$a
export CHRONO_VK_BETA=$b
export BIFROST_URL=http://127.0.0.1:$PORT
export CHRONO_MODEL=$MODEL
export CHRONO_TAVILY_MODE=replay
export CHRONO_AUDIT_LOG=$ROOT/data/audit/freshcache.jsonl
# The session proxy injects the real Tavily key into the Bearer header; the app only needs a
# non-empty value. A real TAVILY_API_KEY env var, if present, wins.
export TAVILY_API_KEY="\${TAVILY_API_KEY:-proxy-injected-placeholder}"
export PATH="$H/venv/bin:$H/cargo/bin:\$PATH"
export GOTOOLCHAIN=$GOTOOLCHAIN
EOF
  fi
  # shellcheck disable=SC1091
  source "$H/demo.env"
  python3 - "$ROOT/gateway/config.json" "$H/app/config.json" "$H" "$ROOT" "${HTTPS_PROXY:-}" <<'PY'
import json, os, sys
src, dst, h, root, proxy = sys.argv[1:6]
c = json.load(open(src))
vks = {"team-alpha": os.environ["CHRONO_VK_ALPHA"], "team-beta": os.environ["CHRONO_VK_BETA"]}
for vk in c["governance"]["virtual_keys"]:
    vk["value"] = vks[vk["id"]]                                          # workaround 3
p = c["plugins"][0]
p["path"] = f"{h}/bin/freshcache.so"
p["config"]["audit_path"] = f"{root}/data/audit/freshcache.jsonl"        # workaround 5
p["config"]["snapshot_path"] = f"{root}/data/audit/freshcache-snapshot.json"
gem = c["providers"]["gemini"]
gem["keys"][0]["value"] = "placeholder-proxy-injects-real-key"           # workaround 4
if proxy:
    gem["proxy_config"] = {"type": "http", "url": proxy}                 # workaround 2
c["framework"] = {"pricing": {                                           # workaround 1
    "pricing_url": f"file://{h}/pricing/datasheet.json",
    "model_parameters_url": f"file://{h}/pricing/model-parameters.json",
    "mcp_library_url": f"file://{h}/pricing/mcp-library.json",
}}
json.dump(c, open(dst, "w"), indent=2)
PY
  chmod 600 "$H/app/config.json" "$H/demo.env"
  log "config written to $H/app/config.json (outside the repo)"
}

python_env() {
  if [ ! -x "$H/venv/bin/python" ]; then
    log "creating Python venv"
    if command -v uv >/dev/null; then uv venv -q "$H/venv"; else python3 -m venv "$H/venv"; fi
  fi
  log "installing requirements.lock + chronobrief + asciinema $ASCIINEMA_VERSION"
  if command -v uv >/dev/null; then
    VIRTUAL_ENV="$H/venv" uv pip install -q -r "$ROOT/requirements.lock"
    VIRTUAL_ENV="$H/venv" uv pip install -q -e "$ROOT" --no-deps
    VIRTUAL_ENV="$H/venv" uv pip install -q "asciinema==$ASCIINEMA_VERSION"
  else
    "$H/venv/bin/pip" install -q -r "$ROOT/requirements.lock"
    "$H/venv/bin/pip" install -q -e "$ROOT" --no-deps
    "$H/venv/bin/pip" install -q "asciinema==$ASCIINEMA_VERSION"
  fi
}

recording_tools() {
  if [ ! -x "$H/cargo/bin/agg" ]; then
    command -v cargo >/dev/null || die "cargo not installed (needed to build agg)"
    log "building agg $AGG_TAG from git (the crates.io 'agg' is an unrelated library; ~2 min)"
    cargo install -q --locked --root "$H/cargo" --git https://github.com/asciinema/agg --tag "$AGG_TAG" \
      > "$H/agg-build.log" 2>&1 || { tail -20 "$H/agg-build.log"; die "agg build failed"; }
  fi
  command -v ffmpeg >/dev/null || die "ffmpeg not installed"
  # Capture first: with pipefail, `cmd | grep -q` fails when grep exits early (SIGPIPE).
  local encoders fonts
  encoders="$(ffmpeg -hide_banner -encoders 2>/dev/null)"
  fonts="$(fc-list)"
  grep -q libx264 <<<"$encoders" || die "ffmpeg lacks libx264"
  grep -q 'DejaVu Sans Mono' <<<"$fonts" || die "DejaVu Sans Mono font missing"
  log "recording tools ready: asciinema, agg, ffmpeg+libx264, DejaVu Sans Mono"
}

gateway_pid() { pgrep -f "$H/bin/bifrost-http" || true; }

start_gateway() {
  [ -f "$H/app/config.json" ] || die "run setup first"
  # Regenerate on every start: the session proxy port changes when the container restarts, and a
  # stale proxy_config makes every Gemini call fail with a 502 inside the gateway.
  write_config
  [ -z "$(gateway_pid)" ] || { log "gateway already running"; return 0; }
  [ -f "$H/pricing/PLACEHOLDER" ] && log "WARNING: placeholder pricing active, \$ figures are fake"
  # Bifrost's own config/log DBs are rebuilt from config.json on every start. The freshcache
  # snapshot in data/audit/ is NOT touched here (restart must prove snapshot restore).
  rm -f "$H/app/"*.db "$H/app/"*.db-*
  SSL_CERT_FILE="$CA_BUNDLE" nohup "$H/bin/bifrost-http" -app-dir "$H/app" -host 127.0.0.1 -port "$PORT" \
    > "$H/bifrost.log" 2>&1 &
  for _ in $(seq 1 60); do
    curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$PORT/health" 2>/dev/null && break
    sleep 1
  done
  curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$PORT/health" || { tail -5 "$H/bifrost.log"; die "gateway did not come up"; }
  grep -q 'plugin status: freshcache - active' "$H/bifrost.log" || die "freshcache plugin not active (see $H/bifrost.log)"
  log "gateway up on 127.0.0.1:$PORT, freshcache active; $(grep -o 'freshcache loaded [0-9]* entries' "$H/bifrost.log" || true)"
}

stop_gateway() {
  local pid; pid="$(gateway_pid)"
  [ -n "$pid" ] || { log "gateway not running"; return 0; }
  kill $pid
  for _ in $(seq 1 20); do [ -z "$(gateway_pid)" ] && break; sleep 0.5; done
  log "gateway stopped (Cleanup() flushed the snapshot)"
}

reset_gateway() {
  stop_gateway
  rm -f "$ROOT/data/audit/freshcache.jsonl" "$ROOT/data/audit/freshcache-snapshot.json"
  (cd "$ROOT" && "$H/venv/bin/python" scripts/inject_article.py stripe --reset >/dev/null)
  log "cache, audit log and stripe fixture reset: next request is MISS_COLD"
  start_gateway
  # shellcheck disable=SC1091
  source "$H/demo.env"
  local entries
  entries="$(curl -fsS -m 5 -X POST -H "x-chrono-admin: metrics" -H "x-bf-vk: $CHRONO_VK_ALPHA" \
    "http://127.0.0.1:$PORT/v1/chat/completions" | python3 -c 'import json,sys; print(json.load(sys.stdin)["cache_entries"])')"
  [ "$entries" = "0" ] || die "cache not empty after reset: cache_entries=$entries"
  log "cache_entries=0 confirmed via x-chrono-admin metrics"
}

preflight() {
  # shellcheck disable=SC1091
  source "$H/demo.env"
  local ok=1
  if curl -fsS -m 3 -o /dev/null "http://127.0.0.1:$PORT/health" 2>/dev/null; then
    log "PASS gateway health"
  else
    log "FAIL gateway health (run: bash scripts/cloud-demo-setup.sh start)"; ok=0
  fi
  [ -f "$H/pricing/PLACEHOLDER" ] && { log "FAIL pricing is a placeholder: \$ figures would be fake"; ok=0; }
  # The live act retrieval, run through the app's own code (chronobrief.retrieval.search with
  # Settings.from_env in live mode): the same 4 news queries, max_results, 30-day window and date
  # parsing, so this cannot drift from what the act sends. That is 4 Tavily calls. The key is never
  # printed. PASS needs: no failed query, and a strict majority of the app's final sources dated.
  # Subject and context must match the live act in scripts/cloud-demo-record.md.
  if (cd "$ROOT" && CHRONO_TAVILY_MODE=live CHRONO_SUBJECT_CONTEXT="${CB_PREFLIGHT_CONTEXT-payments company}" \
      "$H/venv/bin/python" - "${CB_PREFLIGHT_SUBJECT:-Stripe}") <<'PY'
import sys
from collections import Counter
from datetime import UTC, datetime
from urllib.parse import urlparse
import httpx
from chronobrief.pipeline import Settings
from chronobrief.retrieval import RetrievalConfigError, RetrievalError, search

subject = sys.argv[1]
settings = Settings.from_env()
key_src = "proxy-injected" if settings.tavily_api_key in ("", "proxy-injected-placeholder") else "env var"
raw: list[dict] = []

def capture(resp: httpx.Response) -> None:
    resp.read()
    if resp.status_code == 200:
        raw.extend(r for r in resp.json().get("results", []) if isinstance(r, dict))

try:
    with httpx.Client(event_hooks={"response": [capture]}) as client:
        res = search(subject, mode="live", now=datetime.now(UTC), api_key=settings.tavily_api_key,
                     client=client, window_days=settings.window_days, fixtures_dir=settings.fixtures_dir,
                     context=settings.subject_context)
except (RetrievalConfigError, RetrievalError) as e:
    print(f"[cloud-demo] FAIL live Tavily ({key_src} key): {e}"); sys.exit(1)

raw_dated = sum(1 for r in raw if r.get("published_date"))
final = res.sources
final_dated = sum(1 for s in final if s.published is not None)
print(f"[cloud-demo] live Tavily, subject={subject!r}, context={settings.subject_context!r}, "
      f"window={settings.window_days}d, key source: {key_src}")
print(f"             raw: {len(raw)} results from 4 queries, {raw_dated} with published_date, "
      f"failed queries: {len(res.failed_queries)}")
print(f"             app sources after dedupe/window/cap: {len(final)}, dated: {final_dated}, "
      f"undated: {len(final) - final_dated}")
for d, n in Counter(urlparse(s.url).netloc for s in final).most_common():
    print(f"               {n:2d}  {d}")
for s in sorted(final, key=lambda s: (s.published is None, -(s.published.timestamp() if s.published else 0))):
    print(f"             {s.published.date().isoformat() if s.published else 'UNDATED   '}  {s.url[:100]}")
ok = bool(final) and not res.failed_queries and final_dated * 2 > len(final)
print(f"[cloud-demo] {'PASS' if ok else 'FAIL'} live Tavily: {final_dated}/{len(final)} app sources dated"
      + ("" if ok else " (need a strict majority dated and no failed query)"))
sys.exit(0 if ok else 1)
PY
  then :; else ok=0; fi
  [ "$ok" = 1 ] && log "PREFLIGHT PASS: cleared to record" || die "PREFLIGHT FAILED: do not record"
}

status() {
  log "gateway pid: $(gateway_pid || echo none)"
  log "pricing: $([ -f "$H/pricing/PLACEHOLDER" ] && echo PLACEHOLDER || echo real datasheet)"
  ls -la "$ROOT/data/audit/" 2>/dev/null | sed 's/^/             /'
}

case "${1:-setup}" in
  setup)
    # Restart, never reuse a running gateway: it would keep the config and pricing it started with.
    build_gateway; fetch_pricing; write_config; python_env; recording_tools; stop_gateway; start_gateway
    log "setup done. Next: bash scripts/cloud-demo-setup.sh preflight" ;;
  start)     start_gateway ;;
  stop)      stop_gateway ;;
  restart)   stop_gateway; start_gateway ;;
  reset)     reset_gateway ;;
  preflight) preflight ;;
  status)    status ;;
  *) die "unknown command: $1 (setup|start|stop|restart|reset|preflight|status)" ;;
esac
