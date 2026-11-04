// A launcher's quota at its gateway (GET /api/v1/launchers/{name}/quota, docs/launchers.md「额度」), as a quota plugin
// read it, and how its numbers read (iOS LauncherQuotaSection / QuotaText).

export interface QuotaWindow {
  label: string;
  used_ratio?: number;
  used?: number;
  limit?: number;
  /** usd | tokens | requests | credits | percent … */
  unit?: string;
  resets_at?: string;
  exhausted?: boolean;
}

export interface QuotaPool {
  name: string;
  /** available | ok | unsupported | cooldown | exhausted | error … */
  state?: string;
  detail?: string;
  windows?: QuotaWindow[];
  models?: { model: string; windows: QuotaWindow[] }[];
}

export interface QuotaUsage {
  label: string;
  requests: number;
  tokens: number;
  cost_usd?: number;
}

export interface LauncherQuota {
  /** The gateway's id (the plugin's contribution). */
  gateway: string;
  /** What to call it, from the hub (an older hub leaves it out: the id is shown). */
  gateway_name?: string;
  key_name?: string;
  usage?: QuotaUsage[];
  budget?: QuotaWindow[];
  pools?: QuotaPool[];
  note?: string;
}

/** `error` when there is nothing to read (`unsupported`: no plugin knows the gateway) or the gateway said no. */
export interface LauncherQuotaAnswer {
  machine?: string;
  checked_at?: string;
  quota?: LauncherQuota;
  error?: string;
  unsupported?: boolean;
}

export function gatewayName(q: Pick<LauncherQuota, "gateway" | "gateway_name">): string {
  const n = q.gateway_name?.trim();
  if (n) return n;
  return q.gateway || "gateway";
}

/** Something wrong with a pool (cooling down, used up, unreadable); "no quota interface" is just how it is. */
export function poolTrouble(p: QuotaPool): boolean {
  return !(p.state === undefined || ["available", "ok", "unsupported"].includes(p.state));
}

export type QuotaTone = "accent" | "orange" | "red";

/** ≥ 70 % orange, ≥ 90 % or used up red. */
export function quotaTone(ratio: number, exhausted?: boolean): QuotaTone {
  if (exhausted || ratio >= 0.9) return "red";
  if (ratio >= 0.7) return "orange";
  return "accent";
}

/** 1.2 万, 3.4 亿. */
export function quotaCount(v: number): string {
  const short = (x: number) => x.toFixed(1).replace(/\.0$/, "");
  const a = Math.abs(v);
  if (a >= 1e8) return `${short(v / 1e8)} 亿`;
  if (a >= 1e4) return `${short(v / 1e4)} 万`;
  return v.toFixed(0);
}

export function quotaMoney(v: number): string {
  return v >= 100 ? v.toFixed(0) : v.toFixed(2);
}

/** "已用 64%", "6401 / 1 万", "$496 / $800", "已用完". */
export function quotaAmount(w: QuotaWindow): string {
  if (w.exhausted && (w.used_ratio ?? 1) >= 1) return "已用完";
  if (w.used !== undefined && w.limit !== undefined) {
    switch (w.unit) {
      case "usd":
        return `$${quotaMoney(w.used)} / $${quotaMoney(w.limit)}`;
      case "tokens":
        return `${quotaCount(w.used)} / ${quotaCount(w.limit)} token`;
      case "requests":
        return `${quotaCount(w.used)} / ${quotaCount(w.limit)} 次`;
      case "credits":
        return `${quotaCount(w.used)} / ${quotaCount(w.limit)}`;
    }
  }
  if (w.used_ratio !== undefined) return `已用 ${Math.round(w.used_ratio * 100)}%`;
  return "—";
}

export function quotaUsage(u: QuotaUsage): string {
  let s = `${quotaCount(u.requests)} 次 · ${quotaCount(u.tokens)} token`;
  if (u.cost_usd !== undefined) s += ` · $${quotaMoney(u.cost_usd)}`;
  return s;
}

/** "11月6日 15:00 重置", or "已重置" once it is past. */
export function quotaReset(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  if (t <= now) return "已重置";
  return `${new Date(t).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })} 重置`;
}

/** The bar's width, 0…100. */
export function quotaPercent(ratio: number): number {
  return Math.round(Math.min(Math.max(ratio, 0), 1) * 100);
}

/** Nothing to show beyond the key's use: the key has no ceiling. */
export function quotaUnlimited(q: LauncherQuota): boolean {
  return !q.pools?.length && !q.budget?.length && !q.note;
}

/** The line under the section: the key's name at the gateway, which machine read it, when. */
export function quotaFooter(a: LauncherQuotaAnswer, machineName: (id: string) => string, when: (iso: string) => string): string {
  const parts: string[] = [];
  const k = a.quota?.key_name;
  if (k) parts.push(`网关上的 Key 名：${k}`);
  if (a.machine) parts.push(`由 ${machineName(a.machine)} 读取，Key 不离开那台电脑`);
  const w = a.checked_at ? when(a.checked_at) : "";
  if (w) parts.push(`${w}读的，一分钟内再打开用这次的`);
  return parts.length ? parts.join("；") + "。" : "";
}
