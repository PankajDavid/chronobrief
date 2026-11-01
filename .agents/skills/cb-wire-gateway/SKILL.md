---
name: cb-wire-gateway
description: Manual command. Run only when the user types /cb-wire-gateway. Writes the Dockerfile, compose file and gateway config, runs the real MISS, HIT, REFUSE cycle end to end, and checks the audit log with the independent oracle.
---

# /cb-wire-gateway

Preconditions: plugin builds (`/cb-build-plugin`), agent pipeline green. Skill: `plugin-build-and-load`. Time-box: if the plugin still does not load 90 minutes after starting this command, stop and offer ADR-002.

1. **Dockerfile** at the repo root, following the pattern in the skill, with the versions from `docs/BIFROST_RECON.md`. Non-root final image.
2. **Gateway config** `gateway/config.json`: Gemini provider with the key read from the environment (use whatever mechanism the recon verified; never write the key into the file), the plugin entry with its config block from `docs/SPEC_CACHE.md`, and the built-in semantic cache disabled or ordered out of the way as the recon states. Add `gateway/config.example.json` without secrets if the real one needs any.
3. **Compose** `docker-compose.yml`: `bifrost` and `app`, shared `./data/audit` volume (read-write for bifrost, read-only for app), ports only for the app and the gateway, `.env` passed through.
4. **Bring up.** `docker compose up --build -d`. Quote the plugin load line from the gateway logs. List models with `curl $BIFROST_URL/v1/models` and confirm `CHRONO_MODEL` is present.
5. **Hand-driven proof** with `curl`, same body three times (use a tiny prompt): request 1 with fingerprint headers for newest 2026-10-30 (expect `x-chrono-cache: MISS_COLD`), request 2 identical (expect `HIT`), request 3 with newest 2026-11-01 (expect `REFUSE_NEWER_SOURCE`). Quote the three response header sets.
6. **E2E script** `scripts/e2e_cache.py`: in replay mode with a fixed subject runs the full pipeline: MISS, HIT, inject, REFUSE; asserts the decisions in order and exit code 0 from `scripts/verify_audit.py`. Add `make e2e` target. Quote the run.
7. **Negative tests** through the real gateway: no fingerprint headers (refused, not stored); future-dated newest (REFUSE_FUTURE_DATE); streaming request (bypass); `refresh` mode (recompute and replace). Quote decisions from the audit log.
8. Save logs to `docs/evidence/plugin-load.txt` and `docs/evidence/e2e-run.txt`.

Done when: step 6 passes twice in a row from a clean `docker compose down -v && up --build`.
