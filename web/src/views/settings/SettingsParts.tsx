import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export function Group({ title, footer, children, right }: { title?: React.ReactNode; footer?: React.ReactNode; children: React.ReactNode; right?: React.ReactNode }) {
  return (
    <section className="mb-6">
      {(title || right) && (
        <div className="mb-1.5 flex items-end gap-2 px-1">
          <h2 className="flex-1 text-[12px] font-medium text-ink-3">{title}</h2>
          {right}
        </div>
      )}
      <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">{children}</div>
      {footer && <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">{footer}</p>}
    </section>
  );
}

export function Row({ label, hint, children, onClick, mono }: { label: React.ReactNode; hint?: React.ReactNode; children?: React.ReactNode; onClick?: () => void; mono?: boolean }) {
  const body = (
    <>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] text-ink">{label}</div>
        {hint && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{hint}</div>}
      </div>
      {children !== undefined && <div className={cx("flex min-w-0 shrink-0 items-center gap-2 text-[12.5px] text-ink-2", mono && "font-mono")}>{children}</div>}
      {onClick && <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />}
    </>
  );
  return onClick ? (
    <button type="button" onClick={onClick} className="flex w-full items-center gap-3 whitespace-nowrap px-4 py-2.5 text-left transition-colors hover:bg-hover">
      {body}
    </button>
  ) : (
    <div className="flex items-center gap-3 px-4 py-2.5">{body}</div>
  );
}

export function Pill({ children, tone }: { children: React.ReactNode; tone?: "accent" | "green" | "red" }) {
  return (
    <span
      className={cx(
        "inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full px-1.5 text-[11px] font-medium",
        tone === "accent" ? "bg-accent-tint text-accent-ink" : tone === "green" ? "bg-green-tint text-green" : tone === "red" ? "bg-red-tint text-red" : "bg-inset text-ink-2 shadow-hairline",
      )}
    >
      {children}
    </span>
  );
}
