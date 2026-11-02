// Beautiful UI's Sidebar Nav: workspace row, quick search, the accent action, sections, one gliding highlight.
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { agentGrouping, AGENT_GROUPINGS, buildAgentGroups, isQuiet, type AgentGroup } from "../lib/agent-groups";
import { api, setToken } from "../lib/api";
import { ago, baseName, sourceSubtitle, sessionTitle, stateOf, tabLabel } from "../lib/format";
import { href, navigate, type Route } from "../lib/router";
import { useAgentSessions, useCounts, useSpaces } from "../lib/sessions";
import { reset, useStore } from "../lib/store";
import type { Machine, Session } from "../lib/types";
import { Collapse, cx, EASE, Kbd, Menu, StatusDot, useMenu } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { setTheme, useTheme, type Theme } from "./theme";

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
  const launches = useStore((s) => Object.values(s.launches).filter((launch) => launch.phase !== "ready"));
  const [grouping, setGrouping] = useState(() => agentGrouping(localStorage.getItem("jarvis.agentGrouping")));
  const groupingMenu = useMenu();
  const groups = useMemo(() => grouping === "space" ? [] : buildAgentGroups(all, machines, grouping, mutes), [all, machines, grouping, mutes]);
  const groupingLabel = AGENT_GROUPINGS.find((g) => g.key === grouping)!.label;
  const counts = useCounts();
  const nav = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const [hovered, setHovered] = useState<string | null>(null);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const [showAllRecent, setShowAllRecent] = useState(false);
  const [recentFolded, toggleRecent] = useFolded("recent", true);
  const [jarvisFolded, toggleJarvis] = useFolded("jarvis");
  const [idleFolded, toggleIdle] = useFolded("idle-spaces", true);

  const activeKey =
    route.name === "session" ? `s:${route.mid}/${route.sid}` : route.name === "jarvis" ? (route.sid ? `j:${route.sid}` : "j:new") : route.name === "machines" ? "machines" : route.name === "settings" ? "settings" : "home";
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
          <NavRow k="machines" reg={reg} hover={setHovered} active={activeKey === "machines"} to={{ name: "machines" }} go={go} icon="machine" label="机器" hint={machines.length ? `${online}/${machines.length}` : undefined} />
          <NavRow k="settings" reg={reg} hover={setHovered} active={activeKey === "settings"} to={{ name: "settings" }} go={go} icon="gear" label="设置" />
        </div>

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
          <Menu anchor={groupingMenu.anchor} onClose={groupingMenu.close} align="right" header="Agent 分组" items={AGENT_GROUPINGS.map((g) => ({
            key: g.key, label: g.label, icon: g.key === "space" ? "folder" : g.key === "machine" ? "machine" : "layers", checked: grouping === g.key,
            onSelect: () => { localStorage.setItem("jarvis.agentGrouping", g.key); setGrouping(g.key); setHovered(null); },
          }))} />
        )}

        {launches.length > 0 && <Section title="正在启动">
          {launches.map((launch) => <NavRow key={launch.id} k={`s:${launch.machineId}/${launch.id}`} reg={reg} hover={setHovered} active={activeKey === `s:${launch.machineId}/${launch.id}`} to={{ name: "session", mid: launch.machineId, sid: launch.id }} go={go} icon={launch.phase === "failed" ? "retry" : "agent"} label={launch.text || baseName(launch.cwd) || "新 Agent"} hint={launch.phase === "failed" ? "启动失败" : "后台启动中"} />)}
        </Section>}

        {grouping === "space" ? <>
        {active.map((sp) => (
          <SpaceSection key={sp.key} sp={sp} machineName={machines.length > 1 ? machines.find((m) => m.id === sp.machineId)?.name : undefined} reg={reg} hover={setHovered} activeKey={activeKey} go={go} />
        ))}

        {idle.length > 0 && (
          <Section title={`空闲的空间 ${idle.length}`} folded={idleFolded} onToggle={toggleIdle}>
            {idle.flatMap((sp) =>
              sp.sessions.map((s) => (
                <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={sp.sessions.length > 1 ? `${sp.name} › ${tabLabel(s)}` : sp.name} />
              )),
            )}
          </Section>
        )}

        {loose.length > 0 && (
          <Section title="其他 Agent">
            {loose.map((s) => (
              <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} />
            ))}
          </Section>
        )}

        </> : groups.map((group) => (
          <AgentSection key={group.key} group={group} machines={machines} byMachine={grouping === "machine"} reg={reg} hover={setHovered} activeKey={activeKey} go={go} />
        ))}

        {recent.length > 0 && (
          <Section title="最近（未运行）" folded={recentFolded} onToggle={toggleRecent}>
            {(showAllRecent ? recent : recent.slice(0, 8)).map((s) => (
              <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={setHovered} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} />
            ))}
            {recent.length > 8 && (
              <button type="button" onClick={() => setShowAllRecent((x) => !x)} className="relative z-10 w-full rounded-[7px] px-2 py-1 text-left text-[12px] text-ink-3 hover:text-ink-2">
                {showAllRecent ? "收起" : `再显示 ${recent.length - 8} 个`}
              </button>
            )}
          </Section>
        )}
      </div>
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

function AgentSection({ group, machines, byMachine, reg, hover, activeKey, go }: { group: AgentGroup; machines: Machine[]; byMachine: boolean; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn }) {
  const [folded, toggle] = useFolded(group.key, group.folded);
  const [showIdle, setShowIdle] = useState(false);
  const idle = byMachine ? group.sessions.filter((s) => isQuiet(s) && activeKey !== `s:${s.machine_id}/${s.session_id}`) : [];
  const idleKeys = new Set(idle.map((s) => `${s.machine_id}/${s.session_id}`));
  const visible = group.sessions.filter((s) => !idleKeys.has(`${s.machine_id}/${s.session_id}`));
  const row = (s: Session) => {
    const key = `s:${s.machine_id}/${s.session_id}`;
    const context = [s.space_name || baseName(s.cwd), !byMachine && machines.length > 1 ? machines.find((m) => m.id === s.machine_id)?.name : ""].filter(Boolean).join(" · ");
    return <SessionRow key={key} s={s} k={key} reg={reg} hover={hover} active={activeKey === key} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} hint={context} />;
  };
  return (
    <Section title={<span className="flex min-w-0 items-center gap-1.5">{group.state && <span aria-hidden><StatusDot state={group.state} size={6} /></span>}<span className="truncate">{group.name}</span></span>} folded={folded} onToggle={toggle} right={<span className="text-[10.5px] text-ink-3 tabular-nums">{group.sessions.length}</span>}>
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

function SpaceSection({ sp, machineName, reg, hover, activeKey, go }: { sp: ReturnType<typeof useSpaces>["spaces"][number]; machineName?: string; reg: RegFn; hover: (k: string | null) => void; activeKey: string; go: GoFn }) {
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
        <SessionRow key={s.session_id} s={s} k={`s:${s.machine_id}/${s.session_id}`} reg={reg} hover={hover} active={activeKey === `s:${s.machine_id}/${s.session_id}`} to={{ name: "session", mid: s.machine_id, sid: s.session_id }} go={go} tab />
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

function SessionRow({ s, k, reg, hover, active, to, go, tab, label, hint, showState = true }: { s: Session; k: string; reg: RegFn; hover: (k: string | null) => void; active: boolean; to: Route; go: GoFn; tab?: boolean; label?: string; hint?: string; showState?: boolean }) {
  const st = stateOf(s);
  const t = [sourceSubtitle(s), hint ?? (tab ? tabLabel(s) : "")].filter(Boolean).join(" · ");
  return (
    <a
      ref={reg(k)}
      href={href(to)}
      onClick={go(to)}
      onMouseEnter={() => hover(k)}
      onFocus={() => hover(k)}
      onBlur={() => hover(null)}
      aria-current={active ? "page" : undefined}
      title={`${sessionTitle(s)}${sourceSubtitle(s) ? `\n${sourceSubtitle(s)}` : ""}${s.last_text ? `\n${s.last_text}` : ""}`}
      className="relative z-10 flex w-full items-center gap-2 rounded-[7px] px-2 py-[5px] transition-transform duration-150 active:scale-[0.98]"
    >
      <span className="flex size-3.5 shrink-0 items-center justify-center">{showState ? <StatusDot state={st} size={7} /> : <Icon name="chat" size={12} className="text-ink-3" />}</span>
      <span className={cx("min-w-0 flex-1 truncate text-[13px]", active ? "font-medium text-ink" : st === "offline" ? "text-ink-3" : "text-ink-2", st === "unread" && !active && "font-medium text-ink")}>
        {label ?? sessionTitle(s)}
      </span>
      {t && <span className="max-w-[72px] shrink-0 truncate text-[11px] text-ink-3">{t}</span>}
      {!t && (st === "offline" || !showState) && <span className="shrink-0 text-[11px] text-ink-3">{ago(s.updated_at)}</span>}
      {(s.pending_interactions ?? 0) > 0 && <span className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-red px-1 text-[10px] font-semibold text-white tabular-nums">{s.pending_interactions}</span>}
    </a>
  );
}
