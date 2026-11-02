// Beautiful UI's Sidebar Nav: workspace row, quick search, the accent action, sections, one gliding highlight.
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { agentGrouping, AGENT_GROUPINGS, buildAgentGroups, isQuiet, type AgentGroup } from "../lib/agent-groups";
import { addSection, arrangeSessions, assignSection, GROUP_LABEL, isDefaultView, providerLabel, removeSection, resetView, setView, SHOW_LABEL, SORT_LABEL, toggleProvider, togglePin, totalUnread, updateAgentPrefs, useAgentList, useAgentPrefs, viewOf, type AgentPrefs, type AgentSession, type GroupBy, type ShowMode, type SortBy } from "../lib/agents";
import { api, errorText, setToken } from "../lib/api";
import { ago, baseName, sourceSubtitle, sessionTitle, stateOf, tabLabel } from "../lib/format";
import { href, navigate, type Route } from "../lib/router";
import { useAgentSessions, useCounts, useSpaces } from "../lib/sessions";
import { reset, refresh, refreshTodos, useStore } from "../lib/store";
import { buildTodoAgentGroups, TODO_STATUS_INFO, unlinkAgentFromTodo, type TodoStatus } from "../lib/todos";
import type { Machine, Session } from "../lib/types";
import { Collapse, cx, EASE, Kbd, Menu, StatusDot, toast, useMenu, type MenuItem } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { setTheme, useTheme, type Theme } from "./theme";
import { AgentAvatar } from "./agents/AgentAvatar";
import { LinkTodoDialog } from "./LinkTodoDialog";

function useFolded(key: string, initial = false): [boolean, () => void] {
  const [v, setV] = useState(() => {
    const s = localStorage.getItem(`fold:${key}`);
    return s === null ? initial : s === "1";
  });
  return [
    v,
    () =>
      setV((x) => {
        localStorage.setItem(`fold:${key}`, x ? "0" : "1");
        return !x;
      }),
  ];
}

export function Sidebar({ route, onSearch, onNewAgent, onNavigate }: { route: Route; onSearch: () => void; onNewAgent: () => void; onNavigate?: () => void }) {
  const conn = useStore((s) => s.conn);
  const machines = useStore((s) => s.machines);
  const assistant = useStore((s) => s.assistant);
  const { spaces, loose, recent } = useSpaces();
  const all = useAgentSessions();
  const mutes = useStore((s) => s.mutes);
  const todos = useStore((s) => s.todos);
  const todosSupported = useStore((s) => s.todosSupported);
  const uncompletedTodosCount = useMemo(() => todos.filter((t) => !t.done_at).length, [todos]);
  const launches = useStore((s) => Object.values(s.launches).filter((launch) => launch.phase !== "ready"));
  const [grouping, setGrouping] = useState(() => agentGrouping(localStorage.getItem("jarvis.agentGrouping")));
  const groupingMenu = useMenu();
  const availableGroupings = useMemo(() => {
    if (todosSupported === false) return AGENT_GROUPINGS.filter((g) => g.key !== "todo");
    return AGENT_GROUPINGS;
  }, [todosSupported]);
  const groups = useMemo(() => {
    if (grouping === "space") return [];
    if (grouping === "todo") return buildTodoAgentGroups(all, machines, todos, mutes);
    return buildAgentGroups(all, machines, grouping, mutes);
  }, [all, machines, grouping, mutes, todos]);
  const groupingLabel = (availableGroupings.find((g) => g.key === grouping) ?? availableGroupings[0]).label;
  const counts = useCounts();
  const nav = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const [hovered, setHovered] = useState<string | null>(null);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const [showAllRecent, setShowAllRecent] = useState(false);
  const [recentFolded, toggleRecent] = useFolded("recent", true);
  const [jarvisFolded, toggleJarvis] = useFolded("jarvis");
  const [idleFolded, toggleIdle] = useFolded("idle-spaces", true);
  const [sessionContextMenu, setSessionContextMenu] = useState<{ s: Session; el: HTMLElement } | null>(null);
  const [linkTargetSession, setLinkTargetSession] = useState<Session | undefined>();

  const activeKey =
    route.name === "session" ? `s:${route.mid}/${route.sid}` : route.name === "jarvis" ? (route.sid ? `j:${route.sid}` : "j:new") : route.name === "todos" ? "todos" : route.name === "machines" ? "machines" : route.name === "settings" ? "settings" : route.name === "agents" ? (route.id ? `a:${route.id}` : "agents") : "home";
  // a space with nothing to look at (every agent seen and waiting) goes to「空闲的空间」, like the app; the open
  // conversation's space stays where it is so the row under the pointer doesn't move away
  const busy = (sp: (typeof spaces)[number]) => sp.sessions.some((s) => !isQuiet(s) || activeKey === `s:${s.machine_id}/${s.session_id}`);
  const active = spaces.filter(busy);
  const idle = spaces.filter((sp) => !busy(sp));

  useLayoutEffect(() => {
    const el = rows.current.get(hovered ?? activeKey);
    let next: { top: number; height: number } | null = null;
    if (el && nav.current) {
      const n = nav.current.getBoundingClientRect(),
        r = el.getBoundingClientRect();
      if (r.height) next = { top: r.top - n.top + nav.current.scrollTop, height: r.height };
    }
    // runs after every render (rows fold, lists change): only a real move updates
    setBox((b) => (b?.top === next?.top && b?.height === next?.height ? b : next));
  });

  const reg = (k: string) => (el: HTMLElement | null) => {
    if (el) rows.current.set(k, el);
    else rows.current.delete(k);
  };

  const go = (to: Route) => (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
    onNavigate?.();
  };

  const online = machines.filter((m) => m.online).length;
  const convs = (assistant?.conversations ?? [])
    .slice()
    .sort((a, b) => (a.session_id === assistant?.main ? -1 : b.session_id === assistant?.main ? 1 : (b.updated_at ?? "").localeCompare(a.updated_at ?? "")))
    .slice(0, 6);

  return (
    <aside className="flex h-full w-full flex-col bg-canvas">
      <div className="px-2.5 pt-2.5">
        <AccountRow conn={conn} online={online} total={machines.length} />
        <button type="button" onClick={onSearch} className="mb-1 flex h-8 w-full items-center gap-2 rounded-control bg-inset px-2.5 text-left shadow-hairline transition-colors hover:bg-hover">
          <Icon name="search" size={12} stroke={2} className="text-ink-3" />
          <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-3">搜索会话</span>
          <span className="hidden sm:inline-flex"><Kbd>⌘K</Kbd></span>
        </button>
        <button
          type="button"
          onClick={onNewAgent}
          className="mb-1 flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-[13px] font-medium text-accent transition-[background-color,transform] duration-100 hover:bg-accent-tint active:scale-[0.98]"
        >
          <span className="min-w-0 flex-1 truncate text-left">新建 Agent</span>
          <span className="flex size-4 shrink-0 items-center justify-center rounded-full bg-accent text-white">
            <Icon name="plus" size={9} stroke={3} />
          </span>
        </button>
      </div>

      <div ref={nav} onMouseLeave={() => setHovered(null)} className="relative min-h-0 flex-1 overflow-y-auto px-2.5 pb-3">
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-2.5 rounded-[7px] bg-hover"
          style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 220ms ${EASE}, height 220ms ${EASE}, opacity 150ms ease` }}
        />
        <div className="flex flex-col gap-px pt-1">
          <NavRow k="home" reg={reg} hover={setHovered} active={activeKey === "home"} to={{ name: "home" }} go={go} icon="home" label="总览" badge={counts.needs || undefined} badgeTone="red" />
          <NavRow k="j:new" reg={reg} hover={setHovered} active={activeKey === "j:new"} to={{ name: "jarvis" }} go={go} icon="spark" label="Jarvis" />
          {todosSupported !== false && (
            <NavRow k="todos" reg={reg} hover={setHovered} active={activeKey === "todos"} to={{ name: "todos" }} go={go} icon="todo" label="待办" badge={uncompletedTodosCount || undefined} />
          )}
          <NavRow k="machines" reg={reg} hover={setHovered} active={activeKey === "machines"} to={{ name: "machines" }} go={go} icon="machine" label="机器" hint={machines.length ? `${online}/${machines.length}` : undefined} />
          <NavRow k="settings" reg={reg} hover={setHovered} active={activeKey === "settings"} to={{ name: "settings" }} go={go} icon="gear" label="设置" />
        </div>

        <PersonalAgents reg={reg} hover={setHovered} activeKey={activeKey} go={go} />

        {convs.length > 0 && (
          <Section title="Jarvis 对话" folded={jarvisFolded} onToggle={toggleJarvis}>
            {convs.map((c) => (
              <SessionRow key={c.session_id} s={c} k={`j:${c.session_id}`} reg={reg} hover={setHovered} active={activeKey === `j:${c.session_id}`} to={{ name: "jarvis", sid: c.session_id }} go={go} label={c.session_id === assistant?.main ? "主对话" : undefined} showState={false} />
            ))}
          </Section>
        )}

        <div className="mt-3 flex items-center justify-between gap-2 px-2">
          <span className="text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">Agents</span>
          <button type="button" aria-label={`Agent 分组：${groupingLabel}`} aria-haspopup="menu" aria-expanded={Boolean(groupingMenu.anchor)} onClick={groupingMenu.open} className="relative z-10 flex items-center gap-1 whitespace-nowrap rounded-control px-2 py-1 text-[11px] text-ink-3 hover:bg-hover hover:text-ink-2">
            {groupingLabel}
            <Icon name="chevron" size={10} stroke={2} />
          </button>
        </div>
        {groupingMenu.anchor && (
          <Menu anchor={groupingMenu.anchor} onClose={groupingMenu.close} align="right" header="Agent 分组" items={availableGroupings.map((g) => ({
            key: g.key, label: g.label, icon: g.key === "space" ? "folder" : g.key === "machine" ? "machine" : g.key === "todo" ? "todo" : "layers", checked: grouping === g.key,
            onSelect: () => { localStorage.setItem("jarvis.agentGrouping", g.key); setGrouping(g.key); setHovered(null); },
          }))} />
        )}

        {launches.length > 0 && <Section title="正在启动">
          {launches.map((launch) => <NavRow key={launch.id} k={`s:${launch.machineId}/${launch.id}`} reg={reg} hover={setHovered} active={activeKey === `s:${launch.machineId}/${launch.id}`} to={{ name: "session", mid: launch.machineId, sid: launch.id }} go={go} icon={launch.phase === "failed" ? "retry" : "agent"} label={launch.text || baseName(launch.cwd) || "新 Agent"} hint={launch.phase === "failed" ? "启动失败" : "后台启动中"} />)}
        </Section>}

        {grouping === "space" ? <>
        {active.map((sp) => (
          <SpaceSection key={sp.key} sp={sp} machineName={machines.length > 1 ? machines.find((m) => m.id === sp.machineId)?.name : undefined} reg={reg} hover={setHovered} activeKey={activeKey} go={go} onSessionMenu={(s, el) => setSessionContextMenu({ s, el })} />
        ))}

        {idle.length > 0 && (
          <Section title={`空闲的空间 ${idle.length}`} folded={idleFolded} onToggle={toggleIdle}>
            {idle.flatMap((sp) =>
              sp.sessions.map((s) => (
                <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={sp.sessions.length > 1 ? `${sp.name} › ${tabLabel(s)}` : sp.name} onContextMenu={(el) => setSessionContextMenu({ s, el })} />
              )),
            )}
          </Section>
        )}

        {loose.length > 0 && (
          <Section title="其他 Agent">
            {loose.map((s) => (
              <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} onContextMenu={(el) => setSessionContextMenu({ s, el })} />
            ))}
          </Section>
        )}

        </> : groups.map((group) => (
          <AgentSection key={group.key} group={group} machines={machines} byMachine={grouping === "machine"} byTodo={grouping === "todo"} reg={reg} hover={setHovered} activeKey={activeKey} go={go} onSessionMenu={(s, el) => setSessionContextMenu({ s, el })} />
        ))}

        {recent.length > 0 && (
          <Section title="最近（未运行）" folded={recentFolded} onToggle={toggleRecent}>
            {(showAllRecent ? recent : recent.slice(0, 8)).map((s) => (
              <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} onContextMenu={(el) => setSessionContextMenu({ s, el })} />
            ))}
            {recent.length > 8 && (
              <button type="button" onClick={() => setShowAllRecent((x) => !x)} className="relative z-10 w-full rounded-[7px] px-2 py-1 text-left text-[12px] text-ink-3 hover:text-ink-2">
                {showAllRecent ? "收起" : `再显示 ${recent.length - 8} 个`}
              </button>
            )}
          </Section>
        )}
      </div>

      {sessionContextMenu && (
        <Menu
          anchor={sessionContextMenu.el}
          onClose={() => setSessionContextMenu(null)}
          align="left"
          items={[
            ...(sessionContextMenu.s.todo
              ? [
                  {
                    key: "unlink-todo",
                    label: "解除关联",
                    icon: "x" as const,
                    onSelect: async () => {
                      try {
                        await unlinkAgentFromTodo(sessionContextMenu.s.todo!.id, sessionContextMenu.s.machine_id, sessionContextMenu.s.session_id);
                        await refreshTodos();
                        await refresh();
                        toast("已解除待办关联", "ok");
                      } catch (e) {
                        toast(`解除关联失败：${errorText(e)}`, "error");
                      }
                    },
                  },
                  {
                    key: "relink-todo",
                    label: "关联到待办…",
                    icon: "todo" as const,
                    onSelect: () => setLinkTargetSession(sessionContextMenu.s),
                  },
                ]
              : [
                  {
                    key: "link-todo",
                    label: "关联到待办…",
                    icon: "todo" as const,
                    onSelect: () => setLinkTargetSession(sessionContextMenu.s),
                  },
                ]),
          ]}
        />
      )}

      <LinkTodoDialog
        open={Boolean(linkTargetSession)}
        session={linkTargetSession}
        onClose={() => setLinkTargetSession(undefined)}
      />
    </aside>
  );
}

function AccountRow({ conn, online, total }: { conn: string; online: number; total: number }) {
  const menu = useMenu();
  const theme = useTheme();
  const themes: { k: Theme; label: string; icon: IconName }[] = [
    { k: "system", label: "跟随系统", icon: "monitor" },
    { k: "light", label: "浅色", icon: "sun" },
    { k: "dark", label: "深色", icon: "moon" },
  ];
  const logout = async () => {
    const id = localStorage.getItem("jarvis.tokenId");
    if (id) await api(`/auth/tokens/${encodeURIComponent(id)}`, { method: "DELETE" }).catch(() => {});
    localStorage.removeItem("jarvis.tokenId");
    setToken("");
    reset();
    location.href = "/";
  };
  return (
    <>
      <button type="button" onClick={menu.open} className="mb-2 flex w-full items-center gap-2.5 rounded-control p-1.5 text-left transition-[background-color,transform] duration-100 hover:bg-hover active:scale-[0.98]">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-[8px] bg-ink text-surface">
          <Icon name="spark" size={15} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] leading-tight font-medium text-ink">Jarvis</span>
          <span className="flex items-center gap-1.5 truncate text-[11px] leading-tight text-ink-3">
            <span className={cx("size-1.5 rounded-full", conn === "online" ? "bg-green" : conn === "connecting" ? "bg-orange" : "bg-red")} style={conn === "connecting" ? { animation: "breathe 1.4s ease-in-out infinite" } : undefined} />
            {conn === "online" ? `${online} / ${total} 台电脑在线` : conn === "connecting" ? "连接中…" : "连不上 hub"}
          </span>
        </span>
        <Icon name="chevron" size={12} stroke={2} className="text-ink-3" />
      </button>
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          width={220}
          header="外观"
          items={[
            ...themes.map((t) => ({ key: t.k, label: t.label, icon: t.icon, checked: theme === t.k, onSelect: () => setTheme(t.k) })),
            { key: "settings", label: "设置", icon: "gear", onSelect: () => navigate({ name: "settings" }) },
            { key: "logout", label: "断开这个浏览器", icon: "logout", danger: true, onSelect: () => void logout() },
          ]}
        />
      )}
    </>
  );
}

function Section({ title, children, folded, onToggle, right }: { title: React.ReactNode; children: React.ReactNode; folded?: boolean; onToggle?: () => void; right?: React.ReactNode }) {
  return (
    <div className="mt-3">
      <div className="group/sec flex items-center gap-1 px-2 pb-1">
        <button type="button" aria-expanded={onToggle ? !folded : undefined} disabled={!onToggle} onClick={onToggle} className="relative z-10 flex min-w-0 flex-1 items-center gap-1 whitespace-nowrap text-left text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase enabled:hover:text-ink-2">
          <span className="min-w-0 truncate">{title}</span>
          {onToggle && <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 opacity-0 transition-[opacity,transform] duration-200 group-hover/sec:opacity-100" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />}
        </button>
        {right}
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px" inert={folded || undefined}>{children}</div>
      </Collapse>
    </div>
  );
}

function AgentSection({ group, machines, byMachine, byTodo, reg, hover, activeKey, go, onSessionMenu }: { group: AgentGroup; machines: Machine[]; byMachine: boolean; byTodo?: boolean; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onSessionMenu?: (s: Session, el: HTMLElement) => void }) {
  const [folded, toggle] = useFolded(group.key, group.folded);
  const [showIdle, setShowIdle] = useState(false);
  const idle = byMachine ? group.sessions.filter((s) => isQuiet(s) && activeKey !== `s:${s.machine_id}/${s.session_id}`) : [];
  const idleKeys = new Set(idle.map((s) => `${s.machine_id}/${s.session_id}`));
  const visible = group.sessions.filter((s) => !idleKeys.has(`${s.machine_id}/${s.session_id}`));
  const row = (s: Session) => {
    const key = `s:${s.machine_id}/${s.session_id}`;
    const context = [s.space_name || baseName(s.cwd), !byMachine && machines.length > 1 ? machines.find((m) => m.id === s.machine_id)?.name : ""].filter(Boolean).join(" · ");
    return <SessionRow key={key} s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} showTodoTag={!byTodo} onContextMenu={(el) => onSessionMenu?.(s, el)} />;
  };

  const statusInfo = group.todoStatus ? TODO_STATUS_INFO[group.todoStatus as TodoStatus] : undefined;
  const statusTone = statusInfo?.tone === "red" ? "text-red" : statusInfo?.tone === "green" ? "text-green" : statusInfo?.tone === "amber" ? "text-orange" : "text-ink-3";

  const titleNode = group.todoId ? (
    <span className="flex min-w-0 items-center gap-1.5">
      <Icon name="todo" size={12} stroke={2} className="shrink-0 text-ink-3" />
      <span
        onClick={(e) => {
          e.stopPropagation();
          go({ name: "todos", id: group.todoId })(e);
        }}
        className="truncate normal-case tracking-normal hover:underline hover:text-ink cursor-pointer"
      >
        {group.name}
      </span>
    </span>
  ) : (
    <span className="flex min-w-0 items-center gap-1.5">
      {group.state && <span aria-hidden><StatusDot state={group.state} size={6} /></span>}
      <span className="truncate">{group.name}</span>
    </span>
  );

  const rightNode = group.todoStatus ? (
    <span className={cx("text-[11px] font-semibold tracking-normal normal-case", statusTone)}>
      {statusInfo?.label}
    </span>
  ) : (
    <span className="text-[10.5px] text-ink-3 tabular-nums">{group.sessions.length}</span>
  );

  return (
    <Section title={titleNode} folded={folded} onToggle={toggle} right={rightNode}>
      {visible.map(row)}
      {idle.length > 0 && <>
        <button type="button" aria-expanded={showIdle} onClick={() => setShowIdle((v) => !v)} className="relative z-10 flex w-full items-center gap-1.5 whitespace-nowrap rounded-control px-2 py-1.5 text-left text-[11px] text-ink-3 hover:bg-hover hover:text-ink-2">
          <Icon name="chevron" size={10} style={{ transform: showIdle ? "none" : "rotate(-90deg)" }} />
          <span>空闲 {idle.length}</span>
        </button>
        <Collapse open={showIdle}><div inert={!showIdle || undefined}>{idle.map(row)}</div></Collapse>
      </>}
    </Section>
  );
}

function SpaceSection({ sp, machineName, reg, hover, activeKey, go, onSessionMenu }: { sp: ReturnType<typeof useSpaces>["spaces"][number]; machineName?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onSessionMenu?: (s: Session, el: HTMLElement) => void }) {
  const allIdle = sp.sessions.every(isQuiet);
  const [folded, toggle] = useFolded(`space:${sp.key}`, allIdle && sp.sessions.length > 3);
  const states = sp.sessions.map(stateOf);
  const dots = (["needs_you", "unread", "working"] as const).filter((s) => states.includes(s));
  return (
    <Section
      folded={folded}
      onToggle={toggle}
      title={
        <span className="flex items-center gap-1.5">
          <span className="truncate normal-case tracking-normal text-[11.5px] text-ink-2">{sp.name}</span>
          {machineName && <span className="truncate normal-case tracking-normal text-[11px] font-normal text-ink-3">{machineName}</span>}
        </span>
      }
      right={
        folded && dots.length ? (
          <span className="flex items-center gap-1">
            {dots.map((d) => (
              <StatusDot key={d} state={d} size={6} />
            ))}
          </span>
        ) : (
          <span className="text-[10.5px] text-ink-3 tabular-nums">{sp.sessions.length}</span>
        )
      }
    >
      {sp.sessions.map((s) => (
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} tab showTodoTag onContextMenu={(el) => onSessionMenu?.(s, el)} />
      ))}
    </Section>
  );
}

type RegFn = (k: string) => (el: HTMLElement | null) => void;
type GoFn = (to: Route) => (e: React.MouseEvent) => void;

function NavRow({ k, reg, hover, active, to, go, icon, label, badge, badgeTone, hint }: { k: string; reg: RegFn; hover: (k: string | null) => void; active: boolean; to: Route; go: GoFn; icon: IconName; label: string; badge?: number; badgeTone?: "red"; hint?: string }) {
  return (
    <a
      ref={reg(k)}
      href={href(to)}
      onClick={go(to)}
      onMouseEnter={() => hover(k)}
      onFocus={() => hover(k)}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      className="relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 transition-transform duration-150 active:scale-[0.98]"
    >
      <span className={active ? "text-ink" : "text-ink-3"}>
        <Icon name={icon} size={14} stroke={1.8} />
      </span>
      <span className={cx("min-w-0 flex-1 truncate text-[13px]", active ? "font-medium text-ink" : "text-ink-2")}>{label}</span>
      {hint && <span className="text-[11px] text-ink-3 tabular-nums">{hint}</span>}
      {badge !== undefined && (
        <span className={cx("flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-[10.5px] font-semibold tabular-nums", badgeTone === "red" ? "bg-red-tint text-red" : "bg-accent-tint text-accent-ink")} style={{ animation: `pop-in 250ms ${EASE} both` }} key={badge}>
          {badge}
        </span>
      )}
    </a>
  );
}

function SessionRow({ s, k, reg, hover, active, to, go, tab, label, hint, showState = true, showTodoTag, onContextMenu }: { s: Session; k: string; reg: RegFn; hover: (k: string | null) => void; active: boolean; to: Route; go: GoFn; tab?: boolean; label?: string; hint?: string; showState?: boolean; showTodoTag?: boolean; onContextMenu?: (el: HTMLElement) => void }) {
  const st = stateOf(s);
  const t = [sourceSubtitle(s), hint ?? (tab ? tabLabel(s) : "")].filter(Boolean).join(" · ");
  return (
    <a
      ref={reg(k)}
      href={href(to)}
      onClick={go(to)}
      onContextMenu={(e) => {
        if (onContextMenu) {
          e.preventDefault();
          onContextMenu(e.currentTarget);
        }
      }}
      onMouseEnter={() => hover(k)}
      onFocus={() => hover(k)}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      title={`${sessionTitle(s)}${sourceSubtitle(s) ? `\n${sourceSubtitle(s)}` : ""}${s.todo ? `\n待办：${s.todo.title}` : ""}${s.last_text ? `\n${s.last_text}` : ""}`}
      className="group/row relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-[5px] transition-transform duration-150 active:scale-[0.98]"
    >
      <span className="flex size-3.5 shrink-0 items-center justify-center">{showState ? <StatusDot state={st} size={7} /> : <Icon name="chat" size={12} className="text-ink-3" />}</span>
      <span className={cx("min-w-0 flex-1 truncate text-[13px]", active ? "font-medium text-ink" : st === "offline" ? "text-ink-3" : "text-ink-2", st === "unread" && !active && "font-medium text-ink")}>
        {label ?? sessionTitle(s)}
      </span>
      {showTodoTag && s.todo && (
        <span
          className="flex shrink-0 items-center justify-center rounded bg-hover px-1 py-0.5 text-ink-3"
          title={`待办：${s.todo.title}`}
          aria-label={`待办：${s.todo.title}`}
        >
          <Icon name="todo" size={10} stroke={2} />
        </span>
      )}
      {t && <span className="max-w-[72px] shrink-0 truncate text-[11px] text-ink-3">{t}</span>}
      {!t && (st === "offline" || !showState) && <span className="shrink-0 text-[11px] text-ink-3">{ago(s.updated_at)}</span>}
      {(s.pending_interactions ?? 0) > 0 && <span className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-red px-1 text-[10px] font-semibold text-white tabular-nums">{s.pending_interactions}</span>}
    </a>
  );
}

// 个人 Agent: lark-connector's personal agents with a view menu (分组 / 排序 / 显示), a search field, pins and custom
// sections — the prefs live on the hub so iOS sees the same pins.
type MenuPage = "root" | "group" | "sort" | "show";
function agentViewItems(page: MenuPage, setPage: (p: MenuPage) => void, p: AgentPrefs, providers: string[]): MenuItem[] {
  const v = viewOf(p);
  const back: MenuItem = { key: "back", label: "‹ 返回", keepOpen: true, onSelect: () => setPage("root") };
  if (page === "group")
    return [back, ...(Object.keys(GROUP_LABEL) as GroupBy[]).map((g, i) => ({ key: g, label: GROUP_LABEL[g], divider: i === 0, checked: v.group === g, onSelect: () => updateAgentPrefs((x) => setView(x, { group: g })) }))];
  if (page === "sort")
    return [back, ...(Object.keys(SORT_LABEL) as SortBy[]).map((s, i) => ({ key: s, label: SORT_LABEL[s], divider: i === 0, checked: v.sort === s, onSelect: () => updateAgentPrefs((x) => setView(x, { sort: s })) }))];
  if (page === "show")
    return [
      back,
      ...(Object.keys(SHOW_LABEL) as ShowMode[]).map((s, i) => ({ key: s, label: SHOW_LABEL[s], divider: i === 0, checked: v.show === s, keepOpen: true, onSelect: () => updateAgentPrefs((x) => setView(x, { show: s })) })),
      ...providers.map((pr, i) => ({ key: `p:${pr}`, label: providerLabel(pr), hint: i === 0 ? "来源" : undefined, divider: i === 0, checked: !v.providers.length || v.providers.includes(pr), keepOpen: true, onSelect: () => updateAgentPrefs((x) => (viewOf(x).providers.length ? toggleProvider(x, pr) : setView(x, { providers: providers.filter((y) => y !== pr) }))) })),
    ];
  const showHint = v.show !== "all" ? SHOW_LABEL[v.show] : v.providers.length ? `${v.providers.length} 个来源` : SHOW_LABEL.all;
  return [
    { key: "group", label: "分组方式", hint: GROUP_LABEL[v.group], submenu: true, onSelect: () => setPage("group") },
    { key: "sort", label: "排序方式", hint: SORT_LABEL[v.sort], submenu: true, onSelect: () => setPage("sort") },
    { key: "show", label: "显示", hint: showHint, submenu: true, onSelect: () => setPage("show") },
    {
      key: "section",
      label: "新建分组",
      icon: "plus",
      divider: true,
      onSelect: () => {
        const name = prompt("分组名称")?.trim();
        if (name) updateAgentPrefs((x) => addSection(x, name));
      },
    },
    { key: "reset", label: "重置为默认", divider: true, disabled: isDefaultView(p), onSelect: () => updateAgentPrefs(resetView) },
    { key: "all", label: "查看全部", onSelect: () => navigate({ name: "agents" }) },
  ];
}

// Row actions (context menu on web): pin, move to a custom section, remove a section.
function agentRowItems(s: AgentSession, p: AgentPrefs): MenuItem[] {
  const pinned = p.pins.includes(s.id);
  const cur = p.assign[s.id];
  return [
    { key: "pin", label: pinned ? "取消置顶" : "置顶", icon: "pin", onSelect: () => updateAgentPrefs((x) => togglePin(x, s.id)) },
    ...p.sections.map((sec, i) => ({ key: `sec:${sec.id}`, label: `移到「${sec.name}」`, divider: i === 0, checked: cur === sec.id, onSelect: () => updateAgentPrefs((x) => assignSection(x, s.id, cur === sec.id ? null : sec.id)) })),
    ...(cur ? [{ key: "unsec", label: "移出分组", onSelect: () => updateAgentPrefs((x) => assignSection(x, s.id, null)) }] : []),
  ];
}

function PersonalAgents({ reg, hover, activeKey, go }: { reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn }) {
  const { sessions, loaded, error } = useAgentList();
  const prefs = useAgentPrefs();
  const [folded, toggle] = useFolded("personal-agents");
  const [q, setQ] = useState("");
  const [searching, setSearching] = useState(false);
  const [all, setAll] = useState<Record<string, boolean>>({});
  const menu = useMenu();
  const [page, setPage] = useState<MenuPage>("root");
  const [rowMenu, setRowMenu] = useState<{ s: AgentSession; el: HTMLElement } | null>(null);
  if (loaded && !sessions.length && !error) return null;
  const groups = arrangeSessions(sessions, prefs, q);
  const unread = totalUnread(sessions);
  const providers = [...new Set(sessions.map((s) => s.provider))];
  const filtered = !isDefaultView(prefs) || !!q;
  const sectionOf = (key: string) => (key.startsWith("sec:") ? key.slice(4) : "");
  return (
    <Section
      title="个人 Agent"
      folded={folded}
      onToggle={toggle}
      right={
        <>
          {unread ? <span className="flex h-4 min-w-4 items-center justify-center rounded-full bg-accent-tint px-1 text-[10px] font-semibold text-accent-ink tabular-nums">{unread}</span> : <span className="text-[10.5px] text-ink-3 tabular-nums">{sessions.length || ""}</span>}
          <button type="button" title="搜索个人 Agent" aria-label="搜索个人 Agent" onClick={() => setSearching((x) => !x || !!q)} className={cx("relative z-10 flex size-5 items-center justify-center rounded-[5px] text-ink-3 hover:bg-hover hover:text-ink-2", (searching || q) && "text-ink-2")}>
            <Icon name="search" size={12} />
          </button>
          <button
            type="button"
            title="筛选与排序"
            aria-label="筛选与排序"
            aria-haspopup="menu"
            onClick={(e) => {
              setPage("root");
              menu.open(e);
            }}
            className={cx("relative z-10 flex size-5 items-center justify-center rounded-[5px] hover:bg-hover hover:text-ink-2", filtered ? "text-accent" : "text-ink-3", menu.anchor && "bg-hover")}
          >
            <Icon name="filter" size={12} />
          </button>
          {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} align="right" width={220} items={agentViewItems(page, setPage, prefs, providers)} />}
        </>
      }
    >
      {!loaded && <p className="px-2 py-1 text-[12px] text-ink-3">加载中…</p>}
      {error && <p className="px-2 py-1 text-[12px] text-red" title={error}>个人 Agent 加载失败</p>}
      {(searching || q) && (
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQ("");
              setSearching(false);
            }
          }}
          placeholder="搜索个人 Agent"
          className="relative z-10 mx-1 mb-1 h-7 rounded-control bg-inset px-2 text-[12px] text-ink shadow-hairline outline-none placeholder:text-ink-3"
        />
      )}
      {loaded && !error && !groups.length && <p className="px-2 py-1 text-[12px] text-ink-3">没有匹配的会话</p>}
      {groups.map((g) => {
        const more = !q && !all[g.key] && g.key !== "pinned" && g.sessions.length > 5;
        const sec = sectionOf(g.key);
        return (
          <div key={g.key} className="flex flex-col gap-px">
            {g.label && (groups.length > 1 || g.key === "pinned" || sec) && (
              <div className="group/g flex items-center gap-1.5 px-2 pt-1.5 pb-0.5 text-[11px] text-ink-3">
                {g.key === "pinned" && <Icon name="pin" size={10} />}
                <span className="min-w-0 flex-1 truncate">{g.label}</span>
                {g.unread > 0 && <span className="tabular-nums text-accent-ink">{g.unread}</span>}
                {sec && (
                  <button type="button" title="删除分组" aria-label={`删除分组 ${g.label}`} onClick={() => confirm(`删除分组「${g.label}」？会话会回到原来的位置。`) && updateAgentPrefs((x) => removeSection(x, sec))} className="relative z-10 opacity-0 group-hover/g:opacity-100 hover:text-ink-2">
                    <Icon name="x" size={10} />
                  </button>
                )}
              </div>
            )}
            {(more ? g.sessions.slice(0, 5) : g.sessions).map((s) => (
              <AgentRow key={s.id} s={s} reg={reg} hover={hover} active={activeKey === `a:${s.id}`} go={go} pinned={prefs.pins.includes(s.id)} onMenu={(el) => setRowMenu({ s, el })} />
            ))}
            {g.sessions.length > 5 && !q && g.key !== "pinned" && (
              <button type="button" onClick={() => setAll((x) => ({ ...x, [g.key]: !x[g.key] }))} className="relative z-10 w-full rounded-[7px] px-2 py-1 text-left text-[12px] text-ink-3 hover:text-ink-2">
                {more ? `再显示 ${g.sessions.length - 5} 个` : "收起"}
              </button>
            )}
          </div>
        );
      })}
      {rowMenu && <Menu anchor={rowMenu.el} onClose={() => setRowMenu(null)} align="right" width={200} items={agentRowItems(rowMenu.s, prefs)} />}
    </Section>
  );
}

function AgentRow({ s, reg, hover, active, go, pinned, onMenu }: { s: AgentSession; reg: RegFn; hover: (k: string | null) => void; active: boolean; go: GoFn; pinned: boolean; onMenu: (el: HTMLElement) => void }) {
  const k = `a:${s.id}`;
  const to: Route = { name: "agents", id: s.id };
  const unread = s.unreadCount > 0;
  return (
    <div className="group/row relative" onContextMenu={(e) => {
      e.preventDefault();
      onMenu(e.currentTarget);
    }}>
      <a
        ref={reg(k)}
        href={href(to)}
        onClick={go(to)}
        onMouseEnter={() => hover(k)}
        onFocus={() => hover(k)}
        onBlur={() => hover(null)}
        aria-current={active ? "page" : undefined}
        title={`${s.title || "未命名会话"} · ${providerLabel(s.provider)}`}
        className="relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-[5px] transition-transform duration-150 active:scale-[0.98]"
      >
        <AgentAvatar s={s} size={16} />
        <span className={cx("min-w-0 flex-1 truncate text-[13px]", active || unread ? "font-medium text-ink" : "text-ink-2")}>{s.title || "未命名会话"}</span>
        <span className="flex shrink-0 items-center group-hover/row:invisible">
          {unread ? (
            <span className="flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-white tabular-nums">{s.unreadCount}</span>
          ) : (
            s.rankTimeMs > 0 && <span className="text-[11px] text-ink-3">{ago(new Date(s.rankTimeMs).toISOString())}</span>
          )}
        </span>
      </a>
      <button
        type="button"
        title={pinned ? "取消置顶" : "置顶"}
        aria-label={pinned ? "取消置顶" : "置顶"}
        onClick={() => updateAgentPrefs((x) => togglePin(x, s.id))}
        className={cx("absolute top-1/2 right-1 z-20 flex size-5 -translate-y-1/2 items-center justify-center rounded-[5px] opacity-0 hover:bg-hover group-hover/row:opacity-100 focus-visible:opacity-100", pinned ? "text-accent" : "text-ink-3")}
      >
        <Icon name="pin" size={11} />
      </button>
    </div>
  );
}
