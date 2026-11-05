// 终端: every computer's terminal panes — agents and plain shells — as machine → workspace → tab → pane, and any of them
// opened as a terminal (docs/terminal.md「独立终端」, docs/web.md). The list is on the left (a page of its own on a
// phone), the terminal on the right: the session terminal's own panel, kept for the last two panes of each machine so
// going back is instant, and let go when the page goes to the background or is left.
import { useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { usePanes, type Listing } from "../../lib/terminal-panes";
import { machineWithHub } from "../../lib/mesh";
import { href, navigate, onLinkClick, type Route } from "../../lib/router";
import { openPanel } from "../../lib/panel";
import { terminalTarget } from "../../lib/session-view";
import { refresh as refreshSessions, useStore } from "../../lib/store";
import {
  buildTerminalTree,
  filterTerminalTree,
  keepOnlyShown,
  keepRecent,
  paneCwd,
  paneIds,
  paneTrail,
  sameTerminal,
  type OpenTerminal,
  type TermSpace,
  type TermTab,
} from "../../lib/terminals";
import type { Machine } from "../../lib/types";
import { AgentMark } from "../../ui/AgentMark";
import { Collapse, cx, IconButton, Loading, toast } from "../../ui/atoms";
import { StatusMark } from "../../ui/StatusMark";
import { contextMenuProps } from "../../ui/context-menu";
import type { MenuAt } from "../../ui/menu-model";
import { machineNode, termPaneNode, termSpaceNode, termTabNode, type MenuNode } from "../../lib/node-actions";
import { useFolded } from "../../lib/use-folded";
import { NodeMenu } from "../NodeMenu";
import { onNodeChanged, setKnownTree } from "../../lib/node-palette";
import { Icon } from "../../ui/icons";
import { SearchField } from "../../ui/SearchField";
import { Empty } from "../session/files/parts";
import { TerminalPanel } from "../session/TerminalPanel";
import { t } from "../../i18n";

type TerminalRoute = Extract<Route, { name: "terminal" }>;

export function TerminalPage({ route, onToggleSidebar }: { route: TerminalRoute; onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines);
  const sessions = useStore((s) => s.sessions);
  const { listing, busy, refresh, reload } = usePanes(machines);
  const [query, setQuery] = useState("");
  const [menu, setMenu] = useState<{ node: MenuNode; at: MenuAt; header?: string } | null>(null);
  const shown: OpenTerminal | undefined = route.mid && route.id ? { mid: route.mid, id: route.id } : undefined;

  const trees = useMemo(() => {
    const out: Record<string, TermSpace[]> = {};
    for (const m of machines) out[m.id] = buildTerminalTree(listing[m.id]?.data, sessions[m.id] ?? []);
    return out;
  }, [machines, listing, sessions]);

  // the palette's node commands (lib/node-palette.ts) read the panes from here, and say when they changed some
  useEffect(() => {
    for (const m of machines) setKnownTree(m.id, trees[m.id] ?? []);
  }, [machines, trees]);
  useEffect(() => onNodeChanged(() => void refresh()), [refresh]);

  // ── the terminals kept open: the one on screen and the one before it, per machine ──
  const [open, setOpen] = useState<OpenTerminal[]>([]);
  const shownKey = shown ? `${shown.mid}\n${shown.id}` : "";
  useEffect(() => {
    if (shown) setOpen((list) => keepRecent(list, shown));
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `shown` is built anew each render; its key says it all
  }, [shownKey]);
  // in the background only what is on screen stays open; the others give their panes back to the computer
  const current = useRef(shown);
  current.current = shown;
  useEffect(() => {
    const hide = () => document.visibilityState === "hidden" && setOpen((list) => keepOnlyShown(list, current.current));
    document.addEventListener("visibilitychange", hide);
    return () => document.removeEventListener("visibilitychange", hide);
  }, []);

  // drawn in the order they were first opened, whatever the recency: moving an xterm in the DOM would be for nothing
  const opened = useRef(new Map<string, number>());
  const stable = [...open].sort((a, b) => {
    for (const t of [a, b]) if (!opened.current.has(`${t.mid}\n${t.id}`)) opened.current.set(`${t.mid}\n${t.id}`, opened.current.size);
    return opened.current.get(`${a.mid}\n${a.id}`)! - opened.current.get(`${b.mid}\n${b.id}`)!;
  });

  const close = (t: OpenTerminal) => {
    setOpen((list) => list.filter((x) => !sameTerminal(x, t)));
    if (sameTerminal(t, shown)) navigate({ name: "terminal" });
  };

  // Jump To: type to narrow the panes of every machine, ↑ ↓ to walk them, Enter to open
  const visible = useMemo(() => {
    const out: { mid: string; spaces: TermSpace[] }[] = [];
    for (const m of machines) {
      const spaces = filterTerminalTree(trees[m.id] ?? [], query, machineWithHub(m.id, machines));
      if (spaces.length || !query.trim()) out.push({ mid: m.id, spaces }); // searching: only the machines with a match
    }
    return out;
  }, [machines, trees, query]);
  const flat = useMemo(() => visible.flatMap((v) => paneIds(v.spaces).map((id) => ({ mid: v.mid, id }))), [visible]);
  const [active, setActive] = useState(0);
  useEffect(() => setActive(0), [query]);
  const activeItem = flat[Math.min(active, flat.length - 1)];

  const openShell = async (mid: string, space: TermSpace) => {
    try {
      const r = await api<{ terminal: string }>(`/machines/${enc(mid)}/panes`, { method: "POST", json: { kind: space.host, workspace_id: space.id } });
      void reload([mid]);
      navigate({ name: "terminal", mid, id: r.terminal });
    } catch (e) {
      toast(t("terminal-terminalpage.new_failed", { error: errorText(e) }), "error");
    }
  };

  const trail = (t: OpenTerminal) => paneTrail(trees[t.mid] ?? [], t.id);
  const cwdOf = (t: OpenTerminal) => paneCwd(trees[t.mid] ?? [], t.id);
  const listHidden = shown ? "max-lg:hidden" : "";
  const termHidden = shown ? "" : "max-lg:hidden";
  const anyPanes = flat.length > 0 || machines.some((m) => (trees[m.id] ?? []).length > 0);

  return (
    <div className="flex h-full flex-col">
      <header className={cx("flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4", shown && "max-lg:hidden")}>
        <IconButton icon="sidebar" label={t("terminal-terminalpage.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="flex-1 text-[14px] font-semibold text-ink">{t("terminal-terminalpage.title")}</h1>
        <IconButton icon="retry" label={t("terminal-terminalpage.refresh")} onClick={() => void refresh()} disabled={busy} />
      </header>
      <div className="flex min-h-0 flex-1">
        <section aria-label={t("terminal-terminalpage.panes")} className={cx("flex min-h-0 w-full flex-col lg:w-[320px] lg:shrink-0 lg:border-r lg:border-line", listHidden)}>
          <SearchField
            tone="inset"
            className="mx-3 mt-3 mb-1 shrink-0"
            value={query}
            onChange={setQuery}
            onKeyDown={(e) => {
              if (e.nativeEvent.isComposing) return;
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                if (flat.length) setActive((a) => (Math.min(a, flat.length - 1) + (e.key === "ArrowDown" ? 1 : flat.length - 1)) % flat.length);
              } else if (e.key === "Enter" && activeItem) {
                e.preventDefault();
                navigate(terminalTarget(activeItem.mid, activeItem.id, sessions[activeItem.mid] ?? []));
              } else if (e.key === "Escape") setQuery("");
            }}
            placeholder={t("terminal-terminalpage.search_placeholder")}
            label={t("terminal-terminalpage.search_label")}
          />
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-6">
            {machines.length === 0 && <Empty icon="machine" title={t("terminal-terminalpage.no_computers")} detail={t("terminal-terminalpage.no_computers_detail")} />}
            {visible.map(({ mid, spaces }) => {
              const m = machines.find((x) => x.id === mid)!;
              return (
                <MachineBlock
                  key={mid}
                  machine={m}
                  named={machines.length > 1}
                  spaces={spaces}
                  listing={listing[mid]}
                  searching={Boolean(query.trim())}
                  loading={busy}
                  shown={shown}
                  activeItem={query.trim() ? activeItem : undefined}
                  onOpenShell={(sp) => void openShell(mid, sp)}
                  onRetry={() => void refresh()}
                  open={(node, at, header, toggle) => setMenu((m) => (toggle && at.anchor && m?.at.anchor === at.anchor ? null : { node, at, header }))}
                />
              );
            })}
            {query.trim() && flat.length === 0 && machines.length > 0 && <p className="px-3 py-8 text-center text-[12.5px] text-ink-3">{t("terminal-terminalpage.no_results", { query: query.trim() })}</p>}
          </div>
        </section>
        {menu && (
          <NodeMenu
            node={menu.node}
            at={menu.at}
            header={menu.header}
            onClose={() => setMenu(null)}
            changed={() => {
              void refresh();
              void refreshSessions().catch(() => {});
            }}
          />
        )}
        <section aria-label={t("terminal-terminalpage.title")} className={cx("relative flex min-h-0 min-w-0 flex-1 flex-col", termHidden)}>
          {stable.map((t) => {
            const on = sameTerminal(t, shown);
            return (
              // A kept terminal stays mounted under the one shown, at the same size, only unseen: its stream goes on and
              // coming back is instant. Not display:none — a hidden xterm measures its box as 100px and would resize the pane.
              <div key={`${t.mid}\n${t.id}`} inert={!on || undefined} className={cx("absolute inset-0 flex flex-col", !on && "pointer-events-none invisible")}>
                <TerminalPanel
                  mid={t.mid}
                  sid={t.id}
                  label={trail(t) || undefined}
                  cwd={cwdOf(t)}
                  // a plain shell has no session id; the Files panel only needs the folder
                  onFiles={() => openPanel({ kind: "files", mid: t.mid, sid: t.id.startsWith("pane:") ? "" : t.id, cwd: cwdOf(t) })}
                  onClose={() => close(t)}
                />
              </div>
            );
          })}
          {!shown && (
            <div className="flex min-h-0 flex-1 flex-col">
              {busy && !anyPanes ? <Loading /> : <Empty icon="terminal" title={t("terminal-terminalpage.pick_pane")} detail={t("terminal-terminalpage.pick_pane_detail")} />}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

/** A fold's chevron, in the room the row leaves on its left (no row moves for it): on hover, and on a touch screen; a
 *  folded row shows it. */
function FoldMark({ folded, className }: { folded: boolean; className: string }) {
  return (
    <Icon
      name="chevron"
      size={9}
      stroke={2.4}
      className={cx("pointer-events-none absolute text-ink-3 transition-[opacity,transform] duration-200 group-hover/fold:opacity-100 [@media(hover:none)]:opacity-100", folded ? "opacity-100" : "opacity-0", className)}
      style={{ transform: folded ? "rotate(-90deg)" : "none" }}
    />
  );
}

/** A row's ⋯ (the menu for a touch screen and for a mouse that has not right clicked): on hover, always on a touch screen. */
function MoreButton({ label, onOpen, className = "" }: { label: string; onOpen: (el: HTMLElement) => void; className?: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-haspopup="menu"
      title={t("terminal-terminalpage.more")}
      onClick={(e) => onOpen(e.currentTarget.parentElement ?? e.currentTarget)}
      className={cx(
        "touch-hit absolute top-1/2 right-1 z-10 hidden size-6 -translate-y-1/2 items-center justify-center rounded-[6px] bg-hover text-ink-3 hover:bg-hover-2 hover:text-ink focus-visible:flex group-hover/fold:flex [@media(hover:none)]:flex [@media(hover:none)]:bg-transparent",
        className,
      )}
    >
      <Icon name="more" size={14} />
    </button>
  );
}

interface Menus {
  /** `toggle`: from a ⋯, which closes its own menu again. */
  open: (node: MenuNode, at: MenuAt, header?: string, toggle?: boolean) => void;
}

function MachineBlock({
  machine,
  named,
  spaces,
  listing,
  searching,
  loading,
  shown,
  activeItem,
  onOpenShell,
  onRetry,
  open,
}: {
  machine: Machine;
  named: boolean;
  spaces: TermSpace[];
  listing?: Listing;
  searching: boolean;
  loading: boolean;
  shown?: OpenTerminal;
  activeItem?: OpenTerminal;
  onOpenShell: (space: TermSpace) => void;
  onRetry: () => void;
} & Menus) {
  const mid = machine.id;
  const [folded, toggle] = useFolded(`term:m:${mid}`);
  if (searching && spaces.length === 0) return null;
  const hide = named && folded && !searching; // a search shows what it found, whatever is folded
  const name = machine.name;
  const node = machineNode(machine, { fold: { folded, toggle }, onRefresh: onRetry });
  return (
    <div className="mt-2">
      {named && (
        <div className="group/fold relative flex h-7 items-center" {...contextMenuProps((at) => open(node, at, name))}>
          <button type="button" aria-expanded={!hide} onClick={toggle} className="flex h-7 min-w-0 flex-1 items-center gap-2 px-2.5 text-left">
            <Icon name="machine" size={12} className={machine.online ? "text-ink-2" : "text-ink-3"} />
            <span className={cx("min-w-0 flex-1 truncate text-[12.5px] font-semibold", machine.online ? "text-ink" : "text-ink-3")}>{machine.name}</span>
            {!machine.online && <span className="shrink-0 whitespace-nowrap text-[11px] text-ink-3">{t("terminal-terminalpage.offline")}</span>}
          </button>
          <FoldMark folded={hide} className="top-1/2 left-0.5 -translate-y-1/2" />
          <MoreButton label={t("terminal-terminalpage.actions_for", { name })} onOpen={(el) => open(node, { anchor: el }, name, true)} />
        </div>
      )}
      <Collapse open={!hide}>
        <div inert={hide || undefined}>
          {machine.online && !listing && loading && <p className="px-2.5 py-2 text-[12px] text-ink-3">{t("terminal-terminalpage.loading")}</p>}
          {machine.online && listing?.error && !listing.data && (
            <p className="flex items-center gap-2 px-2.5 py-2 text-[12px] text-red">
              <span className="min-w-0 flex-1 truncate">{listing.error}</span>
              <button type="button" onClick={onRetry} className="shrink-0 whitespace-nowrap rounded-control px-2 py-0.5 text-ink-2 hover:bg-hover hover:text-ink">
                {t("terminal-terminalpage.retry")}
              </button>
            </p>
          )}
          {machine.online && listing?.data && spaces.length === 0 && !searching && <p className="px-2.5 py-2 text-[12px] text-ink-3">{t("terminal-terminalpage.no_panes")}</p>}
          {spaces.map((sp) => (
            <SpaceBlock key={`${sp.host}/${sp.id}`} machine={machine} sp={sp} searching={searching} shown={shown} activeItem={activeItem} onOpenShell={onOpenShell} open={open} />
          ))}
        </div>
      </Collapse>
    </div>
  );
}

function SpaceBlock({ machine, sp, searching, shown, activeItem, onOpenShell, open }: { machine: Machine; sp: TermSpace; searching: boolean; shown?: OpenTerminal; activeItem?: OpenTerminal; onOpenShell: (space: TermSpace) => void } & Menus) {
  const mid = machine.id;
  const [folded, toggle] = useFolded(`term:w:${mid}/${sp.host}:${sp.label}`);
  const hide = folded && !searching;
  const node = termSpaceNode(mid, sp, machine.online, { fold: { folded, toggle }, onOpenShell: sp.canOpen ? () => onOpenShell(sp) : undefined });
  return (
    <div className="mt-1">
      <div className="group/fold relative flex h-7 items-center gap-1.5 pr-1 pl-2.5" {...contextMenuProps((at) => open(node, at, t("terminal-terminalpage.space_header", { name: sp.label })))}>
        <button type="button" aria-expanded={!hide} onClick={toggle} className="flex h-7 min-w-0 flex-1 items-center gap-1.5 text-left">
          <Icon name="layers" size={12} className="shrink-0 text-ink-3" />
          <span className="min-w-0 truncate text-[12px] font-medium text-ink-2">{sp.label}</span>
          {sp.branch && <span className="min-w-0 shrink truncate rounded-chip bg-field px-1.5 text-[10.5px] text-ink-3">{sp.branch}</span>}
          <span className="flex-1" />
        </button>
        <FoldMark folded={hide} className="top-1/2 left-0.5 -translate-y-1/2" />
        {sp.canOpen && <IconButton icon="plus" label={t("terminal-terminalpage.new_in_space", { name: sp.label })} size={24} iconSize={13} onClick={() => onOpenShell(sp)} />}
        <MoreButton label={t("terminal-terminalpage.actions_for", { name: sp.label })} onOpen={(el) => open(node, { anchor: el }, t("terminal-terminalpage.space_header", { name: sp.label }), true)} className="right-8" />
      </div>
      <Collapse open={!hide}>
        <div inert={hide || undefined}>
          {sp.tabs.map((t) => (
            <TabBlock key={t.id} machine={machine} sp={sp} t={t} searching={searching} shown={shown} activeItem={activeItem} open={open} />
          ))}
        </div>
      </Collapse>
    </div>
  );
}

function TabBlock({ machine, sp, t: tab, searching, shown, activeItem, open }: { machine: Machine; sp: TermSpace; t: TermTab; searching: boolean; shown?: OpenTerminal; activeItem?: OpenTerminal } & Menus) {
  const mid = machine.id;
  const mySessions = useStore((s) => s.sessions[mid]);
  const labelled = sp.tabs.length > 1 || tab.panes.length > 1; // the tab's name is shown (and folds) when it tells tabs or panes apart
  const [folded, toggle] = useFolded(`term:t:${mid}/${sp.host}:${sp.label}/${tab.label}`);
  const hide = labelled && folded && !searching;
  const tabNode = termTabNode(mid, sp, tab, machine.online, { fold: labelled ? { folded, toggle } : undefined });
  const tabHeader = t("terminal-terminalpage.tab_header", { name: tab.label });
  return (
    <div>
      {labelled && (
        <div className="group/fold relative" {...contextMenuProps((at) => open(tabNode, at, tabHeader))}>
          <button type="button" aria-expanded={!hide} onClick={toggle} className="block w-full truncate px-2.5 pt-1 pl-7 text-left text-[11px] text-ink-3">
            {tab.label}
          </button>
          <FoldMark folded={hide} className="top-[calc(50%+2px)] left-4 -translate-y-1/2" />
          {tab.panes.length > 1 && <MoreButton label={t("terminal-terminalpage.actions_for", { name: tab.label })} onOpen={(el) => open(tabNode, { anchor: el }, tabHeader, true)} />}
        </div>
      )}
      <Collapse open={!hide}>
        <div inert={hide || undefined}>
          {tab.panes.map((p) => {
            // an agent's pane opens on its own page, in the terminal view; a plain shell here (it has no conversation)
            const to: Route = terminalTarget(mid, p.id, mySessions ?? []);
            const on = sameTerminal(shown, { mid, id: p.id });
            const hot = sameTerminal(activeItem, { mid, id: p.id });
            // a tab with one pane is that pane: its menu has the tab's rename and close too
            const node = tab.panes.length === 1 ? termTabNode(mid, sp, tab, machine.online) : termPaneNode(mid, sp, tab, p, machine.online);
            const header = tab.panes.length === 1 ? tabHeader : p.title;
            return (
              <div key={p.id} className="group/fold relative">
                <a
                  href={href(to)}
                  onClick={(e) => onLinkClick(e, to)}
                  aria-current={on ? "page" : undefined}
                  {...contextMenuProps((at) => open(node, at, header))}
                  className={cx("flex min-h-9 items-center gap-2.5 rounded-[7px] py-1 pr-2.5 pl-7 transition-colors", on ? "bg-hover" : "hover:bg-hover", hot && !on && "bg-inset")}
                >
                  <span className="flex size-4 shrink-0 items-center justify-center text-ink-3">
                    {p.state === "shell" ? <Icon name="terminal" size={13} /> : <StatusMark state={p.state} />}
                  </span>
                  {p.state !== "shell" && p.agent && <AgentMark agent={p.agent} size={14} />}
                  <span className="min-w-0 flex-1">
                    <span className={cx("block truncate text-[12.5px]", on ? "font-medium text-ink" : "text-ink")}>{p.title}</span>
                    {p.detail && <span className="block truncate text-[11px] text-ink-3">{p.detail}</span>}
                  </span>
                </a>
                <MoreButton label={t("terminal-terminalpage.actions_for", { name: p.title })} onOpen={(el) => open(node, { anchor: el }, header, true)} />
              </div>
            );
          })}
        </div>
      </Collapse>
    </div>
  );
}
