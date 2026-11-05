// 总览: what needs you, what is running, what just finished — Beautiful UI's Task Rows — and the computers.
import { useMemo, useState } from "react";
import { ago, sessionPreview, shortDuration, sourceSubtitle, sessionTitle, stateOf, STATE_LABEL, tabLabel } from "../lib/format";
import { homeOverview, paneRoute, type TabGroup } from "../lib/herdr-tree";
import { agentLabel } from "../lib/mentions";
import { hubLabel, machineWithHub } from "../lib/mesh";
import { href, onLinkClick, type Route } from "../lib/router";
import { useAgentSessions, useCounts } from "../lib/sessions";
import { useStore } from "../lib/store";
import type { AgentState, Interaction, Session } from "../lib/types";
import { Button, cx, EASE, IconButton, Loading, StatusDot, useNow } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { MarkSeenDialog } from "./AgentMenus";
import { OverviewCards } from "./extensions/ExtensionSlots";
import { t } from "../i18n";

const KIND: Record<Interaction["kind"], string> = { question: t("home.kind_question"), plan: t("home.kind_plan"), permission: t("home.kind_permission"), screen: t("home.kind_screen") };

function greeting() {
  const h = new Date().getHours();
  return h < 5 ? t("home.greeting_late") : h < 11 ? t("home.greeting_morning") : h < 13 ? t("home.greeting_noon") : h < 18 ? t("home.greeting_afternoon") : t("home.greeting_evening");
}

export function Home({ onNewAgent, onToggleSidebar }: { onNewAgent: () => void; onToggleSidebar: () => void }) {
  const sessions = useAgentSessions();
  const machines = useStore((s) => s.machines);
  const interactions = useStore((s) => s.interactions);
  const loaded = useStore((s) => s.loaded);
  const counts = useCounts();
  const now = useNow(5000, counts.working > 0);

  // the agents of one herdr tab are one row (#2320), filed under the most urgent of them; a card waiting counts as
  // something to do, and 「全部标为已读」 marks every unread agent, wherever its tab is filed
  const { groups, unread, markAllAt, quiet } = useMemo(() => homeOverview(sessions, interactions), [sessions, interactions]);
  const name = (mid: string) => machineWithHub(mid, machines);
  const [confirmSeen, setConfirmSeen] = useState<Session[] | null>(null);
  const markAll = <MarkAllSeen count={unread.length} onClick={() => setConfirmSeen(unread)} />;

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4 lg:border-transparent">
        <IconButton icon="sidebar" label={t("home.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex-1" />
        <Button tone="secondary" icon="plus" onClick={onNewAgent}>
          {t("home.new_agent")}
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[760px] flex-col gap-9 px-5 pt-6 pb-16 sm:px-8 sm:pt-10">
          <div style={{ animation: `fade-up 420ms ${EASE} both` }}>
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">{greeting()}</h1>
            <div className="mt-1 flex items-center gap-2">
              <p className="min-w-0 flex-1 text-[13.5px] text-ink-2">
                {!loaded ? t("home.connecting") : quiet ? t("home.all_quiet") : t("home.summary", { list: [counts.needs && t("home.summary_needs", { count: counts.needs }), counts.working && t("home.summary_working", { count: counts.working }), counts.unread && t("home.summary_unread", { count: counts.unread })].filter(Boolean).join(t("home.summary_separator")) })}
              </p>
              {markAllAt === "summary" && markAll}
            </div>
          </div>

          {/* the extensions' cards (docs/extensions.md「UI 插槽」 overview_card), under the agents' summary */}
          <OverviewCards />

          {!loaded && <Loading label={t("common.loading")} />}

          {interactions.length > 0 && (
            <Group title={t("home.needs_you")} count={interactions.length}>
              {interactions.map((ix, i) => {
                const s = sessions.find((x) => x.machine_id === ix.machine_id && x.session_id === ix.session_id);
                return (
                  <TaskRow
                    key={ix.id}
                    i={i}
                    state="needs_you"
                    to={{ name: "session", mid: ix.machine_id, sid: ix.session_id }}
                    title={ix.session_title || (s ? sessionTitle(s) : ix.cwd || t("home.session"))}
                    meta={[name(ix.machine_id), s?.space_name, s && tabLabel(s)].filter(Boolean).join(" › ")}
                    detail={ix.kind === "question" ? ix.questions?.[0]?.question : ix.kind === "permission" ? ix.summary || ix.tool_name : ix.kind === "screen" ? ix.dialog?.question : t("home.plan_ready")}
                    pill={<Pill tone="red">{KIND[ix.kind]}</Pill>}
                  />
                );
              })}
            </Group>
          )}

          {groups.needs.length > 0 && (
            <Group title={t("home.waiting_on_computer")} count={groups.needs.length}>
              {groups.needs.map((g, i) => (
                <GroupTask key={g.key} g={g} i={i} machine={name(g.lead.machine_id)} pill={<Pill tone="red">{t("home.state_needs_you")}</Pill>} />
              ))}
            </Group>
          )}

          {groups.working.length > 0 && (
            <Group title={t("home.working")} count={groups.working.length}>
              {groups.working.map((g, i) => (
                <GroupTask key={g.key} g={g} i={i} machine={name(g.lead.machine_id)} pill={<span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{g.lead.state_since ? shortDuration(now - Date.parse(g.lead.state_since)) : ""}</span>} />
              ))}
            </Group>
          )}

          {groups.unread.length > 0 && (
            <Group
              title={t("home.unread")}
              count={groups.unread.length}
              right={markAllAt === "section" && markAll}
            >
              {groups.unread.map((g, i) => (
                <GroupTask key={g.key} g={g} i={i} machine={name(g.lead.machine_id)} pill={<Pill tone="green">{ago(g.lead.updated_at) || t("home.done")}</Pill>} />
              ))}
            </Group>
          )}

          {loaded && quiet && interactions.length === 0 && (
            <div className="flex flex-col items-start gap-3 rounded-card bg-surface p-4 shadow-card">
              <span className="flex size-8 items-center justify-center rounded-full bg-green-tint text-green">
                <Icon name="check" size={15} stroke={2.6} />
              </span>
              <div>
                <p className="text-[13.5px] font-medium text-ink">{t("home.all_set")}</p>
                <p className="mt-0.5 text-[12.5px] text-ink-2">{t("home.all_set_hint")}</p>
              </div>
              <Button tone="primary" icon="plus" onClick={onNewAgent}>
                {t("home.new_agent")}
              </Button>
            </div>
          )}

          {machines.length > 0 && (
            <Group title={t("home.computers")} count={machines.length} right={<a href="/machines" onClick={(e) => onLinkClick(e, { name: "machines" })} className="text-[12px] text-ink-3 hover:text-ink-2">{t("home.monitor")}</a>}>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {machines.map((m) => {
                  const mine = m.online ? sessions.filter((s) => s.machine_id === m.id && stateOf(s) !== "offline") : [];
                  const hub = hubLabel(m, machines);
                  const detail = { name: "machine" as const, mid: m.id };
                  return (
                    <a key={m.id} href={href(detail)} onClick={(e) => onLinkClick(e, detail)} className="flex items-center gap-3 rounded-card bg-surface px-3 py-2.5 shadow-card transition-colors hover:bg-hover">
                      <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-[8px]", m.online ? "bg-field text-ink-2" : "bg-inset text-ink-3")}>
                        <Icon name="machine" size={15} />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="flex min-w-0 items-center gap-1.5">
                          <span className="truncate text-[13px] font-medium text-ink">{m.name}</span>
                          {hub && <span className="shrink-0 truncate text-[11.5px] text-ink-3">{hub}</span>}
                        </span>
                        <span className="block truncate text-[11.5px] text-ink-3">{m.online ? t("home.machine_agents", { count: mine.length }) + (m.system?.name ? ` · ${m.system.name}` : "") : m.error || t("home.offline")}</span>
                      </span>
                      <span className={cx("size-2 shrink-0 rounded-full", m.online ? "bg-green" : "bg-line-strong")} title={m.online ? t("home.online") : t("home.offline")} />
                    </a>
                  );
                })}
              </div>
            </Group>
          )}
        </div>
      </div>
      <MarkSeenDialog list={confirmSeen} onClose={() => setConfirmSeen(null)} />
    </div>
  );
}

// Marks every unread agent seen, after the dialog says how many (MarkSeenDialog).
function MarkAllSeen({ count, onClick }: { count: number; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} title={t("home.mark_all_seen_title", { count })} className="flex h-6 shrink-0 items-center gap-1 whitespace-nowrap rounded-control px-1.5 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink-2">
      <Icon name="check" size={12} stroke={2.4} />
      {t("home.mark_all_seen")}
    </button>
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
  return <TaskRow i={i} state={stateOf(s)} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} title={sessionTitle(s)} meta={[sourceSubtitle(s), machine, s.space_name, tabLabel(s)].filter(Boolean).join(" › ")} detail={s.source_kind ? [sourceSubtitle(s), sessionPreview(s)].filter(Boolean).join(" · ") : sessionPreview(s)} pill={pill} />;
}

// One tab's agents as one row (#2320): the 发起者's title (the first pane's when the panes are peers), the most urgent
// one's latest words and state, and how many there are; it opens the tab on that most urgent pane.
function GroupTask({ g, i, machine, pill }: { g: TabGroup; i: number; machine: string; pill: React.ReactNode }) {
  if (g.sessions.length === 1) return <SessionTask s={g.lead} i={i} machine={machine} pill={pill} />;
  const head = g.launcher ?? g.sessions[0]!;
  const s = g.lead;
  const preview = sessionPreview(s);
  return (
    <TaskRow
      i={i}
      state={g.state}
      to={paneRoute(s)}
      title={sessionTitle(head)}
      meta={[machine, s.space_name, tabLabel(s)].filter(Boolean).join(" › ")}
      detail={s === head ? preview : agentLabel(s) && preview ? t("home.agent_preview", { agent: agentLabel(s), preview }) : agentLabel(s) || preview}
      pill={
        <>
          <span className="flex shrink-0 items-center gap-1 text-[12px] whitespace-nowrap text-ink-3 tabular-nums" title={g.sessions.map((x) => `${agentLabel(x)} · ${sessionTitle(x)} · ${STATE_LABEL[stateOf(x)]}`).join("\n")}>
            <span aria-hidden className="flex items-center gap-0.5">
              {g.sessions.map((x) => <StatusDot key={x.session_id} state={stateOf(x)} size={6} />)}
            </span>
            <span className="max-sm:sr-only">{t("home.machine_agents", { count: g.sessions.length })}</span>
          </span>
          {pill}
        </>
      }
    />
  );
}

// One Task Rows capsule: status badge, title, the latest words, a pill.
function TaskRow({ i, state, to, title, meta, detail, pill }: { i: number; state: AgentState; to: Route; title: string; meta: string; detail?: string; pill: React.ReactNode }) {
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
