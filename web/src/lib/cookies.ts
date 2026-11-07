// Cookies the user picked in a file, for importing into ego lite on a Mac (#1304, docs/plans/plan-ego-browser.md "Importing
// cookies"): parsed here in the page, grouped by site for the user to tick, and sent straight to the hub, which passes
// them to the Mac and keeps nothing. Nothing here stores, logs or shows a value. Pure; tested in cookies.test.ts.
import { t } from "../i18n";

export interface Cookie {
  name: string;
  value: string;
  domain: string;
  path?: string;
  expires?: number; // Unix seconds; missing: a session cookie
  secure?: boolean;
  http_only?: boolean;
  same_site?: "Strict" | "Lax" | "None";
  host_only?: boolean;
}

/** A site in the file: every cookie of it, and when the last one expires. */
export interface CookieSite {
  site: string;
  cookies: Cookie[];
  expired: number; // already expired: not imported
  expires?: number;
}

export interface ParsedCookies {
  format: "netscape" | "json";
  cookies: Cookie[];
  bad: number; // lines or entries that are no cookie
}

/** A site with cookies in ego lite now (GET …/browser/cookies). */
export interface CookieSiteNow {
  domain: string;
  count: number;
  expires?: string;
}

const SAME = { strict: "Strict", lax: "Lax", none: "None", no_restriction: "None" } as const;

function sameSite(v: unknown): Cookie["same_site"] {
  return typeof v === "string" ? SAME[v.toLowerCase() as keyof typeof SAME] : undefined;
}

/** Netscape cookies.txt: tab-separated domain, include-subdomains, path, secure, expires, name, value. */
function netscape(text: string): ParsedCookies {
  const cookies: Cookie[] = [];
  let bad = 0;
  for (let line of text.split(/\r?\n/)) {
    let httpOnly = false;
    if (line.startsWith("#HttpOnly_")) {
      httpOnly = true;
      line = line.slice("#HttpOnly_".length);
    } else if (line.startsWith("#") || !line.trim()) continue;
    const f = line.split("\t");
    if (f.length < 7 || !f[0] || !f[5]) {
      bad++;
      continue;
    }
    const exp = Number(f[4]);
    const sub = f[1].toUpperCase() === "TRUE";
    cookies.push({
      name: f[5],
      value: f.slice(6).join("\t"),
      domain: f[0],
      path: f[2] || "/",
      secure: f[3].toUpperCase() === "TRUE",
      expires: exp > 0 ? exp : undefined,
      http_only: httpOnly,
      host_only: !sub && !f[0].startsWith("."),
    });
  }
  return { format: "netscape", cookies, bad };
}

/** Cookie-Editor / EditThisCookie / Chrome's own export: an array of objects (or { cookies: [...] }). */
function json(text: string): ParsedCookies {
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error(t("lib-cookies.unreadable"));
  }
  const list = Array.isArray(data) ? data : data && typeof data === "object" && Array.isArray((data as { cookies?: unknown }).cookies) ? (data as { cookies: unknown[] }).cookies : null;
  if (!list) throw new Error(t("lib-cookies.json_no_list"));
  const cookies: Cookie[] = [];
  let bad = 0;
  for (const raw of list) {
    const c = raw as Record<string, unknown>;
    const name = typeof c.name === "string" ? c.name : "";
    const domain = typeof c.domain === "string" ? c.domain : typeof c.host === "string" ? c.host : "";
    if (!name || !domain || typeof c.value !== "string") {
      bad++;
      continue;
    }
    const exp = Number(c.expirationDate ?? c.expires ?? c.expiry ?? 0);
    cookies.push({
      name,
      value: c.value,
      domain,
      path: typeof c.path === "string" ? c.path : "/",
      expires: c.session === true || !(exp > 0) ? undefined : Math.floor(exp > 1e11 ? exp / 1000 : exp), // milliseconds in some exports
      secure: c.secure === true,
      http_only: c.httpOnly === true || c.http_only === true,
      same_site: sameSite(c.sameSite ?? c.same_site),
      host_only: c.hostOnly === true || c.host_only === true,
    });
  }
  return { format: "json", cookies, bad };
}

/** A cookie file the user picked: JSON when it looks like JSON, else cookies.txt. Throws with a reason when it is neither. */
export function parseCookies(text: string): ParsedCookies {
  const body = text.replace(/^﻿/, "").trim();
  const out = body.startsWith("[") || body.startsWith("{") ? json(body) : netscape(body);
  if (!out.cookies.length) throw new Error(out.format === "json" ? t("lib-cookies.json_empty") : t("lib-cookies.txt_empty"));
  return out;
}

/** The site a cookie domain belongs to, as people know it: example.com for .www.example.com; two-part suffixes
 * (example.co.uk, example.com.cn) keep three labels. The node groups the same way with the public suffix list. */
export function siteOf(domain: string): string {
  const host = domain.replace(/^\./, "").toLowerCase();
  const parts = host.split(".");
  if (parts.length <= 2 || /^\d+$/.test(parts[parts.length - 1])) return host;
  const two = parts.slice(-2).join(".");
  const second = /^(co|com|net|org|gov|edu|ac|ne|or|go)\.[a-z]{2}$/.test(two);
  return parts.slice(second ? -3 : -2).join(".");
}

/** The file's cookies by site, the sites by name; expired ones counted, not kept. */
export function sitesOf(cookies: Cookie[], now = Date.now() / 1000): CookieSite[] {
  const by = new Map<string, CookieSite>();
  for (const c of cookies) {
    const site = siteOf(c.domain);
    if (!site) continue;
    let s = by.get(site);
    if (!s) by.set(site, (s = { site, cookies: [], expired: 0 }));
    if (c.expires && c.expires < now) {
      s.expired++;
      continue;
    }
    s.cookies.push(c);
    if (c.expires && (!s.expires || c.expires > s.expires)) s.expires = c.expires;
  }
  return [...by.values()].sort((a, b) => a.site.localeCompare(b.site));
}

/** The cookies of the sites the user ticked. */
export function picked(sites: CookieSite[], ticked: Set<string>): Cookie[] {
  return sites.filter((s) => ticked.has(s.site)).flatMap((s) => s.cookies);
}
