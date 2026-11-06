import { t } from "../../../i18n";
import { MODE_LABEL, MODE_TITLE, type TerminalMode } from "../../../lib/terminal-mode";
import { cx } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";

/** "Single pane" or "Full view": icons where the panel is narrow, words where it is not. */
export function ModeToggle({ mode, onChange }: { mode: TerminalMode; onChange: (mode: TerminalMode) => void }) {
  const options: { mode: TerminalMode; icon: "terminal" | "sidebar" }[] = [
    { mode: "pane", icon: "terminal" },
    { mode: "full", icon: "sidebar" },
  ];
  return (
    <div role="radiogroup" aria-label={t("terminal-full.mode_group")} className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
      {options.map((o) => (
        <button
          key={o.mode}
          type="button"
          role="radio"
          aria-checked={mode === o.mode}
          aria-label={MODE_LABEL[o.mode]}
          title={t("terminal-full.mode_tooltip", { label: MODE_LABEL[o.mode], title: MODE_TITLE[o.mode] })}
          onClick={() => onChange(o.mode)}
          className={cx(
            "flex h-6 min-w-7 items-center justify-center gap-1.5 whitespace-nowrap rounded-chip px-1.5 text-[12px] font-medium transition-[background-color,color,box-shadow] duration-150 @md:px-2",
            mode === o.mode ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink",
          )}
        >
          <Icon name={o.icon} size={13} className="@md:hidden" />
          <span className="hidden @md:inline">{MODE_LABEL[o.mode]}</span>
        </button>
      ))}
    </div>
  );
}
