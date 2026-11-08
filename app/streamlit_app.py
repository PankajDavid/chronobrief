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
from chronobrief.metrics import load_scoreboard  # noqa: E402
from chronobrief.pipeline import BriefResult, Settings, generate_brief  # noqa: E402
from chronobrief.prompt import BriefParseError  # noqa: E402
from chronobrief.retrieval import RetrievalConfigError, RetrievalError, slugify  # noqa: E402
from chronobrief.ui import (  # noqa: E402
    SECTIONS,
    cache_banner,
    claim_line,
    md_escape,
    simulated_ids,
    source_line,
)
from scripts.inject_article import inject_simulated_article, reset_fixture  # noqa: E402

COOLDOWN_SECONDS = 2.0

try:
    from dotenv import load_dotenv

    load_dotenv(ROOT / ".env")
except ImportError:
    pass

st.set_page_config(page_title="ChronoBrief", page_icon="🗞️", layout="centered")

# Static, app-authored CSS only (never source or model text). Print view = the brief alone.
# Screen: dark text on the amber primary button, matching the demo video (the theme has no button-text option).
st.markdown(
    """<style>
[data-testid^="stBaseButton-primary"], [data-testid^="stBaseButton-primary"] * { color: #0E1420 !important; }
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

# ---------------------------------------------------------------- T12: cache scoreboard
with st.expander("Cache scoreboard (from the gateway audit log)", expanded=False):
    try:
        board = load_scoreboard(settings.audit_log)
    except OSError as exc:
        board = None
        st.warning(f"Could not read the audit log: {exc.strerror or exc}.")
    if board is None:
        st.write(f"No cache decisions recorded yet. The gateway writes them to `{md_escape(str(settings.audit_log))}`.")
    else:
        s = board.summary
        c1, c2, c3 = st.columns(3)
        c1.metric("Reuse (hit) rate", f"{s['hit_rate'] * 100:.0f}%", help=f"{s['hits']} of {s['lookups']} lookups")
        c2.metric("AI tokens saved", f"{s['saved_tokens']:,}")
        c3.metric("AI cost saved (USD)", f"${s['saved_cost_usd']:.4f}")
        c4, c5, c6 = st.columns(3)
        c4.metric("Stale briefs served", str(s["stale_serves"]),
                  help="Reused briefs whose evidence was older than the request. Must be 0.")
        c5.metric("Stale briefs prevented", str(s["stale_answers_prevented"]),
                  help="Times a newer source arrived and the stored brief was refused.")
        hit_ms, miss_ms = board.latency_ms.get("HIT"), board.latency_ms.get("MISS_COLD")
        c6.metric("Median latency HIT / MISS", f"{hit_ms if hit_ms is not None else '—'} / "
                                               f"{miss_ms if miss_ms is not None else '—'} ms",
                  help="Gateway-side time recorded in the audit log.")
        verdict = "PASS" if s["stale_serves"] == 0 and s["served_without_evidence"] == 0 else "FAIL"
        st.write(f"Independent check (scripts/verify_audit.py): **{verdict}** · "
                 f"decisions: {md_escape(', '.join(f'{k} {v}' for k, v in sorted(s['decisions'].items())))}")
        st.caption("Savings count AI generation only. Tavily search runs on every request.")
        st.dataframe(board.recent, hide_index=True, width="stretch")

# ---------------------------------------------------------------- T13: demo controls (replay only)
if settings.mode == "replay":
    with st.expander("Demo controls (replay mode only)", expanded=False):
        demo_subject = (res.subject if res is not None else subject.strip()) or "Stripe"
        st.write(f"Subject: **{md_escape(demo_subject)}**. "
                 "“Simulate breaking news” adds one clearly badged SIMULATED article dated now to the recorded "
                 "search. Then press Generate brief: the cache must refuse the stored brief.")
        d1, d2 = st.columns(2)
        if d1.button("Simulate breaking news"):
            try:
                art = inject_simulated_article(slugify(demo_subject), fixtures_dir=settings.fixtures_dir, mode="replay")
                st.session_state["demo_msg"] = f"Added: {art['title']} ({art['published_date']}). Now press Generate brief."
            except (FileNotFoundError, ValueError, RuntimeError) as exc:
                st.session_state["demo_msg"] = f"Could not simulate: {exc}"
        if d2.button("Reset demo"):
            try:
                reset_fixture(slugify(demo_subject), fixtures_dir=settings.fixtures_dir)
                st.session_state["demo_msg"] = "Recorded search restored. Simulated articles removed."
            except (FileNotFoundError, ValueError) as exc:
                st.session_state["demo_msg"] = f"Nothing to reset: {exc}"
        if msg := st.session_state.get("demo_msg"):
            st.info(md_escape(msg))
