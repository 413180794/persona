// 机器详情 (/machines/<id>, docs/machines.md「App」, iOS MachineDetailView): one page, the most used on top — how the
// computer is (online, system, which hub when there are several), its agents by herdr space (a row opens the chat),
// its values now, CPU / memory / network / load over 1, 6 or 24 hours (hover a chart to read a moment, all follow) and
// each core now (Linux), the system, the launchers. The metrics refresh only while the page is visible.
import { useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, enc, errorText, isAbort } from "../../lib/api";
import { ago, sessionTitle, stateOf } from "../../lib/format";
import {
  agentMeta,
  agentsBySpace,
  bytesAxis,
  bytesBin,
  connectionText,
  coresNow,
  countsText,
  isEmpty,
  level,
  load as loadText,
  loadAxis,
  localOffset,
  loudState,
  memPercent,
  percent,
  RANGE_KEYS,
  RANGES,
  rate,
  systemFacts,
  systemLine,
  type DetailMachine,
  type DetailMetrics,
  type Level,
  type MetricRange,
} from "../../lib/machine-detail";
import { hubLabel, routeText } from "../../lib/mesh";
import { href, onLinkClick } from "../../lib/router";
import { getState, refresh, useStore } from "../../lib/store";
import type { Session } from "../../lib/types";
import { Button, cx, EASE, IconButton, Loading, StatePill, StatusDot, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { MetricChart, type ChartSeries } from "./MetricChart";

interface MachineLauncher {
  name: string;
  model?: string;
  agent?: string;
  agent_type?: string;
}

const NO_SESSIONS: Session[] = [];

function initialRange(): MetricRange {
  const r = new URLSearchParams(location.search).get("range");
  return RANGE_KEYS.includes(r as MetricRange) ? (r as MetricRange) : "1h";
}

export function MachineDetail({ mid, onToggleSidebar }: { mid: string; onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines) as DetailMachine[];
  const loaded = useStore((s) => s.loaded);
  const sessions = useStore((s) => s.sessions[mid] ?? NO_SESSIONS);
  const m = machines.find((x) => x.id === mid);
  const online = m?.online ?? false;

  const [range, setRangeState] = useState<MetricRange>(initialRange);
  const [data, setData] = useState<{ metrics: DetailMetrics; range: MetricRange; at: number } | null>(null);
  const [failure, setFailure] = useState("");
  const [hover, setHover] = useState<number | null>(null);
  const [launchers, setLaunchers] = useState<MachineLauncher[] | null>(null);
  const [launcherError, setLauncherError] = useState("");
  const [reconnecting, setReconnecting] = useState(false);
  const alive = useRef(true);
  useEffect(() => () => void (alive.current = false), []);

  const setRange = (r: MetricRange) => {
    setRangeState(r);
    setHover(null);
    history.replaceState(history.state, "", r === "1h" ? location.pathname : `${location.pathname}?range=${r}`);
  };

  // Metrics: now, then every 10 s (the hour) or every minute, and again when the page comes back into view; nothing
  // while the tab is hidden.
  useEffect(() => {
    let dead = false;
    let ctrl: AbortController | null = null;
    const load = async () => {
      if (document.visibilityState !== "visible") return;
      ctrl?.abort();
      ctrl = new AbortController();
      try {
        const r = await api<{ metrics: DetailMetrics }>(`/machines/${enc(mid)}/metrics?range=${range}`, { signal: ctrl.signal });
        if (dead) return;
        setData({ metrics: { ...r.metrics, points: r.metrics.points ?? [] }, range, at: Date.now() / 1000 });
        setFailure("");
      } catch (e) {
        if (dead || isAbort(e)) return;
        setFailure(e instanceof ApiError && e.status === 404 ? "这个 hub 还没有监控数据：部署新版本的 hub 和这台电脑上的 node 后再看。" : errorText(e));
      }
    };
    void load();
    const t = setInterval(load, RANGES[range].poll);
    const vis = () => void load();
    document.addEventListener("visibilitychange", vis);
    return () => {
      dead = true;
      ctrl?.abort();
      clearInterval(t);
      document.removeEventListener("visibilitychange", vis);
    };
  }, [mid, range]);

  // Launchers: an offline computer has none to list; again once it comes online.
  useEffect(() => {
    if (!online) {
      setLaunchers(null);
      return;
    }
    let dead = false;
    api<{ launchers: MachineLauncher[] }>(`/machines/${enc(mid)}/launchers`)
      .then((r) => !dead && (setLaunchers(r.launchers ?? []), setLauncherError("")))
      .catch((e) => !dead && setLauncherError(errorText(e)));
    return () => {
      dead = true;
    };
  }, [mid, online]);

  // Asks the hub to reconnect, then watches the machine list (up to 15 s) for it to come online.
  const reconnect = async () => {
    setReconnecting(true);
    try {
      await api(`/machines/${enc(mid)}/reconnect`, { method: "POST" });
    } catch (e) {
      toast(`重新连接失败：${errorText(e)}`, "error");
      setReconnecting(false);
      return;
    }
    for (let i = 0; i < 10 && alive.current; i++) {
      await new Promise((r) => setTimeout(r, 1500));
      await refresh().catch(() => {});
      if (getState().machines.find((x) => x.id === mid)?.online) {
        if (alive.current) setReconnecting(false);
        toast("已连上", "ok");
        return;
      }
    }
    if (!alive.current) return;
    setReconnecting(false);
    const err = getState().machines.find((x) => x.id === mid)?.error;
    toast(`还没连上${err ? `：${err}` : ""}`, "error");
  };
  // The hub installs its own build there; the machine list says when it is done (deploying / deploy_error).
  const deploy = async () => {
    try {
      await api(`/machines/${enc(mid)}/deploy`, { method: "POST" });
    } catch (e) {
      toast(`部署失败：${errorText(e)}`, "error");
    }
  };

  const groups = useMemo(() => (online ? agentsBySpace(sessions) : []), [online, sessions]);
  const agentCount = groups.reduce((n, g) => n + g.sessions.length, 0);
  const hub = hubLabel(m, machines);
  const route = routeText(m, machines);
  const system = data?.metrics.system ?? m?.system;
  const tz = useMemo(() => localOffset(), []);

  if (!m) {
    return (
      <Page mid={mid} title={mid} onToggleSidebar={onToggleSidebar}>
        {!loaded ? (
          <Loading />
        ) : (
          <div className="flex flex-col items-start gap-3 rounded-card bg-surface p-4 shadow-card">
            <p className="text-[13.5px] font-medium text-ink">找不到这台机器</p>
            <p className="text-[12.5px] text-ink-2">hub 的配置里没有「{mid}」，可能已经删掉或改了名字。</p>
            <a href="/machines" onClick={(e) => onLinkClick(e, { name: "machines" })} className="text-[12.5px] font-medium text-accent-ink hover:underline">
              回到机器列表
            </a>
          </div>
        )}
      </Page>
    );
  }

  return (
    <Page mid={mid} title={m.name} onToggleSidebar={onToggleSidebar} right={<span className="hidden whitespace-nowrap text-[11.5px] text-ink-3 sm:inline">{range === "1h" ? "每 10 秒刷新" : "每分钟刷新"}</span>}>
      {/* the computer */}
      <section className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} both` }}>
        <div className="flex items-start gap-3 px-4 py-3.5">
          <span className={cx("flex size-10 shrink-0 items-center justify-center rounded-[10px]", online ? "bg-field text-ink-2" : "bg-inset text-ink-3")}>
            <Icon name="machine" size={18} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <h2 className="truncate text-[15px] font-semibold text-ink">{m.name}</h2>
              <span className={cx("inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full px-1.5 text-[11px] font-medium", online ? "bg-green-tint text-green" : "bg-field text-ink-3")}>
                <span className={cx("size-1.5 rounded-full", online ? "bg-green" : "bg-ink-3")} />
                {online ? "在线" : "离线"}
              </span>
              {hub && (
                <span className="inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full bg-inset px-1.5 text-[11px] font-medium text-ink-2 shadow-hairline" title={`属于 hub「${hub}」`}>
                  <Icon name="hub" size={11} />
                  {hub}
                </span>
              )}
            </div>
            <p className="mt-0.5 truncate text-[12px] text-ink-3" title={systemLine(system, m)}>
              {systemLine(system, m)}
            </p>
            {route && <p className="mt-0.5 truncate text-[11.5px] text-ink-3" title="hub 怎么连到这台电脑">线路：{route}</p>}
          </div>
          {!online && (
            <Button tone="secondary" icon="retry" busy={reconnecting} onClick={() => void reconnect()}>
              {reconnecting ? "正在连接" : "重新连接"}
            </Button>
          )}
        </div>
        {!online && (
          <div className="mx-4 mb-3.5 rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red">
            <p className="break-words">{m.error || "hub 连不上这台电脑"}</p>
            <p className="mt-0.5 text-red/80">{[m.last_seen && ago(m.last_seen) && `最后在线：${ago(m.last_seen)}`, connectionText(m)].filter(Boolean).join(" · ")}</p>
          </div>
        )}
        {online && m.error && <p className="mx-4 mb-3.5 rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red">{m.error}</p>}
        {(m.node_mismatch || m.deploying) && (
          <div className="mx-4 mb-3.5 flex items-center gap-2 rounded-[8px] bg-orange-tint py-1.5 pr-1.5 pl-2.5 text-[12px] text-orange">
            <p className="min-w-0 flex-1">{m.deploying ? "正在把 hub 这一版的 jarvisd 装到这台电脑…" : m.deploy_error ? `上次部署没成：${m.deploy_error}` : "这台电脑上的 jarvisd 和 hub 不是同一版。"}</p>
            <Button tone="secondary" icon="retry" busy={m.deploying} disabled={m.deploying} onClick={() => void deploy()}>
              {m.deploying ? "部署中" : "重新部署"}
            </Button>
          </div>
        )}
      </section>

      {/* agents */}
      <section>
        <Heading title="Agent" count={online ? agentCount : undefined} />
        {groups.length === 0 ? (
          <p className="rounded-card bg-surface px-4 py-3 text-[12.5px] text-ink-3 shadow-card">{online ? "没有运行中的 Agent" : "机器离线，连上后显示它的 Agent"}</p>
        ) : (
          <div className="flex flex-col gap-2.5">
            {groups.map((g, gi) => (
              <div key={g.key} className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} ${Math.min(gi, 6) * 50}ms both` }}>
                <div className="flex items-center gap-2 border-b border-line px-4 py-2">
                  <Icon name={g.key ? "layers" : "agent"} size={13} className="shrink-0 text-ink-3" />
                  <span className="truncate text-[12.5px] font-medium text-ink">{g.name}</span>
                  <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">{countsText(g.counts)}</span>
                  <span className="shrink-0 text-[11.5px] text-ink-3 tabular-nums">{g.sessions.length}</span>
                </div>
                <div className="divide-y divide-line">
                  {g.sessions.map((s) => (
                    <AgentRow key={s.session_id} s={s} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* monitoring */}
      <section>
        <Heading
          title="监控"
          right={
            <div className="flex items-center gap-0.5 rounded-control bg-inset p-0.5 shadow-hairline" role="radiogroup" aria-label="时间范围">
              {RANGE_KEYS.map((k) => (
                <button
                  key={k}
                  type="button"
                  role="radio"
                  aria-checked={range === k}
                  onClick={() => setRange(k)}
                  className={cx(
                    "flex h-7 items-center whitespace-nowrap rounded-[7px] px-2.5 text-[12px] transition-[background-color,color] duration-150",
                    range === k ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
                  )}
                >
                  {RANGES[k].label}
                </button>
              ))}
            </div>
          }
        />
        {!data ? (
          failure ? (
            <div className="rounded-card bg-surface px-4 py-6 text-center shadow-card">
              <p className="text-[13px] font-medium text-ink">看不到监控数据</p>
              <p className="mt-1 text-[12px] text-ink-3">{failure}</p>
            </div>
          ) : (
            <div className="flex h-40 items-center justify-center rounded-card bg-surface shadow-card">
              <Loading />
            </div>
          )
        ) : (
          <Monitor data={data} range={range} online={online} system={system} hover={hover} onHover={setHover} tz={tz} onRange={setRange} />
        )}
        {data && failure && <p className="mt-2 px-1 text-[11.5px] text-red">刷新失败：{failure}</p>}
        <p className="mt-2 px-1 text-[11.5px] leading-relaxed text-ink-3">监控历史保留 24 小时。最近 1 小时每 10 秒一个点，更早的每分钟一个点；中间断开的时段不连线。</p>
      </section>

      {/* system */}
      <section>
        <Heading title="系统" />
        <div className="overflow-hidden rounded-card bg-surface shadow-card">
          <dl className="-mb-px grid grid-cols-1 md:grid-cols-2">
            {systemFacts(system, m, Date.now() / 1000, tz).map(([k, v, mono]) => (
              <div key={k} className="flex items-baseline gap-3 border-b border-line px-4 py-2 md:odd:border-r">
                <dt className="w-[84px] shrink-0 whitespace-nowrap text-[12px] text-ink-3">{k}</dt>
                <dd className={cx("min-w-0 flex-1 text-[12.5px] break-words text-ink-2 select-text", mono && "font-mono text-[12px]")}>{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      {/* launchers */}
      <section>
        <Heading title="启动器" count={online && launchers?.length ? launchers.length : undefined} />
        <div className="overflow-hidden rounded-card bg-surface shadow-card">
          {!online ? (
            <p className="px-4 py-3 text-[12.5px] text-ink-3">机器离线，连上后显示</p>
          ) : launcherError && !launchers ? (
            <p className="px-4 py-3 text-[12.5px] text-red">读不到启动器：{launcherError}</p>
          ) : !launchers ? (
            <div className="px-4 py-3">
              <Loading />
            </div>
          ) : launchers.length === 0 ? (
            <p className="px-4 py-3 text-[12.5px] text-ink-3">这台电脑上没有启动器</p>
          ) : (
            <div className="divide-y divide-line">
              {launchers.map((l) => {
                const kind = l.agent_type || l.agent || "claude";
                return (
                  <div key={l.name} className="flex items-center gap-3 px-4 py-2">
                    <span className="min-w-0 truncate font-mono text-[12.5px] text-ink">{l.name}</span>
                    {kind !== "claude" && <span className="inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full bg-inset px-1.5 text-[11px] font-medium text-ink-2 shadow-hairline">{kind === "codex" ? "Codex" : kind === "traex" ? "TRAEX" : kind}</span>}
                    <span className="flex-1" />
                    {l.model && <span className="shrink-0 truncate font-mono text-[11.5px] text-ink-3">{l.model}</span>}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </section>
    </Page>
  );
}

function Page({ mid, title, right, onToggleSidebar, children }: { mid: string; title: string; right?: React.ReactNode; onToggleSidebar: () => void; children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <a
          href="/machines"
          onClick={(e) => onLinkClick(e, { name: "machines" })}
          className="flex h-7 shrink-0 items-center gap-0.5 whitespace-nowrap rounded-[7px] pr-2 pl-1 text-[12.5px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
          title="回到机器列表"
        >
          <Icon name="chevronLeft" size={14} stroke={2.2} />
          机器
        </a>
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink" title={mid}>
          {title}
        </h1>
        {right}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[880px] flex-col gap-7 px-4 py-6 sm:px-8">{children}</div>
      </div>
    </div>
  );
}

function Heading({ title, count, right }: { title: string; count?: number; right?: React.ReactNode }) {
  return (
    <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
      <h2 className="text-[13px] font-medium text-ink">{title}</h2>
      {count !== undefined && <span className="text-[12px] text-ink-3 tabular-nums">{count}</span>}
      <span className="flex-1" />
      {right}
    </div>
  );
}

function AgentRow({ s }: { s: Session }) {
  const st = stateOf(s);
  const to = { name: "session" as const, mid: s.machine_id, sid: s.session_id };
  const meta = agentMeta(s);
  return (
    <a href={href(to)} onClick={(e) => onLinkClick(e, to)} className="group flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-hover">
      <span className="flex size-4 shrink-0 items-center justify-center">
        <StatusDot state={st} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="truncate text-[13px] font-medium text-ink">{sessionTitle(s)}</span>
          {meta && <span className="hidden shrink-0 font-mono text-[11px] text-ink-3 sm:inline">{meta}</span>}
        </span>
        {(s.last_text || meta) && (
          <span className="mt-0.5 block truncate text-[12px] text-ink-3">
            {meta && <span className="font-mono text-[11px] sm:hidden">{meta}{s.last_text ? " · " : ""}</span>}
            {s.last_text}
          </span>
        )}
      </span>
      {loudState(st) && <StatePill state={st} />}
      <Icon name="chevronRight" size={13} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-150 group-hover:translate-x-0.5" />
    </a>
  );
}

// ── monitoring: the values now, then the charts ──

function Monitor({
  data,
  range,
  online,
  system,
  hover,
  onHover,
  tz,
  onRange,
}: {
  data: { metrics: DetailMetrics; range: MetricRange; at: number };
  range: MetricRange;
  online: boolean;
  system: DetailMachine["system"];
  hover: number | null;
  onHover: (t: number | null) => void;
  tz: number;
  onRange: (r: MetricRange) => void;
}) {
  const { metrics } = data;
  const r = RANGES[data.range];
  const t1 = data.at,
    t0 = t1 - r.secs;
  const live = online ? (metrics.latest ?? undefined) : undefined;
  const memTotal = live?.mem_total ?? system?.memory ?? 0;
  const memPct = memPercent(live?.mem_used, live?.mem_total);

  const cpu = useMemo<ChartSeries[]>(() => [{ name: "CPU", color: "var(--accent)", read: (p) => p.cpu, now: live?.cpu }], [live?.cpu]);
  const mem = useMemo<ChartSeries[]>(() => [{ name: "内存", color: "var(--green)", read: (p) => (p.mem !== undefined && memTotal ? (100 * p.mem) / memTotal : undefined), now: memPct }], [memTotal, memPct]);
  const net = useMemo<ChartSeries[]>(
    () => [
      { name: "下行", color: "var(--orange)", read: (p) => p.rx, now: live?.net_rx },
      { name: "上行", color: "var(--accent)", read: (p) => p.tx, now: live?.net_tx },
    ],
    [live?.net_rx, live?.net_tx],
  );
  const load = useMemo<ChartSeries[]>(() => [{ name: "负载", color: "var(--chart-load)", read: (p) => p.load, now: live?.load }], [live?.load]);
  const cores = system?.cores ?? 0;
  const perCore = coresNow(metrics.latest, online);
  const common = { points: metrics.points, t0, t1, step: r.step, hover, onHover, tz };

  // Nothing in the whole range (offline all along, or just started): one line instead of three empty charts.
  if (isEmpty(metrics.points))
    return (
      <>
        {live && <NowTiles s={live} cores={system?.cores} />}
        {coresNow(metrics.latest, online) && <CoreBars cores={coresNow(metrics.latest, online)!} />}
        <div className={cx("flex flex-col items-center gap-2.5 rounded-card bg-surface px-4 py-7 text-center shadow-card transition-opacity duration-200", data.range !== range && "opacity-60")}>
          <p className="text-[12.5px] text-ink-2">
            {online ? "刚开始记录：过一会儿这里就有曲线了。" : `最近 ${RANGES[data.range].label}这台机器没有连上，没有数据。`}
          </p>
          {!online && data.range !== "24h" && (
            <Button tone="secondary" onClick={() => onRange("24h")}>
              看最近 24 小时
            </Button>
          )}
        </div>
      </>
    );

  return (
    <>
      {live && <NowTiles s={live} cores={system?.cores} />}
      <div className={cx("grid grid-cols-1 gap-3 transition-opacity duration-200 md:grid-cols-2", data.range !== range && "opacity-60")}>
        <MetricChart title="CPU" series={cpu} percent format={percent} axisFormat={(v) => `${v}%`} {...common} />
        <MetricChart title={memTotal ? `内存（共 ${bytesBin(memTotal).replace(/\.00 /, " ")}）` : "内存"} series={mem} percent format={percent} axisFormat={(v) => `${v}%`} {...common} />
        <MetricChart title="网络" series={net} binary format={rate} axisFormat={bytesAxis} {...common} />
        <MetricChart title="负载（1 分钟）" series={load} format={loadText} axisFormat={loadAxis} reference={cores > 0 ? { value: cores, label: `${cores} 核` } : undefined} {...common} />
      </div>
      {perCore && <CoreBars cores={perCore} />}
    </>
  );
}

function NowTiles({ s, cores }: { s: NonNullable<DetailMetrics["latest"]>; cores?: number }) {
  const memPct = memPercent(s.mem_used, s.mem_total);
  const diskPct = memPercent(s.disk_used, s.disk_total);
  const swapPct = s.swap_total ? memPercent(s.swap_used, s.swap_total) : undefined;
  const perCore = s.load !== undefined && cores ? s.load / cores : undefined;
  const tiles: { label: string; value: string; sub?: string; frac?: number; lv: Level }[] = [
    { label: "CPU", value: percent(s.cpu), sub: cores ? `${cores} 核` : undefined, frac: s.cpu, lv: level(s.cpu, 75, 90) },
    { label: "内存", value: percent(memPct), sub: s.mem_used !== undefined ? `${bytesBin(s.mem_used)} / ${bytesBin(s.mem_total)}` : undefined, frac: memPct, lv: level(memPct, 80, 90) },
    { label: "网络", value: `↓ ${rate(s.net_rx)}`, sub: `↑ ${rate(s.net_tx)}`, lv: "normal" },
    { label: "负载", value: loadText(s.load), sub: cores ? `${cores} 核` : undefined, lv: level(perCore, 1, 2) },
    { label: "磁盘", value: percent(diskPct), sub: s.disk_used !== undefined ? `${bytesBin(s.disk_used)} / ${bytesBin(s.disk_total)}` : undefined, frac: diskPct, lv: level(diskPct, 80, 90) },
    { label: "交换", value: s.swap_total ? bytesBin(s.swap_used) : "未启用", sub: s.swap_total ? `共 ${bytesBin(s.swap_total)}` : undefined, frac: swapPct, lv: "normal" },
  ];
  return (
    <div className="mb-3 grid grid-cols-3 gap-2 lg:grid-cols-6">
      {tiles.map((t) => (
        <div key={t.label} className="min-w-0 rounded-card bg-surface px-2.5 py-2.5 shadow-card sm:px-3">
          <div className="flex items-center gap-1.5 text-[11px] text-ink-3">
            <span className="truncate">{t.label}</span>
            {t.lv !== "normal" && (
              <span className="inline-flex shrink-0 items-center gap-1 whitespace-nowrap text-ink-2">
                <span className={cx("size-1.5 rounded-full", t.lv === "critical" ? "bg-red" : "bg-orange")} />
                {t.lv === "critical" ? "过高" : "偏高"}
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate text-[15px] leading-6 font-medium text-ink tabular-nums sm:text-[17px]" title={t.value}>{t.value}</div>
          <div className="truncate text-[10.5px] text-ink-3 tabular-nums">{t.sub ?? " "}</div>
          <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-field">
            {t.frac !== undefined && (
              <div
                className={cx("h-full rounded-full transition-[width] duration-500", t.lv === "critical" ? "bg-red" : t.lv === "warning" ? "bg-orange" : "bg-accent")}
                style={{ width: `${Math.max(3, Math.min(100, t.frac))}%` }}
              />
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

/** Each core now (Linux), one thin column each (iOS CoreBars); hover a column for its number. */
function CoreBars({ cores }: { cores: number[] }) {
  const busiest = Math.max(...cores);
  return (
    <section className="mt-3 rounded-card bg-surface px-4 pt-3 pb-3.5 shadow-card" role="img" aria-label={`每个核心的使用率：${cores.length} 个核心，最忙 ${percent(busiest)}`}>
      <div className="flex items-baseline gap-2">
        <h3 className="truncate text-[12.5px] font-medium text-ink-2">每个核心</h3>
        <span className="flex-1" />
        <span className="shrink-0 text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums">最忙 {percent(busiest)}</span>
      </div>
      <div className={cx("mt-2.5 flex h-11 items-end", cores.length > 32 ? "gap-px" : "gap-0.5")}>
        {cores.map((c, i) => (
          <div key={i} title={`核心 ${i + 1}：${percent(c)}`} className="relative h-full min-w-px flex-1 overflow-hidden rounded-[2px] bg-field">
            <div className="absolute inset-x-0 bottom-0 rounded-[2px] bg-accent transition-[height] duration-500" style={{ height: `${Math.max(4, c)}%` }} />
          </div>
        ))}
      </div>
    </section>
  );
}
