"""Swarit Track: Step 7 Negative Gateway Tests & ADR-005/ADR-006 Verification.

Runs against live Bifrost gateway at http://127.0.0.1:8080.
Validates:
1. No Fingerprint -> REFUSE_NO_FINGERPRINT (not stored)
2. Future-Dated Newest -> REFUSE_FUTURE_DATE (not stored)
3. Streaming Request -> BYPASS_STREAM (not stored)
4. refresh Mode -> BYPASS_REFRESH then HIT (recomputed and replaced)
5. ADR-006 Missing VK & Tenant Isolation -> REFUSE_NO_TENANT and Team Beta gets MISS_COLD
6. ADR-005 ExactSet Headline Correction -> REFUSE_SET_CHANGED
"""
import os
from datetime import UTC, datetime
from pathlib import Path

import httpx
from dotenv import load_dotenv

ROOT = Path(__file__).resolve().parent.parent
load_dotenv(ROOT / ".env")

BIFROST_URL = os.getenv("BIFROST_URL", "http://127.0.0.1:8080").rstrip("/")
VK_ALPHA = os.getenv("CHRONO_VK_ALPHA")
VK_BETA = os.getenv("CHRONO_VK_BETA")
MODEL = os.getenv("CHRONO_MODEL", "gemini-3.1-flash-lite")
AUDIT_LOG = ROOT / "data" / "audit" / "freshcache.jsonl"

def get_last_audit_line() -> str:
    if not AUDIT_LOG.exists():
        return ""
    lines = AUDIT_LOG.read_text(encoding="utf-8").strip().splitlines()
    return lines[-1] if lines else ""

def run_tests() -> tuple[bool, str]:
    client = httpx.Client(timeout=30.0)
    output_lines = []
    all_passed = True

    def log(msg: str):
        output_lines.append(msg)
        print(msg)

    log("=" * 70)
    log("CHRONOBRIEF GATEWAY NEGATIVE & ADR PROOFS")
    log(f"Timestamp: {datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%SZ')}")
    log(f"Gateway: {BIFROST_URL} | Model: {MODEL}")
    log("=" * 70)

    url = f"{BIFROST_URL}/v1/chat/completions"
    base_body = {
        "model": MODEL,
        "messages": [{"role": "user", "content": "Reply with exact JSON: {\"status\": \"ok\"}"}],
        "temperature": 0.0,
        "max_tokens": 100,
    }

    # ---------------------------------------------------------
    # TEST 1: No Fingerprint Headers (Step 7.1)
    # ---------------------------------------------------------
    log("\n[TEST 1] No Fingerprint Headers -> REFUSE_NO_FINGERPRINT (Not Stored)")
    headers_t1 = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-No-Fingerprint",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-cache-mode": "normal",
    }
    r1 = client.post(url, json=base_body, headers=headers_t1)
    dec1 = r1.headers.get("x-chrono-cache")
    audit1 = get_last_audit_line()
    log(f"HTTP Status: {r1.status_code}")
    log(f"Response Header x-chrono-cache: {dec1}")
    log(f"Audit Log Record: {audit1}")
    pass1 = (dec1 == "REFUSE_NO_FINGERPRINT" and "REFUSE_NO_FINGERPRINT" in audit1)
    
    # Verify not stored: subsequent request with same subject still REFUSE_NO_FINGERPRINT (never HIT)
    r1_sub = client.post(url, json=base_body, headers=headers_t1)
    dec1_sub = r1_sub.headers.get("x-chrono-cache")
    log(f"Subsequent Call Header x-chrono-cache: {dec1_sub} (expected REFUSE_NO_FINGERPRINT, not HIT)")
    pass1 = pass1 and (dec1_sub == "REFUSE_NO_FINGERPRINT")
    log(f"Result: {'PASS' if pass1 else 'FAIL'}")
    all_passed = all_passed and pass1

    # ---------------------------------------------------------
    # TEST 2: Future-Dated Newest (Step 7.2)
    # ---------------------------------------------------------
    log("\n[TEST 2] Future-Dated Newest (>24h ahead) -> REFUSE_FUTURE_DATE (Not Stored)")
    headers_t2 = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-Future-Date",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-11-10T00:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "2",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "2222222222222222222222222222222222222222222222222222222222222222",
        "x-chrono-cache-mode": "normal",
    }
    r2 = client.post(url, json=base_body, headers=headers_t2)
    dec2 = r2.headers.get("x-chrono-cache")
    audit2 = get_last_audit_line()
    log(f"HTTP Status: {r2.status_code}")
    log(f"Response Header x-chrono-cache: {dec2}")
    log(f"Audit Log Record: {audit2}")
    
    # Verify not stored: subsequent call still REFUSE_FUTURE_DATE (never HIT)
    r2_sub = client.post(url, json=base_body, headers=headers_t2)
    dec2_sub = r2_sub.headers.get("x-chrono-cache")
    log(f"Subsequent Call Header x-chrono-cache: {dec2_sub} (expected REFUSE_FUTURE_DATE, not HIT)")
    pass2 = (dec2 == "REFUSE_FUTURE_DATE" and "REFUSE_FUTURE_DATE" in audit2 and dec2_sub == "REFUSE_FUTURE_DATE")
    log(f"Result: {'PASS' if pass2 else 'FAIL'}")
    all_passed = all_passed and pass2

    # ---------------------------------------------------------
    # TEST 3: Streaming Request Bypass (Step 7.3)
    # ---------------------------------------------------------
    log("\n[TEST 3] Streaming Request -> BYPASS_STREAM in Audit Log (Not Stored)")
    body_t3 = {**base_body, "stream": True}
    headers_t3 = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-Streaming",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-10-30T12:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "2",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "3333333333333333333333333333333333333333333333333333333333333333",
        "x-chrono-cache-mode": "normal",
    }
    r3 = client.post(url, json=body_t3, headers=headers_t3)
    audit3 = get_last_audit_line()
    log(f"HTTP Status: {r3.status_code}")
    log(f"Audit Log Record: {audit3}")
    
    # Verify streaming request was NOT stored in cache:
    # A subsequent non-streaming request with identical subject & fingerprint should be MISS_COLD (not HIT)
    r3_sub = client.post(url, json=base_body, headers=headers_t3)
    dec3_sub = r3_sub.headers.get("x-chrono-cache")
    log(f"Subsequent Non-Streaming Call Header x-chrono-cache: {dec3_sub} (expected MISS_COLD, proving stream was not cached)")
    pass3 = ("BYPASS_STREAM" in audit3 and dec3_sub == "MISS_COLD")
    log(f"Result: {'PASS' if pass3 else 'FAIL'}")
    all_passed = all_passed and pass3

    # ---------------------------------------------------------
    # TEST 4: refresh Mode Recomputes and Replaces (Step 7.4)
    # ---------------------------------------------------------
    log("\n[TEST 4] refresh Mode Recomputes & Replaces -> BYPASS_REFRESH then HIT")
    headers_t4 = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-Refresh-Flow",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-10-30T15:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "3",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "4444444444444444444444444444444444444444444444444444444444444444",
        "x-chrono-cache-mode": "refresh",
        "x-chrono-refresh": "true",
    }
    r4_refresh = client.post(url, json=base_body, headers=headers_t4)
    dec4_refresh = r4_refresh.headers.get("x-chrono-cache")
    audit4_refresh = get_last_audit_line()
    log(f"Call 1 (mode=refresh) Header: {dec4_refresh}")
    log(f"Call 1 Audit Record: {audit4_refresh}")

    headers_t4["x-chrono-cache-mode"] = "normal"
    del headers_t4["x-chrono-refresh"]
    r4_hit = client.post(url, json=base_body, headers=headers_t4)
    dec4_hit = r4_hit.headers.get("x-chrono-cache")
    audit4_hit = get_last_audit_line()
    log(f"Call 2 (mode=normal) Header: {dec4_hit}")
    log(f"Call 2 Audit Record: {audit4_hit}")
    pass4 = (dec4_refresh == "BYPASS_REFRESH" and dec4_hit == "HIT" and "HIT" in audit4_hit)
    log(f"Result: {'PASS' if pass4 else 'FAIL'}")
    all_passed = all_passed and pass4

    # ---------------------------------------------------------
    # TEST 5: ADR-006 Missing Virtual Key & Cross-Tenant Isolation
    # ---------------------------------------------------------
    log("\n[TEST 5A] ADR-006 Missing Virtual Key Fails Closed -> REFUSE_NO_TENANT")
    headers_t5a = {
        "x-chrono-subject": "Test-Tenant-Missing",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-10-30T10:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "1",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "5555555555555555555555555555555555555555555555555555555555555555",
        "x-chrono-cache-mode": "normal",
    }
    r5a = client.post(url, json=base_body, headers=headers_t5a)
    dec5a = r5a.headers.get("x-chrono-cache")
    log(f"Missing VK Header x-chrono-cache: {dec5a}")
    pass5a = (dec5a == "REFUSE_NO_TENANT")
    log(f"Result 5A: {'PASS' if pass5a else 'FAIL'}")
    all_passed = all_passed and pass5a

    log("\n[TEST 5B] ADR-006 Cross-Tenant Isolation (Team Alpha HIT vs Team Beta MISS_COLD)")
    headers_t5b_alpha = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-Tenant-Isolation-Subject",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-10-30T10:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "2",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "5656565656565656565656565656565656565656565656565656565656565656",
        "x-chrono-cache-mode": "normal",
    }
    # Alpha call 1: MISS_COLD
    r5b_a1 = client.post(url, json=base_body, headers=headers_t5b_alpha)
    dec5b_a1 = r5b_a1.headers.get("x-chrono-cache")
    log(f"Alpha Call 1 Header: {dec5b_a1}")

    # Alpha call 2: HIT
    r5b_a2 = client.post(url, json=base_body, headers=headers_t5b_alpha)
    dec5b_a2 = r5b_a2.headers.get("x-chrono-cache")
    log(f"Alpha Call 2 Header: {dec5b_a2}")

    # Beta call 1 (identical subject & evidence, but different tenant): MISS_COLD
    headers_t5b_beta = {**headers_t5b_alpha, "x-bf-vk": VK_BETA}
    r5b_b1 = client.post(url, json=base_body, headers=headers_t5b_beta)
    dec5b_b1 = r5b_b1.headers.get("x-chrono-cache")
    log(f"Beta Call 1 Header (identical request, different tenant): {dec5b_b1}")
    pass5b = (dec5b_a2 == "HIT" and dec5b_b1 == "MISS_COLD")
    log(f"Result 5B: {'PASS' if pass5b else 'FAIL'}")
    all_passed = all_passed and pass5b

    # ---------------------------------------------------------
    # TEST 6: ADR-005 ExactSet Same-Day Headline Correction
    # ---------------------------------------------------------
    log("\n[TEST 6] ADR-005 ExactSet Headline Correction -> REFUSE_SET_CHANGED")
    headers_t6_base = {
        "x-bf-vk": VK_ALPHA,
        "x-chrono-subject": "Test-Headline-Correction",
        "x-chrono-intent": "brief",
        "x-chrono-window-days": "30",
        "x-chrono-prompt-version": "brief-v1",
        "x-chrono-newest": "2026-10-31T12:00:00Z",
        "x-chrono-count-at-newest": "1",
        "x-chrono-dated": "2",
        "x-chrono-undated": "0",
        "x-chrono-set-hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "x-chrono-cache-mode": "normal",
    }
    # Call 1: MISS_COLD
    r6_c1 = client.post(url, json=base_body, headers=headers_t6_base)
    log(f"Headline Base Call 1: {r6_c1.headers.get('x-chrono-cache')}")
    # Call 2: HIT
    r6_c2 = client.post(url, json=base_body, headers=headers_t6_base)
    log(f"Headline Base Call 2: {r6_c2.headers.get('x-chrono-cache')}")

    # Call 3: Same newest date, modified set hash (content changed)
    headers_t6_mod = {**headers_t6_base, "x-chrono-set-hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
    r6_c3 = client.post(url, json=base_body, headers=headers_t6_mod)
    dec6_c3 = r6_c3.headers.get("x-chrono-cache")
    audit6_c3 = get_last_audit_line()
    log(f"Headline Modified Call 3 Header: {dec6_c3}")
    log(f"Headline Modified Call 3 Audit: {audit6_c3}")
    pass6 = (dec6_c3 == "REFUSE_SET_CHANGED" and "REFUSE_SET_CHANGED" in audit6_c3)
    log(f"Result 6: {'PASS' if pass6 else 'FAIL'}")
    all_passed = all_passed and pass6

    log("\n" + "=" * 70)
    log(f"OVERALL RESULT: {'ALL PASS' if all_passed else 'SOME FAILED'}")
    log("=" * 70)

    report_text = "\n".join(output_lines)
    evidence_file = ROOT / "docs" / "evidence" / "gateway-negative-tests.txt"
    evidence_file.parent.mkdir(parents=True, exist_ok=True)
    evidence_file.write_text(report_text + "\n", encoding="utf-8")
    log(f"\nSaved report to -> {evidence_file.relative_to(ROOT)}")
    return all_passed, report_text

if __name__ == "__main__":
    passed, _ = run_tests()
    exit(0 if passed else 1)
