---
name: cb-recon
description: Manual command. Run only when the user types /cb-recon. Establishes verified facts about the Bifrost plugin API and Tavily's response shape, and proves a plugin can load, before any real plugin code.
---

# /cb-recon

Two independent recons. Run them in parallel if the task-group feature is available, otherwise one after the other, Bifrost first because it carries the most risk.

## A. Bifrost
Follow the `bifrost-plugin-recon` skill end to end. Record the start time. Deliverables: `docs/BIFROST_RECON.md` with all ten answers, citations, the pinned tag, Go version and core version; `docs/evidence/hello-world-load.txt` showing the upstream hello-world plugin loaded in a gateway built in the same Docker build and each hook firing on one request.

## B. Tavily
Follow the probe procedure in the `tavily-retrieval` skill. Deliverables: `scripts/tavily_probe.py`, `docs/evidence/tavily-probe-<date>.json`, `docs/TAVILY_PROBE.md` stating the real date format and whether every result has `published_date`. If any result lacks a date, quantify how many and say how the fingerprint treats them.

## Gate
Do not proceed to `/cb-spec-cache` until: the hello-world load proof exists, or the 90-minute fallback trigger has fired and the user chose the fallback; and `docs/TAVILY_PROBE.md` exists. If the probe shows dates are missing on most results, stop and tell the user, because the freshness design depends on them; propose using only dated results and report the effect.

Also record in `docs/BIFROST_RECON.md` the exact model string for Gemini through the gateway (from `/v1/models`) and put it in `.env` as `CHRONO_MODEL`. Do not guess it.

Output: a six-line summary of what is verified, what is UNVERIFIED, and any change this forces on `docs/ARCHITECTURE.md` or the ADRs.
