---
name: cb-build-ui
description: Manual command. Run only when the user types /cb-build-ui. Builds the Streamlit dashboard and one-page brief view with accessibility checks and AppTest coverage.
---

# /cb-build-ui

Preconditions: pipeline from `/cb-build-agent` is green. Skill: `streamlit-dashboard`. Rule: `.agents/rules/70-ui-accessibility.md`.

1. **Skeleton.** `app/streamlit_app.py` wires `chronobrief.pipeline.generate_brief`, `chronobrief.metrics`, and the render helpers. All rendering helpers live in `chronobrief/ui.py` as pure functions that return strings or small data structures, so they can be unit tested without Streamlit.
2. **Form and states.** `st.form` with the labelled input and the two buttons; per-session cooldown; step status as text; errors as plain sentences with a next step.
3. **Cache banner.** Exactly the three banner texts in the skill, built from `CacheInfo`, with both dates in UTC. Unit test each text.
4. **Brief and sources.** Sections, numbered references that match the source list, link text from publisher and headline, SIMULATED badge, validator notice with the removed count. Escape everything that came from sources or the model; no `unsafe_allow_html` for those strings.
5. **Metrics panel and decision log.** From `verify_audit.py --json`. Show the last ten decisions with both dates.
6. **Demo drawer.** Visible only in replay mode; buttons call `scripts/inject_article.py` logic through an importable function, not a shell.
7. **Print CSS.** Static block, hides controls and metrics; check "fits one page" with a print-to-PDF run for one fixture subject and save the PDF under `docs/evidence/`.
8. **Tests.** `AppTest` cases from the skill. Verify the current `AppTest` API in the Streamlit docs first.
9. **Browser walkthrough.** Start the app, drive it with the browser agent keyboard-only: generate, regenerate, inspect banner and sources. Save screenshots to `docs/evidence/ui-walkthrough/`. List accessibility checklist items as pass or fail with evidence.
10. `make check`. Quote output.

Done when: the walkthrough screenshots exist, the checklist has no unexplained fail, and tests pass.
