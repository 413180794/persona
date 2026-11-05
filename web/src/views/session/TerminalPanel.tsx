// A session's terminal in the side panel (docs/terminal.md, docs/web.md「侧边面板」), as iOS's terminal: the pane live
// over the hub's stream (polling the screen where it cannot stream), driven at the panel's size (接管) or watched at the
// pane's (只看). Watching, scrolling up reads herdr's history above the screen and 换行 folds long rows at the panel's
// width; driving, the key bar has the keys a phone's keyboard lacks. The logic is in terminal/feed.ts; xterm loads only
// when a terminal opens.
import { useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState, useSyncExternalStore } from "react";
import { sessionTitle } from "../../lib/format";
import { machineWithHub } from "../../lib/mesh";
import { getSession, useStore } from "../../lib/store";
import { lineCount } from "../../lib/terminal";
import { shouldRefocus, terminalMenu, wantsOwnMenu, type TerminalMenuKey } from "../../lib/terminal-details";
import { FIND_CLOSED, findReducer, isFindShortcut, queryFromSelection } from "../../lib/terminal-search";
import { Button, cx, Dialog, IconButton, Loading, Menu, Spinner, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { PanelHeader } from "../SidePanel";
import { DrawnRows, rowsStyle } from "./terminal/DrawnRows";
import { TerminalFeed, type FeedView } from "./terminal/feed";
import { FindBar } from "./terminal/FindBar";
import { focusFacts, selectionInside } from "./terminal/focus";
import { KeyBar } from "./terminal/KeyBar";
import { LinkStatus } from "./terminal/LinkStatus";
import { ShortcutsEditor } from "./terminal/Shortcuts";
import { TerminalOptions } from "./terminal/TerminalOptions";
import { t } from "../../i18n";

const KEYBAR_KEY = "jarvis.terminal.keyBar";

/** The key bar: as chosen in this browser, else up where the pointer is a finger (a phone's keyboard has no Esc / Ctrl). */
function savedKeyBar(): boolean {
  const v = localStorage.getItem(KEYBAR_KEY);
  return v === null ? matchMedia("(pointer: coarse)").matches : v === "1";
}

const nothing = () => () => {};

function useFeed(feed: TerminalFeed | null): FeedView | null {
  return useSyncExternalStore(feed?.subscribe ?? nothing, () => feed?.snapshot() ?? null);
}

export function TerminalPanel({
  mid,
  sid,
  label,
  cwd,
  onFiles,
  onClose,
}: {
  mid: string;
  sid: string;
  label?: string;
  /** The pane's working directory when the page knows it (else the session's): dropped Files entries are named relative to it. */
  cwd?: string;
  /** Opens the Files panel beside the terminal (the terminal page; as a side panel the Files panel would replace this one). */
  onFiles?: () => void;
  onClose: () => void;
}) {
  const session = useStore(() => getSession(mid, sid));
  const machine = useStore((s) => machineWithHub(mid, s.machines));
  const canDrive = useStore((s) => s.features.includes("terminal_control"));
  const host = useRef<HTMLDivElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const [feed, setFeed] = useState<TerminalFeed | null>(null);
  const v = useFeed(feed);
  const [keyBar, setKeyBar] = useState(savedKeyBar);
  const [options, setOptions] = useState<HTMLElement | null>(null);
  const [editing, setEditing] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const [find, dispatchFind] = useReducer(findReducer, FIND_CLOSED);
  const [menu, setMenu] = useState<{ x: number; y: number; selection: boolean } | null>(null);
  const atBottom = useRef(true);
  const lastTop = useRef(0);
  const lastHeight = useRef(0);
  const lastOlder = useRef(0);

  useEffect(() => {
    const f = new TerminalFeed(mid, sid);
    setFeed(f);
    void f.mount(host.current!, viewport.current!);
    return () => f.dispose();
  }, [mid, sid]);

  const dir = cwd || session?.cwd || "";
  useEffect(() => {
    if (feed) feed.cwd = dir;
  }, [feed, dir]);

  const subtitle = [label ?? (session ? sessionTitle(session) : ""), machine].filter(Boolean).join(" · ");
  const streaming = v?.mode === "stream";
  const takeBack = Boolean(v && streaming && v.wants && v.control === "declined" && v.why !== "unsupported");
  const driving = v?.control === "active" || v?.control === "starting";
  const drawn = Boolean(v?.screen);
  const older = v?.older ?? [];
  const showKeyBar = Boolean(feed && v && keyBar && streaming && canDrive && v.wants);

  // A page of history placed above keeps what is on screen where it was; the drawn screen follows its end unless the
  // user scrolled up.
  useLayoutEffect(() => {
    const el = viewport.current;
    if (!el) return;
    if (older.length > lastOlder.current) el.scrollTop += el.scrollHeight - lastHeight.current;
    else if (drawn && atBottom.current && !v?.paused) el.scrollTop = el.scrollHeight;
    lastOlder.current = older.length;
    lastHeight.current = el.scrollHeight;
    lastTop.current = el.scrollTop;
  }, [older, drawn, v?.screen, v?.paused]);

  const nearTop = () => {
    if (feed && v && !driving && v.loaded && v.hasOlder && !v.loadingOlder && !v.offline) void feed.loadOlder();
  };
  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 8;
    if (el.scrollTop < lastTop.current && el.scrollTop < 24) nearTop(); // scrolled up to the top
    lastTop.current = el.scrollTop;
  };
  const onWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    if (e.deltaY < 0 && e.currentTarget.scrollTop <= 0) nearTop(); // nothing to scroll: the wheel still asks for more
  };
  const backToLatest = () => {
    atBottom.current = true;
    feed?.resume();
  };
  const chooseKeyBar = (on: boolean) => {
    localStorage.setItem(KEYBAR_KEY, on ? "1" : "0");
    setKeyBar(on);
  };

  // ── focus: driving, the keyboard goes back to the terminal after a click in the panel and when the window returns ──
  const refocus = useCallback(() => {
    const el = root.current;
    if (el && feed && shouldRefocus(focusFacts(el, feed.driving, feed.hasSelection()))) feed.focus();
  }, [feed]);
  useEffect(() => {
    addEventListener("focus", refocus);
    return () => removeEventListener("focus", refocus);
  }, [refocus]);

  // ── find (lib/terminal-search.ts): ⌘F / Ctrl+Shift+F in the panel, or 搜索… in the right-click menu ──
  const openFind = () => {
    const picked = feed?.selectedText() || (viewport.current && selectionInside(viewport.current) ? (window.getSelection()?.toString() ?? "") : "");
    dispatchFind({ type: "open", query: queryFromSelection(picked) ?? undefined });
  };
  const closeFind = () => {
    dispatchFind({ type: "close" });
    feed?.clearFind();
    if (feed?.driving) feed.focus();
  };
  const onKeyDownCapture = (e: React.KeyboardEvent) => {
    if (!isFindShortcut(e.nativeEvent, { mac: /Mac|iPhone|iPad/.test(navigator.platform), driving: Boolean(feed?.driving) })) return;
    e.preventDefault(); // not the browser's own find
    e.stopPropagation(); // nor xterm's: the program does not get the key
    openFind();
  };
  // The current match is brought into view: a marked row of the page's, or xterm's screen under the history.
  const current = v?.marks?.current ?? null;
  useEffect(() => {
    if (current === null) return;
    viewport.current?.querySelector('[data-find="current"]')?.scrollIntoView({ block: "center" });
  }, [current]);
  const screenMatch = v?.find && v.find.index >= 0 && current === null ? v.find.index : -1;
  useEffect(() => {
    if (screenMatch >= 0) host.current?.scrollIntoView({ block: "nearest" });
  }, [screenMatch]);

  // ── right-click menu ──
  const onContextMenu = (e: React.MouseEvent) => {
    if (!wantsOwnMenu({ shiftKey: e.shiftKey, pointerType: (e.nativeEvent as PointerEvent).pointerType })) return;
    e.preventDefault();
    const own = Boolean(viewport.current && selectionInside(viewport.current));
    // xterm stops the keys it has focus for (Esc would go to the program): the menu takes them while it is open
    (document.activeElement as HTMLElement | null)?.blur();
    setMenu({ x: e.clientX, y: e.clientY, selection: Boolean(feed?.hasSelection()) || own });
  };
  const closeMenu = useCallback(() => setMenu(null), []);
  const menuItems = (): MenuItem[] => {
    const icons: Record<TerminalMenuKey, MenuItem["icon"]> = { copy: "copy", paste: "paste", selectAll: "list", search: "search", clearSelection: "x" };
    const run: Record<TerminalMenuKey, () => void> = {
      copy: () => {
        if (feed?.hasSelection()) feed.copySelection();
        else feed?.copyText(window.getSelection()?.toString() ?? "");
      },
      paste: () => void feed?.pasteClipboard(),
      selectAll: () => {
        if (drawn && viewport.current) window.getSelection()?.selectAllChildren(viewport.current);
        else feed?.selectAll();
      },
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

  return (
    <div
      ref={root}
      onKeyDownCapture={onKeyDownCapture}
      onClick={(e) => !(e.target as Element).closest('[role="search"]') && setTimeout(refocus, 0)} // the find bar keeps its own focus
      tabIndex={-1} // a click anywhere in the panel leaves the focus in it, so ⌘F finds in the terminal (watching, nothing else takes it)
      className="@container flex min-h-0 flex-1 flex-col outline-none"
    >
      <PanelHeader
        icon="terminal"
        title={t("session-terminalpanel.title")}
        subtitle={subtitle}
        onClose={onClose}
        actions={
          <>
            {v && <SizeLabel v={v} />}
            {feed && v && canDrive && streaming && <ModeSwitch drive={v.wants} onChange={(on) => feed.setWants(on)} />}
            {feed && v && !driving && (
              <IconButton
                icon="wrap"
                label={v.wrap ? t("session-terminalpanel.wrap_on") : t("session-terminalpanel.wrap_off")}
                active={v.wrap}
                onClick={() => feed.setWrap(!v.wrap)}
              />
            )}
            {onFiles && dir && <IconButton icon="folder" label={t("session-terminalpanel.files")} onClick={onFiles} className="max-lg:hidden" />}
            <IconButton
              icon="gear"
              label={t("session-terminalpanel.settings")}
              active={Boolean(options)}
              disabled={!feed}
              onClick={(e) => {
                const target = e.currentTarget;
                setOptions((o) => (o ? null : target));
              }}
            />
            <IconButton icon="retry" label={t("session-terminalpanel.reconnect")} onClick={() => feed?.retry()} disabled={!feed} />
          </>
        }
      />
      {feed && v && !driving && v.loaded && !v.offline && <HistoryStatus v={v} onLoad={() => void feed.loadOlder()} />}
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={viewport}
          onScroll={onScroll}
          onWheel={onWheel}
          onContextMenu={onContextMenu}
          className={cx(
            "relative min-h-0 flex-1 p-2 transition-[opacity,filter] duration-300 [overflow-anchor:none]",
            drawn ? "overflow-x-hidden overflow-y-auto" : v?.pinned || older.length ? "overflow-auto" : "overflow-hidden",
            v?.stale && v.loaded && "opacity-45 grayscale",
            v?.dropping && "ring-2 ring-inset ring-accent",
          )}
        >
          {v && older.length > 0 && (
            <div className={cx("font-mono", !drawn && "w-max min-w-full")} style={rowsStyle(v.palette, v.fontFamily, v.fontSize)}>
              <DrawnRows rows={older} fold={drawn} fromEnd marks={v.marks} />
            </div>
          )}
          {v?.screen && (
            <div className="font-mono" style={rowsStyle(v.palette, v.fontFamily, v.fontSize)}>
              <DrawnRows rows={v.screen.rows} fold cursor={v.screen.cursor} marks={v.marks} offset={older.length} />
            </div>
          )}
          <div
            ref={host}
            aria-hidden={drawn || undefined}
            className={cx("w-full font-mono", drawn ? "pointer-events-none invisible absolute inset-2 overflow-hidden" : "h-full")}
          />
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
        {feed && v && (
          <div className="pointer-events-none absolute inset-x-2 top-2 z-10 flex flex-col gap-1.5">
            {v.stale && <LinkStatus feed={feed} />}
            {v.uploading && (
              <div
                role="status"
                className="flex items-center gap-2 rounded-control bg-surface px-2.5 py-1.5 text-[12px] text-ink-2 shadow-raised"
                style={{ animation: "fade-in 160ms ease-out both" }}
              >
                <Spinner size={12} />
                <span className="min-w-0 flex-1 truncate">{v.uploading}</span>
              </div>
            )}
            {takeBack && (
              <div className="pointer-events-auto flex items-center gap-2 rounded-control bg-surface py-1.5 pr-1.5 pl-2.5 text-[12px] text-ink-2 shadow-raised" style={{ animation: "fade-in 160ms ease-out both" }}>
                <Icon name="info" size={13} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1 truncate">{v.why === "taken" ? t("session-terminalpanel.taken_elsewhere") : v.note}</span>
                <button
                  type="button"
                  onClick={() => feed.takeOver()}
                  className="h-6 shrink-0 whitespace-nowrap rounded-chip bg-ink px-2.5 text-[12px] font-medium text-surface transition-transform duration-150 active:scale-[0.96]"
                >
                  {t("session-terminalpanel.take_control")}
                </button>
              </div>
            )}
          </div>
        )}
        {v?.paused && (
          <div className="pointer-events-none absolute inset-x-0 bottom-3 z-10 flex justify-center">
            <button
              type="button"
              onClick={backToLatest}
              className="pointer-events-auto inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-full bg-surface px-3 text-[12px] font-medium text-ink shadow-raised transition-transform duration-150 active:scale-[0.96]"
              style={{ animation: "fade-up 180ms ease-out both" }}
            >
              <Icon name="arrowDown" size={13} />
              {t("session-terminalpanel.back_to_latest")}
            </button>
          </div>
        )}
        {(!v || !v.loaded) && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
            <Loading label={t("session-terminalpanel.connecting")} />
          </div>
        )}
      </div>
      {showKeyBar && feed && v && <KeyBar feed={feed} v={v} onEditShortcuts={() => setEditing(true)} />}
      {options && feed && v && (
        <TerminalOptions
          anchor={options}
          feed={feed}
          v={v}
          keyBar={keyBar}
          onKeyBar={chooseKeyBar}
          onEditShortcuts={() => setEditing(true)}
          onClose={() => setOptions(null)}
        />
      )}
      {menu && (
        <Menu
          items={menuItems()}
          anchor={null}
          point={{ x: menu.x, y: menu.y }}
          onClose={() => {
            closeMenu();
            setTimeout(refocus, 0); // after Esc or a choice: back to the terminal, unless the choice went elsewhere (the find field)
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

      <Dialog
        open={v?.control === "busy"}
        onClose={() => feed?.watchOnly()}
        title={t("session-terminalpanel.busy_title")}
        footer={
          <>
            <Button tone="quiet" onClick={() => feed?.watchOnly()}>
              {t("session-terminalpanel.watch_only")}
            </Button>
            <Button tone="primary" onClick={() => feed?.takeOver()}>
              {t("session-terminalpanel.take_control")}
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">{t("session-terminalpanel.busy_detail")}</p>
      </Dialog>

      <Dialog
        open={Boolean(v?.pasting)}
        onClose={() => feed?.confirmPaste(false)}
        title={t("session-terminalpanel.paste_title", { count: v?.pasting ? lineCount(v.pasting) : 0 })}
        footer={
          <>
            <Button tone="quiet" onClick={() => feed?.confirmPaste(false)}>
              {t("common.cancel")}
            </Button>
            <Button tone="primary" onClick={() => feed?.confirmPaste(true)}>
              {t("session-terminalpanel.paste")}
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">{t("session-terminalpanel.paste_detail")}</p>
      </Dialog>
    </div>
  );
}

/**
 * Over the terminal while watching (iOS historyStatus): scrolling up reads the page before (or a click does), and
 * where history ends.
 */
function HistoryStatus({ v, onLoad }: { v: FeedView; onLoad: () => void }) {
  const cls = "flex h-7 shrink-0 items-center justify-center gap-1.5 overflow-hidden px-2 whitespace-nowrap text-[11.5px] text-ink-3";
  if (v.loadingOlder)
    return (
      <div className={cls}>
        <Spinner size={11} />
        {t("session-terminalpanel.loading_older")}
      </div>
    );
  if (v.atLimit) return <div className={cls}>{t("session-terminalpanel.history_limit")}</div>;
  if (!v.hasOlder) return <div className={cls}>{t("session-terminalpanel.history_start")}</div>;
  return (
    <div className={cls}>
      <button type="button" onClick={onLoad} className="inline-flex h-6 items-center gap-1 rounded-chip px-2 hover:bg-hover hover:text-ink-2">
        <Icon name="up" size={12} />
        {t("session-terminalpanel.load_older")}
      </button>
    </div>
  );
}

/** 接管 120×40 while driving, 只看 172×45 while watching the pane (只看 alone while reading the screen). */
function SizeLabel({ v }: { v: FeedView }) {
  if (!v.ready || !v.loaded) return null;
  const size = v.cols && v.rows ? `${v.cols}×${v.rows}` : "";
  const text =
    v.mode === "poll" ? t("session-terminalpanel.size_watch_only") : v.control === "active" ? t("session-terminalpanel.size_control", { size }).trim() : v.control === "starting" ? t("session-terminalpanel.size_starting") : t("session-terminalpanel.size_watch", { size }).trim();
  const title =
    v.mode === "poll"
      ? t("session-terminalpanel.size_poll_hint")
      : v.control === "active"
        ? t("session-terminalpanel.size_control_hint")
        : t("session-terminalpanel.size_watch_hint");
  return (
    <span title={title} className="hidden shrink-0 whitespace-nowrap font-mono text-[11.5px] text-ink-3 tabular-nums @md:inline">
      {text}
    </span>
  );
}

/** 接管 (the pane takes the panel's size, keys go in) or 只看 (the pane as the computer has it). */
function ModeSwitch({ drive, onChange }: { drive: boolean; onChange: (drive: boolean) => void }) {
  const options: [string, boolean, string][] = [
    [t("session-terminalpanel.mode_control"), true, t("session-terminalpanel.mode_control_hint")],
    [t("session-terminalpanel.mode_watch"), false, t("session-terminalpanel.mode_watch_hint")],
  ];
  return (
    <div role="radiogroup" aria-label={t("session-terminalpanel.mode_label")} className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
      {options.map(([label, value, title]) => (
        <button
          key={label}
          type="button"
          role="radio"
          aria-checked={drive === value}
          title={title}
          onClick={() => onChange(value)}
          className={cx(
            "h-6 whitespace-nowrap rounded-chip px-2 text-[12px] font-medium transition-[background-color,color,box-shadow] duration-150",
            drive === value ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
