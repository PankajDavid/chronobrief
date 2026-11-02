"""SEC 8-K Native Cache Guard Demo: Financial filings restatement proof.

Demonstrates the universal x-bf-freshness-* contract and native cache guard mode:
1. Q1 10-Q filing query -> MISS_COLD (or stores initial filing data)
2. Q1 10-Q repeated query -> HIT (sub-millisecond served from freshcache)
3. Breaking 8-K material restatement arrives -> REFUSE_NEWER_SOURCE.
   Notice: A standard 24h TTL cache would have served false stale earnings restatement (HIT_STALE)!
"""
import os
import sys
from pathlib import Path

import httpx

ROOT = Path(__file__).resolve().parent.parent.parent

def load_env() -> None:
    env_file = ROOT / ".env"
    if env_file.exists():
        for line in env_file.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, v = line.split("=", 1)
                os.environ.setdefault(k.strip(), v.strip())

def main() -> int:
    load_env()
    vk = os.environ.get("CHRONO_VK_ALPHA", "")
    if not vk:
        print("[FAIL] Missing CHRONO_VK_ALPHA in .env")
        return 1

    gateway_url = "http://127.0.0.1:8080/v1/chat/completions"
    model = os.environ.get("CHRONO_MODEL", "gemini-3.1-flash-lite")

    base_headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {vk}",
        "x-bf-vk": vk,
        "x-bf-cache-key": "sec-intel:AAPL:quarterly-filings",
        "x-bf-freshness-subject": "Apple-Inc",
        "x-bf-freshness-intent": "financial-earnings-analysis",
        "x-bf-freshness-window-days": "90",
        "x-bf-freshness-prompt-version": "sec-v1",
        "x-bf-freshness-dated": "10",
        "x-bf-freshness-undated": "0",
        "x-bf-freshness-mode": "replay",
    }

    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are a financial analyst summarizing SEC filings."},
            {"role": "user", "content": "Summarize Q1 revenue from recent filings."}
        ]
    }

    client = httpx.Client(timeout=30.0)

    # Step 1: Initial 10-Q filing
    headers_10q = dict(base_headers)
    headers_10q["x-bf-freshness-newest"] = "2026-10-15T18:00:00Z"
    headers_10q["x-bf-freshness-count-at-newest"] = "1"
    headers_10q["x-bf-freshness-set-hash"] = "a" * 64

    print("=== Step 1: Query with initial 10-Q filing evidence ===")
    try:
        resp1 = client.post(gateway_url, json=payload, headers=headers_10q)
        cache_dec1 = resp1.headers.get("x-bf-freshness-cache", resp1.headers.get("x-chrono-cache", "UNKNOWN"))
        overhead1 = resp1.headers.get("x-chrono-plugin-overhead-us", "0")
        print(f"Status: {resp1.status_code} | Decision: {cache_dec1} | Plugin Overhead: {overhead1} us")
    except Exception as e:
        print(f"Request 1 error: {e}")
        return 1

    print("\n=== Step 2: Identical repeated query ===")
    try:
        resp2 = client.post(gateway_url, json=payload, headers=headers_10q)
        cache_dec2 = resp2.headers.get("x-bf-freshness-cache", resp2.headers.get("x-chrono-cache", "UNKNOWN"))
        overhead2 = resp2.headers.get("x-chrono-plugin-overhead-us", "0")
        saved_tokens = resp2.headers.get("x-chrono-saved-tokens", "0")
        print(f"Status: {resp2.status_code} | Decision: {cache_dec2} | Saved Tokens: {saved_tokens} | Overhead: {overhead2} us")
        assert cache_dec2 == "HIT", f"Expected HIT, got {cache_dec2}"
    except Exception as e:
        print(f"Request 2 error: {e}")
        return 1

    print("\n=== Step 3: Material 8-K Restatement arrives (Newer Source) ===")
    headers_8k = dict(base_headers)
    headers_8k["x-bf-freshness-newest"] = "2026-10-31T20:00:00Z"
    headers_8k["x-bf-freshness-count-at-newest"] = "1"
    headers_8k["x-bf-freshness-set-hash"] = "b" * 64

    try:
        resp3 = client.post(gateway_url, json=payload, headers=headers_8k)
        cache_dec3 = resp3.headers.get("x-bf-freshness-cache", resp3.headers.get("x-chrono-cache", "UNKNOWN"))
        shadow_24h = resp3.headers.get("x-chrono-shadow-ttl-24h", "UNKNOWN")
        guard = resp3.headers.get("x-bf-freshness-guard", "UNKNOWN")
        print(f"Status: {resp3.status_code} | Decision: {cache_dec3} | Native Guard: {guard}")
        print(f"[STALE-CACHE COURT] Counterfactual 24h TTL would have done: {shadow_24h}")
        assert cache_dec3 in ("REFUSE_NEWER_SOURCE", "REFUSE_SET_DIVERGED"), f"Expected refusal, got {cache_dec3}"
        assert shadow_24h == "HIT_STALE", f"Expected shadow TTL 24h to be HIT_STALE, got {shadow_24h}"
        print("\n>>> PROOF SUCCESS: Freshcache protected against 24h stale financial hallucination! <<<")
    except Exception as e:
        print(f"Request 3 error: {e}")
        return 1

    return 0

if __name__ == "__main__":
    sys.exit(main())
