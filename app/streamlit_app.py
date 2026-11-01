"""ChronoBrief dashboard. Run: streamlit run app/streamlit_app.py

Wiring and rendering only. Logic lives in chronobrief/. The gateway is the ONLY cache: nothing in
the generation path uses st.cache_* (enforced by scripts/check_invariants.py).
"""
from __future__ import annotations

import sys
import time
from datetime import UTC, datetime
from pathlib import Path

import streamlit as st

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from chronobrief.gateway import CacheMode, GatewayConfigError, GatewayError  # noqa: E402
from chronobrief.pipeline import BriefResult, Settings, generate_brief  # noqa: E402
from chronobrief.prompt import BriefParseError  # noqa: E402
from chronobrief.retrieval import RetrievalConfigError, RetrievalError  # noqa: E402
from chronobrief.ui import (  # noqa: E402
    SECTIONS,
    cache_banner,
    claim_line,
    md_escape,
    simulated_ids,
    source_line,
)

COOLDOWN_SECONDS = 2.0

try:
    from dotenv import load_dotenv

    load_dotenv(ROOT / ".env")
except ImportError:
    pass

st.set_page_config(page_title="ChronoBrief", page_icon="🗞️", layout="centered")

# Static, app-authored CSS only (never source or model text). Print view = the brief alone.
st.markdown(
    """<style>
@media print {
  header, footer, [data-testid="stForm"], [data-testid="stStatusWidget"], [data-testid="stSidebar"],
  [data-testid="stExpander"], .stButton, [data-testid="stToolbar"] { display: none !important; }
  .main .block-container { padding-top: 0 !important; max-width: 100% !important; }
  body { font-size: 11pt; }
  li { break-inside: avoid; }
}
</style>""",
    unsafe_allow_html=True,
)

settings = Settings.from_env()

st.title("ChronoBrief")
st.write("A one-page brief on any company or person from the last 30 days of news, with every claim sourced. "
         "Reuses earlier work only when the evidence is unchanged.")
st.caption(f"Search mode: **{'REPLAY (recorded search)' if settings.mode == 'replay' else 'LIVE'}** · "
           f"Model: {md_escape(settings.model)}")

with st.form("brief_form"):
    subject = st.text_input("Person or company", max_chars=200, placeholder="e.g. Stripe")
    meeting = st.text_input("Meeting time (optional, UTC)", placeholder="2026-11-03T10:00:00Z")
    col1, col2 = st.columns(2)
    generate = col1.form_submit_button("Generate brief", type="primary")
    regenerate = col2.form_submit_button("Regenerate (skip cache)")


def run(subject_text: str, mode: CacheMode, meeting_time: str | None) -> None:
    now_mono = time.monotonic()
    last = float(st.session_state.get("last_run", 0.0))
    if now_mono - last < COOLDOWN_SECONDS:
        st.warning("Please wait a moment before generating again.")
        return
    st.session_state["last_run"] = now_mono
    with st.status("Searching sources…", expanded=False) as status:
        try:
            status.update(label="Searching sources, asking the gateway, validating citations…")
            result = generate_brief(subject_text, settings=settings, now=datetime.now(UTC),
                                    cache_mode=mode, meeting_time=meeting_time)
        except ValueError as exc:
            status.update(label="Stopped", state="error")
            st.session_state["error"] = str(exc)
            return
        except (RetrievalConfigError, GatewayConfigError) as exc:
            status.update(label="Setup needed", state="error")
            st.session_state["error"] = f"Setup needed: {exc}"
            return
        except (RetrievalError, GatewayError, BriefParseError) as exc:
            status.update(label="Could not build the brief", state="error")
            st.session_state["error"] = f"Could not build the brief: {exc}. Try again, or use Regenerate."
            return
        status.update(label="Done", state="complete")
    st.session_state["result"] = result
    st.session_state.pop("error", None)


if generate or regenerate:
    if not subject.strip():
        st.session_state.pop("result", None)
        st.session_state["error"] = "Enter a company or person name."
    else:
        run(subject, "refresh" if regenerate else "normal", meeting.strip() or None)

if err := st.session_state.get("error"):
    st.error(err)

res: BriefResult | None = st.session_state.get("result")
if res is not None and not st.session_state.get("error"):
    banner = cache_banner(res)
    getattr(st, banner.kind)(banner.text)
    for note in res.notices:
        st.caption(f"ℹ {note}")

    st.header(f"{md_escape(res.subject)} — last 30 days")
    if res.brief is None:
        st.write("No brief: there was no usable coverage in the window.")
    else:
        sims = simulated_ids(res)
        for key, title in SECTIONS:
            st.subheader(title)
            claims = [c for c in res.brief.claims if c.section == key]
            if not claims:
                st.write("_Nothing sourced in this window._")
            for c in claims:
                st.markdown(claim_line(c.text, c.source_ids, sims, c.freshness_age_days, c.date_unverified))
        if res.brief.icebreakers:
            st.subheader("Questions to open with")
            st.caption("Suggestions based on the claims above, not facts.")
            for q in res.brief.icebreakers:
                st.markdown(f"- {md_escape(q.text)}")
        if res.removed:
            with st.expander(f"{len(res.removed)} item(s) removed by the citation checker"):
                for r in res.removed:
                    st.markdown(f"- {md_escape(r.ref)}: {md_escape(r.reason)}")

        st.subheader("Sources")
        for sid, s in res.sources.items():
            st.markdown(source_line(sid, s, s.url in res.retrieval.simulated_urls))
