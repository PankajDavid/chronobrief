#!/bin/sh
set -e

CONFIG_FILE="/etc/bifrost/config.json"
if [ -f "$CONFIG_FILE" ]; then
    # Substitute environment variables into config.json
    VK_ALPHA="${CHRONO_VK_ALPHA:-sk-bf-alpha-dev-fallback}"
    VK_BETA="${CHRONO_VK_BETA:-sk-bf-beta-dev-fallback}"
    sed -i "s|\${CHRONO_VK_ALPHA}|$VK_ALPHA|g" "$CONFIG_FILE"
    sed -i "s|\${CHRONO_VK_BETA}|$VK_BETA|g" "$CONFIG_FILE"
fi

exec /usr/local/bin/bifrost-http "$@"
