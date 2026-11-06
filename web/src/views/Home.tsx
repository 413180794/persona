// Overview: what needs you, what is running, what just finished — Beautiful UI's Task Rows — and the computers.
import { useMemo, useRef, useState } from "react";
import { ago, sessionCan, sessionPreview, shortDuration, sourceSubtitle, sessionTitle, stateOf, STATE_LABEL, tabLabel } from "../lib/format";
import { cardState } from "../lib/card-focus";
import { interactionAsk } from "../lib/inbox";
import { answerLines, type Resolution } from "../lib/answered";
import { quickAnswers, withHeld, type HeldRow, type QuickChoice } from "../lib/quick-answers";
import { optionDisplay, permissionDetail } from "../lib/tool-detail";
import { toolDisplayName } from "../lib/turns";
import { plainMarkdown } from "../lib/plain-markdown";
import { homeOverview, paneRoute, type TabGroup } from "../lib/herdr-tree";
import { agentLabel } from "../lib/mentions";
import { hubLabel, machineWithHub } from "../lib/mesh";
import { isPending, machineErrorText } from "../lib/add-computer";
import { machineDeployment } from "../lib/machine-deploy";
import { classifyConnection } from "../lib/node-health";
import { href, navigate, onLinkClick, type Route } from "../lib/router";
import { useScrollMemory } from "../lib/scroll-memory";
import { useAgentSessions, useCounts } from "../lib/sessions";
import { getSession, useStore } from "../lib/store";
import { BUSY_MOTION, spinStyle } from "../lib/busy-motion";
import type { AgentState, Interaction, Machine, Session } from "../lib/types";
import { Button, Collapse, cx, EASE, IconButton, Loading, useNow } from "../ui/atoms";
import { RecommendedTag } from "../ui/RecommendedTag";
import { StatusMark } from "../ui/StatusMark";
import { Icon } from "../ui/icons";
import { MarkSeenDialog } from "./AgentMenus";
import { answeredSessions } from "./assistant/answered-lookup";
import { resolve } from "./chat/Interactions";
import { OverviewCards } from "./extensions/ExtensionSlots";
import { t } from "../i18n";

const KIND: Record<Interaction["kind"], string> = { question: t("home.kind_question"), plan: t("home.kind_plan"), permission: t("home.kind_permission"), screen: t("home.kind_screen"), elicitation: t("home.kind_elicitation") };

function greeting() {
  const h = new Date().getHours();
  return h < 5 ? t("home.greeting_late") : h < 11 ? t("home.greeting_morning") : h < 13 ? t("home.greeting_noon") : h < 18 ? t("home.greeting_afternoon") : t("home.greeting_evening");
}

export function Home({ onNewAgent, onToggleSidebar }: { onNewAgent: () => void; onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines);
  const interactions = useStore((s) => s.interactions);
  const answered = useStore((s) => s.answered);
  // a card answered here moves its agent out of "Stuck" at once, not at the hub's next push (#4286)
  const agentSessions = useAgentSessions();
  const sessions = useMemo(() => answeredSessions(agentSessions, interactions, answered), [agentSessions, interactions, answered]);
  const loaded = useStore((s) => s.loaded);
  const noMachines = loaded && machines.length === 0;
  const counts = useCounts();
  const now = useNow(5000, counts.working > 0);

  // the agents of one herdr tab are one row (#2320), filed under the most urgent of them; a card waiting counts as
  // something to do, and "Mark all as seen" marks every unread agent, wherever its tab is filed
  const { groups, unread, markAllAt, quiet } = useMemo(() => homeOverview(sessions, interactions), [sessions, interactions]);
  const name = (mid: string) => machineWithHub(mid, machines);
  const [confirmSeen, setConfirmSeen] = useState<Session[] | null>(null);
  const markAll = <MarkAllSeen count={unread.length} onClick={() => setConfirmSeen(unread)} />;

  // A card answered on its row (#4090) stays in its slot while it is sent and, answered, says so for a moment before
  // it folds away: the next row does not slide under the pointer and take a second click meant for this one.
  const [held, setHeld] = useState<HeldRow[]>([]);
  const answer = async (ix: Interaction, index: number, before: string | undefined, c: QuickChoice) => {
    const phase = (p: HeldRow["phase"]) => setHeld((h) => h.map((x) => (x.ix.id === ix.id ? { ...x, phase: p } : x)));
    setHeld((h) => [...h.filter((x) => x.ix.id !== ix.id), { ix, index, before, phase: "sending", pick: c.key, body: c.body }]);
    if (!(await resolve(ix, c.body))) return setHeld((h) => h.filter((x) => x.ix.id !== ix.id)); // it said why (toast)
    phase("done");
    setTimeout(() => phase("leaving"), ANSWERED_HOLD_MS);
    setTimeout(() => setHeld((h) => h.filter((x) => x.ix.id !== ix.id)), ANSWERED_HOLD_MS + 400);
  };

  // Back from a card opened here lands where the reader was, not at the top (#5133)
  const scroller = useRef<HTMLDivElement>(null);
  useScrollMemory(scroller, "/overview");

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4 lg:border-transparent">
        <IconButton icon="sidebar" label={t("home.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex-1" />
        {!noMachines && (
          <Button tone="secondary" icon="plus" onClick={onNewAgent}>
            {t("home.new_agent")}
          </Button>
        )}
      </header>
      <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[760px] flex-col gap-9 px-5 pt-6 pb-16 sm:px-8 sm:pt-10">
          <div style={{ animation: `fade-up 420ms ${EASE} both` }}>
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">{greeting()}</h1>
            <div className="mt-1 flex items-center gap-2">
              <p className="min-w-0 flex-1 text-[13.5px] text-ink-2">
                {!loaded ? t("home.connecting") : quiet ? noMachines ? t("home.no_machines_summary") : t("home.all_quiet") : t("home.summary", { list: [counts.needs && t("home.summary_needs", { count: counts.needs }), counts.working && t("home.summary_working", { count: counts.working }), counts.unread && t("home.summary_unread", { count: counts.unread })].filter(Boolean).join(t("home.summary_separator")) })}
              </p>
              {loaded && markAllAt === "summary" && markAll}
            </div>
          </div>

          {/* the extensions' cards (docs/extensions.md"UI slots" overview_card), under the agents' summary */}
          <OverviewCards />

          {/* Before the first snapshot the socket streams the machines' agents and cards in one by one, each moving the
              groups already painted, and "Loading" then went from over them: until it is in only "Loading", then all at once. */}
          {!loaded && <Loading label={t("common.loading")} />}

          {/* the headline counts agents, this counts cards: say so when one agent has several cards waiting */}
          {loaded && (interactions.length > 0 || held.length > 0) && (
            <Group title={t("home.needs_you")} count={interactions.length} hint={new Set(interactions.map((ix) => `${ix.machine_id}/${ix.session_id}`)).size < interactions.length ? t("home.needs_you_hint") : undefined}>
              {withHeld(interactions, held).map(({ ix, held: h }, i, rows) => {
                const s = sessions.find((x) => x.machine_id === ix.machine_id && x.session_id === ix.session_id);
                const title = ix.session_title || (s ? sessionTitle(s) : ix.cwd || t("home.session"));
                // what the row answers in one tap, as iOS's "Needs you" cards; the rest (plans, forms, typing) opens the chat
                const quick = sessionCan(s ?? getSession(ix.machine_id, ix.session_id), "approve") ? quickAnswers(ix) : null;
                // allowed in one tap, a permission says the tool and what it runs or touches (iOS: "Bash: swift test"),
                // not the agent's description of it
                const tool = quick && ix.kind === "permission" ? toolDisplayName(ix.tool_name) : "";
                // and a terminal dialog what it is about (the command under its question), as iOS's card does. Both
                // as they are: read as Markdown, `ls **/*.ts` lost its stars and `a <in >out` its redirections
                const about = quick && ix.kind === "screen" ? [ix.dialog?.question, ix.dialog?.detail].filter(Boolean).join(" · ") : "";
                const raw = (tool && permissionDetail(ix.tool_name, ix.tool_input)?.fields[0]?.value) || about;
                const ask = (ix.kind === "plan" ? t("home.plan_ready") : raw || plainMarkdown(interactionAsk(ix))).replace(/\s+/g, " ").trim();
                const done = h !== undefined && h.phase !== "sending";
                // every row in a Collapse, so an answered one folds away where it is; answered, it keeps its words
                // and its height ("Answered" in the buttons' place) until then
                return (
                  // the room around the row (-m-2 / p-2) is its shadow's: the collapse clips what is outside it
                  <Collapse key={ix.id} open={h?.phase !== "leaving"} className="-m-2">
                    <div className="p-2">
                      <TaskRow
                        i={i}
                        state="needs_you"
                        done={done}
                        to={{ name: "session", mid: ix.machine_id, sid: ix.session_id }}
                        title={title}
                        meta={[name(ix.machine_id), s?.space_name, s && tabLabel(s)].filter(Boolean).join(" › ")}
                        detail={(tool && ask && ask !== tool ? t("home.permission_detail", { tool, what: ask }) : ask) || undefined}
                        pill={!done && <Pill tone="red">{KIND[ix.kind]}</Pill>}
                        entry={cardState(ix.id)}
                        actions={done ? <AnsweredLine ix={ix} body={h.body} /> : quick && <QuickAnswers choices={quick} sending={h?.pick} onPick={(c) => void answer(ix, i, rows[i + 1]?.ix.id, c)} />}
                      />
                    </div>
                  </Collapse>
                );
              })}
            </Group>
          )}

          {loaded && groups.needs.length > 0 && (
            <Group title={t("home.waiting_on_computer")} count={groups.needs.length} hint={t("home.waiting_on_computer_hint")}>
              {groups.needs.map((g, i) => (
                <GroupTask key={g.key} g={g} i={i} machine={name(g.lead.machine_id)} pill={<Pill tone="red">{t("home.state_needs_you")}</Pill>} />
              ))}
            </Group>
          )}

          {loaded && groups.working.length > 0 && (
            <Group title={t("home.working")} count={groups.working.length}>
              {groups.working.map((g, i) => (
                <GroupTask key={g.key} g={g} i={i} machine={name(g.lead.machine_id)} pill={<span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{g.lead.state_since ? shortDuration(now - Date.parse(g.lead.state_since)) : ""}</span>} />
              ))}
            </Group>
          )}

          {loaded && groups.unread.length > 0 && (
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

          {loaded && quiet && interactions.length === 0 && held.length === 0 && (
            <div className="flex flex-col items-start gap-3 rounded-card bg-surface p-4 shadow-card">
              <span className={cx("flex size-8 items-center justify-center rounded-full", noMachines ? "bg-field text-ink-2" : "bg-green-tint text-green")}>
                <Icon name={noMachines ? "machine" : "check"} size={15} stroke={noMachines ? undefined : 2.6} />
              </span>
              <div>
                <p className="text-[13.5px] font-medium text-ink">{noMachines ? t("home.no_machines") : t("home.all_set")}</p>
                <p className="mt-0.5 text-[12.5px] text-ink-2">{noMachines ? t("home.no_machines_hint") : t("home.all_set_hint")}</p>
              </div>
              <Button tone="primary" icon={noMachines ? "machine" : "plus"} onClick={noMachines ? () => navigate({ name: "machines" }) : onNewAgent}>
                {noMachines ? t("home.open_machines") : t("home.new_agent")}
              </Button>
            </div>
          )}

          {loaded && machines.length > 0 && (
            <Group title={t("home.computers")} count={machines.length} right={<a href={href({ name: "machines" })} onClick={(e) => onLinkClick(e, { name: "machines" })} className="text-[12px] text-ink-3 hover:text-ink-2">{t("home.monitor")}</a>}>
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
                        <span className="block truncate text-[11.5px] text-ink-3" title={!m.online && m.error && offlineLine(m) !== m.error ? m.error : undefined} data-machine-line>{m.online ? t("home.machine_agents", { count: mine.length }) + (m.system?.name ? ` · ${m.system.name}` : "") : offlineLine(m)}</span>
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

function Group({ title, count, children, right, hint }: { title: string; count: number; children: React.ReactNode; right?: React.ReactNode; hint?: string }) {
  return (
    <section>
      <div className={cx("flex items-baseline gap-2", hint ? "mb-1" : "mb-2.5")}>
        <h2 className="text-[13px] font-medium text-ink">{title}</h2>
        <span className="text-[12px] text-ink-3 tabular-nums">{count}</span>
        <span className="flex-1" />
        {right}
      </div>
      {/* what the section is and why its rows are here, for a name that does not say it alone */}
      {hint && <p className="mb-2.5 text-[12.5px] leading-[1.5] text-ink-2">{hint}</p>}
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

// One tab's agents as one row (#2320): the "Lead"'s title (the first pane's when the panes are peers), the most urgent
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
              {g.sessions.map((x) => <StatusMark key={x.session_id} state={stateOf(x)} size="sm" />)}
            </span>
            <span className="max-sm:sr-only">{t("home.machine_agents", { count: g.sessions.length })}</span>
          </span>
          {pill}
        </>
      }
    />
  );
}

const ANSWERED_HOLD_MS = 1600;

/** A row's one-tap answers (lib/quick-answers.ts), under its words; the one being sent spins, the others wait. */
function QuickAnswers({ choices, sending, onPick }: { choices: QuickChoice[]; sending?: string; onPick: (c: QuickChoice) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5 px-3 pb-2.5 pl-12">
      {choices.map((c) => {
        const shown = optionDisplay(c.label);
        return (
          <Button key={c.key} tone={c.primary ? "primary" : "secondary"} busy={sending === c.key} disabled={sending !== undefined} title={c.hint || shown.text} onClick={() => onPick(c)} className="max-w-full">
            <span className="min-w-0 truncate">{shown.text}</span>
            {shown.recommended && <RecommendedTag />}
          </Button>
        );
      })}
    </div>
  );
}

/** In an answered row the buttons' place: "Answered" and the answer, a button's height, for a moment before it folds. */
function AnsweredLine({ ix, body }: { ix: Interaction; body: Resolution }) {
  // the hub may say it was answered already, elsewhere: what took effect, as the chat's answered card shows it
  const done = useStore((s) => s.answered[ix.id]);
  const answer = answerLines(ix, done?.resolution ?? body).map((l) => optionDisplay(l.answer).text).join(t("lib-answered.separator"));
  return (
    <div className="px-3 pb-2.5 pl-12">
      <p role="status" className="flex h-8 min-w-0 items-center gap-1.5 text-[12.5px] font-medium text-green" style={{ animation: `fade-in 200ms ${EASE} both` }}>
        <Icon name="check" size={13} stroke={2.6} className="shrink-0" />
        <span className="truncate">{t("home.answered", { answer })}</span>
      </p>
    </div>
  );
}

// One Task Rows capsule: status badge, title, the latest words, a pill. With `actions` (a card's one-tap answers) they
// sit under it in the same capsule, outside the link, and its words take up to three lines: read before answering.
function TaskRow({ i, state, done, to, title, meta, detail, pill, entry, actions }: { i: number; state: AgentState; done?: boolean; to: Route; title: string; meta: string; detail?: string; pill: React.ReactNode; entry?: Record<string, unknown>; actions?: React.ReactNode }) {
  const enter = { animation: `fade-up 420ms ${EASE} ${Math.min(i, 8) * 50}ms both` };
  const link = (
    <a
      href={href(to)}
      onClick={(e) => onLinkClick(e, to, entry)}
      className={cx("group flex items-center gap-3 rounded-[16px] px-3 py-2.5 transition-[background-color,transform] duration-150", !actions && "bg-surface shadow-card hover:bg-inset active:scale-[0.995]")}
      style={actions ? undefined : enter}
    >
      {done ? (
        <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-green text-white" aria-label={t("chat-interactions.answered")}>
          <Icon name="check" size={13} stroke={3.2} />
        </span>
      ) : (
        <Badge state={state} />
      )}
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{title}</span>
          <span className="hidden shrink-0 truncate text-[11.5px] text-ink-3 sm:inline">{meta}</span>
        </span>
        {detail && <span className={cx("mt-0.5 text-[12.5px] text-ink-2", actions ? "line-clamp-3 [overflow-wrap:anywhere]" : "block truncate")} title={actions ? detail : undefined}>{detail}</span>}
      </span>
      {pill}
      <Icon name="chevronRight" size={14} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-150 group-hover:translate-x-0.5" />
    </a>
  );
  if (!actions) return link;
  return (
    // the capsule shades while the link part is hovered, not while a button is
    <div className="rounded-[16px] bg-surface shadow-card transition-colors duration-150 has-[>a:hover]:bg-inset" style={enter}>
      {link}
      {actions}
    </div>
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
        <svg width={size} height={size} className={`absolute inset-0 ${BUSY_MOTION}`} style={spinStyle(1100)}>
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
      <StatusMark state={state} />
    </span>
  );
}

/** An offline computer's line on the overview, read in the machines page's order (#4038): waiting to be confirmed, the
 * hub's error_code in our words, a planned restart, then the plain reason from the error text (#3633); else the raw
 * error or "Offline". */
export function offlineLine(m: Machine): string {
  if (isPending(m)) return t("machines.never_connected");
  const coded = machineErrorText(m.error_code);
  if (coded) return coded;
  if (machineDeployment(m).expectedDisconnect) return t("machines.waiting_node");
  return classifyConnection(m)?.title ?? (m.error || t("home.offline"));
}
