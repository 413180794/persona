// REST client for the hub. The page is served by the hub itself (same origin), so every path is relative.

import { BASE } from "./base";
import { currentLanguage, t } from "../i18n";

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

export async function api<T>(path: string, init: RequestInit & { json?: unknown; token?: string } = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${init.token ?? getToken()}`);
  // the hub writes its error and status text in the interface language (docs/i18n.md「协议」)
  if (!headers.has("Accept-Language")) headers.set("Accept-Language", currentLanguage());
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
