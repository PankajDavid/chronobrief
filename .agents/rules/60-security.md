---
trigger: model_decision
description: Apply when handling keys, headers, user input, logs, dependencies, Docker, or anything that crosses a trust boundary
---

# Security

- Keys only in `.env` (git-ignored) and passed to containers through environment. `.env.example` holds placeholders. `make invariants` scans for key-shaped strings.
- Treat user input, Tavily content, model output and request headers as untrusted. Validate shape and length at the boundary; never use them in file paths, shell commands, or HTML without escaping. Streamlit: do not enable `unsafe_allow_html` for any text that came from a source or model.
- Rendered links come from the retrieved set only, and open with `rel="noopener noreferrer"`. A URL the model invented is dropped by the validator.
- Logs and the audit file contain decisions, hashes and counts. They do not contain keys, full source text or full prompts.
- Containers: run as non-root, pin base image tags, expose only the ports the demo needs, mount the audit directory read-write for the plugin and read-only for the app.
- Dependencies: pin exact versions in a lock file after the first successful install, and prefer releases that have been public for at least two weeks.
- The freshness headers are not authentication. Say so in the README threat model, and offer the shared-secret header as the hardening step.
- Rate limit the Streamlit action that triggers Tavily and model calls (simple per-session cooldown) so a demo loop cannot burn the quota.
