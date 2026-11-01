---
name: cb-submit
description: Manual command. Run only when the user types /cb-submit. Builds the submission pack: README, diagram, threat model, evidence index, pitch outline, and the final verified archive, against the official submission rules.
---

# /cb-submit

Preconditions: feature freeze is in effect, a final `/cb-audit` is done.

1. Re-read `docs/HACKATHON_RULES.md` for the submission format, deadline, required artifacts, and anything about licences or public repos. List each requirement with where it is met. Anything unmet is a blocker you report first.
2. **README.md**: one-paragraph problem statement; what ChronoBrief does; the architecture diagram (from `docs/ARCHITECTURE.md`); run it in five commands or fewer from a clean checkout; replay mode for offline use; the cache design in plain words (fingerprint, key, decision order); metrics and how they are computed independently; tests and how to run them; threat model and known limits (header trust, in-memory single instance, exact-key matching, Windows needs WSL2); what is simulated in the demo and why; credits for any third-party code.
3. **Clean-checkout test.** Clone the repo to a temp directory, follow the README commands exactly, run `make e2e`. Fix the README where it was wrong. Quote the final output.
4. **Evidence index** `docs/evidence/INDEX.md`: each file and what it proves.
5. **Pitch outline** `docs/PITCH.md`: three minutes. Problem, mechanism, proof, limits, next step. Include the exact sentences for the SIMULATED disclosure and for the "plain cache would have served yesterday's brief" moment.
6. **Archive.** Build the archive the rules require. Exclude `.env`, `.venv`, `.recon`, `data/`, `__pycache__`, `*.so`. Run `python scripts/check_invariants.py --strict` on the archive contents, which must find no secrets.
7. Tag the demoed commit. Print the checklist with every item ticked or explained.

Done when: the clean-checkout run passes, the archive is built, and the user has the final checklist.
