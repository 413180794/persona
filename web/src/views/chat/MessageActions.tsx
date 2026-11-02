// What you can do with a message (iOS MessageMenu.swift: 复制 / 选择 / 引用 / 编辑): with a mouse, a small bar under it
// on hover and the same items on right-click; on a touch screen, a long press opens them (and 选择 shows the text to
// select, since the press no longer selects). 引用 puts it as a quote in front of the draft, 编辑 (your own messages)
// puts it back in the composer to change and send again. Sharing as a card is the phone's.
import { useEffect, useRef, useState } from "react";
import { clock } from "../../lib/format";
import { sendDraft } from "../../lib/drafts";
import { Button, copyText, cx, Dialog, Menu, type MenuItem } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { useChatActions } from "./chat-actions";

const coarse = () => typeof matchMedia === "function" && matchMedia("(hover: none)").matches;

function pointAnchor(x: number, y: number): HTMLElement {
  const el = document.createElement("span");
  el.style.cssText = `position:fixed;left:${x}px;top:${y}px;width:0;height:0;pointer-events:none`;
  document.body.appendChild(el);
  return el;
}

/** The actions of one message; empty when there is nothing to act on. */
interface Action {
  key: string;
  label: string;
  icon: IconName;
  onSelect: () => void;
}

function useActions(text: string, isUser: boolean, select: () => void): Action[] {
  const chat = useChatActions();
  if (!text.trim()) return [];
  const out: Action[] = [{ key: "copy", label: "复制", icon: "copy", onSelect: () => void copyText(text) }];
  if (coarse()) out.push({ key: "select", label: "选择文本", icon: "read", onSelect: select });
  if (chat?.canCompose) {
    out.push({ key: "quote", label: "引用", icon: "quote", onSelect: () => void sendDraft(chat.draftKey, { type: "quote", text }) });
    if (isUser) out.push({ key: "edit", label: "编辑", icon: "pencil", onSelect: () => void sendDraft(chat.draftKey, { type: "replace", text }) });
  }
  return out;
}

/** Wraps a message: right-click / long press opens its menu. The hover bar goes under it (with the time), or beside it
 *  (`side`, your own bubbles: they had no row under them, so the layout does not move; the time stays the tooltip). */
export function MessageActions({
  text,
  isUser,
  ts,
  side,
  children,
  className,
}: {
  text: string;
  isUser?: boolean;
  ts?: string;
  side?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  const [selecting, setSelecting] = useState(false);
  const actions = useActions(text, Boolean(isUser), () => setSelecting(true));
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const anchorRef = useRef<HTMLElement | null>(null);
  const press = useRef<{ timer: number; x: number; y: number } | null>(null);
  const opened = useRef(0);

  const openAt = (x: number, y: number) => {
    anchorRef.current?.remove();
    const el = pointAnchor(x, y);
    anchorRef.current = el;
    setAnchor(el);
  };
  const close = () => {
    anchorRef.current?.remove();
    anchorRef.current = null;
    setAnchor(null);
  };
  useEffect(() => () => anchorRef.current?.remove(), []);

  const cancelPress = () => {
    if (press.current) clearTimeout(press.current.timer);
    press.current = null;
  };

  if (!actions.length) return <div className={className}>{children}</div>;

  const items: MenuItem[] = actions.map((a) => ({ key: a.key, label: a.label, icon: a.icon, onSelect: a.onSelect }));

  return (
    <div className={cx("group/msg", side && "relative", className)}>
      <div
        className="[@media(hover:none)]:select-none [@media(hover:none)]:[-webkit-touch-callout:none]"
        onContextMenu={(e) => {
          if (Date.now() - opened.current < 1000) {
            e.preventDefault(); // the long press already opened ours
            return;
          }
          // a selection inside the message (the browser's menu copies it), a link, a picture or code: the browser's own
          const sel = getSelection();
          if (sel && !sel.isCollapsed && sel.anchorNode && e.currentTarget.contains(sel.anchorNode)) return;
          if ((e.target as Element).closest?.("a, img, pre")) return;
          e.preventDefault();
          openAt(e.clientX, e.clientY);
        }}
        onPointerDown={(e) => {
          if (e.pointerType !== "touch") return;
          cancelPress();
          const x = e.clientX,
            y = e.clientY;
          press.current = {
            x,
            y,
            timer: window.setTimeout(() => {
              press.current = null;
              opened.current = Date.now();
              openAt(x, y);
            }, 450),
          };
        }}
        onPointerMove={(e) => {
          const p = press.current;
          if (p && Math.hypot(e.clientX - p.x, e.clientY - p.y) > 8) cancelPress();
        }}
        onPointerUp={cancelPress}
        onPointerCancel={cancelPress}
      >
        {children}
      </div>
      <div
        className={cx(
          "hidden h-6 items-center gap-0.5 opacity-0 transition-opacity duration-200 group-hover/msg:opacity-100 focus-within:opacity-100 [@media(hover:hover)]:flex",
          side ? "absolute right-full bottom-0.5 mr-1.5" : "mt-1.5",
        )}
      >
        {actions
          .filter((a) => a.key !== "select")
          .map((a) => (
            <button
              key={a.key}
              type="button"
              aria-label={a.key === "copy" ? (isUser ? "复制消息" : "拷贝回复") : a.label}
              title={a.label}
              onClick={a.onSelect}
              className="flex size-6 items-center justify-center rounded-[6px] text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2"
            >
              <Icon name={a.icon} size={14} />
            </button>
          ))}
        {!side && ts && <span className="ml-1 text-[11px] text-ink-3">{clock(ts)}</span>}
      </div>
      {anchor && <Menu anchor={anchor} onClose={close} items={items} width={168} />}
      <Dialog
        open={selecting}
        onClose={() => setSelecting(false)}
        title="选择文本"
        footer={
          <>
            <Button tone="quiet" onClick={() => setSelecting(false)}>
              完成
            </Button>
            <Button
              tone="primary"
              onClick={() => {
                void copyText(text);
                setSelecting(false);
              }}
            >
              全部拷贝
            </Button>
          </>
        }
      >
        <div className="max-h-[50vh] overflow-y-auto text-[14px] leading-relaxed whitespace-pre-wrap text-ink select-text [overflow-wrap:anywhere]">{text}</div>
      </Dialog>
    </div>
  );
}
