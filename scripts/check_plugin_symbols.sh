#!/bin/sh
# Fails unless every hook symbol is exported by the plugin .so.
# Bifrost silently ignores a misspelt hook, so this check is the only protection against a typo.
# A plugin's main package shows up in `go tool nm` as plugin/unnamed-<hash>.<Name> (exported = type T).
# Usage: sh scripts/check_plugin_symbols.sh path/to/freshcache.so
set -eu
SO="${1:?usage: check_plugin_symbols.sh path/to/plugin.so}"
missing=0
for sym in Init GetName HTTPTransportPreHook PreLLMHook PostLLMHook HTTPTransportPostHook Cleanup; do
  if go tool nm "$SO" 2>/dev/null | grep -Eq "[[:space:]]T (main|plugin/unnamed-[0-9a-f]+)\.${sym}\$"; then
    echo "ok       ${sym}"
  else
    echo "MISSING  ${sym}"
    missing=1
  fi
done
exit $missing
