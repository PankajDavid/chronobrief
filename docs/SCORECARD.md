# Scorecard (Bifrost Priority Track 04 + Tavily Track 02)

Aligned with Geek Room Global AI Hackathon tracks. After every `/cb-audit`, fix the lowest-scoring axis first.

| Axis | Weight | Self-score (0-100) | Evidence (command, file, screenshot) | Lowest-first fix |
|------|--------|-------------------|--------------------------------------|------------------|
| Bifrost Track 04: Gateway Extension Value & Operator Utility (Freshness-Aware Cache Plugin + Audit Oracle) | 25% | 45 (seed) | `gateway/plugins/freshcache/internal/decide/`, `scripts/verify_audit.py` | Complete `main.go` hooks + Docker load proof in `/cb-build-plugin` |
| Tavily Track 02: 30-Day Pre-Meeting Brief Completeness (Company/Person + Meeting Time, News, Hires, Launches, Open Questions, 100% Sourced) | 25% | 45 (seed) | `chronobrief/brief.py`, `chronobrief/fingerprint.py`, `tests/test_brief.py` | Build `retrieval.py`, `prompt.py`, `pipeline.py` in `/cb-build-agent` |
| Code Quality, Purity & Architecture Invariants | 15% | 85 (seed) | `make check`, `scripts/check_invariants.py --strict` | Keep `mypy --strict` and `ruff` 100% clean as modules are added |
| Testing & Mathematical Proof (0% Stale-Serve Property Tests + E2E) | 15% | 75 (seed) | `TestNoStaleServeInvariant` (300k runs), `pytest` | Add `scripts/e2e_cache.py` against live Docker gateway |
| Security & Prompt-Injection Guardrails | 10% | 60 (seed) | `.agents/rules/60-security.md`, numeric/citation validator | Add fenced `<source>` prompt tests and session cooldown |
| UI/UX, Accessibility & One-Page Print Layout | 10% | 20 (seed) | `.agents/rules/70-ui-accessibility.md` | Build `app/streamlit_app.py` with A4 print CSS & AppTest |
