import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { STATE_LABEL } from "../lib/format";
import { t } from "../i18n";
import type { AgentState } from "../lib/types";
import { Icon, type IconName } from "./icons";
import { StatusMark } from "./StatusMark";
import { collapseMounted } from "../lib/disclosure";
import { FeedbackGate, offlineNow } from "../lib/feedback";
import { useCloseOnBack } from "../lib/back-close";
import { BUSY_MOTION } from "../lib/busy-motion";
import { keyboardFrame } from "../lib/viewport";
import { accelLabel, activation, edge, enabled, keyIntent, renameCommit, rowLabel, type MenuItem } from "./menu-model";
import { focusables, NARROW_QUERY, placeMenu, placeSubmenu, SHEET_QUERY, trapTab, useMedia, type MenuPlace } from "./overlay";

export const EASE = "cubic-bezier(0.23, 1, 0.32, 1)";

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(" ");
}

// A light sweep across a label: work in progress.
export function Shimmer({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={`bg-clip-text text-transparent ${BUSY_MOTION} ${className}`}
      style={{
        backgroundImage: "linear-gradient(90deg, var(--ink-3) 35%, var(--ink) 50%, var(--ink-3) 65%)",
        backgroundSize: "200% 100%",
        animation: "shimmer-text 1.4s linear infinite",
      }}
    >
      {children}
    </span>
  );
}

const CHEVRON = Array.from({ length: 9 }, (_, i) => {
  const r = Math.floor(i / 3),
    c = i % 3;
  return (c + Math.abs(r - 1)) * 90;
});

// Beautiful UI's pixel-grid loader (the "Drive" variant).
export function PixelGrid({ className = "" }: { className?: string }) {
  return (
    <span aria-hidden className={`grid shrink-0 grid-cols-[repeat(3,4px)] gap-[1.5px] ${className}`}>
      {CHEVRON.map((d, i) => (
        <span key={i} className={`size-[4px] rounded-[1px] bg-ink ${BUSY_MOTION}`} style={{ opacity: 0.15, animation: `pixel-on 650ms ease-in-out ${d}ms infinite` }} />
      ))}
    </span>
  );
}

export function useNow(every = 1000, on = true) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!on) return;
    const t = setInterval(() => setNow(Date.now()), every);
    return () => clearInterval(t);
  }, [every, on]);
  return now;
}

export function Loading({ label = t("common.loading") }: { label?: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <PixelGrid />
      <Shimmer className="text-[13px] font-medium">{label}</Shimmer>
    </div>
  );
}

export function StatePill({ state }: { state: AgentState }) {
  const tone: Record<AgentState, string> = {
    needs_you: "bg-red-tint text-red",
    working: "bg-orange-tint text-orange",
    unread: "bg-green-tint text-green",
    idle: "bg-field text-ink-2",
    offline: "bg-field text-ink-3",
    unknown: "bg-field text-ink-3",
  };
  return (
    <span className={`inline-flex h-5 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-[11px] font-medium ${tone[state]}`}>
      {(state === "working" || state === "needs_you") && <StatusMark state={state} size="sm" label={false} />}
      {STATE_LABEL[state]}
    </span>
  );
}

export function IconButton({
  icon,
  label,
  onClick,
  active,
  size = 28,
  className = "",
  disabled,
  iconSize = 15,
}: {
  icon: IconName;
  label: string;
  onClick?: (e: React.MouseEvent<HTMLElement>) => void;
  active?: boolean;
  size?: number;
  className?: string;
  disabled?: boolean;
  iconSize?: number;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className={cx(
        "touch-hit flex shrink-0 items-center justify-center rounded-[8px] transition-[background-color,color,transform] duration-150 enabled:active:scale-[0.94] disabled:opacity-40",
        active ? "bg-hover text-ink" : "text-ink-3 enabled:hover:bg-hover enabled:hover:text-ink",
        className,
      )}
      style={{ width: size, height: size }}
    >
      <Icon name={icon} size={iconSize} />
    </button>
  );
}

type BtnTone = "primary" | "secondary" | "quiet" | "danger" | "accent";

export function Button({
  children,
  tone = "secondary",
  onClick,
  disabled,
  type = "button",
  className = "",
  icon,
  busy,
  title,
  href,
}: {
  children: React.ReactNode;
  tone?: BtnTone;
  onClick?: () => void;
  disabled?: boolean;
  type?: "button" | "submit";
  className?: string;
  icon?: IconName;
  busy?: boolean;
  title?: string;
  /** Opens this in a new tab, as a link (noopener, noreferrer: the page gets no handle on this one, nor its address). */
  href?: string;
}) {
  const base = "inline-flex h-8 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-control px-3 text-[12.5px] font-medium transition-[background-color,color,transform,opacity,filter] duration-150";
  if (href && !disabled && !busy) {
    // an <a> is never :enabled, so the link has its own hover / press
    const links: Record<BtnTone, string> = {
      primary: "bg-ink text-surface shadow-[inset_0_1px_0_rgba(255,255,255,0.14)] hover:opacity-90",
      accent: "bg-accent text-white hover:brightness-105",
      secondary: "bg-surface text-ink shadow-btn hover:bg-hover",
      quiet: "text-ink-2 hover:bg-hover hover:text-ink",
      danger: "bg-red text-white hover:brightness-105",
    };
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" title={title} onClick={onClick} className={cx(base, "active:scale-[0.96]", links[tone], className)}>
        {icon ? <Icon name={icon} size={14} stroke={2.1} /> : null}
        {children}
      </a>
    );
  }
  const tones: Record<BtnTone, string> = {
    primary: "bg-ink text-surface shadow-[inset_0_1px_0_rgba(255,255,255,0.14)] enabled:hover:opacity-90",
    accent: "bg-accent text-white enabled:hover:brightness-105",
    secondary: "bg-surface text-ink shadow-btn enabled:hover:bg-hover",
    quiet: "text-ink-2 enabled:hover:bg-hover enabled:hover:text-ink",
    danger: "bg-red text-white enabled:hover:brightness-105",
  };
  return (
    <button
      type={type}
      title={title}
      disabled={disabled || busy}
      onClick={onClick}
      className={cx(
        base,
        "enabled:active:scale-[0.96] disabled:opacity-45",
        tones[tone],
        className,
      )}
    >
      {busy ? <Spinner /> : icon ? <Icon name={icon} size={14} stroke={2.1} /> : null}
      {children}
    </button>
  );
}

export function Spinner({ size = 12 }: { size?: number }) {
  return (
    <span
      aria-hidden
      className={`inline-block shrink-0 rounded-full border-[1.5px] border-line-strong border-t-ink-2 ${BUSY_MOTION}`}
      style={{ width: size, height: size, animation: "spin 700ms linear infinite" }}
    />
  );
}

export function Kbd({ children }: { children: React.ReactNode }) {
  return <kbd className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-[5px] bg-surface px-1 font-sans text-[10px] text-ink-3 shadow-hairline">{children}</kbd>;
}

// Height-animating disclosure (grid 0fr → 1fr), the expand grammar every Beautiful UI trace uses.
/** Folds its children up and down: the grid's row runs between 0fr and 1fr in CSS (no heights measured in JS), with
 *  a fade. `lazy`: the children mount when it opens and stay while it closes, so they go down with the height instead of
 *  vanishing, then unmount (a long trace costs nothing while folded). Once closed, the content is inert. A chat list holds
 *  the clicked row still while the height runs (`anchoredScroll`, #1124). */
export function Collapse({ open, children, className = "", lazy = false }: { open: boolean; children: React.ReactNode; className?: string; lazy?: boolean }) {
  const [closing, setClosing] = useState(false);
  const [moving, setMoving] = useState(false);
  // A lazy one's new content is laid out at 0fr first and the row opens a frame later: a long trace's first layout
  // would otherwise land inside the transition, which then starts most of the way open.
  const [laidOut, setLaidOut] = useState(open || !lazy);
  const [was, setWas] = useState(open);
  if (was !== open) {
    // adjusted while rendering: the first frame of the change already has the content (opening) or keeps it (closing)
    setWas(open);
    setMoving(true);
    setClosing(!open);
    if (open && lazy && !closing) setLaidOut(false);
  }
  const settle = () => {
    setMoving(false);
    setClosing(false);
  };
  useEffect(() => {
    if (!moving) return;
    const t = setTimeout(settle, 450); // transitionend may not come (hidden tab, interrupted, no change in height)
    return () => clearTimeout(t);
  }, [moving, open]);
  useEffect(() => {
    if (!open || laidOut) return;
    const f = requestAnimationFrame(() => setLaidOut(true));
    return () => cancelAnimationFrame(f);
  }, [open, laidOut]);
  const shown = open && laidOut;
  return (
    <div
      className={`grid transition-[grid-template-rows,opacity] duration-300 ${className}`}
      style={{ gridTemplateRows: shown ? "1fr" : "0fr", opacity: shown ? 1 : 0, transitionTimingFunction: EASE, willChange: moving ? "opacity" : undefined }}
      onTransitionEnd={(e) => e.target === e.currentTarget && e.propertyName === "grid-template-rows" && settle()}
      // inert once closed, not while it closes: switching a long trace inert is a full style pass (34 ms for 650 steps)
      inert={(!open && !moving) || undefined}
    >
      <div className="min-h-0 overflow-hidden [contain:layout_paint]">{collapseMounted(lazy, open, closing) && children}</div>
    </div>
  );
}

// ── popover menu with a gliding highlight ───────────────

export type { MenuItem };

/** How long a row's 「确认关闭」 waits for the second choice before it asks no more. */
const CONFIRM_MS = 4000;

const errMessage = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** One open submenu: its rows (loaded when they come from a function), the row that opened it and where that row is. */
interface Sub {
  from: number;
  items: MenuItem[];
  rect: { left: number; right: number; top: number; bottom: number };
  loading?: boolean;
  error?: string;
}

/** A menu next to its anchor (a button, or a row's ⋯), or at the pointer (`point`, a right click). ↑ ↓ move, → opens a
 *  submenu and ← goes back, Enter chooses, Esc closes, a row's letter chooses it, ⌫ chooses the close action. A row
 *  can ask again before it runs (`confirm`), turn into an input (`rename`), open a submenu (`children`) or show its
 *  work and failure inside the menu (`inline`). A menu without those keys is the plain one it always was. */
export function Menu({
  items,
  anchor,
  point,
  onClose,
  align = "left",
  side = "bottom",
  width = 208,
  header,
}: {
  items: MenuItem[];
  anchor: HTMLElement | null;
  /** Opens at this pointer position instead of next to `anchor`. */
  point?: { x: number; y: number };
  onClose: () => void;
  align?: "left" | "right";
  side?: "top" | "bottom";
  width?: number;
  header?: string;
}) {
  const id = useId();
  const [subs, setSubs] = useState<Sub[]>([]); // the open submenus: depth d (d ≥ 1) is subs[d - 1]
  const [actives, setActives] = useState<number[]>([-1]); // the highlighted row of each level
  const [confirming, setConfirming] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<{ depth: number; text: string } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const asking = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const loads = useRef(0);
  const rows = useRef<(HTMLElement | null)[][]>([]);

  const levels = [items, ...subs.map((s) => s.items)];
  // the key handler is registered once and reads this: what the menu is now
  const live = useRef({ levels, actives, subs, confirming, renaming, busy });
  live.current = { levels, actives, subs, confirming, renaming, busy };

  const setActive = (depth: number, index: number) => {
    setActives((a) => {
      const next = a.slice(0, depth + 1);
      for (let i = next.length; i < depth; i++) next[i] = -1;
      next[depth] = index;
      return next;
    });
    const it = live.current.levels[depth]?.[index];
    // moving to another row takes the question back; the pointer leaving (-1) does not: a finger lifting off sends that
    if (index >= 0 && it?.key !== live.current.confirming) setConfirming(null);
  };
  const closeSubsTo = (depth: number) => {
    setSubs((s) => (s.length > depth ? s.slice(0, depth) : s));
    setActives((a) => (a.length > depth + 1 ? a.slice(0, depth + 1) : a));
  };
  const openSub = (depth: number, index: number, focus: boolean) => {
    const { levels: ls, subs: open } = live.current;
    const it = ls[depth]?.[index];
    const el = rows.current[depth]?.[index];
    if (!it?.children || !el) return;
    if (open[depth]?.from === index) {
      if (focus) setActives((a) => [...a.slice(0, depth + 1), a[depth + 1] >= 0 ? a[depth + 1] : edge(open[depth].items, false)]);
      return;
    }
    const r = el.getBoundingClientRect();
    const rect = { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
    const token = ++loads.current;
    const put = (sub: Sub) => {
      setSubs((s) => [...s.slice(0, depth), sub]);
      setActives((a) => [...a.slice(0, depth + 1), focus ? edge(sub.items, false) : -1]);
    };
    setFailure(null);
    if (typeof it.children !== "function") return put({ from: index, items: it.children, rect });
    put({ from: index, items: [], rect, loading: true });
    const settle = (patch: Partial<Sub>) => {
      if (loads.current !== token) return; // another row's submenu has replaced it since
      setSubs((s) => s.map((x, d) => (d === depth ? { ...x, ...patch, loading: false } : x)));
      if (focus && patch.items) setActives((a) => [...a.slice(0, depth + 1), edge(patch.items!, false)]);
    };
    (it.children as () => Promise<MenuItem[]>)().then((list) => settle({ items: list }), (e) => settle({ error: errMessage(e) }));
  };

  const run = async (depth: number, it: MenuItem) => {
    setConfirming(null);
    setFailure(null);
    if (!it.inline) {
      void it.onSelect();
      if (!it.submenu && !it.keepOpen) onClose();
      return;
    }
    setBusy(it.key);
    try {
      await it.onSelect();
      onClose();
    } catch (e) {
      setFailure({ depth, text: errMessage(e) });
      setBusy(null);
    }
  };
  const choose = (depth: number, index: number, focus: boolean) => {
    const { levels: ls, confirming: asked, busy: working } = live.current;
    const it = ls[depth]?.[index];
    if (working) return;
    switch (activation(it, asked)) {
      case "children":
        return openSub(depth, index, focus);
      case "rename":
        setFailure(null);
        return setRenaming(it!.key);
      case "confirm":
        setFailure(null);
        setConfirming(it!.key);
        clearTimeout(asking.current);
        asking.current = setTimeout(() => setConfirming(null), CONFIRM_MS); // the question lapses if nobody answers
        return;
      case "run":
        return void run(depth, it!);
    }
  };
  const commitRename = async (depth: number, it: MenuItem, name: string) => {
    setBusy(it.key);
    setFailure(null);
    try {
      await it.rename!.commit(name);
      onClose();
    } catch (e) {
      setFailure({ depth, text: errMessage(e) });
      setBusy(null);
    }
  };
  const hover = (depth: number, index: number) => {
    clearTimeout(timer.current);
    const { levels: ls, subs: open } = live.current;
    const it = ls[depth]?.[index];
    if (!enabled(it)) return;
    setActive(depth, index);
    // a submenu opens (and the one beside it closes) a moment after the pointer settles on a row, so a pointer
    // crossing rows on its way to the submenu does not swap it
    if (it.children) timer.current = setTimeout(() => openSub(depth, index, false), 100);
    else if (open.length > depth) timer.current = setTimeout(() => closeSubsTo(depth), 100);
  };
  useEffect(() => () => {
    clearTimeout(timer.current);
    clearTimeout(asking.current);
  }, []);

  useEffect(() => {
    // pointerdown, not mousedown: lifting the finger after a long press that opened the menu sends a compatibility
    // mousedown at the finger, outside the menu, which closed it at once. A tap outside still closes it.
    const down = (e: PointerEvent) => {
      const t = e.target as Element | null;
      if (t?.closest?.(`[data-menu="${CSS.escape(id)}"]`) || anchor?.contains(t as Node)) return;
      onClose();
    };
    const key = (e: KeyboardEvent) => {
      const { levels: ls, actives: act, subs: open, renaming: renamed } = live.current;
      if (e.isComposing || renamed) return; // an input in the menu takes its own keys
      const t = e.target as HTMLElement | null;
      const typing = Boolean(t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable));
      // a field elsewhere on the page keeps its letters and its Backspace
      if (typing && (e.key.length === 1 || e.key === "Backspace" || e.key === "Delete")) return;
      const depth = open.length;
      const intent = keyIntent(e, ls[depth], act[depth] ?? -1, depth);
      if (intent.type === "none") return;
      e.preventDefault();
      e.stopPropagation();
      if (intent.type === "close") onClose();
      else if (intent.type === "back") closeSubsTo(depth - 1);
      else if (intent.type === "move") setActive(depth, intent.index);
      else choose(depth, intent.index, true);
    };
    addEventListener("pointerdown", down);
    addEventListener("keydown", key, true);
    return () => {
      removeEventListener("pointerdown", down);
      removeEventListener("keydown", key, true);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the handlers read `live`; only the menu's anchor and close change them
  }, [anchor, onClose, id]);

  const panelProps = (depth: number) => ({
    id,
    depth,
    items: levels[depth],
    active: actives[depth] ?? -1,
    confirming,
    renaming,
    busy,
    failure: failure?.depth === depth ? failure.text : undefined,
    rows: (list: (HTMLElement | null)[]) => {
      rows.current[depth] = list;
    },
    onHover: (i: number) => hover(depth, i),
    onEnter: () => clearTimeout(timer.current),
    onLeave: () => live.current.subs.length <= depth && setActive(depth, -1),
    onChoose: (i: number) => choose(depth, i, false),
    onRename: (it: MenuItem, name: string) => void commitRename(depth, it, name),
    onCancelRename: () => {
      setRenaming(null);
      setFailure(null);
    },
  });

  return createPortal(
    <>
      <MenuPanel {...panelProps(0)} place={{ anchor, point, align, side, width }} header={header} />
      {subs.map((s, i) => (
        <MenuPanel key={`${s.from}-${i}`} {...panelProps(i + 1)} place={{ rect: s.rect, width }} loading={s.loading} loadError={s.error} />
      ))}
    </>,
    document.body,
  );
}

type Place =
  | { anchor: HTMLElement | null; point?: { x: number; y: number }; align: "left" | "right"; side: "top" | "bottom"; width: number; rect?: undefined }
  | { rect: { left: number; right: number; top: number; bottom: number }; width: number };

/** One level of a menu: the rows, the highlight gliding under them, a rename input or a failure where they call for it. */
function MenuPanel({
  id, depth, items, active, confirming, renaming, busy, failure, rows: report, place, header, loading, loadError,
  onHover, onEnter, onLeave, onChoose, onRename, onCancelRename,
}: {
  id: string;
  depth: number;
  items: MenuItem[];
  active: number;
  confirming: string | null;
  renaming: string | null;
  busy: string | null;
  failure?: string;
  rows: (list: (HTMLElement | null)[]) => void;
  place: Place;
  header?: string;
  loading?: boolean;
  loadError?: string;
  onHover: (i: number) => void;
  onEnter: () => void;
  onLeave: () => void;
  onChoose: (i: number) => void;
  onRename: (it: MenuItem, name: string) => void;
  onCancelRename: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const els = useRef<(HTMLElement | null)[]>([]);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const [pos, setPos] = useState<React.CSSProperties>({ visibility: "hidden" });
  const narrow = useMedia(NARROW_QUERY);
  report(els.current);
  const root = place.rect === undefined;
  const origin = root && place.point ? "top left" : root && place.side === "top" ? `bottom ${place.align}` : root ? `top ${place.align}` : "top left";

  useLayoutEffect(() => {
    if (!ref.current) return;
    // On a phone it keeps to what the keyboard leaves visible (the frame --kb-top / --kb-h follow).
    const view = narrow ? keyboardFrame(innerHeight, window.visualViewport) : undefined;
    const h = ref.current.offsetHeight;
    let at: MenuPlace;
    if (place.rect) at = placeSubmenu(place.rect, h, { width: place.width, screenW: innerWidth, screenH: innerHeight, view });
    else if (place.point) {
      const { x, y } = place.point;
      const right = x + Math.min(place.width, innerWidth - 16) + 8 > innerWidth; // no room on the right: it opens leftwards
      at = placeMenu({ left: x, right: x, top: y, bottom: y }, h, { width: place.width, align: right ? "right" : "left", side: "bottom", screenW: innerWidth, screenH: innerHeight, view, gap: 0 });
    } else if (place.anchor) {
      at = placeMenu(place.anchor.getBoundingClientRect(), h, { width: place.width, align: place.align, side: place.side, screenW: innerWidth, screenH: innerHeight, view });
    } else return;
    const { maxHeight, ...rest } = at;
    setPos(maxHeight === undefined ? rest : { ...rest, maxHeight: `min(70vh, 520px, ${maxHeight}px)` });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- placed again when what decides its place or height changes
  }, [place.rect, root ? place.anchor : null, root ? place.point?.x : 0, root ? place.point?.y : 0, place.width, items.length, narrow, loading, loadError]);

  useLayoutEffect(() => {
    const el = els.current[active];
    setBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
  }, [active]);

  return (
    <div
      ref={ref}
      role="menu"
      data-menu={id}
      className="fixed z-50 max-h-[min(70vh,520px)] overflow-y-auto rounded-[10px] bg-surface p-1 shadow-overlay"
      style={{ ...pos, animation: `pop-in 160ms ${EASE} both`, transformOrigin: origin }}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      {header && <div className="px-2 pt-1.5 pb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">{header}</div>}
      <div className="relative">
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-0 rounded-[6px] bg-hover"
          style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}, opacity 120ms ease` }}
        />
        {loading && (
          <div className="relative z-10 flex h-8 items-center gap-2 px-2 text-[12.5px] text-ink-3 pointer-coarse:min-h-11">
            <Spinner size={12} />
            {t("ui-atoms.menu_loading")}
          </div>
        )}
        {loadError && <div role="alert" className="relative z-10 px-2 py-1.5 text-[12px] break-words whitespace-pre-wrap text-red">{loadError}</div>}
        {!loading && !loadError && depth > 0 && items.length === 0 && <div className="relative z-10 flex h-8 items-center px-2 text-[12.5px] text-ink-3 pointer-coarse:min-h-11">{t("ui-atoms.menu_empty")}</div>}
        {items.map((it, i) => [
          it.divider && i > 0 && <div key={`${it.key}-sep`} role="separator" className="mx-1 my-1 h-px bg-line" />,
          renaming === it.key && it.rename ? (
            <RenameRow
              key={it.key}
              item={it}
              busy={busy === it.key}
              onCommit={(name) => onRename(it, name)}
              onCancel={onCancelRename}
              refCb={(el) => {
                els.current[i] = el;
              }}
            />
          ) : (
            <MenuRow
              key={it.key}
              item={it}
              asking={confirming === it.key}
              working={busy === it.key}
              refCb={(el) => {
                els.current[i] = el;
              }}
              onHover={() => !it.disabled && onHover(i)}
              onChoose={() => onChoose(i)}
            />
          ),
        ])}
      </div>
      {failure && (
        <div role="alert" className="mx-1 mt-1 max-h-28 overflow-y-auto border-t border-line px-1 pt-1.5 pb-1 text-[11.5px] break-words whitespace-pre-wrap text-red">
          {failure}
        </div>
      )}
    </div>
  );
}

function MenuRow({ item: it, asking, working, refCb, onHover, onChoose }: { item: MenuItem; asking: boolean; working: boolean; refCb: (el: HTMLElement | null) => void; onHover: () => void; onChoose: () => void }) {
  const accel = accelLabel(it);
  return (
    <button
      ref={refCb}
      role="menuitem"
      type="button"
      title={it.state ? `${it.label} · ${STATE_LABEL[it.state]}` : undefined}
      disabled={it.disabled}
      aria-haspopup={it.children ? "menu" : undefined}
      aria-keyshortcuts={it.accel}
      onMouseEnter={onHover}
      onClick={onChoose}
      className={cx(
        "relative z-10 flex h-8 w-full items-center gap-2 rounded-[6px] px-2 pointer-coarse:min-h-11 text-left text-[12.5px] disabled:opacity-40",
        it.danger ? "text-red" : "text-ink",
        asking && "bg-red-tint font-medium",
      )}
    >
      {it.state && <StatusMark state={it.state} />}
      {it.icon && <Icon name={it.icon} size={14} className={it.danger ? "" : "text-ink-3"} />}
      <span className="min-w-0 flex-1 truncate">{rowLabel(it, asking ? it.key : null)}</span>
      {it.state && <StatePill state={it.state} />}
      {working ? <Spinner size={12} /> : it.hint && <span className="shrink-0 text-[11px] text-ink-3">{it.hint}</span>}
      {it.checked !== undefined && <Icon name="check" size={13} stroke={2.5} className={it.checked ? "text-ink" : "invisible"} />}
      {accel && !working && <span aria-hidden className={cx("w-3.5 shrink-0 text-center text-[11px]", it.danger ? "" : "text-ink-3")}>{accel}</span>}
      {(it.submenu || it.children) && <Icon name="chevron" size={12} className="-rotate-90 text-ink-3" />}
    </button>
  );
}

/** A row that is an input: Enter commits a changed, non-empty name, Esc (or Enter on the same name) puts the row back. */
function RenameRow({ item, busy, onCommit, onCancel, refCb }: { item: MenuItem; busy: boolean; onCommit: (name: string) => void; onCancel: () => void; refCb: (el: HTMLElement | null) => void }) {
  const rename = item.rename!;
  const [text, setText] = useState(rename.value);
  return (
    <div ref={refCb} className="relative z-10 flex h-8 items-center gap-2 px-1 pointer-coarse:min-h-11">
      <input
        autoFocus
        value={text}
        disabled={busy}
        aria-label={item.label.replace(/…$/, "")}
        placeholder={rename.placeholder}
        onChange={(e) => setText(e.target.value)}
        onFocus={(e) => e.currentTarget.select()}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing) return;
          if (e.key === "Enter") {
            e.preventDefault();
            e.stopPropagation();
            const name = renameCommit(rename.value, text);
            if (name === null) onCancel();
            else onCommit(name);
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          } else e.stopPropagation();
        }}
        className="h-7 min-w-0 flex-1 rounded-[6px] bg-inset px-2 text-[12.5px] text-ink shadow-hairline outline-none placeholder:text-ink-3 pointer-coarse:h-9"
      />
      {busy && <Spinner size={12} />}
    </div>
  );
}

export function useMenu() {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  return {
    anchor,
    open: (e: React.MouseEvent<HTMLElement>) => {
      const target = e.currentTarget;
      setAnchor((a) => (a ? null : target));
    },
    close: () => setAnchor(null),
    /** Opens it at an element of the caller's choosing (another menu's anchor, once that menu has closed). */
    openAt: (el: HTMLElement | null) => setAnchor(el),
  };
}

// ── modal dialog ────────────────────────────────────────

/** A modal. Wide: a card in the middle of the page. Phone width (max-sm): a sheet up from the bottom of what the
 *  keyboard leaves visible (--kb-top / --kb-h), at most 90% of it, its body scrolling between the title and the footer,
 *  and Back closes it. Either way the focus moves in, Tab stays in, and goes back where it was when it closes. */
export function Dialog({
  open,
  onClose,
  title,
  children,
  width = 440,
  footer,
}: {
  open: boolean;
  onClose: () => void;
  title?: React.ReactNode;
  children: React.ReactNode;
  width?: number;
  footer?: React.ReactNode;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const sheet = useMedia(SHEET_QUERY);
  useCloseOnBack(open, onClose, sheet);
  useEffect(() => {
    if (!open) return;
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
  }, [open, onClose]);
  useEffect(() => {
    const el = panel.current;
    if (!open || !el) return;
    const back = document.activeElement as HTMLElement | null;
    // The dialog itself, so a phone's keyboard does not come up for its first field (one with autoFocus took it already).
    if (!el.contains(document.activeElement)) el.focus({ preventScroll: true });
    const tab = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const list = focusables(el);
      const i = trapTab(list.length, list.indexOf(document.activeElement as HTMLElement), e.shiftKey);
      if (i === null) return;
      e.preventDefault();
      (list[i] ?? el).focus();
    };
    el.addEventListener("keydown", tab);
    return () => {
      el.removeEventListener("keydown", tab);
      if (back?.isConnected) back.focus({ preventScroll: true });
    };
  }, [open]);
  if (!open) return null;
  return createPortal(
    <div
      className="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-black/25 px-4 pt-[12vh] pb-8 backdrop-blur-[2px] dark:bg-black/45 max-sm:top-(--kb-top) max-sm:bottom-auto max-sm:h-(--kb-h) max-sm:items-end max-sm:overflow-hidden max-sm:p-0"
      style={{ animation: "fade-in 140ms ease-out both" }}
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        tabIndex={-1}
        className="w-full max-w-(--dialog-w) animate-[slide-up_220ms_var(--ease-out-strong)_both] overflow-hidden rounded-[14px] bg-surface shadow-overlay outline-none max-sm:flex max-sm:max-h-[calc(var(--kb-h)*0.9)] max-sm:max-w-none max-sm:animate-none max-sm:flex-col max-sm:rounded-b-none max-sm:transition-transform max-sm:duration-300 max-sm:ease-(--ease-out-strong) max-sm:starting:translate-y-full"
        style={{ "--dialog-w": `${width}px` } as React.CSSProperties}
      >
        {title && (
          <div className="flex items-center justify-between gap-3 px-4 pt-3.5 pb-1 max-sm:shrink-0">
            <h2 id={titleId} className="text-[14px] font-semibold text-ink">{title}</h2>
            <IconButton icon="x" label={t("ui-atoms.close")} onClick={onClose} size={26} iconSize={14} />
          </div>
        )}
        <div className={cx("px-4 pt-2 pb-4 max-sm:min-h-0 max-sm:flex-1 max-sm:overflow-y-auto max-sm:overscroll-contain", !footer && "max-sm:pb-[calc(16px+env(safe-area-inset-bottom))]")}>{children}</div>
        {footer && <div className="flex items-center justify-end gap-2 border-t border-line bg-inset px-4 py-2.5 max-sm:shrink-0 max-sm:pb-[calc(10px+env(safe-area-inset-bottom))]">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

// ── toasts ──────────────────────────────────────────────

/** A button on a toast (撤销): it runs, and the toast goes. */
export type ToastAction = { label: string; run: () => void };
type Toast = { id: number; text: string; tone: "info" | "error" | "ok"; action?: ToastAction };
let toasts: Toast[] = [];
const toastListeners = new Set<() => void>();
let toastId = 0;

function dropToast(id: number) {
  toasts = toasts.filter((t) => t.id !== id);
  toastListeners.forEach((l) => l());
}

const gate = new FeedbackGate();

/** Shows a line at the bottom. While the hub is out of reach an error that is about it (lib/feedback.ts
 *  isOfflineError) is one calm "didn't go through" at most every few seconds; any other error keeps its words. The
 *  same line again within a few seconds is folded; one with a button always shows. */
export function toast(text: string, tone: Toast["tone"] = "info", action?: ToastAction) {
  const shown = action ? { text, tone } : gate.pass(text, tone, offlineNow(), Date.now());
  if (!shown) return;
  const id = ++toastId;
  toasts = [...toasts.slice(-2), { id, text: shown.text, tone: shown.tone, action }];
  toastListeners.forEach((l) => l());
  // One with a button stays long enough to reach it.
  setTimeout(() => dropToast(id), action ? 6000 : shown.tone === "error" ? 5200 : 2800);
}

export function Toasts() {
  const [, force] = useState(0);
  useEffect(() => {
    const l = () => force((x) => x + 1);
    toastListeners.add(l);
    return () => {
      toastListeners.delete(l);
    };
  }, []);
  return createPortal(
    <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-5 z-50 flex flex-col items-center gap-2 px-4">
      {toasts.map((item) => (
        <div key={item.id} className="pointer-events-auto flex max-w-md items-center gap-2 rounded-[10px] border border-tooltip-border bg-tooltip-bg px-3 py-2 text-[12.5px] text-tooltip-fg shadow-overlay" style={{ animation: `slide-up 220ms ${EASE} both` }}>
          {item.tone === "error" && <Icon name="warning" size={13} stroke={2.2} className="shrink-0 text-red" />}
          {item.tone === "ok" && <Icon name="check" size={13} stroke={2.6} className="text-green" />}
          <span className="min-w-0">{item.text}</span>
          {item.action && (
            <button type="button" className="ml-1 shrink-0 whitespace-nowrap font-semibold text-accent hover:underline"
              onClick={() => { item.action?.run(); dropToast(item.id); }}>
              {item.action.label}
            </button>
          )}
          <button
            type="button"
            aria-label={t("ui-atoms.close")}
            title={t("ui-atoms.close")}
            onClick={() => dropToast(item.id)}
            className="touch-hit -mr-1.5 ml-0.5 flex size-5 shrink-0 items-center justify-center rounded-[5px] text-tooltip-muted transition-colors duration-150 hover:bg-tooltip-border hover:text-tooltip-fg"
          >
            <Icon name="x" size={12} stroke={2.2} />
          </button>
        </div>
      ))}
    </div>,
    document.body,
  );
}

/** Puts text on the clipboard and says so (`done` / `failed` replace the usual words); whether it got there. */
export async function copyText(text: string, words: { done?: string; failed?: string } = {}): Promise<boolean> {
  let ok = true;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // http (not a secure context) has no async clipboard
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    try {
      ta.select();
      ok = document.execCommand("copy");
    } catch {
      ok = false;
    } finally {
      ta.remove();
    }
  }
  if (ok) toast(words.done ?? t("ui-atoms.copied"), "ok");
  else toast(words.failed ?? t("ui-atoms.copy_failed"), "error");
  return ok;
}
