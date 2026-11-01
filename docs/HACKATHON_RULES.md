# Hackathon Rules & Track Requirements (Source of Truth)

## Official Event Details
- **Event**: Geek Room Global AI Hackathon (hosted on HackCulture: https://hackculture.io/hackathons/ai-global-hackathon)
- **Title Partner (Priority Track)**: Bifrost (by Maxim AI) — Open-source high-performance AI Gateway (`github.com/maximhq/bifrost`)
- **Track Partner**: Tavily (by Nebius) — Real-time AI search & retrieval (`tavily.com`)
- **Timeline**:
  - Problem Statements Live: November 1, 2026
  - Prototype & Registration Deadline: November 15, 2026 (11:59 PM IST)
  - Judging Date: November 29, 2026
- **Track Selection Rule**: "Pick 1 problem statement from either track, or combine one from each. Bifrost statements get priority."
- **Prize Pool**:
  - **Bifrost Priority Track**: $1,000 USD total cash ($500 1st, $300 2nd, $200 3rd) + **Direct interview with Bifrost for winning teams** (3 winners in this track).
  - **Tavily Track**: $30K / $20K / $10K USD in Tavily credits for 1st/2nd/3rd (3 winners in this track) + $8K USD credits for everyone who builds on Tavily.

## Selected Combined Problem Statements
- **Bifrost Track — Problem 04 (Priority Track)**:
  > "A feature the open-source gateway is missing: Extend the open-source Bifrost gateway with something its operators would miss if you took it away: a plugin, an integration, a UI, a routing or caching strategy, or a developer tool."
- **Tavily Track — Problem 02**:
  > "Pre-meeting briefing agent: Given a company or person and a meeting time, produce a one-page brief covering the last 30 days of news, hires, launches, and open questions, with every claim sourced."

---

## Extracted Constraints (Verified by /cb-rules-sync)
| ID | Constraint | Source line | Affects | Status |
|----|-----------|-------------|---------|--------|
| R1 | Combine 1 Bifrost + 1 Tavily problem statement; Bifrost gets priority | "Pick 1 problem statement from either track, or combine one from each. Bifrost statements get priority." | Architecture, Demo, Pitch | met |
| R2 | Extend open-source Bifrost gateway with a missing feature operators would miss (caching strategy / Go plugin) | Bifrost Problem 04: "Extend the open-source Bifrost gateway with something its operators would miss if you took it away: a plugin... or caching strategy" | `gateway/plugins/freshcache/` | planned |
| R3 | Accept a company or person AND a meeting time as inputs | Tavily Problem 02: "Given a company or person and a meeting time..." | `chronobrief/brief.py`, `app/streamlit_app.py`, `chronobrief/pipeline.py` | met |
| R4 | Produce a one-page brief covering the last 30 days of news, hires, launches, and open questions | Tavily Problem 02: "...produce a one-page brief covering the last 30 days of news, hires, launches, and open questions..." | `chronobrief/brief.py`, `chronobrief/prompt.py`, UI print CSS | met |
| R5 | Every claim in the brief must be sourced | Tavily Problem 02: "...with every claim sourced." | `chronobrief/brief.py` (`validate_brief`), UI citations | met |
| R6 | Prototype submission before Nov 15, 2026 11:59 PM IST | Event schedule | `docs/plan.json` | met |

## Facts to confirm with organisers
- Confirm whether submission requires a public GitHub repo URL, a deployed live link, or both on the HackCulture portal, and whether demo video length is capped at 3 minutes.
