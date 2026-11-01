#!/bin/sh
set -eu

# Fail closed: refuse to start without real secrets. The previous version fell back to
# guessable built-in virtual keys (sk-bf-*-dev-fallback), which turned a missing .env into
# an open gateway whose tenant ids anyone could guess.
CONFIG_FILE="/etc/bifrost/config.json"

require_secret() {
    name="$1"; value="$2"; min="$3"
    case "$value" in
        ""|*replace*|*fallback*)
            echo "freshcache: refusing to start, $name is not set (put it in .env)" >&2; exit 1 ;;
    esac
    if [ "${#value}" -lt "$min" ]; then
        echo "freshcache: refusing to start, $name is shorter than $min characters" >&2; exit 1
    fi
    case "$value" in
        *[!A-Za-z0-9_.-]*)
            echo "freshcache: refusing to start, $name contains characters other than A-Z a-z 0-9 _ . -" >&2; exit 1 ;;
    esac
}

require_secret CHRONO_VK_ALPHA "${CHRONO_VK_ALPHA:-}" 32
require_secret CHRONO_VK_BETA "${CHRONO_VK_BETA:-}" 32
require_secret GEMINI_API_KEY "${GEMINI_API_KEY:-}" 20

if [ -f "$CONFIG_FILE" ]; then
    # Values are validated above to [A-Za-z0-9_.-], so they cannot break the sed expression.
    sed -i "s|\${CHRONO_VK_ALPHA}|$CHRONO_VK_ALPHA|g" "$CONFIG_FILE"
    sed -i "s|\${CHRONO_VK_BETA}|$CHRONO_VK_BETA|g" "$CONFIG_FILE"
    sed -i "s|\${GEMINI_API_KEY}|$GEMINI_API_KEY|g" "$CONFIG_FILE"
fi

exec /usr/local/bin/bifrost-http "$@"
