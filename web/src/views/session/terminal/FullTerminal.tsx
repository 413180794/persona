// The full surface of a terminal panel (docs/terminal.md「完整 herdr 界面」): herdr's own client, in a PTY on the
// computer, drawn by xterm — its sidebar, tabs and splits, driven from here. The feed is full.ts; what the page shares
// with the single-pane surface (the key bar, the find bar, the right-click menu, the saved commands) is the same
// components. The header's controls for this surface are drawn into the panel's header through `slot`.
import { useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { t } from "../../../i18n";
import { shouldRefocus, terminalMenu, wantsOwnMenu, type TerminalMenuKey } from "../../../lib/terminal-details";
import { FIND_CLOSED, findReducer, isFindShortcut, queryFromSelection } from "../../../lib/terminal-search";
import { STATUS_LABEL, canReconnect, type FullStatus } from "../../../lib/terminal-mode";
import { Button, cx, IconButton, Loading, Menu, type MenuItem } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { FindBar } from "./FindBar";
import { focusFacts, selectionInside } from "./focus";
import { FullFeed, type FullView } from "./full";
import { KeyBar } from "./KeyBar";
import { saveKeyBar, savedKeyBar } from "./keybar-pref";
import { ShortcutsEditor } from "./Shortcuts";

const nothing = () => () => {};

function useFull(feed: FullFeed | null): FullView | null {
  return useSyncExternalStore(feed?.subscribe ?? nothing, () => feed?.snapshot() ?? null);
}

/** Whether the panel is under an `inert` ancestor: a terminal the page keeps mounted but not in front (views/terminal/TerminalPage.tsx). */
export function useUnseen(root: React.RefObject<HTMLElement | null>): boolean {
  const [unseen, setUnseen] = useState(false);
  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const check = () => setUnseen(Boolean(el.closest("[inert]")));
    check();
    const watch = new MutationObserver(check);
    for (let a: HTMLElement | null = el; a; a = a.parentElement) watch.observe(a, { attributes: true, attributeFilter: ["inert"] });
    return () => watch.disconnect();
  }, [root]);
  return unseen;
}

const DOT: Record<FullStatus, string> = {
  connecting: "bg-orange animate-pulse",
  live: "bg-green",
  closed: "bg-ink-3",
  error: "bg-red",
  unsupported: "bg-ink-3",
  forbidden: "bg-red",
};

export function FullTerminal({
  mid,
  sid,
  cwd,
  onFiles,
  slot,
  live,
  delay,
  onFallback,
  onFailed,
}: {
  mid: string;
  sid: string;
  cwd: string;
  onFiles?: () => void;
  /** Where the header's controls for this surface go. */
  slot: HTMLElement | null;
  /** False while this surface is on its way out: the client on the computer is let go, what is drawn stays. */
  live: boolean;
  /** Wait this long (ms) before opening the client: the surface before it is still on screen, driving its own pane. */
  delay: number;
  /** Asks the panel to go back to 单窗格 (the hub or computer cannot do this one). */
  onFallback: () => void;
  /** The client did not start on this machine (why, in words): the panel falls back to 单窗格 by itself. */
  onFailed: (why: string) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const host = useRef<HTMLDivElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const [feed, setFeed] = useState<FullFeed | null>(null);
  const v = useFull(feed);
  const unseen = useUnseen(root);
  const [keyBar, setKeyBar] = useState(savedKeyBar);
  const [editing, setEditing] = useState(false);
  const [find, dispatchFind] = useReducer(findReducer, FIND_CLOSED);
  const [menu, setMenu] = useState<{ x: number; y: number; selection: boolean } | null>(null);
  const delayAtMount = useRef(delay);

  useEffect(() => {
    const f = new FullFeed(mid, sid);
    setFeed(f);
    void f.mount(host.current!, delayAtMount.current);
    return () => f.dispose();
  }, [mid, sid]);

  // The client on the computer lives only while this surface is the one shown and in front.
  const on = live && !unseen;
  useLayoutEffect(() => {
    if (!feed) return;
    if (!on) feed.release();
    else if (feed.isReleased) feed.reconnect();
  }, [feed, on]);

  const status = v?.status ?? "connecting";
  const driving = status === "live";
  const failed = v?.failedToStart ? v.note || STATUS_LABEL[status] : "";
  useEffect(() => {
    if (failed && live) onFailed(failed);
  }, [failed, live, onFailed]);

  const refocus = useCallback(() => {
    const el = root.current;
    if (el && feed && shouldRefocus(focusFacts(el, feed.driving, feed.hasSelection()))) feed.focus();
  }, [feed]);
  useEffect(() => {
    addEventListener("focus", refocus);
    return () => removeEventListener("focus", refocus);
  }, [refocus]);

  const chooseKeyBar = (next: boolean) => {
    saveKeyBar(next);
    setKeyBar(next);
  };

  // ── find (lib/terminal-search.ts), over xterm's screen and scrollback ──
  const openFind = () => {
    const picked = feed?.selectedText() ?? "";
    dispatchFind({ type: "open", query: queryFromSelection(picked) ?? undefined });
  };
  const closeFind = () => {
    dispatchFind({ type: "close" });
    feed?.clearFind();
    feed?.focus();
  };
  const onKeyDownCapture = (e: React.KeyboardEvent) => {
    if (!isFindShortcut(e.nativeEvent, { mac: /Mac|iPhone|iPad/.test(navigator.platform), driving })) return;
    e.preventDefault();
    e.stopPropagation(); // the client does not get the key
    openFind();
  };

  // ── right-click menu: Persona's where herdr is not taking the mouse (it is, nearly always: its own menus win) ──
  const onContextMenu = (e: React.MouseEvent) => {
    if (!wantsOwnMenu({ shiftKey: e.shiftKey, pointerType: (e.nativeEvent as PointerEvent).pointerType })) return;
    const own = Boolean(viewport.current && selectionInside(viewport.current));
    const selection = Boolean(feed?.hasSelection()) || own;
    if (feed?.mouseTracked && !selection) return; // the right click is herdr's
    e.preventDefault();
    (document.activeElement as HTMLElement | null)?.blur();
    setMenu({ x: e.clientX, y: e.clientY, selection });
  };
  const menuItems = (): MenuItem[] => {
    const icons: Record<TerminalMenuKey, MenuItem["icon"]> = { copy: "copy", paste: "paste", selectAll: "list", search: "search", clearSelection: "x" };
    const run: Record<TerminalMenuKey, () => void> = {
      copy: () => (feed?.hasSelection() ? feed.copySelection() : feed?.copyText(window.getSelection()?.toString() ?? "")),
      paste: () => void feed?.pasteClipboard(),
      selectAll: () => feed?.selectAll(),
      search: openFind,
      clearSelection: () => {
        feed?.clearSelection();
        window.getSelection()?.removeAllRanges();
      },
    };
    return terminalMenu({ selection: menu?.selection ?? false, driving }).map((m) => ({
      key: m.key,
      label: m.label,
      icon: icons[m.key],
      disabled: m.disabled || !feed,
      divider: m.divider,
      onSelect: run[m.key],
    }));
  };

  const size = v && v.cols && v.rows ? `${v.cols}×${v.rows}` : "";
  const actions = (
    <>
      <span
        title={
          status === "live"
            ? t("terminal-full.live_hint")
            : v?.note || STATUS_LABEL[status]
        }
        className="hidden shrink-0 items-center gap-1.5 whitespace-nowrap text-[11.5px] text-ink-3 @md:inline-flex"
      >
        <span aria-hidden className={cx("size-1.5 rounded-full", DOT[status])} />
        <span>{STATUS_LABEL[status]}</span>
        {status === "live" && size && <span className="font-mono tabular-nums">{size}</span>}
      </span>
      {onFiles && cwd && <IconButton icon="folder" label={t("terminal-full.files")} onClick={onFiles} className="max-lg:hidden" />}
      <IconButton icon="keyboard" label={keyBar ? t("terminal-full.keybar_hide") : t("terminal-full.keybar_show")} active={keyBar} onClick={() => chooseKeyBar(!keyBar)} />
      <IconButton icon="retry" label={t("terminal-full.reconnect")} onClick={() => feed?.reconnect()} disabled={!feed} />
    </>
  );

  return (
    <div
      ref={root}
      onKeyDownCapture={onKeyDownCapture}
      onClick={(e) => !(e.target as Element).closest('[role="search"]') && setTimeout(refocus, 0)}
      tabIndex={-1}
      className="flex min-h-0 flex-1 flex-col outline-none"
    >
      {slot && on && createPortal(actions, slot)}
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div ref={viewport} onContextMenu={onContextMenu} className="relative min-h-0 flex-1 overflow-hidden p-2">
          <div ref={host} className="h-full w-full font-mono" />
        </div>
        {feed && v && find.open && (
          <FindBar
            feed={feed}
            v={v}
            state={find}
            onQuery={(query) => dispatchFind({ type: "query", query })}
            onToggleCase={() => dispatchFind({ type: "toggleCase" })}
            onClose={closeFind}
          />
        )}
        {(!v || !v.ready || status === "connecting") && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
            <Loading label={t("terminal-full.opening")} />
          </div>
        )}
        {v && v.ready && (canReconnect(status) || status === "unsupported" || status === "forbidden") && (
          <div className="absolute inset-x-0 bottom-4 z-10 flex justify-center px-3" style={{ animation: "fade-up 180ms ease-out both" }}>
            <div role="status" className="flex max-w-full items-center gap-2 rounded-control bg-surface py-1.5 pr-1.5 pl-2.5 text-[12px] text-ink-2 shadow-raised">
              <Icon name="info" size={13} className="shrink-0 text-ink-3" />
              <span className="min-w-0 flex-1 truncate">{v.note || STATUS_LABEL[status]}</span>
              {canReconnect(status) ? (
                <Button tone="primary" onClick={() => feed?.reconnect()}>
                  {t("terminal-full.reconnect")}
                </Button>
              ) : (
                <Button tone="quiet" onClick={onFallback}>
                  {t("terminal-full.back_to_pane")}
                </Button>
              )}
            </div>
          </div>
        )}
      </div>
      {feed && v && v.ready && keyBar && <KeyBar feed={feed} v={v} onEditShortcuts={() => setEditing(true)} />}
      {menu && (
        <Menu
          items={menuItems()}
          anchor={null}
          point={{ x: menu.x, y: menu.y }}
          onClose={() => {
            setMenu(null);
            setTimeout(refocus, 0);
          }}
          width={176}
        />
      )}
      <ShortcutsEditor
        open={editing}
        onClose={() => {
          setEditing(false);
          feed?.focus();
        }}
      />
    </div>
  );
}
