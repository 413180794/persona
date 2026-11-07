// Settings › Devices › Connect a new device (#2731): a one-time pairing code from the hub (POST /api/v1/auth/pair-codes,
// docs/protocol/protocol.md "Temporary pairing"), the page address its browser sign-in link opens, and the device that used it.

export interface QRImage {
  size: number;
  rows: string[];
}

export interface PairCodeReply {
  token: string;
  expires_at: string;
  pair_url: string;
  /** The QR code of pair_url; an older hub leaves it out. */
  qr?: QRImage;
  /** <page address>#token=…, asked for with {"web"}: opening it signs a browser in (lib/auth.ts takeToken). */
  web_url?: string;
  web_qr?: QRImage;
}

/** The root of this page for the sign-in link: the origin and the sub-path the hub serves the page under. */
export function webBase(origin: string, base: string): string {
  return `${origin}${base}/`;
}

/** Whether only this computer can open the origin: a link to it does nothing on another device. */
export function loopbackOnly(origin: string): boolean {
  let host: string;
  try {
    host = new URL(origin).hostname;
  } catch {
    return false;
  }
  return host === "localhost" || host.endsWith(".localhost") || host === "[::1]" || /^127\./.test(host);
}

/** Whole seconds the code has left at `now` (ms); 0 once it has expired or when the time cannot be read. */
export function secondsLeft(expiresAt: string, now: number): number {
  const at = Date.parse(expiresAt);
  return Number.isFinite(at) ? Math.max(0, Math.ceil((at - now) / 1000)) : 0;
}

/** m:ss, for the countdown. */
export function clock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** The device that paired with the code: a credential in use that was not there when the code was made. */
export function pairedDevice<T extends { id: string; revoked_at?: string }>(before: ReadonlySet<string>, tokens: T[]): T | undefined {
  return tokens.find((tok) => !tok.revoked_at && !before.has(tok.id));
}
