---
name: streamlit-dashboard
description: Layout, state, accessibility, demo controls and tests for the Streamlit dashboard and the one-page brief. Use when building or changing app/streamlit_app.py or any rendering code.
---

# Streamlit dashboard

## Screen
1. Header: product name and one sentence of purpose.
2. Input form (`st.form`, so typing does not rerun the script): label "Person or company", submit button "Generate brief". A second button "Regenerate (skip cache)" sends `refresh` mode.
3. Status area in page flow, as text: Searching sources, Asking the gateway, Validating citations, Done. Use `st.status` for steps and also leave the final state as text.
4. Cache banner above the brief, always one of:
   - `HIT: served from cache. Newest source in this request: <date>. Newest source behind the cached brief: <date>.`
   - `MISS: first brief for this subject.`
   - `REFUSED STALE: a newer source arrived (<new date> vs <cached date>), so the brief was recomputed.`
   Text first, colour second. Dates come from the response headers, formatted in UTC.
5. The brief: sections News, Executive moves, Product launches, then "Questions to open with" (labelled as suggestions). Each claim shows numbered references; clicking a number jumps to the source list entry.
6. Source list: publisher, headline as the link text, published date, SIMULATED badge when applicable.
7. Validator notice when claims were removed.
8. Metrics panel (collapsible, hidden in print): output of `scripts/verify_audit.py --json`: hit rate, tokens saved, cost saved, stale serves, stale answers prevented, last 10 decisions with both dates.
9. Demo controls (collapsible, only when `CHRONO_TAVILY_MODE=replay`): "Simulate a newer article" (calls the injector), "Reset demo".

## State and caching rules
- `st.session_state` for the last brief, last response headers, and a per-session cooldown timestamp (reject a second generation within 2 seconds).
- Never use `st.cache_data` or `st.cache_resource` for anything in the generation path. The gateway is the only cache. The invariant checker fails the build otherwise.
- Never use `unsafe_allow_html` with text from sources or the model. For the print stylesheet use a static, app-authored CSS block only.

## One-page print
A static CSS block with `@media print` hides the form, status, metrics and demo controls, sets a readable font size, and avoids page breaks inside a claim. Check by printing to PDF: the brief for a typical subject fits one page.

## Tests (Streamlit AppTest)
Use `streamlit.testing.v1.AppTest.from_file("app/streamlit_app.py")` with a fake gateway and replay retrieval injected through environment variables. Assert: empty input shows an error; a normal run shows the banner, at least one claim with a reference, and the source list; a HIT shows the HIT banner text; the cooldown rejects a double click. Verify `AppTest`'s current API in the Streamlit docs before relying on it.

## Accessibility checklist (run in `/cb-audit`)
Labels on every input; meaningful link text; status as text; contrast 4.5:1; keyboard-only run-through; no information in colour alone; SIMULATED text badge present. Capture a browser-agent walkthrough to `docs/evidence/ui-walkthrough/`.
