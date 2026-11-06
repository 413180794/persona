// The terminal's saved commands (iOS ShortcutsPanel / TerminalPrefs.shortcuts), kept in this browser: over the key bar
// a click types one into the terminal, its ↵ runs it (with return); the editor adds, orders and removes them.
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { decodeShortcuts, moveShortcut } from "../../../lib/terminal";
import { Button, cx, Dialog, EASE, IconButton } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { t } from "../../../i18n";

const KEY = "persona.terminal.shortcuts";
const listeners = new Set<() => void>();
let cached: { raw: string | null; list: string[] } | null = null;

function read(): string[] {
  const raw = localStorage.getItem(KEY);
  if (!cached || cached.raw !== raw) cached = { raw, list: decodeShortcuts(raw) };
  return cached.list;
}

function save(list: string[]) {
  localStorage.setItem(KEY, JSON.stringify(list));
  listeners.forEach((l) => l());
}

function useShortcuts(): string[] {
  return useSyncExternalStore((l) => {
    listeners.add(l);
    return () => {
      listeners.delete(l);
    };
  }, read);
}

export function ShortcutsPopover({ onPick, onEdit, onClose }: { onPick: (text: string, run: boolean) => void; onEdit: () => void; onClose: () => void }) {
  const list = useShortcuts();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const down = (e: PointerEvent) => {
      if (ref.current?.contains(e.target as Node)) return;
      if ((e.target as Element | null)?.closest?.(`[aria-label="${t("session-terminal-keybar.shortcuts")}"]`)) return; // its own button toggles it
      onClose();
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("pointerdown", down, true);
    addEventListener("keydown", key);
    return () => {
      removeEventListener("pointerdown", down, true);
      removeEventListener("keydown", key);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="dialog"
      aria-label={t("session-terminal-shortcuts.title")}
      className="absolute bottom-full left-1.5 z-20 mb-1.5 w-[min(340px,calc(100%-12px))] overflow-hidden rounded-card bg-surface shadow-overlay"
      style={{ animation: `pop-in 160ms ${EASE} both`, transformOrigin: "bottom left" }}
      onMouseDown={(e) => e.preventDefault()} // the terminal keeps the focus
    >
      <div className="flex items-center gap-2 px-3 pt-2.5 pb-1.5">
        <span className="min-w-0 flex-1 truncate text-[11.5px] font-medium text-ink-3">{t("session-terminal-shortcuts.popover_header")}</span>
        <button type="button" onClick={onEdit} className="h-6 shrink-0 whitespace-nowrap rounded-chip px-2 text-[12px] font-medium text-accent-ink hover:bg-hover">
          {t("session-terminal-shortcuts.edit")}
        </button>
      </div>
      {list.length === 0 && <p className="px-3 pb-3 text-[12px] text-ink-3">{t("session-terminal-shortcuts.popover_empty")}</p>}
      <div className="max-h-[230px] overflow-y-auto pb-1">
        {list.map((s, i) => (
          <div key={`${i}:${s}`} className="group flex items-center gap-1 px-1.5">
            <button
              type="button"
              title={t("session-terminal-shortcuts.type", { command: s })}
              onClick={() => onPick(s, false)}
              className="h-8 min-w-0 flex-1 truncate rounded-chip px-1.5 text-left font-mono text-[12.5px] text-ink hover:bg-hover"
            >
              {s}
            </button>
            <IconButton icon="enter" label={t("session-terminal-shortcuts.run", { command: s })} size={28} iconSize={14} onClick={() => onPick(s, true)} />
          </div>
        ))}
      </div>
    </div>
  );
}

/** "Edit shortcuts": add, move up / down, remove (iOS Terminal settings › Shortcuts). */
export function ShortcutsEditor({ open, onClose }: { open: boolean; onClose: () => void }) {
  const list = useShortcuts();
  const [draft, setDraft] = useState("");
  const add = () => {
    const s = draft.trim();
    if (!s) return;
    save([...list, s]);
    setDraft("");
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("session-terminal-shortcuts.title")}
      footer={
        <Button tone="primary" onClick={onClose}>
          {t("session-terminal-shortcuts.done")}
        </Button>
      }
    >
      <p className="mb-3 text-[12.5px] leading-relaxed text-ink-2">{t("session-terminal-shortcuts.editor_hint")}</p>
      <div className="overflow-hidden rounded-control shadow-hairline">
        {list.length === 0 && <p className="px-3 py-2.5 text-[12.5px] text-ink-3">{t("session-terminal-shortcuts.editor_empty")}</p>}
        {list.map((s, i) => (
          <div key={`${i}:${s}`} className={cx("flex items-center gap-1 py-1 pr-1 pl-3", i > 0 && "border-t border-line")}>
            <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink" title={s}>
              {s}
            </span>
            <IconButton icon="up" label={t("session-terminal-shortcuts.move_up")} size={26} iconSize={13} disabled={i === 0} onClick={() => save(moveShortcut(list, i, -1))} />
            <IconButton icon="arrowDown" label={t("session-terminal-shortcuts.move_down")} size={26} iconSize={13} disabled={i === list.length - 1} onClick={() => save(moveShortcut(list, i, 1))} />
            <IconButton icon="trash" label={t("session-terminal-shortcuts.delete")} size={26} iconSize={13} onClick={() => save(list.filter((_, j) => j !== i))} />
          </div>
        ))}
      </div>
      <form
        className="mt-3 flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <div className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-control bg-field px-2.5 shadow-inset-field">
          <Icon name="terminal" size={13} className="shrink-0 text-ink-3" />
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t("session-terminal-shortcuts.new_placeholder")}
            aria-label={t("session-terminal-shortcuts.new_placeholder")}
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            className="min-w-0 flex-1 bg-transparent font-mono text-[12.5px] text-ink outline-none placeholder:text-ink-3"
          />
        </div>
        <Button type="submit" disabled={!draft.trim()}>
          {t("session-terminal-shortcuts.add")}
        </Button>
      </form>
    </Dialog>
  );
}
