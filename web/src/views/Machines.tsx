// 机器: every computer's CPU / memory / disk now and over the last hour (docs/machines.md), as Insight Cards.
import { useEffect, useState } from "react";
import { api, errorText } from "../lib/api";
import { bytes, stateOf } from "../lib/format";
import { useAgentSessions } from "../lib/sessions";
import { useStore } from "../lib/store";
import type { MachineMetrics, MetricPoint } from "../lib/types";
import { Button, cx, EASE, IconButton, Loading, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

export function Machines({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines);
  const sessions = useAgentSessions();
  const [metrics, setMetrics] = useState<MachineMetrics[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let dead = false;
    const load = () =>
      api<{ machines: MachineMetrics[] }>("/metrics")
        .then((r) => !dead && (setMetrics(r.machines ?? []), setError("")))
        .catch((e) => !dead && setError(errorText(e)));
    void load();
    const t = setInterval(load, 10_000);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, []);

  const reconnect = async (id: string) => {
    try {
      await api(`/machines/${encodeURIComponent(id)}/reconnect`, { method: "POST" });
      toast("正在重连", "ok");
    } catch (e) {
      toast(`重连失败：${errorText(e)}`, "error");
    }
  };
  // The hub installs its own build there; the machine list says when it is done (deploying / deploy_error).
  const deploy = async (id: string) => {
    try {
      await api(`/machines/${encodeURIComponent(id)}/deploy`, { method: "POST" });
    } catch (e) {
      toast(`部署失败：${errorText(e)}`, "error");
    }
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="flex-1 text-[14px] font-semibold text-ink">机器</h1>
        <span className="text-[11.5px] text-ink-3">每 10 秒刷新</span>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto grid max-w-[1000px] grid-cols-1 gap-3 px-5 py-6 sm:px-8 md:grid-cols-2">
          {!metrics && !error && <Loading />}
          {error && !metrics && <p className="text-[13px] text-red">{error}</p>}
          {machines.map((m, i) => {
            const mm = metrics?.find((x) => x.machine_id === m.id);
            const l = mm?.latest;
            const agents = m.online ? sessions.filter((s) => s.machine_id === m.id && stateOf(s) !== "offline") : [];
            const working = agents.filter((s) => stateOf(s) === "working").length;
            const memPct = l?.mem_used && l.mem_total ? (l.mem_used / l.mem_total) * 100 : undefined;
            const diskPct = l?.disk_used && l.disk_total ? (l.disk_used / l.disk_total) * 100 : undefined;
            return (
              <section key={m.id} className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} ${i * 60}ms both` }}>
                <div className="flex items-start gap-3 px-4 pt-3.5">
                  <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[9px]", m.online ? "bg-field text-ink-2" : "bg-inset text-ink-3")}>
                    <Icon name="machine" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h2 className="truncate text-[14px] font-semibold text-ink">{m.name}</h2>
                      <span className={cx("inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[11px] font-medium", m.online ? "bg-green-tint text-green" : "bg-field text-ink-3")}>
                        <span className={cx("size-1.5 rounded-full", m.online ? "bg-green" : "bg-ink-3")} />
                        {m.online ? "在线" : "离线"}
                      </span>
                    </div>
                    <p className="truncate text-[11.5px] text-ink-3">{[m.system?.name || m.os, m.system?.model, m.system?.cpu].filter(Boolean).join(" · ") || m.hostname}</p>
                  </div>
                  {!m.online && (
                    <Button tone="secondary" icon="retry" onClick={() => reconnect(m.id)}>
                      重连
                    </Button>
                  )}
                </div>
                {m.online && m.error && <p className="mx-4 mt-2 rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red">{m.error}</p>}
                {(m.node_mismatch || m.deploying) && (
                  <div className="mx-4 mt-2 flex items-center gap-2 rounded-[8px] bg-orange-tint py-1.5 pl-2.5 pr-1.5 text-[12px] text-orange">
                    <p className="min-w-0 flex-1">
                      {m.deploying ? "正在把 hub 这一版的 jarvisd 装到这台电脑…" : m.deploy_error ? `上次部署没成：${m.deploy_error}` : "这台电脑上的 jarvisd 和 hub 不是同一版。"}
                    </p>
                    <Button tone="secondary" icon="retry" busy={m.deploying} disabled={m.deploying} onClick={() => deploy(m.id)}>
                      {m.deploying ? "部署中" : "重新部署"}
                    </Button>
                  </div>
                )}
                <div className="grid grid-cols-3 gap-px px-4 pt-3">
                  <Stat label="CPU" value={l?.cpu !== undefined ? `${Math.round(l.cpu)}%` : "—"} warn={(l?.cpu ?? 0) > 85} />
                  <Stat label="内存" value={memPct !== undefined ? `${Math.round(memPct)}%` : "—"} sub={l?.mem_used ? `${bytes(l.mem_used)} / ${bytes(l.mem_total)}` : undefined} warn={(memPct ?? 0) > 90} />
                  <Stat label="磁盘" value={diskPct !== undefined ? `${Math.round(diskPct)}%` : "—"} sub={l?.disk_total ? `剩 ${bytes((l.disk_total ?? 0) - (l.disk_used ?? 0))}` : undefined} warn={(diskPct ?? 0) > 90} />
                </div>
                <div className="px-2 pt-2">
                  <Spark points={mm?.points ?? []} />
                </div>
                <div className="flex items-center justify-between border-t border-line px-4 py-2 text-[11.5px] text-ink-3">
                  <span>{!m.online ? "连不上这台电脑" : agents.length ? `${agents.length} 个 Agent${working ? `，${working} 个在工作` : ""}` : "没有在跑的 Agent"}</span>
                  <span className="font-mono">{[m.claude_version && `claude ${m.claude_version}`, m.herdr_version && `herdr ${m.herdr_version}`].filter(Boolean).join(" · ")}</span>
                </div>
              </section>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value, sub, warn }: { label: string; value: string; sub?: string; warn?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-ink-3">{label}</div>
      <div className={cx("font-mono text-[18px] leading-tight font-medium tabular-nums", warn ? "text-red" : "text-ink")}>{value}</div>
      {sub && <div className="truncate text-[10.5px] text-ink-3">{sub}</div>}
    </div>
  );
}

// CPU over the last hour; gaps break the line.
function Spark({ points }: { points: MetricPoint[] }) {
  const W = 440,
    H = 56;
  const real = points.filter((p) => !p.gap && p.cpu !== undefined);
  if (real.length < 2) return <div className="flex h-14 items-center justify-center text-[11.5px] text-ink-3">还没有历史</div>;
  const t0 = points[0].t,
    t1 = points[points.length - 1].t || t0 + 1;
  const x = (t: number) => ((t - t0) / Math.max(1, t1 - t0)) * W;
  const y = (v: number) => H - 3 - (Math.min(100, Math.max(0, v)) / 100) * (H - 6);
  const runs: MetricPoint[][] = [[]];
  for (const p of points) {
    if (p.gap || p.cpu === undefined) {
      if (runs[runs.length - 1].length) runs.push([]);
    } else runs[runs.length - 1].push(p);
  }
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="h-14 w-full" aria-label="最近一小时 CPU">
      <defs>
        <linearGradient id="cpu-fill" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.18" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0" />
        </linearGradient>
      </defs>
      {[25, 50, 75].map((v) => (
        <line key={v} x1={0} x2={W} y1={y(v)} y2={y(v)} stroke="var(--line)" strokeWidth={1} vectorEffect="non-scaling-stroke" strokeDasharray="2 4" />
      ))}
      {runs
        .filter((r) => r.length > 1)
        .map((r, i) => {
          const line = r.map((p) => `${x(p.t).toFixed(1)},${y(p.cpu!).toFixed(1)}`).join(" ");
          return (
            <g key={i}>
              <polygon points={`${x(r[0].t)},${H} ${line} ${x(r[r.length - 1].t)},${H}`} fill="url(#cpu-fill)" />
              <polyline points={line} fill="none" stroke="var(--accent)" strokeWidth={1.6} vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
            </g>
          );
        })}
    </svg>
  );
}
