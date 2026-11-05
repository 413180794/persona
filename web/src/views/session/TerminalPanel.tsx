// A session's terminal in the side panel (docs/terminal.md, docs/web.md「侧边面板」), as iOS's terminal: the pane live
// over the hub's stream (polling the screen where it cannot stream), driven at the panel's size (接管) or watched at the
// pane's (只看). Watching, scrolling up reads herdr's history above the screen and 换行 folds long rows at the panel's
// width; driving, the key bar has the keys a phone's keyboard lacks. The logic is in terminal/feed.ts; xterm loads only
// when a terminal opens.
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { sessionTitle } from "../../lib/format";
import { machineWithHub } from "../../lib/mesh";
import { getSession, useStore } from "../../lib/store";
import { lineCount } from "../../lib/terminal";
import { Button, cx, Dialog, IconButton, Loading, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { PanelHeader } from "../SidePanel";
import { DrawnRows, rowsStyle } from "./terminal/DrawnRows";
import { TerminalFeed, type FeedView } from "./terminal/feed";
import { KeyBar } from "./terminal/KeyBar";
import { LinkStatus } from "./terminal/LinkStatus";
import { ShortcutsEditor } from "./terminal/Shortcuts";
import { TerminalOptions } from "./terminal/TerminalOptions";

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

export function TerminalPanel({ mid, sid, label, onClose }: { mid: string; sid: string; label?: string; onClose: () => void }) {
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

  return (
    <div className="@container flex min-h-0 flex-1 flex-col">
      <PanelHeader
        icon="terminal"
        title="终端"
        subtitle={subtitle}
        onClose={onClose}
        actions={
          <>
            {v && <SizeLabel v={v} />}
            {feed && v && canDrive && streaming && <ModeSwitch drive={v.wants} onChange={(on) => feed.setWants(on)} />}
            {feed && v && !driving && (
              <IconButton
                icon="wrap"
                label={v.wrap ? "长行换行：开（按面板宽度折行）" : "长行换行：关（按电脑上的宽度左右滚动）"}
                active={v.wrap}
                onClick={() => feed.setWrap(!v.wrap)}
              />
            )}
            <IconButton
              icon="gear"
              label="终端设置"
              active={Boolean(options)}
              disabled={!feed}
              onClick={(e) => {
                const target = e.currentTarget;
                setOptions((o) => (o ? null : target));
              }}
            />
            <IconButton icon="retry" label="重新连接" onClick={() => feed?.retry()} disabled={!feed} />
          </>
        }
      />
      {feed && v && !driving && v.loaded && !v.offline && <HistoryStatus v={v} onLoad={() => void feed.loadOlder()} />}
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={viewport}
          onScroll={onScroll}
          onWheel={onWheel}
          className={cx(
            "relative min-h-0 flex-1 p-2 transition-[opacity,filter] duration-300 [overflow-anchor:none]",
            drawn ? "overflow-x-hidden overflow-y-auto" : v?.pinned || older.length ? "overflow-auto" : "overflow-hidden",
            v?.stale && v.loaded && "opacity-45 grayscale",
            v?.dropping && "ring-2 ring-inset ring-accent",
          )}
        >
          {v && older.length > 0 && (
            <div className={cx("font-mono", !drawn && "w-max min-w-full")} style={rowsStyle(v.palette, v.fontFamily, v.fontSize)}>
              <DrawnRows rows={older} fold={drawn} fromEnd />
            </div>
          )}
          {v?.screen && (
            <div className="font-mono" style={rowsStyle(v.palette, v.fontFamily, v.fontSize)}>
              <DrawnRows rows={v.screen.rows} fold cursor={v.screen.cursor} />
            </div>
          )}
          <div
            ref={host}
            aria-hidden={drawn || undefined}
            className={cx("w-full font-mono", drawn ? "pointer-events-none invisible absolute inset-2 overflow-hidden" : "h-full")}
          />
        </div>
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
                <span className="min-w-0 flex-1 truncate">{v.why === "taken" ? "被别处接管，现在只能看" : v.note}</span>
                <button
                  type="button"
                  onClick={() => feed.takeOver()}
                  className="h-6 shrink-0 whitespace-nowrap rounded-chip bg-ink px-2.5 text-[12px] font-medium text-surface transition-transform duration-150 active:scale-[0.96]"
                >
                  接管
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
              回到最新
            </button>
          </div>
        )}
        {(!v || !v.loaded) && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
            <Loading label="正在连接终端" />
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
        title="电脑上另一个程序正在使用这个终端"
        footer={
          <>
            <Button tone="quiet" onClick={() => feed?.watchOnly()}>
              只看
            </Button>
            <Button tone="primary" onClick={() => feed?.takeOver()}>
              接管
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">比如另一个 herdr 客户端。接管后它会断开，终端改成这个面板的尺寸；关掉面板时窗格回到电脑上的尺寸。</p>
      </Dialog>

      <Dialog
        open={Boolean(v?.pasting)}
        onClose={() => feed?.confirmPaste(false)}
        title={`粘贴 ${v?.pasting ? lineCount(v.pasting) : 0} 行到终端？`}
        footer={
          <>
            <Button tone="quiet" onClick={() => feed?.confirmPaste(false)}>
              取消
            </Button>
            <Button tone="primary" onClick={() => feed?.confirmPaste(true)}>
              粘贴
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">多行文字在 shell 里可能被逐行执行。</p>
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
        加载更早的输出…
      </div>
    );
  if (v.atLimit) return <div className={cls}>— herdr 只保留最近约 1000 行，更早的过程在「对话」里 —</div>;
  if (!v.hasOlder) return <div className={cls}>— 已到最前面 —</div>;
  return (
    <div className={cls}>
      <button type="button" onClick={onLoad} className="inline-flex h-6 items-center gap-1 rounded-chip px-2 hover:bg-hover hover:text-ink-2">
        <Icon name="up" size={12} />
        往上滚动加载更早的输出
      </button>
    </div>
  );
}

/** 接管 120×40 while driving, 只看 172×45 while watching the pane (只看 alone while reading the screen). */
function SizeLabel({ v }: { v: FeedView }) {
  if (!v.ready || !v.loaded) return null;
  const size = v.cols && v.rows ? `${v.cols}×${v.rows}` : "";
  const text =
    v.mode === "poll" ? "只看" : v.control === "active" ? `接管 ${size}` : v.control === "starting" ? "接管中…" : `只看 ${size}`.trim();
  const title =
    v.mode === "poll"
      ? "这台电脑不能推送终端，每秒读一次屏幕"
      : v.control === "active"
        ? "电脑上的窗格现在是这个面板的行列数"
        : "电脑上窗格本身的行列数";
  return (
    <span title={title} className="hidden shrink-0 whitespace-nowrap font-mono text-[11.5px] text-ink-3 tabular-nums @md:inline">
      {text}
    </span>
  );
}

/** 接管 (the pane takes the panel's size, keys go in) or 只看 (the pane as the computer has it). */
function ModeSwitch({ drive, onChange }: { drive: boolean; onChange: (drive: boolean) => void }) {
  const options: [string, boolean, string][] = [
    ["接管", true, "按这个面板的大小接管终端，键盘直接输入"],
    ["只看", false, "不改电脑上的窗格，只看"],
  ];
  return (
    <div role="radiogroup" aria-label="接管或只看" className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
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
