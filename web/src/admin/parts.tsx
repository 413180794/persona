// Small building blocks of the admin pages, on the web client's tokens (src/styles.css) and atoms.
import { t } from "../i18n";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { visiblePoll } from "../lib/visible-poll";
import { BUSY_MOTION } from "../lib/busy-motion";
import { Button, copyText, cx, EASE, IconButton, PixelGrid, Shimmer } from "../ui/atoms";
import { errorText } from "./api";
import { AdminIcon, type AdminIconName } from "./icons";
import { qrPath } from "./logic";
import { SegmentedTrack } from "../ui/Segmented";

export type Tone = "green" | "red" | "orange" | "accent" | "gray";

const TINT: Record<Tone, string> = {
  green: "bg-green-tint text-green",
  red: "bg-red-tint text-red",
  orange: "bg-orange-tint text-orange",
  accent: "bg-accent-tint text-accent-ink",
  gray: "bg-inset text-ink-2 shadow-hairline",
};

const DOT: Record<Tone, string> = { green: "bg-green", red: "bg-red", orange: "bg-orange", accent: "bg-accent", gray: "bg-ink-3" };

export function Tag({ children, tone = "gray", title }: { children: React.ReactNode; tone?: Tone; title?: string }) {
  return (
    <span title={title} className={cx("inline-flex h-5 shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-1.5 text-[11px] font-medium", TINT[tone])}>
      {children}
    </span>
  );
}

export function Dot({ tone, pulse }: { tone: Tone; pulse?: boolean }) {
  return <span aria-hidden className={cx("inline-block size-2 shrink-0 rounded-full", DOT[tone], pulse && BUSY_MOTION)} style={pulse ? { animation: "breathe 1.8s ease-in-out infinite" } : undefined} />;
}

export function PageHeader({ title, subtitle, right }: { title: string; subtitle?: React.ReactNode; right?: React.ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-end gap-3">
      <div className="min-w-0 flex-1">
        <h1 className="text-[20px] font-semibold tracking-tight text-ink">{title}</h1>
        {subtitle && <p className="mt-1 text-[12.5px] leading-relaxed text-ink-2">{subtitle}</p>}
      </div>
      {right && <div className="flex shrink-0 items-center gap-2">{right}</div>}
    </div>
  );
}

export function Card({ title, right, footer, children, className }: { title?: React.ReactNode; right?: React.ReactNode; footer?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return (
    <section className={cx("mb-6", className)}>
      {(title || right) && (
        <div className="mb-1.5 flex min-h-6 items-end gap-2 px-1">
          <h2 className="flex-1 text-[12px] font-medium text-ink-3">{title}</h2>
          {right}
        </div>
      )}
      <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">{children}</div>
      {footer && <div className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">{footer}</div>}
    </section>
  );
}

// Line is a row of a card: label (and hint) on the left, value or controls on the right; wraps on narrow screens.
export function Line({ label, hint, children, mono, top }: { label: React.ReactNode; hint?: React.ReactNode; children?: React.ReactNode; mono?: boolean; top?: boolean }) {
  return (
    <div className={cx("flex flex-wrap gap-x-4 gap-y-1 px-4 py-2.5", top ? "items-start" : "items-center")}>
      <div className="min-w-[120px] flex-1">
        <div className="text-[13px] text-ink">{label}</div>
        {hint && <div className="mt-0.5 text-[11.5px] leading-relaxed text-ink-3">{hint}</div>}
      </div>
      {children !== undefined && <div className={cx("flex min-w-0 max-w-full items-center gap-2 text-[12.5px] text-ink-2", mono && "font-mono")}>{children}</div>}
    </div>
  );
}

export function CopyValue({ value, label = t("admin-parts.copy"), mono = true, className }: { value: string; label?: string; mono?: boolean; className?: string }) {
  return (
    <span className={cx("flex min-w-0 items-center gap-1", className)}>
      <span className={cx("min-w-0 truncate text-[12.5px] text-ink-2", mono && "font-mono text-[12px]")} title={value}>
        {value}
      </span>
      <IconButton icon="copy" label={label} size={24} iconSize={13} onClick={() => copyText(value)} />
    </span>
  );
}

export function Field({
  label,
  hint,
  value,
  onChange,
  placeholder,
  type = "text",
  mono,
  autoFocus,
  error,
  autoComplete,
  multiline,
  disabled,
  autoCapitalize,
  maxLength,
}: {
  label: string;
  hint?: React.ReactNode;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: "text" | "password";
  mono?: boolean;
  autoFocus?: boolean;
  error?: string;
  autoComplete?: string;
  multiline?: boolean;
  disabled?: boolean;
  autoCapitalize?: string;
  maxLength?: number;
}) {
  const cls = cx(
    "w-full min-w-0 rounded-control bg-inset px-3 text-[13px] text-ink shadow-hairline outline-none transition-shadow placeholder:text-ink-3 focus:shadow-[0_0_0_1px_var(--line-strong),0_0_0_3px_var(--accent-tint)] disabled:opacity-60",
    mono && "font-mono text-[12.5px] placeholder:font-sans",
    error && "shadow-[0_0_0_1px_var(--red)]",
    multiline ? "min-h-[76px] resize-y py-2 leading-relaxed" : "h-9",
  );
  // Only the label text names the control; the hint or error describes it, so a screen reader reads them separately.
  const id = useId();
  const noteId = `${id}-note`;
  const note = error || hint;
  const a11y = { id, "aria-invalid": error ? true : undefined, "aria-describedby": note ? noteId : undefined };
  return (
    <div className="block">
      <label htmlFor={id} className="mb-1 block text-[12px] font-medium text-ink-2">{label}</label>
      {multiline ? (
        <textarea {...a11y} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} autoFocus={autoFocus} disabled={disabled} spellCheck={false} className={cls} />
      ) : (
        <input {...a11y} type={type} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} autoFocus={autoFocus} disabled={disabled} autoComplete={autoComplete} autoCapitalize={autoCapitalize} maxLength={maxLength} spellCheck={false} className={cls} />
      )}
      {note && <span id={noteId} className={cx("mt-1 block text-[11.5px] leading-relaxed", error ? "text-red" : "text-ink-3")}>{note}</span>}
    </div>
  );
}

export function Notice({ tone = "accent", icon, title, children, action }: { tone?: Tone; icon?: AdminIconName; title: React.ReactNode; children?: React.ReactNode; action?: React.ReactNode }) {
  const ring: Record<Tone, string> = {
    green: "bg-green-tint",
    red: "bg-red-tint",
    orange: "bg-orange-tint",
    accent: "bg-accent-tint",
    gray: "bg-inset shadow-hairline",
  };
  const ink: Record<Tone, string> = { green: "text-green", red: "text-red", orange: "text-orange", accent: "text-accent-ink", gray: "text-ink-2" };
  return (
    <div className={cx("mb-5 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-card px-3.5 py-3", ring[tone])} role="status">
      {icon && <AdminIcon name={icon} size={16} className={cx("shrink-0 self-start", ink[tone])} />}
      <div className="min-w-[200px] flex-1">
        <div className={cx("text-[13px] font-medium", ink[tone])}>{title}</div>
        {children && <div className="mt-0.5 text-[12px] leading-relaxed text-ink-2">{children}</div>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
    </div>
  );
}

export function EmptyState({ icon, title, children, action }: { icon: AdminIconName; title: string; children?: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-10 text-center">
      <span className="mb-3 flex size-10 items-center justify-center rounded-[12px] bg-inset text-ink-3 shadow-hairline">
        <AdminIcon name={icon} size={18} />
      </span>
      <div className="text-[13.5px] font-medium text-ink">{title}</div>
      {children && <div className="mt-1 max-w-[420px] text-[12px] leading-relaxed text-ink-3">{children}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function Stat({ icon, label, value, hint, onClick }: { icon: AdminIconName; label: string; value: React.ReactNode; hint?: React.ReactNode; onClick?: () => void }) {
  const body = (
    <>
      <div className="flex items-center gap-1.5 text-[11.5px] font-medium text-ink-3">
        <AdminIcon name={icon} size={13} />
        <span className="truncate">{label}</span>
      </div>
      <div className="mt-1.5 truncate text-[20px] font-semibold tracking-tight text-ink tabular-nums">{value}</div>
      {hint && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{hint}</div>}
    </>
  );
  const cls = "min-w-0 rounded-card bg-surface px-3.5 py-3 text-left shadow-card";
  return onClick ? (
    <button type="button" onClick={onClick} className={cx(cls, "transition-[background-color,transform] duration-150 hover:bg-hover active:scale-[0.99]")}>
      {body}
    </button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

// ConfirmButton asks again in place: the first click turns it into "Confirm" / "Cancel". Focus moves to "Cancel" when it asks and
// back to the trigger when it stops asking (unless the user has moved on), so a keyboard user keeps their place; Escape backs out.
export function ConfirmButton({ label, confirm, onConfirm, icon = "trash", busy }: { label: string; confirm: string; onConfirm: () => void | Promise<void>; icon?: AdminIconName; busy?: boolean }) {
  const [asking, setAsking] = useState(false);
  const box = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const refocus = useRef(false);
  // stop asking; give focus back to the trigger only if it is still in the pair (or was dropped on the body)
  const close = () => {
    const active = document.activeElement;
    refocus.current = !active || active === document.body || !!box.current?.contains(active);
    setAsking(false);
  };
  useEffect(() => {
    if (!asking) {
      if (refocus.current) trigger.current?.focus();
      refocus.current = false;
      return;
    }
    box.current?.querySelector("button")?.focus();
    const timer = setTimeout(close, 6000);
    return () => clearTimeout(timer);
  }, [asking]);
  if (asking)
    return (
      <span
        ref={box}
        className="flex items-center gap-1"
        style={{ animation: `pop-in 140ms ${EASE} both` }}
        onKeyDown={(e) => {
          if (e.key !== "Escape") return;
          e.stopPropagation();
          close();
        }}
      >
        <Button tone="quiet" className="h-7 px-2" onClick={close}>
          {t("admin-parts.cancel")}
        </Button>
        <Button tone="danger" className="h-7 px-2.5" busy={busy} onClick={() => void Promise.resolve(onConfirm()).finally(close)}>
          {confirm}
        </Button>
      </span>
    );
  return (
    <button
      ref={trigger}
      type="button"
      title={label}
      aria-label={label}
      onClick={() => setAsking(true)}
      className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-red-tint hover:text-red"
    >
      <AdminIcon name={icon} size={14} />
    </button>
  );
}

// Qr draws the hub's QR rows. Scanners need dark modules on a light ground, so it stays black on white in dark mode too.
export function Qr({ rows, size = 196, label }: { rows: string[]; size?: number; label: string }) {
  const { d, size: n } = qrPath(rows, 3);
  return (
    <svg role="img" aria-label={label} width={size} height={size} viewBox={`0 0 ${n} ${n}`} shapeRendering="crispEdges" className="rounded-[10px] bg-white text-black shadow-hairline">
      <path d={d} fill="currentColor" />
    </svg>
  );
}

export function Loader({ label = t("admin-parts.loading") }: { label?: string }) {
  return (
    <div className="flex items-center gap-2.5 px-1 py-6">
      <PixelGrid />
      <Shimmer className="text-[13px] font-medium">{label}</Shimmer>
    </div>
  );
}

export function ErrorBox({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <Notice tone="red" icon="alert" title={t("admin-parts.load_failed")} action={onRetry && <Button icon="retry" onClick={onRetry}>{t("admin-parts.retry")}</Button>}>
      {errorText(error)}
    </Notice>
  );
}

// useLoad runs fn now and on reload(); a newer run wins over an older one still in flight.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | undefined>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const run = useCallback(() => {
    const n = ++seq.current;
    setLoading(true);
    fn().then(
      (v) => {
        if (n !== seq.current) return;
        setData(v);
        setError(undefined);
        setLoading(false);
      },
      (e) => {
        if (n !== seq.current) return;
        setError(e);
        setLoading(false);
      },
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(run, [run]);
  return { data, error, loading, reload: run, set: setData };
}

// useVisiblePoll calls tick every ms while the tab is visible, and once at once when a hidden tab is shown again.
// It does not tick when it starts (the page has just loaded); nothing runs while hidden, disabled or unmounted.
export function useVisiblePoll(tick: () => void, ms: number, enabled = true) {
  const latest = useRef(tick);
  useEffect(() => {
    latest.current = tick;
  });
  useEffect(() => {
    if (!enabled) return;
    let started = false;
    const stop = visiblePoll(() => started && latest.current(), ms, {
      visible: () => document.visibilityState !== "hidden",
      subscribe: (fn) => {
        document.addEventListener("visibilitychange", fn);
        return () => document.removeEventListener("visibilitychange", fn);
      },
    });
    started = true;
    return stop;
  }, [ms, enabled]);
}

// Segmented is a small radio group in the shell's switch style (the theme switch): one row of short choices.
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
  className,
}: {
  value: T;
  options: { value: T; label: string; icon?: AdminIconName }[];
  onChange: (v: T) => void;
  label: string;
  className?: string;
}) {
  return (
    <SegmentedTrack role="radiogroup" label={label} className={cx("inline-flex max-w-full items-center gap-0.5 overflow-x-auto rounded-control p-0.5", className)} thumbClassName="rounded-[7px]">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          data-selected={value === o.value}
          onClick={() => onChange(o.value)}
          className={cx(
            "relative z-[1] flex h-7 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[7px] px-2.5 text-[12px] transition-colors duration-150",
            value === o.value ? "font-medium text-ink" : "text-ink-3 hover:text-ink-2",
          )}
        >
          {o.icon && <AdminIcon name={o.icon} size={13} />}
          {o.label}
        </button>
      ))}
    </SegmentedTrack>
  );
}

// CodeBlock shows a command or link in full (wrapped), with a copy button.
export function CodeBlock({ value, label, copyLabel = t("admin-parts.copy") }: { value: string; label?: string; copyLabel?: string }) {
  return (
    <div className="rounded-control bg-inset p-2.5 shadow-hairline">
      {label && <div className="mb-1 text-[11px] font-medium text-ink-3">{label}</div>}
      <div className="flex items-start gap-1.5">
        <code className="min-w-0 flex-1 font-mono text-[12px] leading-relaxed break-all text-ink">{value}</code>
        <IconButton icon="copy" label={copyLabel} size={24} iconSize={13} onClick={() => copyText(value)} />
      </div>
    </div>
  );
}
