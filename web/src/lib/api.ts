// REST client for the hub. The page is served by the hub itself (same origin), so every path is relative.

import { BASE } from "./base";
import { currentLanguage, t } from "../i18n";
import type { Item } from "./types";

const TOKEN_KEY = "jarvis.token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string) {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  body: Record<string, unknown>;
  isJson: boolean;
  /** The hub logs a failed request under this id (X-Request-Id): for a report, not for the message itself. */
  requestId?: string;
  constructor(status: number, message: string, body: Record<string, unknown>, isJson = true, requestId?: string) {
    super(message);
    this.status = status;
    this.body = body;
    this.isJson = isJson;
    this.requestId = requestId;
  }
  get code(): string | undefined {
    return typeof this.body.code === "string" ? this.body.code : undefined;
  }
}

let onUnauthorized: () => void = () => {};
export function handleUnauthorized(fn: () => void) {
  onUnauthorized = fn;
}

/**
 * Who this page is to the hub: the platform and version its socket's client_hello names, and every request's
 * X-Jarvis-Client. The hub answers a request with what this page's socket declared (MCP forms and links: only to a
 * request whose platform and version match a connection that declared them, docs/protocol.md「MCP elicitation」).
 */
export const CLIENT_PLATFORM = "web";
export const CLIENT_VERSION = "1";
/** What this page can render beyond the basics (client_hello `features`). */
export const CLIENT_FEATURES = ["elicitation", "extension_operations", "extension_auth"];

// A read the hub has not answered by then fails instead of loading forever (#4492): a restarting hub, a stuck node or a
// half-dead network. Longer than the hub's own cut-offs for asking a computer (30–40 s), so when the hub has an error
// that says which computer, that one arrives first. Writes have none: one that took effect must not read as failed.
export const READ_TIMEOUT_MS = 45_000;

/** The hub did not answer a read within its time (`api`'s `timeout`). */
export class HubTimeoutError extends Error {
  constructor() {
    super(t("lib-api.hub_no_response"));
    this.name = "HubTimeoutError";
  }
}

/** `timeout`: ms before a read gives up with HubTimeoutError (default READ_TIMEOUT_MS for GET, none otherwise; 0: none). */
export async function api<T>(path: string, init: RequestInit & { json?: unknown; token?: string; timeout?: number } = {}): Promise<T> {
  const { timeout, ...rest } = init;
  const limit = timeout ?? ((init.method ?? "GET").toUpperCase() === "GET" ? READ_TIMEOUT_MS : 0);
  if (!limit) return request<T>(path, rest);
  // The caller's own signal still aborts as before (an AbortError); only the time running out becomes HubTimeoutError.
  const controller = new AbortController();
  const outer = init.signal;
  const forward = () => controller.abort(outer?.reason);
  if (outer?.aborted) forward();
  else outer?.addEventListener("abort", forward, { once: true });
  let late = false;
  const timer = setTimeout(() => {
    late = true;
    controller.abort();
  }, limit);
  try {
    return await request<T>(path, { ...rest, signal: controller.signal });
  } catch (e) {
    throw late && isAbort(e) ? new HubTimeoutError() : e;
  } finally {
    clearTimeout(timer);
    outer?.removeEventListener("abort", forward);
  }
}

async function request<T>(path: string, init: RequestInit & { json?: unknown; token?: string }): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${init.token ?? getToken()}`);
  // the hub writes its error and status text in the interface language (docs/i18n.md「协议」)
  if (!headers.has("Accept-Language")) headers.set("Accept-Language", currentLanguage());
  headers.set("X-Jarvis-Client", `${CLIENT_PLATFORM}/${CLIENT_VERSION}`);
  let body = init.body;
  if (init.json !== undefined) {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(init.json);
  }
  const res = await fetch(`${BASE}/api/v1${path}`, { ...init, headers, body });
  const text = await res.text();
  let data: Record<string, unknown> = {};
  let isJson = false;
  try {
    data = text ? JSON.parse(text) : {};
    isJson = typeof data === "object" && data !== null && !Array.isArray(data);
  } catch {
    data = { error: text };
    isJson = false;
  }
  if (!res.ok) {
    if (res.status === 401 && init.token === undefined) onUnauthorized();
    const msg = typeof data.error === "string" && data.error ? data.error : `HTTP ${res.status}`;
    throw new ApiError(res.status, msg, data, isJson, res.headers?.get("X-Request-Id") ?? undefined);
  }
  return data as T;
}

export const enc = encodeURIComponent;

export function sessionPath(mid: string, sid: string) {
  return `/sessions/${enc(mid)}/${enc(sid)}`;
}

export function attachSession(mid: string, sid: string): Promise<unknown> {
  return api(`${sessionPath(mid, sid)}/attach`, { method: "POST" });
}

// An <img> can't send a header: the hub also takes the token as a query parameter (it never logs queries).
export function imageURL(mid: string, sid: string, itemID: string) {
  return `${BASE}/api/v1${sessionPath(mid, sid)}/items/${enc(itemID)}/image?token=${enc(getToken())}`;
}

/** An item whole, when the page carries it cut (Item.full_size): `GET …/items/{id}/full`. */
export function fullItem(mid: string, sid: string, itemID: string): Promise<Item> {
  return api<Item>(`${sessionPath(mid, sid)}/items/${enc(itemID)}/full`);
}

/** A machine file's bytes by Range (docs/protocol.md「文件预览」): `<video>` / `<audio>` seek through it, a viewer
 * fetches it whole. `inline` lets the browser show it in place (images, media, PDF only; anything else downloads). */
export function fileDownloadURL(mid: string, path: string, cwd?: string, opts: { inline?: boolean } = {}) {
  return `${BASE}/api/v1/machines/${enc(mid)}/file/download?path=${enc(path)}&cwd=${enc(cwd ?? "")}${opts.inline ? "&inline=1" : ""}&token=${enc(getToken())}`;
}

/** A machine file's thumbnail, at most `size` px on its long side; 404 `no_thumbnail` when the machine can't draw it. */
export function fileThumbURL(mid: string, path: string, cwd?: string, size = 256) {
  return `${BASE}/api/v1/machines/${enc(mid)}/file/thumb?path=${enc(path)}&cwd=${enc(cwd ?? "")}&size=${size}&token=${enc(getToken())}`;
}

export function isAbort(e: unknown) {
  return e instanceof DOMException && e.name === "AbortError";
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof TypeError) return t("lib-api.cant_reach_hub");
  return e instanceof Error ? e.message : String(e);
}
