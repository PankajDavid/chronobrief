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

// ---------------------------------------------------------------------------
// Python parity layer.
// chronobrief/fingerprint.py is the reference implementation (it is what the live app
// sends). The helpers below port the exact CPython 3.11+ algorithms it relies on:
// urllib.parse.urlsplit / urlunsplit / parse_qsl / unquote / quote_plus / urlencode,
// email.utils.parsedate_to_datetime and datetime.fromisoformat (extended ISO forms).
// tests/test_fingerprint_parity.py runs both implementations on the same inputs.
// Known out of scope: compact/week ISO dates ("20261030", "2026-W44-1"), month-first
// RFC dates, and sub-millisecond ties (JS Date has millisecond precision).
// ---------------------------------------------------------------------------

const C0_OR_SPACE = /^[\x00-\x20]+/;
const SCHEME_CHARS = /^[A-Za-z0-9+.\-]+$/;
const USES_NETLOC = new Set([
  "", "ftp", "http", "gopher", "nntp", "telnet", "imap", "wais", "file", "mms", "https", "shttp",
  "snews", "prospero", "rtsp", "rtsps", "rtspu", "rsync", "svn", "svn+ssh", "sftp", "nfs", "git",
  "git+ssh", "ws", "wss", "itms-services",
]);
// Python str.isspace() set, used by str.split() / str.strip().
const PY_WS = "\\t\\n\\x0b\\x0c\\r\\x1c-\\x1f \\x85\\xa0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000";
const PY_WS_RUN = new RegExp(`[${PY_WS}]+`, "g");
const PY_WS_EDGES = new RegExp(`^[${PY_WS}]+|[${PY_WS}]+$`, "g");

/** Code-point string comparison, same order as Python's sorted(). */
function cmpCodePoints(a: string, b: string): number {
  const ia = a[Symbol.iterator]();
  const ib = b[Symbol.iterator]();
  for (;;) {
    const x = ia.next();
    const y = ib.next();
    if (x.done || y.done) return x.done ? (y.done ? 0 : -1) : 1;
    const d = (x.value.codePointAt(0) as number) - (y.value.codePointAt(0) as number);
    if (d !== 0) return d;
  }
}

/** Python: " ".join(title.strip().split()) */
function pyNormalizeSpace(s: string): string {
  return s.replace(PY_WS_EDGES, "").split(PY_WS_RUN).filter((t) => t.length > 0).join(" ");
}

interface SplitResult { scheme: string; netloc: string; path: string; query: string }

/** urllib.parse.urlsplit (allow_fragments=True); fragment is discarded by the caller. */
function pyUrlsplit(raw: string): SplitResult {
  let url = raw.replace(C0_OR_SPACE, "").replace(/[\t\r\n]/g, "");
  let scheme = "";
  let netloc = "";
  let query = "";
  const i = url.indexOf(":");
  if (i > 0 && /^[A-Za-z]/.test(url[0]) && SCHEME_CHARS.test(url.slice(0, i))) {
    scheme = url.slice(0, i).toLowerCase();
    url = url.slice(i + 1);
  }
  if (url.startsWith("//")) {
    let delim = url.length;
    for (const c of "/?#") {
      const w = url.indexOf(c, 2);
      if (w >= 0) delim = Math.min(delim, w);
    }
    netloc = url.slice(2, delim);
    url = url.slice(delim);
    if (netloc.includes("[") !== netloc.includes("]")) throw new Error("Invalid IPv6 URL");
  }
  const h = url.indexOf("#");
  if (h >= 0) url = url.slice(0, h);
  const q = url.indexOf("?");
  if (q >= 0) {
    query = url.slice(q + 1);
    url = url.slice(0, q);
  }
  return { scheme, netloc, path: url, query };
}

/** SplitResult.hostname (lower-cased, may be ""). */
function pyHostname(netloc: string): string {
  const hostinfo = netloc.slice(netloc.lastIndexOf("@") + 1);
  let host: string;
  const ob = hostinfo.indexOf("[");
  if (ob >= 0) {
    const rest = hostinfo.slice(ob + 1);
    const cb = rest.indexOf("]");
    host = cb >= 0 ? rest.slice(0, cb) : rest;
  } else {
    const c = hostinfo.indexOf(":");
    host = c >= 0 ? hostinfo.slice(0, c) : hostinfo;
  }
  if (!host) return "";
  const pct = host.indexOf("%");
  return pct >= 0 ? host.slice(0, pct).toLowerCase() + host.slice(pct) : host.toLowerCase();
}

/** urllib.parse.urlunsplit */
function pyUrlunsplit(scheme: string, netloc: string, path: string, query: string): string {
  let url = path;
  if (netloc) {
    if (url && !url.startsWith("/")) url = "/" + url;
    url = "//" + netloc + url;
  } else if (url.startsWith("//")) {
    url = "//" + url;
  } else if (scheme && USES_NETLOC.has(scheme) && (!url || url.startsWith("/"))) {
    url = "//" + url;
  }
  if (scheme) url = scheme + ":" + url;
  if (query) url = url + "?" + query;
  return url;
}

const UTF8 = new TextDecoder("utf-8", { fatal: false });

/** urllib.parse.unquote(errors="replace") after "+" -> " " (as parse_qsl does). */
function pyUnquotePlus(s: string): string {
  const t = s.replace(/\+/g, " ");
  if (!t.includes("%")) return t;
  // Process maximal ASCII runs as bytes (invalid %xx kept literally), like CPython.
  return t.replace(/[\x00-\x7f]+/g, (run) => {
    const bytes: number[] = [];
    for (let i = 0; i < run.length; i++) {
      if (run[i] === "%" && /^[0-9A-Fa-f]{2}$/.test(run.slice(i + 1, i + 3))) {
        bytes.push(parseInt(run.slice(i + 1, i + 3), 16));
        i += 2;
      } else {
        bytes.push(run.charCodeAt(i));
      }
    }
    return UTF8.decode(new Uint8Array(bytes));
  });
}

/** urllib.parse.parse_qsl(keep_blank_values=False) */
function pyParseQsl(qs: string): [string, string][] {
  const out: [string, string][] = [];
  for (const nameValue of qs.split("&")) {
    if (!nameValue) continue;
    const eq = nameValue.indexOf("=");
    if (eq < 0) continue;
    const name = nameValue.slice(0, eq);
    const value = nameValue.slice(eq + 1);
    if (!value.length) continue;
    out.push([pyUnquotePlus(name), pyUnquotePlus(value)]);
  }
  return out;
}

const ALWAYS_SAFE = /[A-Za-z0-9_.\-~]/;
const UTF8_ENC = new TextEncoder();

/** urllib.parse.quote_plus(s, safe="") */
function pyQuotePlus(s: string): string {
  let out = "";
  for (const byte of UTF8_ENC.encode(s)) {
    const ch = String.fromCharCode(byte);
    if (byte < 0x80 && ALWAYS_SAFE.test(ch)) out += ch;
    else if (ch === " ") out += "+";
    else out += "%" + byte.toString(16).toUpperCase().padStart(2, "0");
  }
  return out;
}

/**
 * Stable source identity, identical to chronobrief.fingerprint.canonical_url:
 * lower-case scheme and host, no "www.", no userinfo/port, no tracking params,
 * query sorted by (key, value), no fragment, no trailing slash.
 */
export function canonicalUrl(rawUrl: string): string {
  const parts = pyUrlsplit(rawUrl.trim());
  let host = pyHostname(parts.netloc);
  if (host.startsWith("www.")) host = host.slice(4);
  const kept = pyParseQsl(parts.query).filter(([k]) => !TRACKING_PARAMS.test(k));
  kept.sort((a, b) => cmpCodePoints(a[0], b[0]) || cmpCodePoints(a[1], b[1]));
  const query = kept.map(([k, v]) => `${pyQuotePlus(k)}=${pyQuotePlus(v)}`).join("&");
  const path = parts.path.replace(/\/+$/, "");
  return pyUrlunsplit(parts.scheme || "https", host, path, query);
}

const MONTHS: Record<string, number> = {
  jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6, jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12,
};
const NAMED_ZONES: Record<string, number> = {
  UT: 0, UTC: 0, GMT: 0, Z: 0, AST: -400, ADT: -300, EST: -500, EDT: -400, CST: -600, CDT: -500,
  MST: -700, MDT: -600, PST: -800, PDT: -700,
};

function utcDate(y: number, mo: number, d: number, h: number, mi: number, s: number, ms: number, offsetMin: number): Date | null {
  if (mo < 1 || mo > 12 || d < 1 || h > 23 || mi > 59 || s > 59) return null;
  const t = Date.UTC(y, mo - 1, d, h, mi, s, ms);
  const check = new Date(t);
  if (check.getUTCFullYear() !== y || check.getUTCMonth() !== mo - 1 || check.getUTCDate() !== d) return null;
  return new Date(t - offsetMin * 60000);
}

/** email.utils.parsedate_to_datetime for "[Day,] DD Mon YYYY HH:MM[:SS] [zone]". No zone / -0000 = UTC. */
function pyParseRfc2822(s: string): Date | null {
  const m = /^(?:[A-Za-z]{3},?\s+)?(\d{1,2})\s+([A-Za-z]{3})[A-Za-z]*\s+(\d{2,4})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\s+(\S+))?\s*$/.exec(s);
  if (!m) return null;
  const mo = MONTHS[m[2].toLowerCase()];
  if (!mo) return null;
  let y = parseInt(m[3], 10);
  if (y < 100) y += y > 68 ? 1900 : 2000;
  let off = 0;
  const z = m[7];
  if (z) {
    const zm = /^([+-])(\d{2})(\d{2})$/.exec(z);
    if (zm) {
      off = (zm[1] === "-" ? -1 : 1) * (parseInt(zm[2], 10) * 60 + parseInt(zm[3], 10));
    } else if (z.toUpperCase() in NAMED_ZONES) {
      const v = NAMED_ZONES[z.toUpperCase()];
      off = Math.sign(v) * (Math.floor(Math.abs(v) / 100) * 60 + (Math.abs(v) % 100));
    }
  }
  return utcDate(y, mo, parseInt(m[1], 10), parseInt(m[4], 10), parseInt(m[5], 10), parseInt(m[6] ?? "0", 10), 0, off);
}

/** datetime.fromisoformat (extended forms). A value without a zone is taken as UTC. */
function pyParseIso(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2})(?::(\d{2})(?::(\d{2})(?:[.,](\d+))?)?)?(Z|[+-]\d{2}(?::?\d{2}(?::?\d{2}(?:\.\d+)?)?)?)?)?$/.exec(s);
  if (!m) return null;
  let off = 0;
  const z = m[8];
  if (z && z !== "Z") {
    const zm = /^([+-])(\d{2}):?(\d{2})?/.exec(z) as RegExpExecArray;
    off = (zm[1] === "-" ? -1 : 1) * (parseInt(zm[2], 10) * 60 + parseInt(zm[3] ?? "0", 10));
  }
  const frac = m[7] ? parseInt((m[7] + "000").slice(0, 3), 10) : 0;
  return utcDate(
    parseInt(m[1], 10), parseInt(m[2], 10), parseInt(m[3], 10),
    parseInt(m[4] ?? "0", 10), parseInt(m[5] ?? "0", 10), parseInt(m[6] ?? "0", 10), frac, off,
  );
}

/**
 * Same rules as chronobrief.fingerprint.parse_published: RFC 2822 first, then ISO 8601.
 * Anything else is null, never a guess (JS `new Date(s)` is deliberately not used: it reads
 * zone-less values in the machine's local time zone).
 */
export function parsePublishedDate(value: unknown): Date | null {
  if (value instanceof Date) {
    return isNaN(value.getTime()) ? null : new Date(value.getTime());
  }
  if (typeof value !== "string" || !value.trim()) return null;
  const s = value.trim();
  return pyParseRfc2822(s) ?? pyParseIso(s);
}

/**
 * Computes the deterministic freshness fingerprint from retrieved search sources.
 * Deduplicates by canonical URL, drops future-skewed timestamps, and calculates SHA-256 set hash.
 */
export function computeFingerprint(sources: SourceInput[], now: Date = new Date()): Fingerprint {
  const byUrl = new Map<string, NormalizedSource>();
  const maxFutureTime = now.getTime() + FUTURE_SKEW_MS;

  for (const s of sources) {
    const rawUrl = (s.url || "").trim();
    if (!rawUrl) continue;

    const url = canonicalUrl(rawUrl);
    let published = parsePublishedDate(s.publishedDate);

    // Drop dates that exceed 24 hours future skew
    if (published && published.getTime() > maxFutureTime) {
      published = null;
    }

    const title = pyNormalizeSpace(s.title || "");
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

  const normalized = Array.from(byUrl.values()).sort((a, b) => cmpCodePoints(a.url, b.url));
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
  lines.sort(cmpCodePoints);

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
