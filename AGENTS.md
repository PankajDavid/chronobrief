# ChronoBrief

Pre-meeting brief generator. A Streamlit app searches the last 30 days with Tavily, sends the request and source metadata through the Bifrost AI Gateway, and Bifrost runs our Go plugin: a freshness-aware cache. The plugin serves a stored answer only when no source in the new request is newer than the sources behind that answer.

## Invariants (never trade these for speed)
1. The plugin makes ONE decision: serve or recompute. No network calls, no LLM calls, no Tavily inside it. `make invariants` enforces this.
2. Stale-serve rate is 0. A cached answer is never served when the request carries a newer source. No evidence of freshness means no serve.
3. Every claim in a brief carries at least one source ID that resolves to a URL and date in the retrieved set. An unsupported claim is dropped, never softened.
4. The model proposes, code decides. Dates, hashes, cost math and cache decisions are deterministic code, never model output.
5. Live mode shows only real retrieved data. Simulated articles exist only in replay mode and are visibly badged SIMULATED.
6. Secrets stay in `.env`. Never in code, logs, fixtures or commits.

## Working agreement
- Before planning: read `docs/HACKATHON_RULES.md`. If it still holds the placeholder, stop and run `/cb-rules-sync`.
- Before writing plugin code: `docs/BIFROST_RECON.md` must exist. Do not write a Bifrost signature from memory; read the docs or source at the pinned version.
- One task at a time, from `docs/plan.json`. Small commits. Update task status when done.
- "Done" means `make check` printed clean in this session and you can quote the output. Never write "should work".
- When blocked more than 20 minutes on one problem, stop, write what you tried in `docs/DECISIONS.md`, and take the fallback listed there.
- The IDE model (Gemini 3.8 Flash, High) is not the runtime model behind Bifrost. Keep the two separate in code, docs and prompts.

## Map
Rules: `.agents/rules/`. Knowledge skills: `.agents/skills/<topic>/` (the agent loads them when relevant). Slash commands: skills named `cb-*` in `.agents/skills/`; type `/cb-` to list them (in the IDE, mention the skill by name). Antigravity's older workflow files are retired, so none are used here.
Order of a normal build: `/cb-kickoff` `/cb-rules-sync` `/cb-recon` `/cb-spec-cache` `/cb-build-plugin` `/cb-build-agent` `/cb-build-ui` `/cb-wire-gateway` `/cb-demo` `/cb-audit` `/cb-submit`. Use `/cb-replan` whenever time or scope changes.

## Partnership Ownership & Git Rules

- **Canonical Pitch Line**: `"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."`
- **🟦 Swarit: Gateway Track (Bifrost + Go plugin)**
  - **Tasks**: `T02` Bifrost recon, `T04` cache spec, `T05` decision logic, `T06` plugin hooks/store/audit log, `T07` Docker build + load proof.
  - **Folders Owned**: `gateway/`, `docker-compose.yml`, `docs/BIFROST_RECON.md`, `docs/SPEC_CACHE.md`, `scripts/doctor.py`.
  - **Also Owned**: `ADR-006` — adding a `tenant_id` to the cache key, taken strictly from the Bifrost virtual key and NEVER from a client header, so two teams never get each other's brief.
- **🟩 Pankaj: Agent + UI Track (Tavily + brief + dashboard)**
  - **Tasks**: `T03` Tavily probe, `T08` retrieval + replay + fingerprint, `T09` prompt + brief schema + citation validator, `T11` Streamlit dashboard, `T13` demo injector/replay.
  - **Folders Owned**: `chronobrief/` (Python package), `app/`, `fixtures/`, `tests/`, `scripts/` (except `doctor.py`).
- **🤝 TOGETHER (Meeting Points)**
  - `T10` Wiring: Swarit gets Bifrost running, Pankaj points the agent at it. Done = `curl` proves `MISS -> HIT -> REFUSE`.
  - `T12` Metrics: Swarit's plugin writes the audit log, Pankaj builds the metrics panel that reads it.
  - `T14` Audit: Pankaj does the security pass on the whole repo before we submit.
  - `T15` Pitch: Swarit presents the Bifrost round, Pankaj presents the Tavily round.
- **🔒 Shared Files (Change ONLY after telling the other partner)**
  `AGENTS.md`, `docs/ARCHITECTURE.md` (the header contract is our interface, so it is frozen unless we both agree), `docs/DECISIONS.md`, `docs/plan.json`, `Makefile`, `pyproject.toml`.
- **Git Rules**
  1. Nobody pushes straight to `main` (after this initial baseline push).
  2. Swarit works on `gateway/...` branches; Pankaj works on `agent/...` branches.
  3. Merge to `main` only when `make check` is green, with a quick look from the other person.
  4. Each person updates their own task status in `docs/plan.json`.

