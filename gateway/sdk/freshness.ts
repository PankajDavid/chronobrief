/**
 * Universal Freshness Cache Helper (TypeScript / Node.js SDK)
 *
 * Implements the client-side canonicalization, fingerprinting, and header generation
 * required by the Bifrost gateway freshcache plugin (freshcache.so).
 *
 * Produces byte-for-byte identical set_hash and headers to chronobrief/fingerprint.py.
 */

import { createHash } from "node:crypto";

const TRACKING_PARAMS = /^(utm_.*|fbclid|gclid|mc_cid|mc_eid|ref|ref_src|igshid)$/i;
const FUTURE_SKEW_MS = 24 * 60 * 60 * 1000; // 24 hours

export interface SourceInput {
  url: string;
  title: string;
  publishedDate?: string | Date | null;
  content?: string;
}

export interface NormalizedSource {
  url: string;
  title: string;
  published: Date | null;
  snippet?: string;
}

export interface Fingerprint {
  newest: Date | null;
  countAtNewest: number;
  dated: number;
  undated: number;
  setHash: string;
}

export interface MetadataInput {
  subject: string;
  intent?: string;
  windowDays?: number;
  promptVersion?: string;
  mode?: "live" | "replay" | "refresh";
  refresh?: boolean;
  nativeCacheKey?: string;
}

/**
 * Normalizes a URL for stable source identity:
 * - Lowercases scheme and host
 * - Removes 'www.' prefix
 * - Strips marketing/analytics tracking parameters
 * - Sorts query parameters deterministically
 * - Strips trailing slashes and hash fragments
 */
export function canonicalUrl(rawUrl: string): string {
  const trimmed = rawUrl.trim();
  if (!trimmed) return "";

  try {
    const parsed = new URL(trimmed.startsWith("http") ? trimmed : `https://${trimmed}`);
    let host = parsed.hostname.toLowerCase();
    if (host.startsWith("www.")) {
      host = host.slice(4);
    }

    const scheme = (parsed.protocol || "https:").toLowerCase().replace(":", "");

    // Filter and sort query params
    const keptParams: [string, string][] = [];
    parsed.searchParams.forEach((val, key) => {
      if (!TRACKING_PARAMS.test(key)) {
        keptParams.push([key, val]);
      }
    });
    keptParams.sort(([a], [b]) => a.localeCompare(b));

    const searchStr = keptParams
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
      .join("&");

    let path = parsed.pathname.replace(/\/+$/, "");
    if (!path && !searchStr) {
      path = "";
    }

    const queryPart = searchStr ? `?${searchStr}` : "";
    return `${scheme}://${host}${path}${queryPart}`;
  } catch {
    return trimmed;
  }
}

/**
 * Parses published date strings into aware UTC Dates.
 * Handles ISO 8601, RFC 2822, and standard YYYY-MM-DD formats.
 */
export function parsePublishedDate(value: unknown): Date | null {
  if (!value) return null;
  if (value instanceof Date && !isNaN(value.getTime())) {
    return new Date(value.getTime());
  }
  if (typeof value !== "string" || !value.trim()) {
    return null;
  }

  const s = value.trim();
  const parsed = new Date(s);
  if (!isNaN(parsed.getTime())) {
    return parsed;
  }
  return null;
}

/**
 * Computes the deterministic freshness fingerprint from retrieved search sources.
 * Deduplicates by canonical URL, drops future-skewed timestamps, and calculates SHA-256 set hash.
 */
export function computeFingerprint(sources: SourceInput[], now: Date = new Date()): Fingerprint {
  const byUrl = new Map<string, NormalizedSource>();
  const maxFutureTime = now.getTime() + FUTURE_SKEW_MS;

  for (const s of sources) {
    const rawUrl = s.url || "";
    if (!rawUrl.trim()) continue;

    const url = canonicalUrl(rawUrl);
    let published = parsePublishedDate(s.publishedDate);

    // Drop dates that exceed 24 hours future skew
    if (published && published.getTime() > maxFutureTime) {
      published = null;
    }

    const title = (s.title || "").trim().replace(/\s+/g, " ");
    const prev = byUrl.get(url);

    if (
      !prev ||
      (published && (!prev.published || published.getTime() > prev.published.getTime()))
    ) {
      byUrl.set(url, {
        url,
        title,
        published,
        snippet: s.content || "",
      });
    }
  }

  const normalized = Array.from(byUrl.values()).sort((a, b) => a.url.localeCompare(b.url));
  const datedSources = normalized.filter((s) => s.published !== null);
  const undatedCount = normalized.length - datedSources.length;

  let newest: Date | null = null;
  if (datedSources.length > 0) {
    newest = new Date(
      Math.max(...datedSources.map((s) => (s.published ? s.published.getTime() : 0)))
    );
  }

  let countAtNewest = 0;
  if (newest) {
    const newestTime = newest.getTime();
    countAtNewest = datedSources.filter(
      (s) => s.published && s.published.getTime() === newestTime
    ).length;
  }

  const lines = normalized.map((s) => {
    const timeStr = s.published ? s.published.toISOString().replace(/\.\d{3}Z$/, "Z") : "undated";
    return `${s.url}|${timeStr}|${s.title}`;
  });
  lines.sort((a, b) => a.localeCompare(b));

  let setHash = "";
  if (lines.length > 0) {
    const hasher = createHash("sha256");
    hasher.update(lines.join("\n"), "utf8");
    setHash = hasher.digest("hex");
  }

  return {
    newest,
    countAtNewest,
    dated: datedSources.length,
    undated: undatedCount,
    setHash,
  };
}

/**
 * Converts a Fingerprint and workflow metadata into the canonical Bifrost request headers.
 */
export function toBifrostFreshnessHeaders(
  fp: Fingerprint,
  meta: MetadataInput
): Record<string, string> {
  const newestStr = fp.newest ? fp.newest.toISOString().replace(/\.\d{3}Z$/, "Z") : "";

  const headers: Record<string, string> = {
    "x-bf-freshness-subject": meta.subject || "",
    "x-bf-freshness-intent": meta.intent || "pre-meeting",
    "x-bf-freshness-window-days": String(meta.windowDays ?? 30),
    "x-bf-freshness-prompt-version": meta.promptVersion || "brief-v2",
    "x-bf-freshness-newest": newestStr,
    "x-bf-freshness-count-at-newest": String(fp.countAtNewest),
    "x-bf-freshness-dated": String(fp.dated),
    "x-bf-freshness-undated": String(fp.undated),
    "x-bf-freshness-set-hash": fp.setHash,
    "x-bf-freshness-mode": meta.mode || "live",
  };

  if (meta.refresh) {
    headers["x-bf-freshness-refresh"] = "true";
  }

  if (meta.nativeCacheKey) {
    headers["x-bf-cache-key"] = meta.nativeCacheKey;
  }

  return headers;
}
