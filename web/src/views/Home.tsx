// 总览: what needs you, what is running, what just finished — Beautiful UI's Task Rows — and the computers.
import { useMemo } from "react";
import { ago, shortDuration, sourceSubtitle, sessionTitle, stateOf, STATE_LABEL, tabLabel } from "../lib/format";
import { href, onLinkClick } from "../lib/router";
import { useAgentSessions, useCounts } from "../lib/sessions";
import { useStore } from "../lib/store";
import type { AgentState, Interaction, Session } from "../lib/types";
import { Button, cx, EASE, IconButton, Loading, StatusDot, useNow } from "../ui/atoms";
import { Icon } from "../ui/icons";

const KIND: Record<Interaction["kind"], string> = { question: "提问", plan: "计划待批准", permission: "需要授权", screen: "终端对话框" };

function greeting() {
  const h = new Date().getHours();
  return h < 5 ? "夜深了" : h < 11 ? "早上好" : h < 13 ? "中午好" : h < 18 ? "下午好" : "晚上好";
}

export function Home({ onNewAgent, onToggleSidebar }: { onNewAgent: () => void; onToggleSidebar: () => void }) {
  const sessions = useAgentSessions();
  const machines = useStore((s) => s.machines);
  const interactions = useStore((s) => s.interactions);
  const loaded = useStore((s) => s.loaded);
  const counts = useCounts();
  const now = useNow(5000, counts.working > 0);

  const groups = useMemo(() => {
    const pick = (st: AgentState) => sessions.filter((s) => stateOf(s) === st).sort((a, b) => ((st === "working" ? a.state_since : b.updated_at) ?? "").localeCompare((st === "working" ? b.state_since : a.updated_at) ?? ""));
    return { needs: pick("needs_you"), working: pick("working"), unread: pick("unread") };
  }, [sessions]);
  const name = (mid: string) => machines.find((m) => m.id === mid)?.name ?? mid;
  const quiet = !groups.needs.length && !groups.working.length && !groups.unread.length;

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4 lg:border-transparent">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex-1" />
        <Button tone="secondary" icon="plus" onClick={onNewAgent}>
          新建 Agent
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[760px] flex-col gap-9 px-5 pt-6 pb-16 sm:px-8 sm:pt-10">
          <div style={{ animation: `fade-up 420ms ${EASE} both` }}>
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">{greeting()}</h1>
            <p className="mt-1 text-[13.5px] text-ink-2">
              {!loaded ? "正在连上 hub…" : quiet ? "没有需要你处理的，所有 Agent 都在等你。" : [counts.needs && `${counts.needs} 个需要你`, counts.working && `${counts.working} 个在工作`, counts.unread && `${counts.unread} 个做完了还没看`].filter(Boolean).join("，") + "。"}
            </p>
          </div>

          {!loaded && <Loading label="加载中" />}

          {interactions.length > 0 && (
            <Group title="需要你处理" count={interactions.length}>
              {interactions.map((ix, i) => {
                const s = sessions.find((x) => x.machine_id === ix.machine_id && x.session_id === ix.session_id);
                return (
                  <TaskRow
                    key={ix.id}
                    i={i}
                    state="needs_you"
                    to={{ name: "session", mid: ix.machine_id, sid: ix.session_id }}
                    title={ix.session_title || (s ? sessionTitle(s) : ix.cwd || "会话")}
                    meta={[name(ix.machine_id), s?.space_name, s && tabLabel(s)].filter(Boolean).join(" › ")}
                    detail={ix.kind === "question" ? ix.questions?.[0]?.question : ix.kind === "permission" ? ix.summary || ix.tool_name : ix.kind === "screen" ? ix.dialog?.question : "计划写好了，等你批准"}
                    pill={<Pill tone="red">{KIND[ix.kind]}</Pill>}
                  />
                );
              })}
            </Group>
          )}

          {groups.needs.filter((s) => !interactions.some((i) => i.session_id === s.session_id)).length > 0 && (
            <Group title="停在电脑上" count={groups.needs.length}>
              {groups.needs
                .filter((s) => !interactions.some((i) => i.session_id === s.session_id))
                .map((s, i) => (
                  <SessionTask key={s.session_id} s={s} i={i} machine={name(s.machine_id)} pill={<Pill tone="red">需要你</Pill>} />
                ))}
            </Group>
          )}

          {groups.working.length > 0 && (
            <Group title="正在工作" count={groups.working.length}>
              {groups.working.map((s, i) => (
                <SessionTask key={s.session_id} s={s} i={i} machine={name(s.machine_id)} pill={<span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{s.state_since ? shortDuration(now - Date.parse(s.state_since)) : ""}</span>} />
              ))}
            </Group>
          )}

          {groups.unread.length > 0 && (
            <Group title="做完了，还没看" count={groups.unread.length}>
              {groups.unread.map((s, i) => (
                <SessionTask key={s.session_id} s={s} i={i} machine={name(s.machine_id)} pill={<Pill tone="green">{ago(s.updated_at) || "完成"}</Pill>} />
              ))}
            </Group>
          )}

          {loaded && quiet && interactions.length === 0 && (
            <div className="flex flex-col items-start gap-3 rounded-card bg-surface p-4 shadow-card">
              <span className="flex size-8 items-center justify-center rounded-full bg-green-tint text-green">
                <Icon name="check" size={15} stroke={2.6} />
              </span>
              <div>
                <p className="text-[13.5px] font-medium text-ink">一切就绪</p>
                <p className="mt-0.5 text-[12.5px] text-ink-2">在左边打开一个 Agent 接着聊，或者开一个新的。⌘K 搜索全部会话。</p>
              </div>
              <Button tone="primary" icon="plus" onClick={onNewAgent}>
                新建 Agent
              </Button>
            </div>
          )}

          {machines.length > 0 && (
            <Group title="电脑" count={machines.length} right={<a href="/machines" onClick={(e) => onLinkClick(e, { name: "machines" })} className="text-[12px] text-ink-3 hover:text-ink-2">监控 ›</a>}>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {machines.map((m) => {
                  const mine = m.online ? sessions.filter((s) => s.machine_id === m.id && stateOf(s) !== "offline") : [];
                  return (
                    <div key={m.id} className="flex items-center gap-3 rounded-card bg-surface px-3 py-2.5 shadow-card">
                      <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-[8px]", m.online ? "bg-field text-ink-2" : "bg-inset text-ink-3")}>
                        <Icon name="machine" size={15} />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] font-medium text-ink">{m.name}</span>
                        <span className="block truncate text-[11.5px] text-ink-3">{m.online ? `${mine.length} 个 Agent${m.system?.name ? ` · ${m.system.name}` : ""}` : m.error || "离线"}</span>
                      </span>
                      <span className={cx("size-2 shrink-0 rounded-full", m.online ? "bg-green" : "bg-line-strong")} title={m.online ? "在线" : "离线"} />
                    </div>
                  );
                })}
              </div>
            </Group>
          )}
        </div>
      </div>
    </div>
  );
}

function Group({ title, count, children, right }: { title: string; count: number; children: React.ReactNode; right?: React.ReactNode }) {
  return (
    <section>
      <div className="mb-2.5 flex items-baseline gap-2">
        <h2 className="text-[13px] font-medium text-ink">{title}</h2>
        <span className="text-[12px] text-ink-3 tabular-nums">{count}</span>
        <span className="flex-1" />
        {right}
      </div>
      <div className="flex flex-col gap-2">{children}</div>
    </section>
  );
}

function Pill({ tone, children }: { tone: "red" | "green"; children: React.ReactNode }) {
  return <span className={cx("inline-flex h-5.5 shrink-0 items-center rounded-full px-2 text-[11.5px] font-medium whitespace-nowrap", tone === "red" ? "bg-red-tint text-red" : "bg-green-tint text-green")}>{children}</span>;
}

function SessionTask({ s, i, machine, pill }: { s: Session; i: number; machine: string; pill: React.ReactNode }) {
  return <TaskRow i={i} state={stateOf(s)} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} title={sessionTitle(s)} meta={[sourceSubtitle(s), machine, s.space_name, tabLabel(s)].filter(Boolean).join(" › ")} detail={s.source_kind ? [sourceSubtitle(s), s.last_text].filter(Boolean).join(" · ") : s.last_text} pill={pill} />;
}

// One Task Rows capsule: status badge, title, the latest words, a pill.
function TaskRow({ i, state, to, title, meta, detail, pill }: { i: number; state: AgentState; to: { name: "session"; mid: string; sid: string }; title: string; meta: string; detail?: string; pill: React.ReactNode }) {
  return (
    <a
      href={href(to)}
      onClick={(e) => onLinkClick(e, to)}
      className="group flex items-center gap-3 rounded-[16px] bg-surface px-3 py-2.5 shadow-card transition-[background-color,transform] duration-150 hover:bg-inset active:scale-[0.995]"
      style={{ animation: `fade-up 420ms ${EASE} ${Math.min(i, 8) * 50}ms both` }}
    >
      <Badge state={state} />
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{title}</span>
          <span className="hidden shrink-0 truncate text-[11.5px] text-ink-3 sm:inline">{meta}</span>
        </span>
        {detail && <span className="mt-0.5 block truncate text-[12.5px] text-ink-2">{detail}</span>}
      </span>
      {pill}
      <Icon name="chevronRight" size={14} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-150 group-hover:translate-x-0.5" />
    </a>
  );
}

function Badge({ state }: { state: AgentState }) {
  if (state === "working") {
    const size = 24,
      stroke = 2,
      r = (size - stroke) / 2,
      c = 2 * Math.PI * r;
    return (
      <span className="relative inline-flex size-6 shrink-0 items-center justify-center" aria-label={STATE_LABEL[state]}>
        <svg width={size} height={size} className="absolute inset-0" style={{ animation: "spin 1.1s linear infinite" }}>
          <circle cx={12} cy={12} r={r} fill="none" stroke="var(--line)" strokeWidth={stroke} />
          <circle cx={12} cy={12} r={r} fill="none" stroke="var(--orange)" strokeWidth={stroke} strokeLinecap="round" strokeDasharray={`${c * 0.28} ${c * 0.72}`} />
        </svg>
      </span>
    );
  }
  if (state === "unread")
    return (
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-green text-white" aria-label={STATE_LABEL[state]}>
        <Icon name="check" size={13} stroke={3.2} />
      </span>
    );
  if (state === "needs_you")
    return (
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-red text-[12px] font-bold text-white" aria-label={STATE_LABEL[state]}>
        !
      </span>
    );
  return (
    <span className="flex size-6 shrink-0 items-center justify-center">
      <StatusDot state={state} />
    </span>
  );
}
