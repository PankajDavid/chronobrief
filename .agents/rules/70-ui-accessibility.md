---
trigger: glob
globs: "app/**/*.py, chronobrief/ui*.py, **/*.css"
description: Accessibility and layout rules for the Streamlit dashboard and the one-page brief
---

# UI and accessibility

Skill: `.agents/skills/streamlit-dashboard/SKILL.md`.

- Contrast at least 4.5:1 for text. Never use colour alone to carry meaning: cache status is always a word (HIT, REFRESHED, BLOCKED STALE) plus an icon character, not just green or red.
- Every input has a visible label; the target field's label is "Person or company". Buttons say what they do ("Generate brief", not "Go").
- Status changes (searching, generating, done, error) appear as text in the page flow, not only in spinners or toasts.
- Links have descriptive text (publisher and headline), never "click here". Each claim shows its source as a numbered reference that matches the source list.
- One-page brief: the printable layout fits A4 or Letter in one page at normal zoom, with a print stylesheet that hides controls and the metrics panel.
- Keyboard: the full flow works with Tab and Enter. No hover-only information.
- Errors say what happened and what to do next, in plain words. No stack traces in the UI.
- SIMULATED sources carry a text badge "SIMULATED (demo only)" in the source list and in the claim reference.
