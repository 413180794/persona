import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { stateColor, STATE_LABEL } from "../lib/format";
import type { AgentState } from "../lib/types";
import { Icon, type IconName } from "./icons";

export const EASE = "cubic-bezier(0.23, 1, 0.32, 1)";

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(" ");
}

// A light sweep across a label: work in progress.
export function Shimmer({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={`bg-clip-text text-transparent ${className}`}
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
        <span key={i} className="size-[4px] rounded-[1px] bg-ink" style={{ opacity: 0.15, animation: `pixel-on 650ms ease-in-out ${d}ms infinite` }} />
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

export function Loading({ label = "加载中" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <PixelGrid />
      <Shimmer className="text-[13px] font-medium">{label}</Shimmer>
    </div>
  );
}

export function StatusDot({ state, size = 8, ring }: { state: AgentState; size?: number; ring?: boolean }) {
  const breathe = state === "needs_you" || state === "working";
  return (
    <span
      role="img"
      aria-label={STATE_LABEL[state]}
      className="inline-block shrink-0 rounded-full"
      style={{
        width: size,
        height: size,
        background: stateColor(state),
        boxShadow: ring ? "0 0 0 2px var(--surface)" : undefined,
        animation: breathe ? "breathe 1.8s ease-in-out infinite" : undefined,
      }}
    />
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
      {(state === "working" || state === "needs_you") && <StatusDot state={state} size={6} />}
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
        "flex shrink-0 items-center justify-center rounded-[8px] transition-[background-color,color,transform] duration-150 enabled:active:scale-[0.94] disabled:opacity-40",
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
}) {
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
        "inline-flex h-8 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-control px-3 text-[12.5px] font-medium transition-[background-color,color,transform,opacity,filter] duration-150 enabled:active:scale-[0.96] disabled:opacity-45",
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
      className="inline-block shrink-0 rounded-full border-[1.5px] border-line-strong border-t-ink-2"
      style={{ width: size, height: size, animation: "spin 700ms linear infinite" }}
    />
  );
}

export function Kbd({ children }: { children: React.ReactNode }) {
  return <kbd className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-[5px] bg-surface px-1 font-sans text-[10px] text-ink-3 shadow-hairline">{children}</kbd>;
}

// Height-animating disclosure (grid 0fr → 1fr), the expand grammar every Beautiful UI trace uses.
export function Collapse({ open, children, className = "" }: { open: boolean; children: React.ReactNode; className?: string }) {
  return (
    <div
      className={`grid transition-[grid-template-rows,opacity] duration-300 ${className}`}
      style={{ gridTemplateRows: open ? "1fr" : "0fr", opacity: open ? 1 : 0, transitionTimingFunction: EASE }}
    >
      <div className="min-h-0 overflow-hidden">{children}</div>
    </div>
  );
}

// ── popover menu with a gliding highlight ───────────────

export interface MenuItem {
  key: string;
  label: string;
  hint?: string;
  icon?: IconName;
  state?: AgentState;
  checked?: boolean;
  danger?: boolean;
  disabled?: boolean;
  divider?: boolean; // a hairline above this row
  submenu?: boolean; // shows a › and keeps the menu open (the caller swaps the items)
  keepOpen?: boolean;
  onSelect: () => void;
}

export function Menu({
  items,
  anchor,
  onClose,
  align = "left",
  side = "bottom",
  width = 208,
  header,
}: {
  items: MenuItem[];
  anchor: HTMLElement | null;
  onClose: () => void;
  align?: "left" | "right";
  side?: "top" | "bottom";
  width?: number;
  header?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const rows = useRef<(HTMLButtonElement | null)[]>([]);
  const [active, setActive] = useState(-1);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const [pos, setPos] = useState<React.CSSProperties>({ visibility: "hidden" });

  useLayoutEffect(() => {
    if (!anchor || !ref.current) return;
    const r = anchor.getBoundingClientRect();
    const h = ref.current.offsetHeight;
    const menuWidth = Math.min(width, innerWidth - 16);
    const left = align === "left" ? r.left : r.right - menuWidth;
    let top = side === "bottom" ? r.bottom + 6 : r.top - h - 6;
    if (side === "bottom" && top + h > innerHeight - 8) top = r.top - h - 6;
    if (top < 8) top = 8;
    setPos({ left: Math.max(8, Math.min(left, innerWidth - menuWidth - 8)), top, width: menuWidth });
  }, [anchor, align, side, width, items.length]);

  useLayoutEffect(() => {
    const el = rows.current[active];
    setBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
  }, [active]);

  useEffect(() => {
    const down = (e: MouseEvent) => {
      if (ref.current?.contains(e.target as Node) || anchor?.contains(e.target as Node)) return;
      onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setActive((a) => {
          const n = items.length;
          let i = a;
          for (let k = 0; k < n; k++) {
            i = (i + (e.key === "ArrowDown" ? 1 : n - 1) + (i < 0 && e.key === "ArrowUp" ? 1 : 0)) % n;
            if (!items[i].disabled) return i;
          }
          return a;
        });
      } else if (e.key === "Enter" && active >= 0) {
        e.preventDefault();
        items[active].onSelect();
        if (!items[active].submenu && !items[active].keepOpen) onClose();
      }
    };
    addEventListener("mousedown", down);
    addEventListener("keydown", key);
    return () => {
      removeEventListener("mousedown", down);
      removeEventListener("keydown", key);
    };
  }, [anchor, onClose, items, active]);

  return createPortal(
    <div
      ref={ref}
      role="menu"
      className="fixed z-50 max-h-[min(70vh,520px)] overflow-y-auto rounded-[10px] bg-surface p-1 shadow-overlay"
      style={{ ...pos, animation: `pop-in 160ms ${EASE} both`, transformOrigin: side === "bottom" ? `top ${align}` : `bottom ${align}` }}
      onMouseLeave={() => setActive(-1)}
    >
      {header && <div className="px-2 pt-1.5 pb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">{header}</div>}
      <div className="relative">
        <span
          aria-hidden
          className="pointer-events-none absolute inset-x-0 rounded-[6px] bg-hover"
          style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}, opacity 120ms ease` }}
        />
        {items.map((it, i) => [
          it.divider && i > 0 && <div key={`${it.key}-sep`} role="separator" className="mx-1 my-1 h-px bg-line" />,
          <button
            key={it.key}
            ref={(el) => {
              rows.current[i] = el;
            }}
            role="menuitem"
            type="button"
            title={it.state ? `${it.label} · ${STATE_LABEL[it.state]}` : undefined}
            disabled={it.disabled}
            onMouseEnter={() => !it.disabled && setActive(i)}
            onClick={() => {
              it.onSelect();
              if (!it.submenu && !it.keepOpen) onClose();
            }}
            className={cx(
              "relative z-10 flex h-8 w-full items-center gap-2 rounded-[6px] px-2 text-left text-[12.5px] disabled:opacity-40",
              it.danger ? "text-red" : "text-ink",
            )}
          >
            {it.state && <StatusDot state={it.state} />}
            {it.icon && <Icon name={it.icon} size={14} className={it.danger ? "" : "text-ink-3"} />}
            <span className="min-w-0 flex-1 truncate">{it.label}</span>
            {it.state && <StatePill state={it.state} />}
            {it.hint && <span className="shrink-0 text-[11px] text-ink-3">{it.hint}</span>}
            {it.checked !== undefined && <Icon name="check" size={13} stroke={2.5} className={it.checked ? "text-ink" : "invisible"} />}
            {it.submenu && <Icon name="chevron" size={12} className="-rotate-90 text-ink-3" />}
          </button>,
        ])}
      </div>
    </div>,
    document.body,
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
  };
}

// ── modal dialog ────────────────────────────────────────

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
  useEffect(() => {
    if (!open) return;
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-black/25 px-4 pt-[12vh] pb-8 backdrop-blur-[2px] dark:bg-black/45" style={{ animation: "fade-in 140ms ease-out both" }} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal="true" className="w-full overflow-hidden rounded-[14px] bg-surface shadow-overlay" style={{ maxWidth: width, animation: `slide-up 220ms ${EASE} both` }}>
        {title && (
          <div className="flex items-center justify-between gap-3 px-4 pt-3.5 pb-1">
            <h2 className="text-[14px] font-semibold text-ink">{title}</h2>
            <IconButton icon="x" label="关闭" onClick={onClose} size={26} iconSize={14} />
          </div>
        )}
        <div className="px-4 pt-2 pb-4">{children}</div>
        {footer && <div className="flex items-center justify-end gap-2 border-t border-line bg-inset px-4 py-2.5">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

// ── toasts ──────────────────────────────────────────────

type Toast = { id: number; text: string; tone: "info" | "error" | "ok" };
let toasts: Toast[] = [];
const toastListeners = new Set<() => void>();
let toastId = 0;

export function toast(text: string, tone: Toast["tone"] = "info") {
  const id = ++toastId;
  toasts = [...toasts.slice(-2), { id, text, tone }];
  toastListeners.forEach((l) => l());
  setTimeout(() => {
    toasts = toasts.filter((t) => t.id !== id);
    toastListeners.forEach((l) => l());
  }, tone === "error" ? 5200 : 2800);
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
      {toasts.map((t) => (
        <div key={t.id} className="pointer-events-auto flex max-w-md items-center gap-2 rounded-[10px] border border-tooltip-border bg-tooltip-bg px-3 py-2 text-[12.5px] text-tooltip-fg shadow-overlay" style={{ animation: `slide-up 220ms ${EASE} both` }}>
          {t.tone === "error" && <span className="size-1.5 shrink-0 rounded-full bg-red" />}
          {t.tone === "ok" && <Icon name="check" size={13} stroke={2.6} className="text-green" />}
          <span className="min-w-0">{t.text}</span>
        </div>
      ))}
    </div>,
    document.body,
  );
}

export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // http (not a secure context) has no async clipboard
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
  }
  toast("已拷贝", "ok");
}
