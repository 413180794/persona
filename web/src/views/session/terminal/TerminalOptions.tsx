// "Terminal settings" (iOS TerminalSettingsView, the parts a browser needs): long rows fold or scroll while watching, the key bar,
// the font size (pinching on iOS; ⌘+ / ⌘− stay the browser's zoom), the cursor (shape, blinking), the WebGL renderer,
// and the saved commands. Kept in this browser.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cx, EASE } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { Switch } from "../../assistant/parts";
import { CURSOR_STYLES } from "../../../lib/terminal-details";
import { MOUSE_MODES } from "../../../lib/terminal-mouse";
import { FONT_SIZES, type FeedView, type TerminalFeed } from "./feed";
import { t } from "../../../i18n";

export function TerminalOptions({
  anchor,
  feed,
  v,
  keyBar,
  onKeyBar,
  onEditShortcuts,
  onClose,
}: {
  anchor: HTMLElement;
  feed: TerminalFeed;
  v: FeedView;
  keyBar: boolean;
  onKeyBar: (on: boolean) => void;
  onEditShortcuts: () => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<React.CSSProperties>({ visibility: "hidden" });
  const width = 280;

  useLayoutEffect(() => {
    const r = anchor.getBoundingClientRect();
    const w = Math.min(width, innerWidth - 16);
    setPos({ top: r.bottom + 6, left: Math.max(8, Math.min(r.right - w, innerWidth - w - 8)), width: w });
  }, [anchor]);

  useEffect(() => {
    const down = (e: PointerEvent) => {
      if (ref.current?.contains(e.target as Node) || anchor.contains(e.target as Node)) return;
      onClose();
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("pointerdown", down, true);
    addEventListener("keydown", key);
    return () => {
      removeEventListener("pointerdown", down, true);
      removeEventListener("keydown", key);
    };
  }, [anchor, onClose]);

  const driving = v.control === "active" || v.control === "starting";
  const row = "flex min-h-10 items-center gap-2.5 px-3";

  return createPortal(
    <div
      ref={ref}
      role="dialog"
      aria-label={t("session-terminal-terminaloptions.title")}
      className="fixed z-50 overflow-hidden rounded-[10px] bg-surface py-1 shadow-overlay"
      style={{ ...pos, animation: `pop-in 160ms ${EASE} both`, transformOrigin: "top right" }}
    >
      <div className={row}>
        <Icon name="wrap" size={15} className="shrink-0 text-ink-3" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.wrap")}</div>
          <div className="truncate text-[11px] text-ink-3" title={t("session-terminal-terminaloptions.wrap_hint")}>
            {driving ? t("session-terminal-terminaloptions.wrap_driving") : v.wrap ? t("session-terminal-terminaloptions.wrap_on") : t("session-terminal-terminaloptions.wrap_off")}
          </div>
        </div>
        <Switch checked={v.wrap} onChange={(on) => feed.setWrap(on)} label={t("session-terminal-terminaloptions.wrap")} />
      </div>
      <div className={row}>
        <Icon name="keyboard" size={15} className="shrink-0 text-ink-3" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.key_bar")}</div>
          <div className="truncate text-[11px] text-ink-3">{t("session-terminal-terminaloptions.key_bar_hint")}</div>
        </div>
        <Switch checked={keyBar} onChange={onKeyBar} label={t("session-terminal-terminaloptions.key_bar")} />
      </div>
      <div className={row}>
        <Icon name="textSize" size={15} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.font_size")}</span>
        <div className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
          <StepButton label={t("session-terminal-terminaloptions.font_smaller")} disabled={v.fontSize <= FONT_SIZES.min} onClick={() => feed.setFontSize(v.fontSize - 1)}>
            −
          </StepButton>
          <button
            type="button"
            title={t("session-terminal-terminaloptions.font_default")}
            onClick={() => feed.setFontSize(FONT_SIZES.default)}
            className="h-6 min-w-8 whitespace-nowrap rounded-chip px-1 font-mono text-[12px] text-ink tabular-nums hover:bg-surface"
          >
            {v.fontSize}
          </button>
          <StepButton label={t("session-terminal-terminaloptions.font_larger")} disabled={v.fontSize >= FONT_SIZES.max} onClick={() => feed.setFontSize(v.fontSize + 1)}>
            +
          </StepButton>
        </div>
      </div>
      <div className={row}>
        <Icon name="hand" size={15} className="shrink-0 text-ink-3" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] text-ink">{t("terminal-mouse.label")}</div>
          <div
            className="truncate text-[11px] text-ink-3"
            title={t("terminal-mouse.tooltip")}
          >
            {v.mouse === "program" ? t("terminal-mouse.hint_program") : t("terminal-mouse.hint_local")}
          </div>
        </div>
        <div role="radiogroup" aria-label={t("terminal-mouse.label")} className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
          {MOUSE_MODES.map((m) => (
            <button
              key={m.mode}
              type="button"
              role="radio"
              aria-checked={v.mouse === m.mode}
              onClick={() => feed.setMouse(m.mode)}
              className={cx(
                "h-6 whitespace-nowrap rounded-chip px-2 text-[12px] font-medium transition-[background-color,color,box-shadow] duration-150",
                v.mouse === m.mode ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink",
              )}
            >
              {m.label}
            </button>
          ))}
        </div>
      </div>
      <div className={row}>
        <Icon name="square" size={15} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.cursor_style")}</span>
        <div role="radiogroup" aria-label={t("session-terminal-terminaloptions.cursor_style")} className="flex h-7 shrink-0 items-center rounded-control bg-field p-0.5">
          {CURSOR_STYLES.map((c) => (
            <button
              key={c.value}
              type="button"
              role="radio"
              aria-checked={v.cursorStyle === c.value}
              onClick={() => feed.setCursorStyle(c.value)}
              className={cx(
                "h-6 whitespace-nowrap rounded-chip px-2 text-[12px] font-medium transition-[background-color,color,box-shadow] duration-150",
                v.cursorStyle === c.value ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink",
              )}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>
      <div className={row}>
        <Icon name="spark" size={15} className="shrink-0 text-ink-3" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.cursor_blink")}</div>
          <div className="truncate text-[11px] text-ink-3">{t("session-terminal-terminaloptions.cursor_blink_hint")}</div>
        </div>
        <Switch checked={v.cursorBlink} onChange={(on) => feed.setCursorBlink(on)} label={t("session-terminal-terminaloptions.cursor_blink")} />
      </div>
      <div className={row}>
        <Icon name="gauge" size={15} className="shrink-0 text-ink-3" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.webgl")}</div>
          <div className="truncate text-[11px] text-ink-3" title={t("session-terminal-terminaloptions.webgl_hint_long")}>
            {t("session-terminal-terminaloptions.webgl_hint")}
          </div>
        </div>
        <Switch checked={v.webgl} onChange={(on) => feed.setWebgl(on)} label={t("session-terminal-terminaloptions.webgl")} />
      </div>
      <div className="mx-3 my-1 border-t border-line" />
      <button
        type="button"
        onClick={() => {
          onClose();
          onEditShortcuts();
        }}
        className={cx(row, "w-full text-left hover:bg-hover")}
      >
        <Icon name="bolt" size={15} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink">{t("session-terminal-terminaloptions.edit_shortcuts")}</span>
      </button>
    </div>,
    document.body,
  );
}

function StepButton({ label, disabled, onClick, children }: { label: string; disabled?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="flex h-6 w-6 items-center justify-center whitespace-nowrap rounded-chip text-[14px] text-ink-2 enabled:hover:bg-surface enabled:hover:text-ink disabled:opacity-35"
    >
      {children}
    </button>
  );
}
