// 终端: every computer's terminal panes — agents and plain shells — as machine → workspace → tab → pane, and any of them
// opened as a terminal (docs/terminal.md「独立终端」, docs/web.md). The list is on the left (a page of its own on a
// phone), the terminal on the right: the session terminal's own panel, kept for the last two panes of each machine so
// going back is instant, and let go when the page goes to the background or is left.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { machineWithHub } from "../../lib/mesh";
import { href, navigate, onLinkClick, type Route } from "../../lib/router";
import { openPanel } from "../../lib/panel";
import { useStore } from "../../lib/store";
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
  type PanesResponse,
  type TermSpace,
} from "../../lib/terminals";
import type { Machine } from "../../lib/types";
import { cx, IconButton, Loading, StatusDot, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Empty } from "../session/files/parts";
import { TerminalPanel } from "../session/TerminalPanel";

type TerminalRoute = Extract<Route, { name: "terminal" }>;

/** How often the layout is read again while the page is in front (agents' states are live already, from the sessions). */
const REFRESH_MS = 8000;

interface Listing {
  data?: PanesResponse;
  error?: string;
}

/** Each online machine's panes: read when the page opens, every few seconds while it is in front, and on demand. */
function usePanes(machines: Machine[]) {
  const [listing, setListing] = useState<Record<string, Listing>>({});
  const [busy, setBusy] = useState(false);
  const online = machines.filter((m) => m.online).map((m) => m.id);
  const key = online.join("\n");

  const load = useCallback(async (ids: string[]) => {
    await Promise.all(
      ids.map((mid) =>
        api<PanesResponse>(`/machines/${enc(mid)}/panes`).then(
          (data) => setListing((l) => ({ ...l, [mid]: { data } })),
          // a failed read keeps what was seen: the list does not blink out
          (e) => setListing((l) => ({ ...l, [mid]: { data: l[mid]?.data, error: errorText(e) } })),
        ),
      ),
    );
  }, []);

  useEffect(() => {
    const ids = key ? key.split("\n") : [];
    let dead = false;
    setBusy(true);
    void load(ids).finally(() => !dead && setBusy(false));
    const t = setInterval(() => document.visibilityState === "visible" && void load(ids), REFRESH_MS);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [key, load]);

  const refresh = useCallback(async () => {
    setBusy(true);
    await load(key ? key.split("\n") : []);
    setBusy(false);
  }, [key, load]);
  return { listing, busy, refresh, reload: load };
}

export function TerminalPage({ route, onToggleSidebar }: { route: TerminalRoute; onToggleSidebar: () => void }) {
  const machines = useStore((s) => s.machines);
  const sessions = useStore((s) => s.sessions);
  const { listing, busy, refresh, reload } = usePanes(machines);
  const [query, setQuery] = useState("");
  const shown: OpenTerminal | undefined = route.mid && route.id ? { mid: route.mid, id: route.id } : undefined;

  const trees = useMemo(() => {
    const out: Record<string, TermSpace[]> = {};
    for (const m of machines) out[m.id] = buildTerminalTree(listing[m.id]?.data, sessions[m.id] ?? []);
    return out;
  }, [machines, listing, sessions]);

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
      toast(`新建终端失败：${errorText(e)}`, "error");
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
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="flex-1 text-[14px] font-semibold text-ink">终端</h1>
        <IconButton icon="retry" label="刷新列表" onClick={() => void refresh()} disabled={busy} />
      </header>
      <div className="flex min-h-0 flex-1">
        <section aria-label="窗格" className={cx("flex min-h-0 w-full flex-col lg:w-[320px] lg:shrink-0 lg:border-r lg:border-line", listHidden)}>
          <label className="mx-3 mt-3 mb-1 flex h-8 shrink-0 items-center gap-2 rounded-control bg-inset px-2.5 shadow-hairline">
            <Icon name="search" size={12} stroke={2} className="text-ink-3" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing) return;
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  e.preventDefault();
                  if (flat.length) setActive((a) => (Math.min(a, flat.length - 1) + (e.key === "ArrowDown" ? 1 : flat.length - 1)) % flat.length);
                } else if (e.key === "Enter" && activeItem) {
                  e.preventDefault();
                  navigate({ name: "terminal", ...activeItem });
                } else if (e.key === "Escape") setQuery("");
              }}
              placeholder="跳转到窗格：标题、目录、分支、机器…"
              aria-label="跳转到窗格"
              className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3"
            />
          </label>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-6">
            {machines.length === 0 && <Empty icon="machine" title="还没有电脑" detail="连上一台电脑后，这里列出它的终端" />}
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
                />
              );
            })}
            {query.trim() && flat.length === 0 && machines.length > 0 && <p className="px-3 py-8 text-center text-[12.5px] text-ink-3">没有找到「{query.trim()}」</p>}
          </div>
        </section>
        <section aria-label="终端" className={cx("relative flex min-h-0 min-w-0 flex-1 flex-col", termHidden)}>
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
              {busy && !anyPanes ? <Loading /> : <Empty icon="terminal" title="选一个窗格" detail="Agent 和普通 shell 都可以打开成终端；接管时窗格会变成这里的大小，关掉后恢复" />}
            </div>
          )}
        </section>
      </div>
    </div>
  );
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
}) {
  const mid = machine.id;
  if (searching && spaces.length === 0) return null;
  return (
    <div className="mt-2">
      {named && (
        <div className="flex h-7 items-center gap-2 px-2.5">
          <Icon name="machine" size={12} className={machine.online ? "text-ink-2" : "text-ink-3"} />
          <span className={cx("min-w-0 flex-1 truncate text-[12.5px] font-semibold", machine.online ? "text-ink" : "text-ink-3")}>{machine.name}</span>
          {!machine.online && <span className="shrink-0 whitespace-nowrap text-[11px] text-ink-3">离线</span>}
        </div>
      )}
      {machine.online && !listing && loading && <p className="px-2.5 py-2 text-[12px] text-ink-3">读取中…</p>}
      {machine.online && listing?.error && !listing.data && (
        <p className="flex items-center gap-2 px-2.5 py-2 text-[12px] text-red">
          <span className="min-w-0 flex-1 truncate">{listing.error}</span>
          <button type="button" onClick={onRetry} className="shrink-0 whitespace-nowrap rounded-control px-2 py-0.5 text-ink-2 hover:bg-hover hover:text-ink">
            重试
          </button>
        </p>
      )}
      {machine.online && listing?.data && spaces.length === 0 && !searching && <p className="px-2.5 py-2 text-[12px] text-ink-3">没有窗格</p>}
      {spaces.map((sp) => (
        <div key={`${sp.host}/${sp.id}`} className="mt-1">
          <div className="group/space flex h-7 items-center gap-1.5 pr-1 pl-2.5">
            <Icon name="layers" size={12} className="shrink-0 text-ink-3" />
            <span className="min-w-0 truncate text-[12px] font-medium text-ink-2">{sp.label}</span>
            {sp.branch && <span className="min-w-0 shrink truncate rounded-chip bg-field px-1.5 text-[10.5px] text-ink-3">{sp.branch}</span>}
            <span className="flex-1" />
            {sp.canOpen && <IconButton icon="plus" label={`在「${sp.label}」新建终端`} size={24} iconSize={13} onClick={() => onOpenShell(sp)} />}
          </div>
          {sp.tabs.map((t) => (
            <div key={t.id}>
              {(sp.tabs.length > 1 || t.panes.length > 1) && <div className="truncate px-2.5 pt-1 pl-7 text-[11px] text-ink-3">{t.label}</div>}
              {t.panes.map((p) => {
                const to: Route = { name: "terminal", mid, id: p.id };
                const on = sameTerminal(shown, { mid, id: p.id });
                const hot = sameTerminal(activeItem, { mid, id: p.id });
                return (
                  <a
                    key={p.id}
                    href={href(to)}
                    onClick={(e) => onLinkClick(e, to)}
                    aria-current={on ? "page" : undefined}
                    className={cx("flex min-h-9 items-center gap-2.5 rounded-[7px] py-1 pr-2.5 pl-7 transition-colors", on ? "bg-hover" : "hover:bg-hover", hot && !on && "bg-inset")}
                  >
                    <span className="flex size-4 shrink-0 items-center justify-center text-ink-3">
                      {p.state === "shell" ? <Icon name="terminal" size={13} /> : <StatusDot state={p.state} />}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={cx("block truncate text-[12.5px]", on ? "font-medium text-ink" : "text-ink")}>{p.title}</span>
                      {p.detail && <span className="block truncate text-[11px] text-ink-3">{p.detail}</span>}
                    </span>
                  </a>
                );
              })}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
