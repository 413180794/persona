// Pure helpers behind the admin pages, kept apart so they can be tested without a browser.
import type { UIItem } from "../lib/extension-ui";
import type { Access, AdminMachine, AdminExtension, Device, DoctorItem, ExtensionMetrics, LogLine, MeshHub, MeshView, MetricsWindow, Peer, PeerProbe, Restart } from "./types";

// ── access scope (docs/admin.md「访问范围」) ──────────────

export const ACCESS_INFO: Record<Access, { label: string; detail: string; risk: string }> = {
  loopback: {
    label: "只有本机",
    detail: "只能在 hub 机器上打开（127.0.0.1）。Docker 里等于只能进容器用。",
    risk: "最安全；别的电脑都打不开，要用 ssh 隧道。",
  },
  lan: {
    label: "局域网",
    detail: "本机，加上和 hub 在同一网段的私网地址，以及 security.trusted_cidrs。",
    risk: "默认值。同一个局域网里的设备都能看到登录页，靠密码挡住。",
  },
  cidrs: {
    label: "指定网段",
    detail: "本机，加上 admin.cidrs 里列的网段（例如 VPN 的网段）。",
    risk: "只放你列出的网段；公网网段也只能走 TLS。",
  },
  any: {
    label: "任何地方（需 TLS）",
    detail: "局域网照样能进；此外任何来源经 TLS 端口都能打开。",
    risk: "公网上的人都能看到登录页，只靠密码和限流挡住。",
  },
};

export const ACCESS_ORDER: Access[] = ["loopback", "lan", "cidrs", "any"];

// narrowerChoices: what the web page may switch to from the current scope (widening is the hub machine's call).
export function narrowerChoices(current: Access): Access[] {
  switch (current) {
    case "any":
      return ["loopback", "lan", "cidrs", "any"];
    case "lan":
      return ["loopback", "lan"];
    case "cidrs":
      return ["loopback", "cidrs"];
    default:
      return ["loopback"];
  }
}

export function canChoose(current: Access, next: Access): boolean {
  return narrowerChoices(current).includes(next);
}

// A light syntax check; the hub validates for real (and that the new list is no wider than the old one).
export function validCidr(s: string): boolean {
  const m = /^([^/\s]+)\/(\d{1,3})$/.exec(s.trim());
  if (!m) return false;
  const bits = Number(m[2]);
  if (m[1].includes(":")) return bits <= 128 && /^[0-9a-fA-F:.]+$/.test(m[1]) && m[1].split("::").length <= 2;
  const parts = m[1].split(".");
  return bits <= 32 && parts.length === 4 && parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255);
}

export function lines(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

// ── first setup ──────────────────────────────────────────

// setupRequest is POST setup's body: the setup code only when the hub asked for one, as typed (the hub ignores case
// and dashes).
export function setupRequest(password: string, code: string, codeRequired: boolean): { password: string; setup_code?: string } {
  return codeRequired ? { password, setup_code: code.trim() } : { password };
}

// ── QR code ──────────────────────────────────────────────

// qrPath turns the hub's rows of "0" / "1" into one SVG path (runs of dark modules as rectangles), offset by margin.
export function qrPath(rows: string[], margin = 0): { d: string; size: number } {
  const n = Math.max(rows.length, ...rows.map((r) => r.length));
  let d = "";
  rows.forEach((row, y) => {
    let x = 0;
    while (x < row.length) {
      if (row[x] !== "1") {
        x++;
        continue;
      }
      let run = 1;
      while (row[x + run] === "1") run++;
      d += `M${x + margin} ${y + margin}h${run}v1h-${run}z`;
      x += run;
    }
  });
  return { d, size: n + 2 * margin };
}

// ── time ─────────────────────────────────────────────────

export function uptime(startedAt: string | undefined, now = Date.now()): string {
  const t = startedAt ? Date.parse(startedAt) : NaN;
  if (!Number.isFinite(t)) return "—";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  const d = Math.floor(s / 86400),
    h = Math.floor((s % 86400) / 3600),
    m = Math.floor((s % 3600) / 60);
  if (d > 0) return h ? `${d} 天 ${h} 小时` : `${d} 天`;
  if (h > 0) return m ? `${h} 小时 ${m} 分钟` : `${h} 小时`;
  if (m > 0) return `${m} 分钟`;
  return "不到 1 分钟";
}

// countdown is m:ss until expiresAt, or "" once it has passed.
export function countdown(expiresAt: string | undefined, now = Date.now()): string {
  const t = expiresAt ? Date.parse(expiresAt) : NaN;
  if (!Number.isFinite(t) || t <= now) return "";
  const s = Math.ceil((t - now) / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function dateTime(iso?: string): string {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return "";
  return new Date(t).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// ── labels ───────────────────────────────────────────────

export function restartText(r?: Restart | null): string {
  if (!r?.required) return "";
  const n = r.reasons.length;
  return n > 1 ? `有 ${n} 项改动重启 hub 后生效` : "有改动要重启 hub 后生效";
}

export function groupDoctor(items: DoctorItem[]): Record<DoctorItem["level"], DoctorItem[]> {
  const out: Record<DoctorItem["level"], DoctorItem[]> = { problem: [], ok: [], info: [] };
  for (const it of items) (out[it.level] ?? out.info).push(it);
  return out;
}

// How a hub reaches a computer, in the words the admin page uses (kind of a hub.json entry, or a path's via).
export const KIND_LABEL: Record<string, string> = {
  local: "本机",
  service: "hub 来连",
  link: "电脑来连",
  ssh: "ssh",
  tailcat: "tailcat",
  hub: "经成员 hub",
};

export function viaLabel(via?: string): string {
  return via ? (KIND_LABEL[via] ?? via) : "";
}

export function machineStatus(m: AdminMachine): { tone: "green" | "red" | "orange" | "gray"; label: string } {
  if (m.state === "pending_add") return { tone: "orange", label: "重启后加入" };
  if (m.state === "pending_remove") return { tone: "gray", label: "重启后移除" };
  if (m.online) return { tone: "green", label: "在线" };
  if (m.error) return { tone: "red", label: "连不上" };
  return { tone: "gray", label: "离线" };
}

export function deviceKindLabel(kind: string): string {
  return ({ phone: "手机", watch: "手表", link: "Persona Link", other: "网页 / 其他" } as Record<string, string>)[kind] ?? kind;
}

export function roleLabel(role: string): string {
  return ({ full: "完整权限", answer: "只能回答", link: "电脑链路" } as Record<string, string>)[role] ?? role;
}

export function activeDevices(list: Device[]): Device[] {
  return list.filter((d) => !d.revoked_at);
}

export const REFUSED_TEXT = "对方已断开或不认这台 hub 的凭据";

// peerStatus: how a peer's link reads. A refused link shows as offline with why (its own error, else the usual cause).
export function peerStatus(p: Peer): { tone: "green" | "orange" | "gray"; label: string; error?: string } {
  switch (p.state) {
    case "connected":
      return { tone: "green", label: "已连接" };
    case "connecting":
      return { tone: "orange", label: "连接中", error: p.error };
    case "refused":
      return { tone: "gray", label: "离线", error: p.error || REFUSED_TEXT };
    default:
      return { tone: "gray", label: "离线", error: p.error };
  }
}

// A probed hub can only be linked over TLS: without a TLS port and a key fingerprint there is nothing to pin.
export function probeLinkable(p: PeerProbe): boolean {
  return p.tls_port > 0 && p.fp !== "";
}

export function endpointKindLabel(kind: string): string {
  return ({ ipv6: "IPv6", name: "域名", lan: "局域网", ipv4: "IPv4" } as Record<string, string>)[kind] ?? kind;
}

// Machine ids end up in URLs and hub.json: the same rule the hub enforces.
export function validMachineId(id: string): boolean {
  return /^[a-z0-9][a-z0-9._-]{0,62}$/.test(id);
}

// ── computers: paths, adding, repairing (docs/plan-hub-center.md 4, 5) ──

// hubLabel names the hub a path goes through: this hub, a hub of the group by name, else the start of its id.
export function hubLabel(mesh: MeshView | null | undefined, hubId: string | undefined, selfId?: string): string {
  if (!hubId) return "";
  const h = mesh?.hubs.find((x) => x.hub_id === hubId);
  if (h?.self || hubId === selfId) return "这台 hub";
  return h?.name || hubId.slice(0, 8);
}

export interface PathRow {
  key: string;
  via: string;
  label: string; // 本机 / hub 来连 / 电脑来连 / ssh / tailcat / 经成员 hub
  hub: string; // which hub it goes through, by name
  self: boolean; // through this hub itself
  ok: boolean;
  active: boolean;
  rtt?: number;
  error?: string;
}

// pathRows: a computer's paths for the list, the one in use first, the rest in the hub's order (its preference).
export function pathRows(m: AdminMachine, mesh: MeshView | null | undefined, selfId?: string): PathRow[] {
  const rows = (m.paths ?? []).map((p, i) => ({
    key: `${p.via}:${p.hub_id}:${p.id ?? ""}:${i}`,
    via: p.via,
    label: viaLabel(p.via),
    hub: hubLabel(mesh, p.hub_id, selfId),
    self: p.hub_id === selfId || Boolean(mesh?.hubs.find((h) => h.hub_id === p.hub_id)?.self),
    ok: p.ok,
    active: Boolean(p.active),
    rtt: p.rtt_ms && p.rtt_ms > 0 ? p.rtt_ms : undefined,
    error: p.ok ? undefined : p.error || undefined,
  }));
  return [...rows.filter((r) => r.active), ...rows.filter((r) => !r.active)];
}

// A computer configured on a member only (kind hub) is not in this hub's hub.json: it cannot be removed here.
export function canRemove(m: AdminMachine): boolean {
  return m.kind !== "hub" && m.state !== "pending_remove";
}

// ssh and tailcat entries keep working, but new computers come through the node service instead.
export function isLegacy(m: AdminMachine): boolean {
  return m.kind === "ssh" || m.kind === "tailcat";
}

// needsRepair: a service computer whose connector no longer takes this hub's credential (paired with another hub,
// reinstalled). The hub's error then asks to pair it again.
export function needsRepair(m: AdminMachine): boolean {
  return m.kind === "service" && /重新配对/.test(m.error ?? "");
}

// needsMachines: nothing to drive yet but the hub's own machine; the overview then points at adding computers.
export function needsMachines(list: AdminMachine[]): boolean {
  return list.filter((m) => m.state !== "pending_remove" && m.kind !== "local").length === 0;
}

export interface ConnectorLink {
  addrs: string[];
  fp: string;
  code: string;
}

// parseConnectorLink reads what `jarvis-link pair --listen` prints: jarvis://connector?addr=<host:port>&…&fp=…&code=…
// (the hub checks it again). null when it is not one or misses the key fingerprint or the code.
export function parseConnectorLink(raw: string): ConnectorLink | null {
  const m = /^(?:jarvis|persona):\/\/connector\/?\?(.*)$/is.exec(raw.trim());
  if (!m) return null;
  const q = new URLSearchParams(m[1]);
  const fp = q.get("fp")?.trim() ?? "";
  const code = q.get("code")?.trim() ?? "";
  if (!fp || !code) return null;
  return { addrs: q.getAll("addr").map((a) => a.trim()).filter(validHostPort), fp, code };
}

// validHostPort: host:port, an IPv6 host in brackets — what the hub takes for a connector's address.
export function validHostPort(s: string): boolean {
  const m = /^(\[[0-9a-fA-F:.%a-zA-Z]+\]|[^\s:/[\]@]+):(\d{1,5})$/.exec(s.trim());
  if (!m) return false;
  const port = Number(m[2]);
  return port > 0 && port < 65536;
}

// validProxy: socks5://[user[:password]@]host:port (or socks5h), the only proxy the hub dials connectors through.
export function validProxy(s: string): boolean {
  const m = /^socks5h?:\/\/(?:[^\s@/]+@)?([^\s@/]+)\/?$/i.exec(s.trim());
  return Boolean(m && validHostPort(m[1]));
}

// shellQuote wraps s in single quotes for a POSIX shell.
export function shellQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

// linuxInstallCommand pairs, installs and starts jarvis-link on a Linux computer in one go (docs/link.md「Linux」).
export function linuxInstallCommand(pairUrl: string): string {
  return `jarvis-link install --pair - <<< ${shellQuote(pairUrl)}`;
}

// ── the group of hubs (docs/plan-hub-center.md 2) ──────

// sortMeshHubs: the center first, then this hub, then by name.
export function sortMeshHubs(hubs: MeshHub[]): MeshHub[] {
  const rank = (h: MeshHub) => (h.role === "center" ? 0 : h.self ? 1 : 2);
  return [...hubs].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name, "zh-CN"));
}

export function centerName(mesh: MeshView): string {
  return mesh.center.name || mesh.hubs.find((h) => h.hub_id === mesh.center.hub_id)?.name || mesh.center.hub_id.slice(0, 8);
}

// memberNotice: what every admin page says on a member hub (null on the center): the group is the center's to run.
export function memberNotice(mesh: MeshView | null | undefined): { title: string; detail: string; offline: boolean } | null {
  if (!mesh || mesh.role !== "member") return null;
  const name = centerName(mesh);
  const offline = !mesh.center.online;
  return {
    title: `这台 hub 是组里的成员，组由中心「${name}」管理`,
    detail: offline
      ? "设备、互联等去中心的管理界面。中心现在连不上：这期间 App 连这台会提示中心不可达；它回不来的话，可以在「互联」把这台设为中心。"
      : "设备、互联等去中心的管理界面。",
    offline,
  };
}

// ── extensions ──

// extensionStatus: the state's words (as the app's 设置 › 扩展 says them) and tone.
export function extensionStatus(p: Pick<AdminExtension, "status">): { tone: "green" | "red" | "orange" | "gray"; label: string } {
  switch (p.status) {
    // Running or idle until something needs it (stopped): both are on, as the switch says.
    case "running":
    case "stopped":
      return { tone: "green", label: "启动" };
    case "starting":
      return { tone: "orange", label: "正在启动" };
    case "needs_setup":
      return { tone: "orange", label: "需要设置" };
    case "disabled":
      return { tone: "gray", label: "停用" };
    case "incompatible":
      return { tone: "red", label: "这台 hub 用不了" };
    case "error":
      return { tone: "red", label: "出错了" };
  }
  return { tone: "gray", label: "状态未知" };
}

// extensionRunNote: whether an enabled extension's program runs right now (the details' small print); "" otherwise.
export function extensionRunNote(p: Pick<AdminExtension, "status">): string {
  return p.status === "running" ? "正在运行" : p.status === "stopped" ? "暂未运行，用到时自动启动" : "";
}

export function extensionSourceLabel(source?: string): string {
  return ({ builtin: "内置", "hub.json": "hub.json", installed: "已安装" } as Record<string, string>)[source ?? ""] ?? source ?? "";
}

export function pointLabel(point: string): string {
  return ({ connectors: "连接器", voice: "语音对话" } as Record<string, string>)[point] ?? point;
}

// canUpgrade: only what was installed (built-in ones come with personad, hub.json's are changed there).
export function canUpgrade(p: AdminExtension): boolean {
  return p.source === "installed";
}

// canRollBack: an installed package with the version before its last upgrade still kept (a remote extension has none).
export function canRollBack(p: AdminExtension): boolean {
  return p.source === "installed" && Boolean(p.previous_version) && !(p.origin ?? "").startsWith("remote:");
}

// sourceProblem says what is wrong with a typed source before asking the hub, "" when it may be right (the hub checks
// the rest: whether the path exists, the download's hash).
export function sourceProblem(raw: string): string {
  const s = raw.trim();
  if (!s) return "";
  if (s.startsWith("http://")) return "只收 https 链接";
  if (s.startsWith("https://")) return /#sha256=[0-9a-fA-F]{64}$/.test(s) ? "" : "链接后面要带 #sha256=<64 位 hex>，下载后校验";
  if (s.startsWith("remote:")) return /^remote:(?:(?:rpc|http)=[^,]+)(?:,(?:rpc|http)=[^,]+)?$/.test(s) ? "" : "写成 remote:rpc=<socket>,http=<socket>（至少一个）";
  if (/^(?:dir|file):/.test(s)) return /^(?:dir|file):(?:\/|~\/)/.test(s) ? "" : "写 hub 机器上的绝对路径";
  if (s.startsWith("/") || s.startsWith("~/")) return "";
  return "写 hub 机器上的路径、带 sha256 的 https 链接，或 remote:";
}

// uploadProblem: a file the hub would refuse, before sending it ("" when fine).
export function uploadProblem(file: { name: string; size: number }, most: number): string {
  if (!/\.(?:tar\.gz|tgz|zip)$/i.test(file.name)) return "只能上传 .tar.gz、.tgz 或 .zip";
  if (most > 0 && file.size > most) return `包超过 ${Math.floor(most / (1 << 20))} MB`;
  return "";
}

// ── how the extensions run (docs/extensions.md「运行情况」) ──

/** An error rate as people read it: 0%, 0.4%, 12%; "—" without calls. */
export function rateText(rate: number, calls: number): string {
  if (!calls) return "—";
  const pct = rate * 100;
  if (pct === 0) return "0%";
  if (pct < 0.1) return "<0.1%";
  return `${pct < 10 ? Number(pct.toFixed(1)) : Math.round(pct)}%`;
}

/** A latency: 0.8 ms, 140 ms, 1.2 s; "—" without one. */
export function msText(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "—";
  if (ms < 10) return `${Number(ms.toFixed(1))} ms`;
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${Number((ms / 1000).toFixed(ms < 10_000 ? 1 : 0))} s`;
}

/** The rate is worth a color: orange from 1%, red from 10% (with enough calls to mean it). */
export function rateTone(rate: number, calls: number): "red" | "orange" | "gray" {
  if (calls < 5 || rate < 0.01) return "gray";
  return rate >= 0.1 ? "red" : "orange";
}

/** A step's label on the charts: the hour and minute, with the day for the 7 days. */
export function stepLabel(iso: string, window: MetricsWindow): string {
  const d = new Date(iso);
  const two = (n: number) => String(n).padStart(2, "0");
  const hm = `${two(d.getHours())}:${two(d.getMinutes())}`;
  return window === "7d" ? `${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}时` : hm;
}

/** The two charts of an extension's 运行情况: calls (answered and failed, stacked) and the p95 latency. */
export function metricsCharts(m: Pick<ExtensionMetrics, "series" | "window">): { calls: UIItem; latency: UIItem | null } {
  const label = (at: string) => stepLabel(at, m.window);
  const calls: UIItem = {
    type: "chart",
    kind: "bar",
    stacked: true,
    unit: "次",
    series: [
      { name: "成功", points: m.series.map((p) => ({ label: label(p.at), value: p.calls - p.errors })) },
      { name: "出错", tone: "danger", points: m.series.map((p) => ({ label: label(p.at), value: p.errors })) },
    ],
  };
  if (!m.series.some((p) => p.p95_ms !== null)) return { calls, latency: null };
  const latency: UIItem = {
    type: "chart",
    kind: "line",
    unit: "ms",
    series: [{ name: "p95", points: m.series.map((p) => ({ label: label(p.at), value: p.p95_ms ?? 0, display: msText(p.p95_ms) })) }],
  };
  return { calls, latency };
}

/** Log lines as text to copy: time, where from, the line. */
export function logText(lines: LogLine[]): string {
  return lines.map((l) => `${l.at} ${l.source === "hub" ? "[hub] " : ""}${l.text}`).join("\n");
}

/** Lines kept on the page: the latest most of them, without repeats (a stream that came back resends some). */
export function mergeLines(have: LogLine[], more: LogLine[], most = 2000): LogLine[] {
  const last = have.length ? have[have.length - 1].seq : -1;
  const out = have.concat(more.filter((l) => l.seq > last));
  return out.length > most ? out.slice(out.length - most) : out;
}
