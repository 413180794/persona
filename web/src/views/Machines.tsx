// 机器: every computer's CPU / memory / disk now and over the last hour (docs/machines.md), as Insight Cards. 添加电脑
// (feature machines_add) adds one from here, and a computer waiting to be confirmed (trust pending) says so on its card.
import { useEffect, useState } from "react";
import { t as tr } from "../i18n";
import { canAddComputer, isPending, machineErrorText } from "../lib/add-computer";
import { api, enc, errorText } from "../lib/api";
import { readinessChanged } from "../lib/readiness";
import { bytes, stateOf } from "../lib/format";
import { hubLabel, routeText } from "../lib/mesh";
import { machineDeployment } from "../lib/machine-deploy";
import { classifyConnection } from "../lib/node-health";
import { href, navigate, onLinkClick, type Route } from "../lib/router";
import { useAgentSessions } from "../lib/sessions";
import { useStore } from "../lib/store";
import type { MachineMetrics, MetricPoint } from "../lib/types";
import { Button, cx, EASE, IconButton, Loading, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { Empty } from "./assistant/parts";
import { AddComputer, type PendingComputer } from "./machines/AddComputer";
import { HelpDialog } from "./assistant/Readiness";
import { MachineDeploy } from "./machines/MachineDeploy";
import { t } from "../i18n";

export function Machines({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines);
  const loaded = useStore((s) => s.loaded);
  const canAdd = useStore((s) => canAddComputer(s.features));
  const [adding, setAdding] = useState<{ again: PendingComputer | null } | null>(null);
  // Hubs without machines_add get the plain how-to instead of the in-app flow.
  const [helping, setHelping] = useState(false);
  const [answering, setAnswering] = useState("");
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
    const timer = setInterval(load, 10_000);
    return () => {
      dead = true;
      clearInterval(timer);
    };
  }, []);

  const reconnect = async (id: string) => {
    try {
      await api(`/machines/${encodeURIComponent(id)}/reconnect`, { method: "POST" });
      toast(t("machines.reconnecting"), "ok");
    } catch (e) {
      toast(t("machines.reconnect_failed", { error: errorText(e) }), "error");
    }
  };

  // 是我的电脑 / 不是我的 for a computer waiting to be confirmed; the hub's next machines push shows the result.
  const answer = async (m: { id: string; name: string }, what: "confirm" | "reject") => {
    setAnswering(m.id);
    try {
      await api(`/machines/${enc(m.id)}/${what}`, { method: "POST" });
      toast(t(what === "confirm" ? "machines.confirmed" : "machines.rejected", { name: m.name }), "ok");
      void readinessChanged();
    } catch (e) {
      toast(t("machines.answer_failed", { error: errorText(e) }), "error");
    } finally {
      setAnswering("");
    }
  };

  const add = (
    <Button tone="primary" icon="plus" onClick={() => (canAdd ? setAdding({ again: null }) : setHelping(true))}>
      {t("machines.add")}
    </Button>
  );

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("machines.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="flex-1 text-[14px] font-semibold text-ink">{t("machines.title")}</h1>
        <span className="text-[11.5px] text-ink-3 max-sm:hidden">{t("machines.refresh_interval", { seconds: 10 })}</span>
        {machines.length > 0 && canAdd && add}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto grid max-w-[1000px] grid-cols-1 gap-3 px-5 py-6 sm:px-8 md:grid-cols-2">
          {!metrics && !error && <Loading />}
          {/* a failed refresh says so even with the last numbers on screen (iOS / Android keep their notice up) */}
          {error && (
            <p role="alert" className="flex items-start gap-2 rounded-[8px] bg-red-tint px-3 py-2 text-[12.5px] text-red md:col-span-2">
              <Icon name="warning" size={14} className="mt-px shrink-0" />
              <span className="min-w-0 break-words">{metrics ? t("machines.refresh_failed_stale", { error, seconds: 10 }) : t("machines.load_failed", { error })}</span>
            </p>
          )}
          {loaded && machines.length === 0 && (metrics || error) && (
            <div className="md:col-span-2">
              <Empty icon="machine" title={error && !metrics ? t("machines.empty_unavailable") : t("machines.empty_title")} action={add}>
                {t("machines.empty_body_add")}
              </Empty>
            </div>
          )}
          {machines.map((m, i) => {
            const mm = metrics?.find((x) => x.machine_id === m.id);
            const l = mm?.latest;
            const agents = m.online ? sessions.filter((s) => s.machine_id === m.id && stateOf(s) !== "offline") : [];
            const working = agents.filter((s) => stateOf(s) === "working").length;
            const memPct = l?.mem_used && l.mem_total ? (l.mem_used / l.mem_total) * 100 : undefined;
            const diskPct = l?.disk_used && l.disk_total ? (l.disk_used / l.disk_total) * 100 : undefined;
            const hub = hubLabel(m, machines);
            const route = routeText(m, machines);
            const detail = { name: "machine" as const, mid: m.id };
            const { expectedDisconnect } = machineDeployment(m);
            const pending = isPending(m);
            // why it is offline: the hub's error_code in our words, else from the error text, and what to do
            const coded = pending ? "" : machineErrorText(m.error_code);
            const connFailure = expectedDisconnect || pending || coded ? null : classifyConnection(m);
            return (
              <section key={m.id} onClick={(e) => openFromCard(e, detail)} className="cursor-pointer overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} ${i * 60}ms both` }}>
                <div className="flex items-start gap-3 px-4 pt-3.5">
                  <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[9px]", m.online ? "bg-field text-ink-2" : "bg-inset text-ink-3")}>
                    <Icon name="machine" size={16} />
                  </span>
                  <a href={href(detail)} onClick={(e) => onLinkClick(e, detail)} className="group/title min-w-0 flex-1 rounded-[6px]">
                    <div className="flex items-center gap-2">
                      <h2 className="truncate text-[14px] font-semibold text-ink group-hover/title:underline">{m.name}</h2>
                      {hub && (
                        <span title={route ? `${hub} · ${route}` : hub} className="inline-flex h-5 shrink-0 items-center gap-1 whitespace-nowrap rounded-full bg-field px-1.5 text-[11px] font-medium text-ink-2">
                          <Icon name="hub" size={11} />
                          {hub}
                        </span>
                      )}
                      <span className={cx("inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[11px] font-medium", m.online ? "bg-green-tint text-green" : "bg-field text-ink-3")}>
                        <span className={cx("size-1.5 rounded-full", m.online ? "bg-green" : "bg-ink-3")} />
                        {m.online ? t("machines.online") : t("machines.offline")}
                      </span>
                    </div>
                    <p className="truncate text-[11.5px] text-ink-3">{[m.system?.name || m.os, m.system?.model, m.system?.cpu].filter(Boolean).join(" · ") || m.hostname}</p>
                  </a>
                  {!m.online && !expectedDisconnect && !pending && (
                    <Button tone="secondary" icon="retry" onClick={() => reconnect(m.id)}>
                      {t("machines.reconnect")}
                    </Button>
                  )}
                </div>
                {pending && (
                  <div className="mx-4 mt-2 flex flex-wrap items-center gap-2 rounded-[8px] bg-orange-tint px-2.5 py-2 text-[12px] text-ink" data-trust="pending">
                    <Icon name="shield" size={14} className="shrink-0 text-orange" />
                    <span className="min-w-0 flex-1 basis-48">{m.online ? t("machines.pending_joined", { name: m.name }) : t("machines.pending_waiting")}</span>
                    <span className="flex shrink-0 gap-1.5">
                      {m.online ? (
                        <>
                          <Button tone="secondary" disabled={answering === m.id} onClick={() => void answer(m, "reject")}>
                            {t("machines.reject")}
                          </Button>
                          <Button tone="primary" busy={answering === m.id} onClick={() => void answer(m, "confirm")}>
                            {t("machines.confirm")}
                          </Button>
                        </>
                      ) : (
                        <>
                          <Button tone="secondary" disabled={answering === m.id} onClick={() => void answer(m, "reject")}>
                            {t("machines.remove")}
                          </Button>
                          {canAdd && (
                            <Button tone="primary" onClick={() => setAdding({ again: { id: m.id, name: m.name } })}>
                              {t("machines.new_code")}
                            </Button>
                          )}
                        </>
                      )}
                    </span>
                  </div>
                )}
                {m.online && !pending && (coded || m.error) && <p className="mx-4 mt-2 rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red">{coded || m.error}</p>}
                {!m.online && coded && !expectedDisconnect && (
                  <p className="mx-4 mt-2 break-words rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red" data-error-code={m.error_code} title={m.error}>
                    {coded}
                  </p>
                )}
                {connFailure && (
                  <p className="mx-4 mt-2 break-words rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] text-red" data-failure={connFailure.kind}>
                    <span className="font-medium">{tr("node-health.failure_title", { title: connFailure.title })}</span>
                    {connFailure.next}
                  </p>
                )}
                <MachineDeploy machine={m} className="mt-2" />
                <div className="grid grid-cols-3 gap-px px-4 pt-3">
                  <Stat label="CPU" value={l?.cpu !== undefined ? `${Math.round(l.cpu)}%` : "—"} warn={(l?.cpu ?? 0) > 85} />
                  <Stat label={t("machines.memory")} value={memPct !== undefined ? `${Math.round(memPct)}%` : "—"} sub={l?.mem_used ? `${bytes(l.mem_used)} / ${bytes(l.mem_total)}` : undefined} warn={(memPct ?? 0) > 90} />
                  <Stat label={t("machines.disk")} value={diskPct !== undefined ? `${Math.round(diskPct)}%` : "—"} sub={l?.disk_total ? t("machines.disk_free", { size: bytes((l.disk_total ?? 0) - (l.disk_used ?? 0)) }) : undefined} warn={(diskPct ?? 0) > 90} />
                </div>
                <div className="px-2 pt-2">
                  <Spark points={mm?.points ?? []} />
                </div>
                <div className="flex items-center justify-between border-t border-line px-4 py-2 text-[11.5px] text-ink-3">
                  <span className="whitespace-nowrap">{pending && !m.online ? t("machines.never_connected") : !m.online ? expectedDisconnect ? t("machines.waiting_node") : t("machines.unreachable") : agents.length ? working ? t("machines.agents_working", { count: agents.length, working }) : t("machines.agents", { count: agents.length }) : t("machines.no_agents")}</span>
                  <span className="flex min-w-0 items-center gap-3">
                    <span className="truncate font-mono max-sm:hidden">{[m.claude_version && `claude ${m.claude_version}`, m.herdr_version && `herdr ${m.herdr_version}`].filter(Boolean).join(" · ")}</span>
                    <a href={href(detail)} onClick={(e) => onLinkClick(e, detail)} className="shrink-0 whitespace-nowrap text-ink-2 hover:text-ink">{t("machines.details")}</a>
                  </span>
                </div>
              </section>
            );
          })}
        </div>
      </div>
      <AddComputer open={!!adding} again={adding?.again} onClose={() => setAdding(null)} />
      {helping && <HelpDialog topic="add_computer" machine="" step={null} onClose={() => setHelping(false)} onAction={() => {}} />}
    </div>
  );
}

// Elements that keep their own click inside a card (the links, 重新连接, 部署 …).
const INTERACTIVE = "a,button,input,select,textarea,label,summary,[role=button],[tabindex]";

// A tap anywhere else on a machine's card opens its page (#3578): on a phone the 详情 link alone is a 48 × 17 target.
// The card stays a plain section; the title link is what keyboards and screen readers use, so no control nests in another.
function openFromCard(e: React.MouseEvent<HTMLElement>, to: Route) {
  const own = e.target instanceof Element ? e.target.closest(INTERACTIVE) : null;
  if (own && e.currentTarget.contains(own)) return;
  if (e.button !== 0 || window.getSelection()?.toString()) return; // selecting the error text to copy it is not a tap
  navigate(to);
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
  if (real.length < 2) return <div className="flex h-14 items-center justify-center text-[11.5px] text-ink-3">{t("machines.no_history")}</div>;
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
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="h-14 w-full" aria-label={t("machines.cpu_last_hour")}>
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
