"""HTTP client for the Bifrost gateway (OpenAI-compatible chat completions).

Sends the brief prompt plus the freshness contract headers (x-chrono-*), and the Bifrost virtual
key (x-bf-vk) that the plugin hashes into the tenant id. Reads the plugin's decision back from the
x-chrono-* response headers. A response without a decision header is UNKNOWN, never assumed a HIT.

Never log the headers of a request: they carry the virtual key.
"""
from __future__ import annotations

import time
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Any, Literal

import httpx

from chronobrief.prompt import GENERATION

CacheMode = Literal["normal", "refresh"]
TIMEOUT_SECONDS = 60.0
RETRY_BACKOFF = (1.0, 3.0)


class GatewayConfigError(Exception):
    """Setup problem: missing or rejected virtual key, gateway not reachable at BIFROST_URL."""


class GatewayError(Exception):
    """The gateway or the model failed for this request."""


@dataclass(frozen=True)
class CacheInfo:
    decision: str = "UNKNOWN"
    key: str | None = None
    served_at: str | None = None
    cached_newest: str | None = None
    saved_tokens: int | None = None
    saved_cost_usd: float | None = None

    @property
    def hit(self) -> bool:
        return self.decision == "HIT"

    @classmethod
    def from_headers(cls, headers: Mapping[str, str]) -> CacheInfo:
        h = {k.lower(): v for k, v in headers.items()}

        def _int(v: str | None) -> int | None:
            try:
                return int(v) if v else None
            except ValueError:
                return None

        def _float(v: str | None) -> float | None:
            try:
                return float(v) if v else None
            except ValueError:
                return None

        return cls(
            decision=(h.get("x-chrono-cache") or "UNKNOWN").strip().upper()[:40],
            key=h.get("x-chrono-cache-key"),
            served_at=h.get("x-chrono-served-at"),
            # plugin emits x-chrono-cached-newest; ARCHITECTURE.md names it x-chrono-entry-newest
            cached_newest=h.get("x-chrono-cached-newest") or h.get("x-chrono-entry-newest"),
            saved_tokens=_int(h.get("x-chrono-saved-tokens")),
            saved_cost_usd=_float(h.get("x-chrono-saved-cost-usd")),
        )


@dataclass(frozen=True)
class GatewayReply:
    text: str
    cache: CacheInfo
    usage: dict[str, Any]


def build_headers(
    *,
    virtual_key: str,
    subject: str,
    window_days: int,
    prompt_version: str,
    fingerprint_headers: Mapping[str, str],
    cache_mode: CacheMode,
) -> dict[str, str]:
    headers = {
        "x-bf-vk": virtual_key,
        "x-chrono-subject": subject[:200],
        "x-chrono-intent": "brief",
        "x-chrono-window-days": str(window_days),
        "x-chrono-prompt-version": prompt_version,
        "x-chrono-cache-mode": cache_mode,
    }
    if cache_mode == "refresh":
        headers["x-chrono-refresh"] = "true"
    headers.update(fingerprint_headers)
    return headers


def chat(
    messages: list[dict[str, str]],
    *,
    base_url: str,
    virtual_key: str,
    model: str,
    subject: str,
    window_days: int,
    prompt_version: str,
    fingerprint_headers: Mapping[str, str],
    cache_mode: CacheMode = "normal",
    client: httpx.Client | None = None,
    sleep: Callable[[float], None] = time.sleep,
) -> GatewayReply:
    if not virtual_key or "replace" in virtual_key:
        raise GatewayConfigError("No Bifrost virtual key. Set CHRONO_VK (or CHRONO_VK_ALPHA) in .env.")
    url = base_url.rstrip("/") + "/v1/chat/completions"
    headers = build_headers(
        virtual_key=virtual_key, subject=subject, window_days=window_days,
        prompt_version=prompt_version, fingerprint_headers=fingerprint_headers, cache_mode=cache_mode,
    )
    body = {"model": model, "messages": messages, **GENERATION}
    own = client is None
    http = client or httpx.Client()
    last = "unknown error"
    try:
        for attempt in range(len(RETRY_BACKOFF) + 1):
            try:
                resp = http.post(url, json=body, headers=headers, timeout=TIMEOUT_SECONDS)
            except httpx.ConnectError as exc:
                raise GatewayConfigError(f"Cannot reach the gateway at {base_url}. Is it running?") from exc
            except httpx.TimeoutException:
                last = "timeout"
            except httpx.HTTPError as exc:
                last = type(exc).__name__
            else:
                if resp.status_code in (401, 403):
                    raise GatewayConfigError(
                        f"Gateway refused the request (HTTP {resp.status_code}): {_error_message(resp)}"
                    )
                if resp.status_code == 429 or resp.status_code >= 500:
                    last = f"HTTP {resp.status_code}: {_error_message(resp)}"
                elif resp.status_code >= 400:
                    raise GatewayError(f"HTTP {resp.status_code}: {_error_message(resp)}")
                else:
                    return _parse_reply(resp)
            if attempt < len(RETRY_BACKOFF):
                sleep(RETRY_BACKOFF[attempt])
    finally:
        if own:
            http.close()
    raise GatewayError(f"Gateway failed after retries ({last})")


def _error_message(resp: httpx.Response) -> str:
    try:
        data = resp.json()
    except ValueError:
        return resp.text[:200]
    if isinstance(data, dict):
        err = data.get("error")
        if isinstance(err, dict) and err.get("message"):
            return str(err["message"])[:200]
        if isinstance(err, str):
            return err[:200]
    return str(data)[:200]


def _parse_reply(resp: httpx.Response) -> GatewayReply:
    try:
        data = resp.json()
        content = data["choices"][0]["message"]["content"]
    except (ValueError, KeyError, IndexError, TypeError) as exc:
        raise GatewayError("Gateway reply is not a chat completion") from exc
    if not isinstance(content, str) or not content.strip():
        raise GatewayError("Model returned an empty reply")
    usage = data.get("usage") if isinstance(data.get("usage"), dict) else {}
    return GatewayReply(text=content, cache=CacheInfo.from_headers(resp.headers), usage=usage)
