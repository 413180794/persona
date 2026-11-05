// Beautiful UI's Sidebar Nav: workspace row, quick search, sections, one gliding highlight. Persona's main conversation
// is the first entry (the app opens on 助手); a new conversation and a new agent are the「+」of their sections (#1239).
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { NARROW, useMedia } from "../lib/viewport";
import { AGENT_FILTERS, agentFilter, applyFilter, arrangeSpaces, filterCounts, isSpaceMuted, matchingKeys, shownFilters, unreadOf, type AgentFilter, type FilterCounts } from "../lib/agent-filter";
import { agentGrouping, AGENT_GROUPINGS, buildAgentGroups, buildSpaces, hostsOf, isQuiet, sessionHost, splitAgentSessions, type AgentGroup, type AgentMutes, type Space } from "../lib/agent-groups";
import { buildRepoMachineTree, type RepoMachine, type RepoTree, type RepoWorktree } from "../lib/agent-repos";
import { keyedRefs } from "../lib/keyed-refs";
import { buildMachineTree, paneRoute, resolveTab, tabRowKeys, type ChildStates, type HerdrMachine, type HerdrSpace, type HerdrTab } from "../lib/herdr-tree";
import { addSection, arrangeSessions, assignSection, GROUP_LABEL, isDefaultView, providerLabel, removeSection, resetView, setView, SHOW_LABEL, SORT_LABEL, toggleProvider, togglePin, totalUnread, updateAgentPrefs, useAgentList, useAgentPrefs, viewOf, type AgentPrefs, type AgentSession, type GroupBy, type ShowMode, type SortBy } from "../lib/agents";
import { api, setToken } from "../lib/api";
import { useInboxBadge } from "../lib/inbox";
import { useLarkHome } from "../lib/lark-chats";
import { machineWithHub } from "../lib/mesh";
import { agentLabel } from "../lib/mentions";
import { ago, baseName, sessionPreview, sourceSubtitle, sessionTitle, stateOf, tabLabel } from "../lib/format";
import { agentKey, isSpacePinned, pinnedSessions } from "../lib/pins";
import { NAV_PAGES, firstAndCurrent, otherConversations, extensionNavKey, sidebarActiveKey } from "../lib/persona-nav";
import { useExtensionSlot } from "../lib/extension-slots";
import { toneOf } from "../lib/extension-ui";
import { BADGE_TONE, navIcon, pageRoute } from "./extensions/ExtensionSlots";
import { href, navigate, useRoute, type Route } from "../lib/router";
import { useAgentSessions, useCounts } from "../lib/sessions";
import { terminalTarget } from "../lib/session-view";
import { refresh, reset, useLarkOn, useStore } from "../lib/store";
import { buildTodoAgentGroups, findTodoForSpace, openTodoFromTag, TODO_STATUS_INFO, type TodoStatus } from "../lib/todos";
import type { AgentState, Machine, Session } from "../lib/types";
import { Collapse, cx, EASE, Kbd, Menu, StatusDot, useMenu, type MenuItem } from "../ui/atoms";
import { AppMark } from "../ui/AppMark";
import { Icon, type IconName } from "../ui/icons";
import { DeleteSessionDialog, RenameDialog, RenameSpaceDialog } from "./chat/SessionDialogs";
import { AgentRowMenu, MarkSeenDialog, SpaceMenu } from "./AgentMenus";
import { NodeMenu } from "./NodeMenu";
import { NodePrompt } from "./NodePrompt";
import "../lib/node-palette";
import { machineNode, termPaneNode, termSpaceNode, termTabNode, type MenuNode } from "../lib/node-actions";
import { contextMenuProps } from "../ui/context-menu";
import type { MenuAt } from "../ui/menu-model";
import { useSidebarTerminals } from "../lib/sidebar-terminals";
import { rowDetail, useAgentDetails, useContextAsUsed } from "../lib/context-usage";
import { usePanes } from "../lib/terminal-panes";
import { buildTerminalTree, shellsOnly, type TermSpace } from "../lib/terminals";
import { setTheme, useTheme, type Theme } from "./theme";
import { AgentAvatar } from "./agents/AgentAvatar";
import { LinkTodoDialog, type LinkTargetSpace } from "./LinkTodoDialog";
import { hubMenuItems, useHubs } from "./Hubs";
import { PersonaAvatar } from "./assistant/Persona";
import { useFolded } from "../lib/use-folded";
import { onGrouping } from "../lib/sidebar-api";
import { jumpHint, useBindings } from "../lib/shortcuts";

/** The open row's own fill (#2283): it stays while the pointer is elsewhere in the list, a step darker than hover. */
const CURRENT_ROW = "bg-hover-2";

/** Brings `el` to the middle of its nearest scrolling ancestor when it is not fully in view (rows loading above it
 *  later still leave it on screen). Not `scrollIntoView`: that also scrolls the clipped (overflow-hidden) box of a
 *  Collapse that is still opening, and it stays scrolled. */
function reveal(el: HTMLElement): boolean {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const o = getComputedStyle(p).overflowY;
    if (o !== "auto" && o !== "scroll") continue;
    const r = el.getBoundingClientRect(),
      b = p.getBoundingClientRect();
    if (r.top >= b.top && r.bottom <= b.bottom) return false;
    p.scrollTop += r.top + r.height / 2 - (b.top + b.height / 2);
    return true;
  }
  return false;
}

/** The gliding highlight under a list of rows (Beautiful UI's Sidebar Nav): rows register under their keys, and the
 *  one under the pointer gets the box; the open row has its own fill (CURRENT_ROW), so the box doesn't take it away.
 *  The list is the box's scroll container (`nav`). The open row is kept in view from when it opens until the list is
 *  touched (wheel, pointer, keys): it may come later, and rows loading above it push it down. */
function useGlide(activeKey: string) {
  const nav = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const lastKey = useRef<string | null>(null);
  const following = useRef(false);
  const [hovered, setHovered] = useState<string | null>(null);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  useEffect(() => {
    const n = nav.current;
    if (!n) return;
    const stop = () => (following.current = false);
    const events = ["wheel", "touchstart", "pointerdown", "keydown"] as const;
    for (const e of events) n.addEventListener(e, stop, { passive: true });
    return () => events.forEach((e) => n.removeEventListener(e, stop));
  }, []);
  useLayoutEffect(() => {
    if (lastKey.current !== activeKey) {
      lastKey.current = activeKey;
      following.current = true;
    }
    const open = following.current ? rows.current.get(activeKey) : undefined;
    // a group that opened for it is still growing, and the list can't scroll past its end yet: again once it has
    if (open?.offsetHeight && reveal(open)) setTimeout(() => following.current && open.isConnected && reveal(open), 350);
    // the box rests under the open row when the pointer is away, so it glides from there; that row's fill covers it
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
  const [reg] = useState<RegFn>(() => keyedRefs(rows.current));
  const highlight = (
    <span
      aria-hidden
      className="pointer-events-none absolute inset-x-2.5 rounded-[7px] bg-hover"
      style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 220ms ${EASE}, height 220ms ${EASE}, opacity 150ms ease` }}
    />
  );
  return { nav, reg, hover: setHovered, highlight };
}

/** The row a route lights up in the sidebar: a tab's page is the pane it shows (its row in the tree is the tab's). */
function useActiveKey(route: Route): string {
  const all = useAgentSessions();
  const assistant = useStore((s) => s.assistant);
  const tabPath = route.name === "tab" ? href(route) : "";
  // eslint-disable-next-line react-hooks/exhaustive-deps -- `route` is parsed anew each render; its path says it all
  const tabPane = useMemo(() => (route.name === "tab" ? resolveTab(all, route) : undefined), [all, tabPath]);
  return route.name === "tab" ? (tabPane ? rowKey(tabPane) : "") : sidebarActiveKey(route, assistant?.main);
}

function useGo(onNavigate?: () => void): GoFn {
  return (to: Route) => (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
    onNavigate?.();
  };
}

/** One entry of the pages under Persona (总览, 收件箱 … 设置, and the extensions' pages). */
export type NavPage = { k: string; to: Route; icon: IconName; label: string; short?: string; badge?: number | string; badgeTone?: keyof typeof BADGE_CLASS; hint?: string; title?: string };

export function Sidebar({ route, onSearch, onNewAgent, onNavigate }: { route: Route; onSearch: () => void; onNewAgent: () => void; onNavigate?: () => void }) {
  const conn = useStore((s) => s.conn);
  const searchHint = jumpHint(useBindings());
  const machines = useStore((s) => s.machines);
  // 终端 (docs/terminal.md「独立终端」) is there where the hub can list panes
  const terminalPanes = useStore((s) => s.features.includes("terminal_panes"));
  const assistant = useStore((s) => s.assistant);
  const todos = useStore((s) => s.todos);
  const todosSupported = useStore((s) => s.todosSupported);
  // Quiet tasks (#1731) are told about in the main conversation, not counted here.
  const uncompletedTodosCount = useMemo(() => todos.filter((t) => !t.done_at && !t.quiet).length, [todos]);
  const counts = useCounts();
  const inbox = useInboxBadge();
  const larkSwitch = useLarkHome();
  const lark = useLarkOn();
  // 消息 needs the hub to read Feishu (feature `lark`) as well as this browser's switch
  const larkHome = larkSwitch && lark;
  const extensionPages = useExtensionSlot("nav_page");
  // a phone's drawer has the pages as two rows of icons, so its first screen reaches the Agents (#1996)
  const narrow = useMedia(NARROW);
  const activeKey = useActiveKey(route);
  const convs = otherConversations(assistant?.conversations ?? [], assistant?.main, 5, activeKey.startsWith("j:") ? activeKey.slice(2) : undefined);
  const [jarvisFolded, toggleJarvis] = useFolded("jarvis", false, { holds: activeKey === "history" || convs.some((c) => activeKey === `j:${c.session_id}`) });
  const { nav, reg, hover, highlight } = useGlide(activeKey);
  const go = useGo(onNavigate);

  const online = machines.filter((m) => m.online).length;
  const mainConversation = assistant?.main ? assistant.conversations.find((c) => c.session_id === assistant.main) : undefined;
  const page: Record<(typeof NAV_PAGES)[number], NavPage | false> = {
    home: { k: "home", to: { name: "home" }, icon: "home", label: "总览", badge: counts.badge || undefined, badgeTone: "red" },
    inbox: { k: "inbox", to: { name: "inbox" }, icon: "inbox", label: "收件箱", badge: inbox || undefined, badgeTone: "red" },
    messages: larkHome && { k: "messages", to: { name: "messages" }, icon: "chat", label: "消息" },
    "workspace-board": { k: "workspace-board", to: { name: "workspace-board" }, icon: "layers", label: "工作流看板", short: "看板" },
    todos: todosSupported !== false && { k: "todos", to: { name: "todos" }, icon: "todo", label: "待办", badge: uncompletedTodosCount || undefined },
    routines: { k: "routines", to: { name: "routines" }, icon: "clock", label: "定时任务" },
    kb: { k: "kb", to: { name: "kb" }, icon: "book", label: "知识库" },
    machines: { k: "machines", to: { name: "machines" }, icon: "machine", label: "机器", hint: machines.length ? `${online}/${machines.length}` : undefined },
    terminal: terminalPanes && { k: "terminal", to: { name: "terminal" }, icon: "terminal", label: "终端" },
    settings: { k: "settings", to: { name: "settings" }, icon: "gear", label: "设置" },
  };
  const pages = NAV_PAGES.flatMap((k): NavPage[] => [
    // the extensions' pages (docs/extensions.md「UI 插槽」 nav_page): one entry each, right before 设置
    ...(k === "settings"
      ? extensionPages.map((u) => {
          const key = extensionNavKey(u.extension, u.id);
          return { k: key, to: pageRoute(u.extension, u.id), icon: navIcon(u.icon), label: u.title, badge: u.badge?.text || undefined, badgeTone: BADGE_TONE[toneOf(u.badge?.tone)], title: u.extension_name ? `${u.title} · ${u.extension_name}` : u.title };
        })
      : []),
    ...(page[k] ? [page[k]] : []),
  ]);

  return (
    <aside className="flex h-full w-full flex-col bg-canvas">
      <div className="px-2.5 pt-2.5">
        <AccountRow conn={conn} online={online} total={machines.length} />
        <button type="button" onClick={onSearch} className="touch-hit mb-1 flex h-8 w-full items-center gap-2 rounded-control bg-inset px-2.5 text-left shadow-hairline transition-colors hover:bg-hover">
          <Icon name="search" size={12} stroke={2} className="text-ink-3" />
          <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-3">搜索会话</span>
          <span className="hidden sm:inline-flex"><Kbd>{searchHint}</Kbd></span>
        </button>
      </div>

      <div ref={nav} data-sidebar-rows onMouseLeave={() => hover(null)} className="relative min-h-0 flex-1 overflow-y-auto overscroll-contain px-2.5 pb-3">
        {highlight}
        <div className="flex flex-col gap-px pt-1">
          <PersonaRow reg={reg} hover={hover} active={activeKey === "persona"} go={go} main={mainConversation} />
          {narrow ? <NavGrid pages={pages} activeKey={activeKey} go={go} /> : pages.map((p) => <NavRow key={p.k} {...p} reg={reg} hover={hover} active={activeKey === p.k} go={go} />)}
        </div>

        {assistant && (
          <Section title="Persona 对话" folded={jarvisFolded} onToggle={toggleJarvis} right={<SectionAdd label="新对话" to={{ name: "persona-new" }} go={go} />}>
            {convs.map((c) => (
              <SessionRow key={c.session_id} s={c} k={`j:${c.session_id}`} reg={reg} hover={hover} active={activeKey === `j:${c.session_id}`} to={{ name: "jarvis", sid: c.session_id }} go={go} showState={false} />
            ))}
            <NavRow k="history" reg={reg} hover={hover} active={activeKey === "history"} to={{ name: "history" }} go={go} icon="list" label="全部对话" hint={assistant.conversations.length > convs.length ? String(assistant.conversations.length) : undefined} />
          </Section>
        )}

        <PersonalAgents reg={reg} hover={hover} activeKey={activeKey} go={go} />

        {/* the phone's Agents page shows them already: the drawer over it doesn't build them twice */}
        {!(narrow && route.name === "agent-list") && <AgentsBlock reg={reg} hover={hover} activeKey={activeKey} go={go} onNewAgent={onNewAgent} onNavigate={onNavigate} />}
      </div>
      <NodePrompt />
    </aside>
  );
}

/** The phone's「Agents」page (#1996): the sidebar's Agents block, the same components, filling the page. */
export function AgentsList({ route, onNewAgent }: { route: Route; onNewAgent: () => void }) {
  const activeKey = useActiveKey(route);
  const { nav, reg, hover, highlight } = useGlide(activeKey);
  const go = useGo();
  return (
    <div className="h-full overflow-y-auto overscroll-contain">
      <div ref={nav} onMouseLeave={() => hover(null)} className="relative mx-auto max-w-[640px] px-2.5 pb-6">
        {highlight}
        <AgentsBlock reg={reg} hover={hover} activeKey={activeKey} go={go} onNewAgent={onNewAgent} add={false} />
      </div>
    </div>
  );
}

/** How many columns the phone's page grid has for `n` pages: two even rows (8 → 4 + 4), at most 5 to a row. */
export function navGridColumns(n: number): number {
  return Math.max(1, Math.min(5, Math.ceil(n / 2)));
}

/** The pages as two rows of icons in a phone's drawer: one tap away like the list, a third of its height. */
export function NavGrid({ pages, activeKey, go }: { pages: NavPage[]; activeKey: string; go: GoFn }) {
  return (
    <nav aria-label="页面" className="mb-1 grid gap-0.5" style={{ gridTemplateColumns: `repeat(${navGridColumns(pages.length)}, minmax(0, 1fr))` }}>
      {pages.map((p) => {
        const active = activeKey === p.k;
        return (
          <a
            key={p.k}
            href={href(p.to)}
            onClick={go(p.to)}
            title={p.title ?? p.label}
            aria-label={p.badge !== undefined ? `${p.label} ${p.badge}` : p.label}
            aria-current={active ? "page" : undefined}
            className={cx("relative z-10 flex min-h-12 min-w-0 flex-col items-center justify-center gap-1 rounded-[9px] px-0.5 py-1.5 transition-[background-color,transform] duration-150 active:scale-[0.96]", active ? "bg-hover" : "hover:bg-hover")}
          >
            <span className={cx("relative", active ? "text-ink" : "text-ink-3")}>
              <Icon name={p.icon} size={17} stroke={1.8} />
              {p.badge !== undefined && (
                <span className={cx("absolute -top-1.5 left-2.5 flex h-4 min-w-4 items-center justify-center whitespace-nowrap rounded-full px-1 text-[10px] font-semibold tabular-nums", BADGE_CLASS[p.badgeTone ?? "accent"])}>{p.badge}</span>
              )}
            </span>
            <span className={cx("w-full truncate text-center text-[11px] leading-tight", active ? "font-medium text-ink" : "text-ink-2")}>{p.short ?? p.label}</span>
          </a>
        );
      })}
    </nav>
  );
}

/** The sidebar's「Agents」: grouping, the quick filter, 置顶, 正在启动, the groups (spaces, machines, repositories,
 *  states or todos) and 最近. The drawer and the phone's Agents page both show it. `add`: the title row's「+」. */
function AgentsBlock({ reg, hover, activeKey, go, onNewAgent, onNavigate, add = true }: { reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onNewAgent: () => void; onNavigate?: () => void; add?: boolean }) {
  const machines = useStore((s) => s.machines);
  const all = useAgentSessions();
  const mutes = useStore((s) => s.mutes);
  const pins = useStore((s) => s.pins);
  const loaded = useStore((s) => s.loaded);
  const todos = useStore((s) => s.todos);
  const todosSupported = useStore((s) => s.todosSupported);
  const launches = useStore((s) => Object.values(s.launches).filter((launch) => launch.phase !== "ready"));
  const [grouping, setGrouping] = useState(() => agentGrouping(localStorage.getItem("jarvis.agentGrouping")));
  // the switch-grouping shortcut (lib/sidebar-api.ts)
  useEffect(() => onGrouping(setGrouping), []);
  const groupingMenu = useMenu();
  const availableGroupings = useMemo(() => {
    if (todosSupported === false) return AGENT_GROUPINGS.filter((g) => g.key !== "todo");
    return AGENT_GROUPINGS;
  }, [todosSupported]);
  // a hub without todos shows by machine, a by-todo choice kept for when it has them
  const shownGrouping = todosSupported === false && grouping === "todo" ? "machine" : grouping;
  const groupingLabel = (availableGroupings.find((g) => g.key === shownGrouping) ?? availableGroupings[0]).label;
  // the quick filter (全部 / 需要你 / 未读 / 工作中), stacked on any grouping; what it showed when chosen stays (`kept`)
  const [filter, setFilter] = useState(() => agentFilter(localStorage.getItem("jarvis.agentFilter")));
  const [kept, setKept] = useState<ReadonlySet<string>>(() => new Set());
  const { live, recent: offline } = useMemo(() => splitAgentSessions(all, machines), [all, machines]);
  const seeded = useRef(false);
  useEffect(() => {
    // a filter saved in this browser starts from what it matches once the list has loaded
    if (!loaded || seeded.current) return;
    seeded.current = true;
    setKept(matchingKeys(live, filter, mutes));
  }, [loaded, live, filter, mutes]);
  const filtered = filter !== "all";
  const filterTotals = useMemo(() => filterCounts(live, mutes), [live, mutes]);
  const shown = useMemo(() => applyFilter(live, filter, mutes, kept), [live, filter, mutes, kept]);
  // a pinned agent shows once, under 置顶 (an offline one too, unless a filter is on); the groups below are the rest
  const pinnedAgents = useMemo(() => pinnedSessions(filtered ? shown : [...shown, ...offline], pins), [filtered, shown, offline, pins]);
  const unpinned = useMemo(() => {
    const keys = new Set(pinnedAgents.map(agentKey));
    return keys.size ? shown.filter((s) => !keys.has(agentKey(s))) : shown;
  }, [shown, pinnedAgents]);
  const recent = useMemo(() => (filtered ? [] : offline.filter((s) => !pinnedAgents.some((p) => agentKey(p) === agentKey(s)))), [filtered, offline, pinnedAgents]);
  const { spaces, loose } = useMemo(() => buildSpaces(unpinned, machines), [unpinned, machines]);
  const groups = useMemo(() => {
    if (shownGrouping === "space" || shownGrouping === "repo" || shownGrouping === "machine") return [];
    if (shownGrouping === "todo") return buildTodoAgentGroups(unpinned, machines, todos, mutes);
    return buildAgentGroups(unpinned, machines, "state", mutes);
  }, [unpinned, machines, shownGrouping, mutes, todos]);
  const repoMachines = useMemo(() => (shownGrouping === "repo" ? buildRepoMachineTree(unpinned, machines) : []), [unpinned, machines, shownGrouping]);
  const machineTree = useMemo(() => (shownGrouping === "machine" ? buildMachineTree(machines, unpinned, { pins, hideEmpty: filtered }) : []), [shownGrouping, machines, unpinned, pins, filtered]);
  // by todo a group mixes hosts (herdr, botmux…): when the list has more than one, rows outside herdr name theirs (#1836)
  const mixedHosts = useMemo(() => shownGrouping === "todo" && hostsOf(live).size > 1, [shownGrouping, live]);
  const unreadShown = filter === "unread" ? unreadOf(shown) : [];
  const choose = (f: AgentFilter) => {
    localStorage.setItem("jarvis.agentFilter", f);
    setFilter(f);
    setKept(matchingKeys(live, f, mutes));
    hover(null);
  };
  // 设置 › 外观 › 侧边栏显示终端 (off): the plain shells of each machine's terminal host, under its agents in 按机器
  const showTerminals = useSidebarTerminals() && shownGrouping === "machine";
  const { listing: panes, refresh: refreshPanes } = usePanes(machines, showTerminals);
  const shellTrees = useMemo(() => {
    if (!showTerminals) return undefined;
    return Object.fromEntries(machines.map((m) => [m.id, shellsOnly(buildTerminalTree(panes[m.id]?.data, []))])) as Record<string, TermSpace[]>;
  }, [showTerminals, machines, panes]);
  const [rowMenu, setRowMenu] = useState<{ s: Session; anchor: HTMLElement; at?: MenuAt } | null>(null);
  const [spaceMenu, setSpaceMenu] = useState<{ sp: Space; anchor: HTMLElement; at?: MenuAt; fold?: MenuNode["fold"] } | null>(null);
  const [nodeMenu, setNodeMenu] = useState<{ node: MenuNode; at: MenuAt; header?: string } | null>(null);
  const [renaming, setRenaming] = useState<Session | null>(null);
  const [renamingSpace, setRenamingSpace] = useState<Session | null>(null);
  const [deleting, setDeleting] = useState<Session | null>(null);
  const [confirmSeen, setConfirmSeen] = useState<Session[] | null>(null);
  const onRowMenu: RowMenuFn = (s, anchor, at) => setRowMenu((m) => (!at && m?.anchor === anchor ? null : { s, anchor, at }));
  const onSpaceMenu: SpaceMenuFn = (sp, anchor, at, fold) => setSpaceMenu((m) => (!at && m?.anchor === anchor ? null : { sp, anchor, at, fold }));
  const onNodeMenu: NodeMenuFn = (node, at, header, toggle) => setNodeMenu((m) => (toggle && m?.at.anchor && m.at.anchor === at.anchor ? null : { node, at, header }));
  const [recentFolded, toggleRecent] = useFolded("recent", true, { holds: holdsRow(recent, activeKey) });
  const [idleFolded, toggleIdle] = useFolded("idle-spaces", true);
  const [pinnedFolded, togglePinned] = useFolded("agents-pinned", false, { holds: holdsRow(pinnedAgents, activeKey) });
  const [launchesFolded, toggleLaunches] = useFolded("agents-launching", false, { holds: launches.some((l) => activeKey === `s:${l.machineId}/${l.id}`) });
  const [showAllRecent, setShowAllRecent] = useState(false);
  const recentShown = showAllRecent ? recent : firstAndCurrent(recent, 8, (s) => rowKey(s) === activeKey);
  const [linkTargetSession, setLinkTargetSession] = useState<Session | undefined>();
  const [linkTargetSpace, setLinkTargetSpace] = useState<LinkTargetSpace | undefined>();

  // a space with nothing to look at (every agent seen and waiting) goes to「空闲的空间」, like the app; the open
  // conversation's space stays where it is so the row under the pointer doesn't move away
  const busy = (sp: Space) => sp.sessions.some((s) => !isQuiet(s) || activeKey === `s:${s.machine_id}/${s.session_id}`);
  // pinned spaces lead; under a quick filter nothing folds into 「空闲的空间」
  const { active, idle } = arrangeSpaces(spaces, { pins, mutes, filtered, busy });
  const tree = { machines, reg, hover, activeKey, go, mutes, onSpaceMenu, onRowMenu, onNodeMenu, onNavigate };

  return (
    <>
      <div className="mt-3 flex items-center gap-1 px-2">
        <span className="min-w-0 flex-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">Agents</span>
        <button type="button" aria-label={`Agent 分组：${groupingLabel}`} aria-haspopup="menu" aria-expanded={Boolean(groupingMenu.anchor)} onClick={groupingMenu.open} className="touch-hit relative z-10 flex items-center gap-1 whitespace-nowrap rounded-control px-2 py-1 text-[11px] text-ink-3 hover:bg-hover hover:text-ink-2">
          {groupingLabel}
          <Icon name="chevron" size={10} stroke={2} />
        </button>
        {add && <SectionAdd label="新建 Agent" onClick={onNewAgent} />}
      </div>
      {groupingMenu.anchor && (
        <Menu anchor={groupingMenu.anchor} onClose={groupingMenu.close} align="right" header="Agent 分组" items={availableGroupings.map((g) => ({
          key: g.key, label: g.label, icon: g.key === "space" ? "folder" : g.key === "machine" ? "machine" : g.key === "todo" ? "todo" : "layers", checked: shownGrouping === g.key,
          onSelect: () => { localStorage.setItem("jarvis.agentGrouping", g.key); setGrouping(g.key); hover(null); },
        }))} />
      )}
      <FilterBar selection={filter} counts={filterTotals} onChoose={choose} />

      {unreadShown.length > 0 && (
        <button type="button" onClick={() => setConfirmSeen(unreadShown)} className="relative z-10 mt-1 flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left transition-[background-color,transform] duration-100 hover:bg-hover active:scale-[0.98] [@media(pointer:coarse)]:min-h-11">
          <span className="flex size-3.5 shrink-0 items-center justify-center text-green"><Icon name="check" size={13} stroke={2.4} /></span>
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">全部标为已读</span>
          <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-3 tabular-nums">{unreadShown.length} 个完成未看</span>
        </button>
      )}

      {filtered && shown.length === 0 && (
        <div className="mt-2 flex items-center gap-2 px-2 py-1.5 text-[12px] text-ink-3">
          <span className="min-w-0 flex-1 truncate">没有{AGENT_FILTERS.find((f) => f.key === filter)!.label}的 Agent</span>
          <button type="button" onClick={() => choose("all")} className="touch-hit relative z-10 shrink-0 whitespace-nowrap rounded-control px-1.5 py-0.5 text-[12px] font-medium text-accent-ink hover:bg-hover">看全部</button>
        </div>
      )}

      {pinnedAgents.length > 0 && (
        <Section title={<span className="flex items-center gap-1"><Icon name="pin" size={10} stroke={2.2} />置顶</span>} folded={pinnedFolded} onToggle={togglePinned} right={<SessionTally sessions={pinnedAgents} folded={pinnedFolded} />}>
          {pinnedAgents.map((s) => {
            const key = `s:${s.machine_id}/${s.session_id}`;
            const context = [s.space_name || baseName(s.cwd), machines.length > 1 ? machineWithHub(s.machine_id, machines) : ""].filter(Boolean).join(" · ");
            return <SessionRow key={key} s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} onMenu={onRowMenu} />;
          })}
        </Section>
      )}

      {launches.length > 0 && <Section title="正在启动" folded={launchesFolded} onToggle={toggleLaunches} right={<span className="text-[10.5px] text-ink-3 tabular-nums">{launches.length}</span>}>
        {launches.map((launch) => <NavRow key={launch.id} k={`s:${launch.machineId}/${launch.id}`} reg={reg} hover={hover} active={activeKey === `s:${launch.machineId}/${launch.id}`} to={{ name: "session", mid: launch.machineId, sid: launch.id }} go={go} icon={launch.phase === "failed" ? "retry" : "agent"} label={launch.text || baseName(launch.cwd) || "新 Agent"} hint={launch.phase === "failed" ? "启动失败" : "后台启动中"} />)}
      </Section>}

      {shownGrouping === "space" ? <>
      {active.map((sp) => (
        <SpaceSection key={sp.key} sp={sp} machineName={machines.length > 1 ? machineWithHub(sp.machineId, machines) : undefined} reg={reg} hover={hover} activeKey={activeKey} go={go} pinned={isSpacePinned(sp.machineId, sp.name, pins)} muted={isSpaceMuted(sp, mutes)} onMenu={onSpaceMenu} onRowMenu={onRowMenu} onNavigate={onNavigate} />
      ))}

      {idle.length > 0 && (
        <Section title={`空闲的空间 ${idle.length}`} folded={idleFolded} onToggle={toggleIdle}>
          {idle.flatMap((sp) =>
            sp.sessions.map((s) => (
              <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={sp.sessions.length > 1 ? `${sp.name} › ${tabLabel(s)}` : sp.name} onMenu={onRowMenu} />
            )),
          )}
        </Section>
      )}

      {loose.length > 0 && <LooseSection sessions={loose} reg={reg} hover={hover} activeKey={activeKey} go={go} onRowMenu={onRowMenu} />}

      </> : shownGrouping === "machine" ? machineTree.map((m) => (
        <MachineSection key={m.key} m={m} shells={shellTrees?.[m.machine.id]} {...tree} />
      )) : shownGrouping === "repo" ? (
        repoMachines.map((m) => (
          <RepoMachineSection key={m.key} m={m} flat={repoMachines.length <= 1} machines={machines} reg={reg} hover={hover} activeKey={activeKey} go={go} onRowMenu={onRowMenu} />
        ))
      ) : groups.map((group) => (
        <AgentSection key={group.key} group={group} machines={machines} byTodo={shownGrouping === "todo"} showHost={mixedHosts} reg={reg} hover={hover} activeKey={activeKey} go={go} onRowMenu={onRowMenu} onNavigate={onNavigate} />
      ))}

      {recent.length > 0 && (
        <Section title="最近（未运行）" folded={recentFolded} onToggle={toggleRecent} right={<span className="text-[10.5px] text-ink-3 tabular-nums">{recent.length}</span>}>
          {recentShown.map((s) => (
            <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} onMenu={onRowMenu} />
          ))}
          {recent.length > 8 && (
            <button type="button" onClick={() => setShowAllRecent((x) => !x)} className="relative z-10 w-full rounded-[7px] px-2 py-1 text-left text-[12px] text-ink-3 hover:text-ink-2 [@media(pointer:coarse)]:min-h-11">
              {showAllRecent ? "收起" : `再显示 ${recent.length - recentShown.length} 个`}
            </button>
          )}
        </Section>
      )}
      {/* the row's ⋯ (and its right click): pin, rename, mute, the todo it hangs on, delete — a long press on iOS
          Safari sends no contextmenu, so nothing lives only there */}
      {rowMenu && <AgentRowMenu s={rowMenu.s} anchor={rowMenu.anchor} at={rowMenu.at} onClose={() => setRowMenu(null)} onRename={setRenaming} onDelete={setDeleting} onLinkTodo={setLinkTargetSession} />}
      {spaceMenu && (
        <SpaceMenu
          sp={spaceMenu.sp}
          anchor={spaceMenu.anchor}
          at={spaceMenu.at}
          fold={spaceMenu.fold}
          onClose={() => setSpaceMenu(null)}
          onRename={setRenamingSpace}
          onMarkSeen={setConfirmSeen}
          onLinkTodo={(sp) => setLinkTargetSpace({ machineId: sp.machineId, space: sp.name })}
        />
      )}
      {nodeMenu && <NodeMenu node={nodeMenu.node} at={nodeMenu.at} header={nodeMenu.header} onClose={() => setNodeMenu(null)} changed={() => { void refresh().catch(() => {}); void refreshPanes(); }} />}
      {renaming && <RenameDialog open session={renaming} onClose={() => setRenaming(null)} />}
      {renamingSpace && <RenameSpaceDialog open session={renamingSpace} onClose={() => setRenamingSpace(null)} />}
      {deleting && <DeleteSessionDialog open session={deleting} machineName={machineWithHub(deleting.machine_id, machines)} onClose={() => setDeleting(null)} />}
      <MarkSeenDialog list={confirmSeen} onClose={() => setConfirmSeen(null)} onDone={() => setKept(new Set())} />
      <LinkTodoDialog open={Boolean(linkTargetSession)} session={linkTargetSession} onClose={() => setLinkTargetSession(undefined)} />
      <LinkTodoDialog open={Boolean(linkTargetSpace)} space={linkTargetSpace} onClose={() => setLinkTargetSpace(undefined)} />
    </>
  );
}

/** 全部 / 需要你 / 未读 / 工作中 with their counts (iOS QuickFilterBar): 全部 and the states that have agents now; the
 * chosen one stays while it empties, so it can be left. Hidden when there is nothing to pick. */
function FilterBar({ selection, counts, onChoose }: { selection: AgentFilter; counts: FilterCounts; onChoose: (f: AgentFilter) => void }) {
  const keys = shownFilters(selection, counts);
  if (keys.length < 2) return null;
  return (
    <div role="radiogroup" aria-label="快速筛选" className="mt-1.5 flex flex-wrap gap-0.5 px-1">
      {keys.map((k) => {
        const f = AGENT_FILTERS.find((x) => x.key === k)!;
        const on = k === selection;
        const n = k === "all" ? undefined : counts[k];
        return (
          <button
            key={k}
            type="button"
            role="radio"
            aria-checked={on}
            aria-label={n === undefined ? f.label : `${f.label} ${n}`}
            onClick={() => onChoose(k)}
            className={cx(
              "touch-hit relative z-10 flex h-6 items-center gap-1 whitespace-nowrap rounded-full px-[7px] text-[11.5px] transition-[background-color,color,box-shadow] duration-150",
              on ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-2 hover:bg-hover hover:text-ink",
            )}
          >
            {f.state && <StatusDot state={f.state} size={6} />}
            {f.label}
            {n !== undefined && <span className={cx("tabular-nums", on ? "text-ink-2" : "text-ink-3")}>{n}</span>}
          </button>
        );
      })}
    </div>
  );
}

function AccountRow({ conn, online, total }: { conn: string; online: number; total: number }) {
  const menu = useMenu();
  const theme = useTheme();
  const otherHubs = hubMenuItems(useHubs());
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
        <AppMark size={32} radius={8} className="shadow-hairline" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] leading-tight font-medium text-ink">Persona</span>
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
            // the other hubs in this browser's list (#1014); none, and the menu is as before
            ...otherHubs,
            { key: "settings", label: "设置", icon: "gear", divider: otherHubs.length > 0, onSelect: () => navigate({ name: "settings" }) },
            { key: "logout", label: "断开这个浏览器", icon: "logout", danger: true, onSelect: () => void logout() },
          ]}
        />
      )}
    </>
  );
}

// Every group in the sidebar is one of these: its title folds it, and the fold is kept in this browser (useFolded).
function Section({ title, children, folded, onToggle, right, onMenu }: { title: React.ReactNode; children: React.ReactNode; folded: boolean; onToggle: () => void; right?: React.ReactNode; onMenu?: (at: MenuAt, el: HTMLElement) => void }) {
  return (
    <div className="mt-3">
      <div className="group/sec flex items-center gap-1 px-2 pb-1" {...(onMenu ? contextMenuProps(onMenu) : {})}>
        <button type="button" aria-expanded={!folded} onClick={onToggle} className="touch-hit relative z-10 flex min-w-0 flex-1 items-center gap-1 whitespace-nowrap text-left text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase hover:text-ink-2">
          <span className="min-w-0 truncate">{title}</span>
          <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 opacity-0 transition-[opacity,transform] duration-200 group-hover/sec:opacity-100 [@media(hover:none)]:opacity-100" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        </button>
        {right}
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px" inert={folded || undefined}>{children}</div>
      </Collapse>
    </div>
  );
}

function AgentSection({ group, machines, byTodo, showHost, reg, hover, activeKey, go, onRowMenu, onNavigate }: { group: AgentGroup; machines: Machine[]; byTodo?: boolean; showHost?: boolean; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu: RowMenuFn; onNavigate?: () => void }) {
  const [folded, toggle] = useFolded(group.key, group.folded, { follow: true, holds: holdsRow(group.sessions, activeKey) });
  const row = (s: Session) => {
    const key = `s:${s.machine_id}/${s.session_id}`;
    const context = [s.space_name || baseName(s.cwd), machines.length > 1 ? machineWithHub(s.machine_id, machines) : ""].filter(Boolean).join(" · ");
    return <SessionRow key={key} s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} host={showHost && sessionHost(s) !== "herdr" ? sessionHost(s) : undefined} showTodoTag={!byTodo} onMenu={onRowMenu} />;
  };

  const statusInfo = group.todoStatus ? TODO_STATUS_INFO[group.todoStatus as TodoStatus] : undefined;
  const statusTone = statusInfo?.tone === "red" ? "text-red" : statusInfo?.tone === "green" ? "text-green" : statusInfo?.tone === "amber" ? "text-orange" : "text-ink-3";

  const titleNode = group.todoId ? (
    <span className="flex min-w-0 items-center gap-1.5">
      <Icon name="todo" size={12} stroke={2} className="shrink-0 text-ink-3" />
      <span
        onClick={(e) => {
          e.stopPropagation();
          openTodoFromTag(group.todoId!, navigate, onNavigate);
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
      {group.sessions.map(row)}
    </Section>
  );
}

/** One computer in the repository tree (#2108 / #2126): header folds the whole computer; flat when only one computer has agents. */
function RepoMachineSection({ m, flat, machines, reg, hover, activeKey, go, onRowMenu }: { m: RepoMachine; flat: boolean; machines: Machine[]; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu: RowMenuFn }) {
  const online = m.machine.online;
  const [folded, toggle] = useFolded(m.foldKey, !online, { holds: holdsRow(m.sessions, activeKey) });
  const repos = m.repos.map((tree) => <RepoSection key={tree.key} tree={tree} reg={reg} hover={hover} activeKey={activeKey} go={go} onRowMenu={onRowMenu} />);
  if (flat) return <>{repos}</>;
  return (
    <div className="mt-3">
      <div className="group/sec flex items-center gap-1 px-2 pb-0.5">
        <button type="button" aria-expanded={!folded} onClick={toggle} className="relative z-10 flex min-h-7 min-w-0 flex-1 items-center gap-1.5 whitespace-nowrap text-left [@media(pointer:coarse)]:min-h-11">
          <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", online ? "bg-green" : "bg-line-strong")} />
          <span className={cx("min-w-0 truncate text-[12.5px] font-semibold", online ? "text-ink" : "text-ink-3")}>{machineWithHub(m.machine.id, machines)}</span>
          <span className="sr-only">{online ? "在线" : "离线"}</span>
          <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 text-ink-3 opacity-0 transition-[opacity,transform] duration-200 group-hover/sec:opacity-100 [@media(hover:none)]:opacity-100" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        </button>
        {m.needs > 0 ? (
          <span title={`${m.needs} 个需要你`} className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-red-tint px-1 text-[10.5px] font-semibold text-red tabular-nums">
            {m.needs}<span className="sr-only"> 个需要你</span>
          </span>
        ) : !online ? (
          <span className="shrink-0 text-[10.5px] whitespace-nowrap text-ink-3">离线</span>
        ) : (
          <SessionTally sessions={m.sessions} folded={folded} />
        )}
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px" inert={folded || undefined}>
          {repos}
        </div>
      </Collapse>
    </div>
  );
}

/** One repository on one machine (按仓库): its working trees under it, the main checkout marked and first. */
function RepoSection({ tree, reg, hover, activeKey, go, onRowMenu }: { tree: RepoTree; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu: RowMenuFn }) {
  const [folded, toggle] = useFolded(tree.key, false, { holds: tree.worktrees.some((w) => holdsRow(w.sessions, activeKey)) });
  const row = (s: Session, hint: string) => {
    const key = `s:${s.machine_id}/${s.session_id}`;
    return <SessionRow key={key} s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={hint} onMenu={onRowMenu} />;
  };
  // A repository with only its main checkout needs no working-tree level: its agents sit right under the repository.
  const flat = tree.worktrees.length === 1 && tree.worktrees[0]!.main;
  return (
    <Section
      title={<span className="flex min-w-0 items-center gap-1.5"><Icon name="folder" size={12} stroke={2} className="shrink-0 text-ink-3" /><span className="truncate">{tree.name}</span></span>}
      folded={folded}
      onToggle={toggle}
      right={<span className="text-[10.5px] text-ink-3 tabular-nums">{tree.count}</span>}
    >
      {flat ? tree.worktrees[0]!.sessions.map((s) => row(s, tree.name)) : tree.worktrees.map((w) => <RepoWorktreeRows key={w.key} w={w} repo={tree.name} row={row} activeKey={activeKey} />)}
    </Section>
  );
}

/** One working tree's agents under its own folding row: the main checkout wears a home mark, and a tree with nothing
 * running says so. */
function RepoWorktreeRows({ w, repo, row, activeKey }: { w: RepoWorktree; repo: string; row: (s: Session, hint: string) => React.ReactNode; activeKey: string }) {
  const [folded, toggle] = useFolded(w.key, w.sessions.length === 0, { holds: holdsRow(w.sessions, activeKey) });
  const running = w.sessions.filter((s) => !isQuiet(s)).length;
  return (
    <div className="mt-px">
      <button type="button" aria-expanded={!folded} onClick={toggle} className="relative z-10 flex w-full min-w-0 items-center gap-1.5 whitespace-nowrap rounded-control py-1 pl-3 pr-2 text-left text-[11.5px] text-ink-2 hover:bg-hover">
        <Icon name="chevron" size={9} stroke={2.4} className="shrink-0 opacity-60 transition-transform" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        {w.main && <Icon name="home" size={11} stroke={2} className="shrink-0 text-ink-3" />}
        <span className="min-w-0 truncate">{w.name}</span>
        <span className="ml-auto shrink-0 text-[10.5px] text-ink-3 tabular-nums">{running > 0 ? `${w.sessions.length} 个智能体` : "空闲"}</span>
      </button>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px pl-2" inert={folded || undefined}>{w.sessions.map((s) => row(s, `${repo} › ${w.name}`))}</div>
      </Collapse>
    </div>
  );
}

function SpaceSection({ sp, machineName, reg, hover, activeKey, go, pinned, muted, onMenu, onRowMenu, onNavigate }: { sp: Space; machineName?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; pinned: boolean; muted: boolean; onMenu: SpaceMenuFn; onRowMenu: RowMenuFn; onNavigate?: () => void }) {
  const allIdle = sp.sessions.every(isQuiet);
  const [folded, toggle] = useFolded(`space:${sp.key}`, allIdle && sp.sessions.length > 3, { holds: holdsRow(sp.sessions, activeKey) });

  return (
    <Section
      folded={folded}
      onToggle={toggle}
      onMenu={(at, el) => onMenu(sp, el, at, { folded, toggle })}
      title={
        <span className="flex items-center gap-1.5">
          <span className="truncate normal-case tracking-normal text-[11.5px] text-ink-2">{sp.name}</span>
          <SpaceMarks pinned={pinned} muted={muted} />
          {machineName && <span className="truncate normal-case tracking-normal text-[11px] font-normal text-ink-3">{machineName}</span>}
        </span>
      }
      right={
        <span className="flex items-center gap-1">
          <SpaceTodoTag sp={sp} onNavigate={onNavigate} />
          <SessionTally sessions={sp.sessions} folded={folded} />
          <SpaceMenuButton sp={sp} onMenu={onMenu} />
        </span>
      }
    >
      {sp.sessions.map((s) => (
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} tab showTodoTag onMenu={onRowMenu} />
      ))}
    </Section>
  );
}

function SpaceMarks({ pinned, muted }: { pinned: boolean; muted: boolean }) {
  return (
    <>
      {pinned && <span title="已置顶" className="shrink-0 text-accent"><Icon name="pin" size={10} stroke={2.2} /><span className="sr-only">已置顶</span></span>}
      {muted && <span title="已屏蔽通知" className="shrink-0 text-ink-3"><Icon name="bellOff" size={10} stroke={2.2} /><span className="sr-only">已屏蔽通知</span></span>}
    </>
  );
}

// The todo a space hangs on (docs/todos.md), when the hub supports both: a tag that opens it.
function SpaceTodoTag({ sp, onNavigate }: { sp: Space; onNavigate?: () => void }) {
  const todos = useStore((st) => st.todos);
  const todosSupported = useStore((st) => st.todosSupported);
  const todosLoaded = useStore((st) => st.todosLoaded);
  const spacesSupported = useStore((st) => st.spacesSupported);
  const supportSpaces = todosLoaded && todosSupported !== false && spacesSupported !== false;
  const linkedTodo = supportSpaces ? findTodoForSpace(todos, sp.machineId, sp.name) : undefined;
  if (!linkedTodo) return null;
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        openTodoFromTag(linkedTodo.id, navigate, onNavigate);
      }}
      className="relative z-10 flex shrink-0 items-center justify-center rounded bg-hover px-1 py-0.5 text-ink-3 hover:bg-hover-2 hover:text-ink transition-colors cursor-pointer"
      title={`待办：${linkedTodo.title}`}
      aria-label={`待办：${linkedTodo.title}`}
    >
      <Icon name="todo" size={10} stroke={2} />
    </button>
  );
}

// A space's ⋯ (rename, pin, mute, mark seen, link a todo): on hover with a pointer, always on a touch screen.
function SpaceMenuButton({ sp, onMenu }: { sp: Space; onMenu: (sp: Space, anchor: HTMLElement) => void }) {
  return (
    <button
      type="button"
      aria-label={`空间 ${sp.name} 的操作`}
      aria-haspopup="menu"
      title="空间的操作"
      onClick={(e) => onMenu(sp, e.currentTarget)}
      className="touch-hit relative z-10 -my-1 flex size-5 items-center justify-center rounded-[5px] text-ink-3 opacity-0 transition-opacity duration-150 group-hover/sec:opacity-100 hover:bg-hover-2 hover:text-ink focus-visible:opacity-100 [@media(hover:none)]:size-8 [@media(hover:none)]:opacity-100"
    >
      <Icon name="more" size={12} />
    </button>
  );
}

type SpaceMenuFn = (sp: Space, anchor: HTMLElement, at?: MenuAt, fold?: MenuNode["fold"]) => void;
type TreeProps = { machines: Machine[]; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; mutes: AgentMutes; onSpaceMenu: SpaceMenuFn; onRowMenu: RowMenuFn; onNodeMenu: NodeMenuFn; onNavigate?: () => void };

// 按机器 (lib/herdr-tree.ts): one machine as herdr lays it out, its workspaces, their tabs, then「其他」. A machine that
// is away starts folded; its agents are under「最近（未运行）」.
function MachineSection({ m, shells, ...p }: { m: HerdrMachine; shells?: TermSpace[] } & TreeProps) {
  const online = m.machine.online;
  const [folded, toggle] = useFolded(m.foldKey, !online, { holds: holdsRow(m.sessions, p.activeKey) });
  const name = machineWithHub(m.machine.id, p.machines);
  return (
    <div className="mt-3">
      <div className="group/sec flex items-center gap-1 px-2 pb-0.5" {...contextMenuProps((at) => p.onNodeMenu(machineNode(m.machine, { fold: { folded, toggle } }), at, name))}>
        <button type="button" aria-expanded={!folded} onClick={toggle} className="relative z-10 flex min-h-7 min-w-0 flex-1 items-center gap-1.5 whitespace-nowrap text-left [@media(pointer:coarse)]:min-h-11">
          <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", online ? "bg-green" : "bg-line-strong")} />
          <span className={cx("min-w-0 truncate text-[12.5px] font-semibold", online ? "text-ink" : "text-ink-3")}>{machineWithHub(m.machine.id, p.machines)}</span>
          <span className="sr-only">{online ? "在线" : "离线"}</span>
          <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 text-ink-3 opacity-0 transition-[opacity,transform] duration-200 group-hover/sec:opacity-100 [@media(hover:none)]:opacity-100" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        </button>
        {m.needs > 0 ? (
          <span title={`${m.needs} 个需要你`} className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-red-tint px-1 text-[10.5px] font-semibold text-red tabular-nums">
            {m.needs}<span className="sr-only"> 个需要你</span>
          </span>
        ) : !online ? (
          <span className="shrink-0 text-[10.5px] whitespace-nowrap text-ink-3">离线</span>
        ) : (
          <SessionTally sessions={m.sessions} folded={folded} />
        )}
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px" inert={folded || undefined}>
          {m.spaces.map((sp) => <WorkspaceBlock key={sp.key} sp={sp} {...p} />)}
          {shells && shells.length > 0 && <ShellTree mid={m.machine.id} online={online} spaces={shells} {...p} />}
          {m.other.length > 0 && (
            // 「其他」: the machine's agents outside any workspace (Codex threads, runtimes without spaces)
            <TreeBlock foldKey={m.otherFoldKey} sessions={m.other} activeKey={p.activeKey} title={<span className="min-w-0 truncate font-medium">其他</span>} right={(folded) => <SessionTally sessions={m.other} folded={folded} />}>
              {m.other.map((s) => <TreeSessionRow key={s.session_id} s={s} {...p} />)}
            </TreeBlock>
          )}
          {!m.sessions.length && <p className="truncate px-3 py-1 text-[12px] text-ink-3">{online ? "没有在跑的 Agent" : "它的 Agent 在「最近」里"}</p>}
        </div>
      </Collapse>
    </div>
  );
}

// A workspace or「其他」in the tree: its title row folds what is under it, the fold kept by 机器/空间名.
function TreeBlock({ foldKey, sessions, activeKey, state, title, right, children, onMenu }: { foldKey: string; sessions: Session[]; activeKey: string; state?: AgentState; title: React.ReactNode; right: (folded: boolean) => React.ReactNode; children: React.ReactNode; onMenu?: (at: MenuAt, el: HTMLElement, fold: { folded: boolean; toggle: () => void }) => void }) {
  const [folded, toggle] = useFolded(foldKey, sessions.every(isQuiet) && sessions.length > 3, { holds: holdsRow(sessions, activeKey) });
  return (
    <div>
      <div className="group/sec flex items-center gap-1 pr-2 pl-2" {...(onMenu ? contextMenuProps((at, el) => onMenu(at, el, { folded, toggle })) : {})}>
        <button type="button" aria-expanded={!folded} onClick={toggle} className="relative z-10 flex min-h-7 min-w-0 flex-1 items-center gap-1.5 whitespace-nowrap text-left text-[12px] text-ink-2 hover:text-ink [@media(pointer:coarse)]:min-h-11">
          <Icon name="chevron" size={9} stroke={2.4} className="shrink-0 text-ink-3 transition-transform duration-200" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
          {state ? <StatusDot state={state} size={6} /> : <span className="size-1.5 shrink-0" />}
          {title}
        </button>
        {right(folded)}
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px pl-3" inert={folded || undefined}>{children}</div>
      </Collapse>
    </div>
  );
}

function WorkspaceBlock({ sp, ...p }: { sp: HerdrSpace } & TreeProps) {
  const tabs = sp.tabs.length;
  return (
    <TreeBlock
      foldKey={sp.foldKey}
      sessions={sp.tree}
      activeKey={p.activeKey}
      state={sp.state}
      onMenu={(at, el, fold) => p.onSpaceMenu(sp, el, at, fold)}
      title={<><span className="min-w-0 truncate font-medium">{sp.name}</span><SpaceMarks pinned={sp.pinned} muted={isSpaceMuted(sp, p.mutes)} /></>}
      right={(folded) => (
        <span className="flex shrink-0 items-center gap-1">
          <SpaceTodoTag sp={sp} onNavigate={p.onNavigate} />
          {sp.children.length > 0 && <ChildSummary counts={sp.childStates} />}
          {folded ? <SessionTally sessions={sp.tree} folded /> : tabs > 0 && !sp.children.length && (
            // agents, as the machine and the quick filter count them: a tab with several agents is one row (#2320)
            <span title={`${sp.sessions.length} 个 Agent · ${tabs} 个标签页`} className="text-[10.5px] text-ink-3 tabular-nums">{sp.sessions.length}<span className="sr-only"> 个 Agent</span></span>
          )}
          <SpaceMenuButton sp={sp} onMenu={p.onSpaceMenu} />
        </span>
      )}
    >
      {sp.tabs.map((t) => <TabRow key={t.key} t={t} {...p} />)}
      {sp.loose.map((s) => <TreeSessionRow key={s.session_id} s={s} {...p} />)}
      {sp.children.length > 0 && <ChildSpaces sp={sp} {...p} />}
    </TreeBlock>
  );
}

// A space's child spaces (Orca's「N 个子项」), folded at first like Orca's sidebar: the parent's row already sums up
// what they are doing (ChildSummary), and the fold is kept by 机器/空间名 like the space's own.
function ChildSpaces({ sp, ...p }: { sp: HerdrSpace } & TreeProps) {
  const [folded, toggle] = useFolded(`${sp.foldKey}/子项`, true, { holds: holdsRow(sp.tree.slice(sp.sessions.length), p.activeKey) });
  const n = sp.children.length;
  return (
    <div>
      <button type="button" aria-expanded={!folded} onClick={toggle} className="relative z-10 flex min-h-7 w-full min-w-0 items-center gap-1.5 pr-2 pl-2 text-left text-[11.5px] whitespace-nowrap text-ink-3 hover:text-ink [@media(pointer:coarse)]:min-h-11">
        <Icon name="chevron" size={9} stroke={2.4} className="shrink-0 transition-transform duration-200" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        <span className="min-w-0 flex-1 truncate">{n} 个子项</span>
        {folded && <SessionTally sessions={sp.tree.slice(sp.sessions.length)} folded />}
      </button>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px" inert={folded || undefined}>
          {sp.children.map((c) => <WorkspaceBlock key={c.key} sp={c} {...p} />)}
        </div>
      </Collapse>
    </div>
  );
}

// What a parent space's children are doing, as Orca's parent row shows it: how many need you, work, are done.
function ChildSummary({ counts }: { counts: ChildStates }) {
  const parts = ([["needs_you", "需要你"], ["working", "工作中"], ["done", "完成"]] as const).filter(([k]) => counts[k] > 0);
  if (!parts.length) return null;
  return (
    <span title={`子项：${parts.map(([k, label]) => `${counts[k]} 个${label}`).join("，")}`} className="flex items-center gap-1.5 text-[10.5px] text-ink-3 tabular-nums">
      {parts.map(([k, label]) => (
        <span key={k} className="flex items-center gap-0.5">
          <span aria-hidden className="flex"><StatusDot state={k === "done" ? "unread" : k} size={6} /></span>
          {counts[k]}
          <span className="sr-only"> 个子项{label}</span>
        </span>
      ))}
    </span>
  );
}

function TreeSessionRow({ s, reg, hover, activeKey, go, onRowMenu }: { s: Session } & TreeProps) {
  const key = rowKey(s);
  return <SessionRow s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} showTodoTag onMenu={onRowMenu} />;
}

/** The menu node of a tab of the sidebar's tree: its most urgent pane stands for it. */
function tabNode(t: HerdrTab, machines: Machine[]): MenuNode {
  const s = t.lead;
  return {
    kind: "tab",
    mid: s.machine_id,
    online: machines.find((m) => m.id === s.machine_id)?.online !== false,
    host: s.space_kind,
    label: s.slot_name,
    workspaceId: t.spaceId,
    tabId: t.slotId,
    paneId: s.pane_id,
    tabSessionId: s.session_id,
    cwd: s.cwd,
  };
}

// One herdr tab: `#N` (or its name) and what its most urgent pane is doing; it opens on that pane, the others are the
// chat's pane tabs (PaneTabs). A tab with one pane has that agent's ⋯, one with several the tab's own. A tab with several
// agents (#2320) opens up to them, in pane order: the 发起者 (the first pane, the others split from it) on top, its
// branches under it; the choice is kept per tab like the sidebar's other folds, and folded it is the one row it always was.
function TabRow({ t, ...p }: { t: HerdrTab } & TreeProps) {
  const { reg, hover, activeKey, go, onRowMenu, onNodeMenu, machines } = p;
  const n = t.sessions.length;
  const single = n === 1;
  // open at first: the point is to see which agents belong together (#2320); a tab folded by hand stays folded
  const [folded, toggle] = useFolded(t.foldKey, false);
  const shut = single || folded;
  const open = t.sessions.find((x) => rowKey(x) === activeKey);
  // opened up, the open agent's own row is the one lit; folded, the tab's row is, and the hidden pane rows (still
  // mounted in the collapse) register under no key, so the highlight and the scroll to the open row find the tab's
  const keys = tabRowKeys(`t:${t.key}`, t.sessions.map(rowKey), activeKey, shut);
  const k = keys.row;
  const to: Route = { name: "tab", mid: t.lead.machine_id, space: t.spaceId, slot: t.slotId };
  const row = (
    <SessionRow
      s={t.lead}
      k={k}
      reg={reg}
      hover={hover}
      active={Boolean(open) && shut}
      to={to}
      go={go}
      // a long tab name gives way: it would run under the 「N 个 Agent」 button (endRoom) and hide the agent's title
      prefix={t.label && <span className="min-w-0 max-w-[50%] shrink-0 truncate text-[11.5px] text-ink-3 tabular-nums">{t.label}</span>}
      showTodoTag={single}
      // a tab with several panes has a menu of its own: rename, split, git, close it
      onMenu={single ? onRowMenu : (_s, anchor, at) => onNodeMenu(tabNode(t, machines), at ?? { anchor }, `标签页 ${t.label || ""}`.trim(), !at)}
      menuLabel={single ? undefined : "标签页的操作"}
      endRoom={!single}
    />
  );
  if (single) return row;
  return (
    <div>
      <div className="relative">
        {row}
        <button
          type="button"
          aria-expanded={!folded}
          aria-label={`${t.label ? `${t.label} 的` : ""}${n} 个 Agent`}
          title={folded ? "展开这个标签页的 Agent" : "收起"}
          onClick={toggle}
          // left of the tab's own ⋯ (24 px, 44 on touch), which sits at the row's end
          className="touch-hit absolute top-1/2 right-8 z-20 flex h-6 -translate-y-1/2 items-center gap-0.5 whitespace-nowrap rounded-[6px] pr-0.5 pl-1 text-[11px] text-ink-3 tabular-nums hover:bg-hover hover:text-ink-2 [@media(hover:none)]:right-11"
        >
          {n} 个 Agent
          <Icon name="chevron" size={10} stroke={2.4} className="shrink-0 transition-transform duration-200" style={{ transform: folded ? "rotate(-90deg)" : "none" }} />
        </button>
      </div>
      <Collapse open={!folded}>
        <div className="flex flex-col gap-px pl-3" inert={folded || undefined}>
          {t.sessions.map((s, i) => {
            const key = rowKey(s);
            const branch = Boolean(t.launcher) && s !== t.launcher;
            const paneKey = keys.panes[i];
            const pane = (
              <SessionRow
                key={s.session_id}
                s={s}
                k={paneKey ?? key}
                reg={paneKey ? reg : unregistered}
                hover={hover}
                active={activeKey === key}
                to={paneRoute(s)}
                go={go}
                prefix={<span className="shrink-0 text-[11px] text-ink-3">{agentLabel(s)}</span>}
                host={s === t.launcher ? "发起者" : undefined}
                showTodoTag
                onMenu={onRowMenu}
              />
            );
            // the branches hang off the 发起者 on a thin line
            return branch ? <div key={s.session_id} className="ml-[13px] border-l border-line pl-1">{pane}</div> : pane;
          })}
        </div>
      </Collapse>
    </div>
  );
}

// 终端 under a machine (设置 › 外观 › 侧边栏显示终端): its terminal host's workspaces and the plain shells in them, each
// opening the 终端 page on that pane. Folds are kept by 机器/工作区名 like the Agents' above.
function ShellTree({ mid, online, spaces, reg, hover, go, activeKey, onNodeMenu }: { mid: string; online: boolean; spaces: TermSpace[] } & TreeProps) {
  const route = useRoute();
  const shown = route.name === "terminal" && route.mid === mid ? route.id : undefined;
  const sessions = useStore((s) => s.sessions[mid]);
  const count = spaces.reduce((n, sp) => n + sp.tabs.reduce((m, t) => m + t.panes.length, 0), 0);
  return (
    <TreeBlock foldKey={`term:${mid}`} sessions={[]} activeKey={activeKey} title={<span className="min-w-0 truncate font-medium">终端</span>} right={() => <span className="text-[10.5px] text-ink-3 tabular-nums">{count}</span>}>
      {spaces.map((sp) => (
        <TreeBlock
          key={`${sp.host}/${sp.id}`}
          foldKey={`term:${mid}/${sp.host}:${sp.label}`}
          sessions={[]}
          activeKey={activeKey}
          title={<span className="min-w-0 truncate font-medium">{sp.label}</span>}
          right={() => null}
          onMenu={(at) => onNodeMenu(termSpaceNode(mid, sp, online), at, `空间 ${sp.label}`)}
        >
          {sp.tabs.flatMap((t) =>
            t.panes.map((p) => {
              const k = `term:${mid}/${p.id}`;
              const node = t.panes.length === 1 ? termTabNode(mid, sp, t, online) : termPaneNode(mid, sp, t, p, online);
              return (
                <NavRow
                  key={k}
                  k={k}
                  reg={reg}
                  hover={hover}
                  active={shown === p.id}
                  to={terminalTarget(mid, p.id, sessions ?? [])}
                  go={go}
                  icon="terminal"
                  label={t.panes.length > 1 ? `${t.label} · ${p.title}` : p.title}
                  title={p.detail ? `${p.title}\n${p.detail}` : p.title}
                  menuLabel="终端的操作"
                  onMenu={(at, _el, button) => onNodeMenu(node, at, t.label, button)}
                />
              );
            }),
          )}
        </TreeBlock>
      ))}
    </TreeBlock>
  );
}

// Agents outside any space (Codex sessions and the like): folds like a space, folded at first when it is long and
// nothing in it wants a look. The app lists them as a space of their machine.
function LooseSection({ sessions, reg, hover, activeKey, go, onRowMenu }: { sessions: Session[]; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn; onRowMenu: RowMenuFn }) {
  const [folded, toggle] = useFolded("loose", sessions.every(isQuiet) && sessions.length > 3, { holds: holdsRow(sessions, activeKey) });
  return (
    <Section title="其他 Agent" folded={folded} onToggle={toggle} right={<SessionTally sessions={sessions} folded={folded} />}>
      {sessions.map((s) => (
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} onMenu={onRowMenu} />
      ))}
    </Section>
  );
}

// A group's count; folded, the states that want a look instead, so a folded group doesn't hide them.
function SessionTally({ sessions, folded }: { sessions: Session[]; folded: boolean }) {
  const states = sessions.map(stateOf);
  const dots = (["needs_you", "unread", "working"] as const).filter((s) => states.includes(s));
  if (folded && dots.length)
    return (
      <span className="flex items-center gap-1">
        {dots.map((d) => (
          <StatusDot key={d} state={d} size={6} />
        ))}
      </span>
    );
  return <span className="text-[10.5px] text-ink-3 tabular-nums">{sessions.length}</span>;
}

type RegFn = (k: string) => (el: HTMLElement | null) => void;
/** For a row that is there but hidden (a folded tab's panes): it registers nowhere. */
const unregistered: RegFn = () => () => {};
/** An agent row's key in the sidebar (highlight, active): sidebarActiveKey's for a session. */
const rowKey = (s: Pick<Session, "machine_id" | "session_id">) => `s:${agentKey(s)}`;
/** Whether the open row (`activeKey`) is one of these agents': its group opens for it (useFolded). */
const holdsRow = (sessions: readonly Session[], activeKey: string) => sessions.some((s) => rowKey(s) === activeKey);
type GoFn = (to: Route) => (e: React.MouseEvent) => void;
/** `at`: where a right click or the menu key opened it; without it (the ⋯) the row's menu toggles beside its button. */
type RowMenuFn = (s: Session, anchor: HTMLElement, at?: MenuAt) => void;
/** A machine's, a workspace's or a tab's menu (src/lib/node-actions.ts); `toggle`: from its ⋯, which closes it again. */
type NodeMenuFn = (node: MenuNode, at: MenuAt, header?: string, toggle?: boolean) => void;

// Persona's main conversation: the first and strongest entry (its face, always in ink), with the conversation's state
// when it is working or wants you.
function PersonaRow({ reg, hover, active, go, main }: { reg: RegFn; hover: (k: string | null) => void; active: boolean; go: GoFn; main?: Session }) {
  const to: Route = { name: "jarvis" };
  const st = main ? stateOf(main) : undefined;
  const pending = main?.pending_interactions ?? 0;
  return (
    <a
      ref={reg("persona")}
      href={href(to)}
      onClick={go(to)}
      onMouseEnter={() => hover("persona")}
      onFocus={() => hover("persona")}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      title="Persona 的主对话"
      className={cx("relative z-10 mb-1 flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 transition-transform duration-150 active:scale-[0.98] [@media(pointer:coarse)]:min-h-11", active && CURRENT_ROW)}
    >
      <PersonaAvatar
        size={18}
        fallback={
          <span className="flex size-[18px] shrink-0 items-center justify-center rounded-[5px] bg-ink text-surface">
            <Icon name="spark" size={11} stroke={2} />
          </span>
        }
      />
      <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold text-ink">Persona</span>
      {pending > 0 ? (
        <span className="flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-red-tint px-1 text-[10.5px] font-semibold text-red tabular-nums">{pending}</span>
      ) : st === "working" || st === "needs_you" || st === "unread" ? (
        <StatusDot state={st} size={7} />
      ) : null}
    </a>
  );
}

// A section's「+」(a new conversation, a new agent): quiet, next to the section's title.
function SectionAdd({ label, to, go, onClick }: { label: string; to?: Route; go?: GoFn; onClick?: () => void }) {
  const cls = "touch-hit relative z-10 flex size-5 shrink-0 items-center justify-center rounded-[5px] text-ink-3 hover:bg-hover hover:text-ink-2";
  const icon = <Icon name="plus" size={12} stroke={2} />;
  if (to && go) return <a href={href(to)} onClick={go(to)} title={label} aria-label={label} className={cls}>{icon}</a>;
  return <button type="button" onClick={onClick} title={label} aria-label={label} className={cls}>{icon}</button>;
}

const BADGE_CLASS = { red: "bg-red-tint text-red", orange: "bg-orange-tint text-orange", green: "bg-green-tint text-green", accent: "bg-accent-tint text-accent-ink" } as const;

function NavRow({ k, reg, hover, active, to, go, icon, label, badge, badgeTone, hint, title, onMenu, menuLabel }: { onMenu?: (at: MenuAt, el: HTMLElement, button: boolean) => void; menuLabel?: string; k: string; reg: RegFn; hover: (k: string | null) => void; active: boolean; to: Route; go: GoFn; icon: IconName; label: string; badge?: number | string; badgeTone?: keyof typeof BADGE_CLASS; hint?: string; title?: string }) {
  const row = useRef<HTMLAnchorElement | null>(null);
  const link = (
    <a
      ref={(el) => {
        row.current = el;
        reg(k)(el);
      }}
      {...(onMenu ? contextMenuProps((at, el) => onMenu(at, el, false)) : {})}
      href={href(to)}
      title={title}
      onClick={go(to)}
      onMouseEnter={() => hover(k)}
      onFocus={() => hover(k)}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      className={cx("relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 transition-transform duration-150 active:scale-[0.98] [@media(pointer:coarse)]:min-h-11", active && CURRENT_ROW)}
    >
      <span className={active ? "text-ink" : "text-ink-3"}>
        <Icon name={icon} size={14} stroke={1.8} />
      </span>
      <span className={cx("min-w-0 flex-1 truncate text-[13px]", active ? "font-medium text-ink" : "text-ink-2")}>{label}</span>
      {hint && <span className="text-[11px] text-ink-3 tabular-nums">{hint}</span>}
      {badge !== undefined && (
        <span className={cx("flex h-4.5 min-w-4.5 shrink-0 items-center justify-center whitespace-nowrap rounded-full px-1 text-[10.5px] font-semibold tabular-nums", BADGE_CLASS[badgeTone ?? "accent"])} style={{ animation: `pop-in 250ms ${EASE} both` }} key={badge}>
          {badge}
        </span>
      )}
    </a>
  );
  if (!onMenu) return link;
  // the ⋯ shows over the row's end on hover (a pointer); on a touch screen it stays, in the room the row leaves for it
  return (
    <div className="group/row relative">
      {link}
      <button
        type="button"
        aria-label={menuLabel}
        aria-haspopup="menu"
        title="更多"
        tabIndex={-1}
        onClick={() => row.current && onMenu({ anchor: row.current }, row.current, true)}
        className="absolute top-1/2 right-1 z-20 hidden size-6 -translate-y-1/2 items-center justify-center rounded-[6px] bg-hover text-ink-3 hover:bg-hover-2 hover:text-ink group-hover/row:flex [@media(hover:none)]:right-0 [@media(hover:none)]:flex [@media(hover:none)]:size-11 [@media(hover:none)]:bg-transparent"
      >
        <Icon name="more" size={14} />
      </button>
    </div>
  );
}

export function SessionRow({ s, k, reg, hover, active, to, go, tab, label, prefix, hint, host, showState = true, showTodoTag, onMenu, menuLabel = "Agent 的操作", endRoom }: { menuLabel?: string; s: Session; k: string; reg: RegFn; hover: (k: string | null) => void; active: boolean; to: Route; go: GoFn; tab?: boolean; label?: string; prefix?: React.ReactNode; hint?: string; host?: string; showState?: boolean; showTodoTag?: boolean; onMenu?: RowMenuFn; endRoom?: boolean }) {
  const st = stateOf(s);
  const t = [sourceSubtitle(s), hint ?? (tab ? tabLabel(s) : "")].filter(Boolean).join(" · ");
  const preview = sessionPreview(s);
  const row = useRef<HTMLAnchorElement | null>(null);
  // 设置 › 外观 › Agent 行显示详情 (#2430): a second line under an agent's title — folder · model · context · age
  const details = useAgentDetails();
  const contextUsed = useContextAsUsed();
  const detail = details && showState && !s.assistant ? rowDetail(s, contextUsed) : [];
  // what sits at the row's end steps aside for the hover ⋯; the open row leaves room for its ⋯ instead
  const aside = active ? undefined : "group-hover/row:invisible";
  // one ref per key, so letting go takes out only this row's entry (keyedRefs)
  const regRow = useMemo(() => reg(k), [reg, k]);
  const link = (
    <a
      ref={(el) => {
        row.current = el;
        regRow(el);
      }}
      href={href(to)}
      onClick={go(to)}
      // right click (at the pointer), the menu key and Shift+F10 (beside the row): the row's menu, the same as its ⋯
      {...(onMenu ? contextMenuProps((at, el) => onMenu(s, el, at)) : {})}
      onMouseEnter={() => hover(k)}
      onFocus={() => hover(k)}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      title={`${sessionTitle(s)}${sourceSubtitle(s) ? `\n${sourceSubtitle(s)}` : ""}${hint ? `\n${hint}` : ""}${s.todo ? `\n待办：${s.todo.title}` : ""}${preview ? `\n${preview}` : ""}`}
      // a touch screen: rows a finger can hit (44), and room at the end for the ⋯ that is always there; the open row
      // keeps its ⋯ too, so it has room for it
      className={cx("relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-[5px] transition-transform duration-150 active:scale-[0.98] [@media(pointer:coarse)]:min-h-11", active && CURRENT_ROW, // room for 「N 个 Agent」 and the ⋯ beside it (instead of the ⋯'s own)
        endRoom ? "pr-[108px] [@media(hover:none)]:pr-[120px]" : cx(onMenu && active && "pr-8", onMenu && "[@media(hover:none)]:pr-11"))}
    >
      <span className="flex size-3.5 shrink-0 items-center justify-center">{showState ? <StatusDot state={st} size={7} /> : <Icon name="chat" size={12} className="text-ink-3" />}</span>
      {prefix}
      {detail.length > 0 ? (
        <span className="flex min-w-0 flex-1 flex-col">
          <span className={cx("truncate text-[13px]", active ? "font-medium text-ink" : st === "offline" ? "text-ink-3" : "text-ink-2", st === "unread" && !active && "font-medium text-ink")}>
            {label ?? sessionTitle(s)}
          </span>
          {/* one line high: a piece that does not fit wraps out of sight instead of squeezing the others */}
          <span className="flex h-4 min-w-0 flex-wrap gap-x-1 overflow-hidden whitespace-nowrap text-[11px] leading-4 text-ink-3">
            {detail.map((d, i) => (
              <span key={i} title={d.title} className={cx("flex shrink-0 items-center gap-1 tabular-nums", d.warn && "font-medium text-orange")}>
                {i > 0 && <span aria-hidden="true">·</span>}
                {d.text}
              </span>
            ))}
          </span>
        </span>
      ) : (
        <span className={cx("min-w-0 flex-1 truncate text-[13px]", active ? "font-medium text-ink" : st === "offline" ? "text-ink-3" : "text-ink-2", st === "unread" && !active && "font-medium text-ink")}>
          {label ?? sessionTitle(s)}
        </span>
      )}
      {host && <span className={cx("shrink-0 rounded bg-hover px-1 text-[10px] leading-4 text-ink-3", aside)}>{host}</span>}
      {showTodoTag && s.todo && (
        <span
          className={cx("flex shrink-0 items-center justify-center rounded bg-hover px-1 py-0.5 text-ink-3", aside)}
          title={`待办：${s.todo.title}`}
          aria-label={`待办：${s.todo.title}`}
        >
          <Icon name="todo" size={10} stroke={2} />
        </span>
      )}
      {/* the row's ⋯ takes the end on hover: what is there steps aside instead of showing half under it */}
      {t && <span className={cx("max-w-[72px] shrink-0 truncate text-[11px] text-ink-3", aside)}>{t}</span>}
      {!t && (st === "offline" || !showState) && <span className={cx("shrink-0 text-[11px] text-ink-3", aside)}>{ago(s.updated_at)}</span>}
      {(s.pending_interactions ?? 0) > 0 && <span className={cx("flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-red px-1 text-[10px] font-semibold text-white tabular-nums", aside)}>{s.pending_interactions}</span>}
    </a>
  );
  if (!onMenu) return link;
  // the ⋯ shows over the row's end on hover (a pointer); on the open row and on a touch screen it stays, in the room
  // the row leaves for it
  return (
    <div className="group/row relative">
      {link}
      <button
        type="button"
        aria-label={menuLabel}
        aria-haspopup="menu"
        title="更多"
        tabIndex={-1}
        onClick={() => row.current && onMenu(s, row.current)}
        className={cx("absolute top-1/2 right-1 z-20 size-6 -translate-y-1/2 items-center justify-center rounded-[6px] text-ink-3 hover:text-ink group-hover/row:flex [@media(hover:none)]:right-0 [@media(hover:none)]:flex [@media(hover:none)]:size-11 [@media(hover:none)]:bg-transparent", active ? "flex hover:bg-hover" : "hidden bg-hover hover:bg-hover-2")}
      >
        <Icon name="more" size={14} />
      </button>
    </div>
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
  const [folded, toggle] = useFolded("personal-agents", false, { holds: activeKey.startsWith("a:") });
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
          <button type="button" title="搜索个人 Agent" aria-label="搜索个人 Agent" onClick={() => setSearching((x) => !x || !!q)} className={cx("touch-hit relative z-10 flex size-5 items-center justify-center rounded-[5px] text-ink-3 hover:bg-hover hover:text-ink-2", (searching || q) && "text-ink-2")}>
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
            className={cx("touch-hit relative z-10 flex size-5 items-center justify-center rounded-[5px] hover:bg-hover hover:text-ink-2", filtered ? "text-accent" : "text-ink-3", menu.anchor && "bg-hover")}
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
        const isOpen = (s: AgentSession) => activeKey === `a:${s.id}`;
        const rows = more ? firstAndCurrent(g.sessions, 5, isOpen) : g.sessions;
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
            {rows.map((s) => (
              <AgentRow key={s.id} s={s} reg={reg} hover={hover} active={isOpen(s)} go={go} pinned={prefs.pins.includes(s.id)} onMenu={(el) => setRowMenu({ s, el })} />
            ))}
            {g.sessions.length > 5 && !q && g.key !== "pinned" && (
              <button type="button" onClick={() => setAll((x) => ({ ...x, [g.key]: !x[g.key] }))} className="relative z-10 w-full rounded-[7px] px-2 py-1 text-left text-[12px] text-ink-3 hover:text-ink-2">
                {more ? `再显示 ${g.sessions.length - rows.length} 个` : "收起"}
              </button>
            )}
          </div>
        );
      })}
      {rowMenu && <Menu anchor={rowMenu.el} onClose={() => setRowMenu(null)} align="right" width={200} items={agentRowItems(rowMenu.s, prefs)} />}
    </Section>
  );
}

export function AgentRow({ s, reg, hover, active, go, pinned, onMenu }: { s: AgentSession; reg: RegFn; hover: (k: string | null) => void; active: boolean; go: GoFn; pinned: boolean; onMenu: (el: HTMLElement) => void }) {
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
        className={cx("relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-[5px] transition-transform duration-150 active:scale-[0.98] [@media(hover:none)]:pr-11 [@media(pointer:coarse)]:min-h-11", active && cx(CURRENT_ROW, "pr-7"))}
      >
        <AgentAvatar s={s} size={16} />
        <span className={cx("min-w-0 flex-1 truncate text-[13px]", active || unread ? "font-medium text-ink" : "text-ink-2")}>{s.title || "未命名会话"}</span>
        <span className={cx("flex shrink-0 items-center", !active && "group-hover/row:invisible")}>
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
        className={cx("absolute top-1/2 right-1 z-20 flex size-5 -translate-y-1/2 items-center justify-center rounded-[5px] hover:bg-hover group-hover/row:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:hidden", active ? "opacity-100" : "opacity-0", pinned ? "text-accent" : "text-ink-3")}
      >
        <Icon name="pin" size={11} />
      </button>
      {/* a touch screen has no hover pin and no right click: the row's menu (置顶 is in it) behind a ⋯ that stays */}
      <button
        type="button"
        aria-label="个人 Agent 的操作"
        aria-haspopup="menu"
        title="更多"
        onClick={(e) => onMenu(e.currentTarget.parentElement ?? e.currentTarget)}
        className="absolute top-1/2 right-0 z-20 hidden size-11 -translate-y-1/2 items-center justify-center text-ink-3 [@media(hover:none)]:flex"
      >
        <Icon name="more" size={14} />
      </button>
    </div>
  );
}
