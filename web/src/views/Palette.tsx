// ⌘K: Beautiful UI's Search, over every agent and Persona conversation.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { machineWithHub } from "../lib/mesh";
import { createPortal } from "react-dom";
import { ago, sessionTitle, shortPath, stateOf, STATE_LABEL, tabLabel } from "../lib/format";
import { navigate, type Route } from "../lib/router";
import { useAgentSessions } from "../lib/sessions";
import { useStore } from "../lib/store";
import type { Session } from "../lib/types";
import { EASE, Kbd, StatusDot } from "../ui/atoms";
import { Icon } from "../ui/icons";

interface Hit {
  key: string;
  s: Session;
  to: Route;
  assistant?: boolean;
  score: number;
}

function score(s: Session, q: string, machine: string): number {
  if (!q) return 1;
  const title = sessionTitle(s).toLowerCase();
  if (title.startsWith(q)) return 100;
  if (title.includes(q)) return 80;
  const hay = [s.cwd, s.space_name, s.slot_name, machine, s.last_text].join(" ").toLowerCase();
  return q.split(/\s+/).every((w) => hay.includes(w) || title.includes(w)) ? 40 : 0;
}

export function Palette({ open, onClose, onNewAgent }: { open: boolean; onClose: () => void; onNewAgent: () => void }) {
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const sessions = useAgentSessions();
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const rows = useRef<(HTMLButtonElement | null)[]>([]);
  const list = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      setQ("");
      setActive(0);
    }
  }, [open]);

  const hits = useMemo(() => {
    const query = q.trim().toLowerCase();
    const name = (m: string) => machineWithHub(m, machines);
    const out: Hit[] = [];
    for (const s of sessions) {
      const sc = score(s, query, name(s.machine_id));
      if (sc) out.push({ key: `s:${s.machine_id}/${s.session_id}`, s, to: { name: "session", mid: s.machine_id, sid: s.session_id }, score: sc + (stateOf(s) === "offline" ? 0 : 5) });
    }
    for (const s of assistant?.conversations ?? []) {
      const sc = score(s, query, "jarvis");
      if (sc) out.push({ key: `j:${s.session_id}`, s, to: { name: "jarvis", sid: s.session_id }, assistant: true, score: sc });
    }
    out.sort((a, b) => b.score - a.score || (b.s.updated_at ?? "").localeCompare(a.s.updated_at ?? ""));
    return out.slice(0, 40);
  }, [q, sessions, assistant, machines]);

  useEffect(() => setActive(0), [q]);
  useLayoutEffect(() => {
    const el = rows.current[active];
    setBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, hits]);

  if (!open) return null;
  const pick = (h: Hit) => {
    navigate(h.to);
    onClose();
  };

  return createPortal(
    <div className="fixed inset-0 z-40 flex items-start justify-center bg-black/20 px-4 pt-[14vh] backdrop-blur-[2px] dark:bg-black/45" style={{ animation: "fade-in 120ms ease-out both" }} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-label="搜索会话" className="w-full max-w-[560px] overflow-hidden rounded-[14px] bg-surface shadow-overlay" style={{ animation: `slide-up 200ms ${EASE} both` }}>
        <label className="flex h-12 items-center gap-2.5 border-b border-line px-4">
          <Icon name="search" size={15} stroke={2} className="text-ink-3" />
          <input
            autoFocus
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.nativeEvent.isComposing) return;
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                if (hits.length) setActive((a) => (a + (e.key === "ArrowDown" ? 1 : hits.length - 1)) % hits.length);
              } else if (e.key === "Enter") {
                e.preventDefault();
                if (hits[active]) pick(hits[active]);
              } else if (e.key === "Escape") onClose();
            }}
            placeholder="搜索会话、目录、空间、机器…"
            className="min-w-0 flex-1 bg-transparent text-[14px] text-ink outline-none placeholder:text-ink-3"
          />
          <Kbd>Esc</Kbd>
        </label>
        <div ref={list} className="relative max-h-[52vh] overflow-y-auto p-1.5" onMouseLeave={() => {}}>
          <span aria-hidden className="pointer-events-none absolute inset-x-1.5 rounded-[8px] bg-hover" style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 180ms ${EASE}, height 180ms ${EASE}` }} />
          {hits.map((h, i) => {
            const st = stateOf(h.s);
            const meta = h.assistant ? "Persona" : [machineWithHub(h.s.machine_id, machines), h.s.space_name, tabLabel(h.s)].filter(Boolean).join(" › ");
            return (
              <button
                key={h.key}
                ref={(el) => {
                  rows.current[i] = el;
                }}
                type="button"
                onMouseMove={() => setActive(i)}
                onClick={() => pick(h)}
                className="relative z-10 flex w-full items-center gap-3 rounded-[8px] px-2.5 py-2 text-left"
              >
                <span className="flex size-5 shrink-0 items-center justify-center">{h.assistant ? <Icon name="spark" size={13} className="text-ink-3" /> : <StatusDot state={st} />}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-ink">{sessionTitle(h.s)}</span>
                  <span className="block truncate text-[11.5px] text-ink-3">{[meta, shortPath(h.s.cwd)].filter(Boolean).join(" · ")}</span>
                </span>
                <span className="shrink-0 text-[11px] text-ink-3">{st === "offline" || h.assistant ? ago(h.s.updated_at) : STATE_LABEL[st]}</span>
              </button>
            );
          })}
          {hits.length === 0 && (
            <div className="flex flex-col items-center gap-3 px-4 py-10 text-center">
              <span className="flex size-9 items-center justify-center rounded-full bg-field text-ink-3 shadow-hairline">
                <Icon name="search" size={15} />
              </span>
              <div>
                <p className="text-[13px] font-medium text-ink">没有找到「{q}」</p>
                <p className="mt-0.5 text-[12px] text-ink-3">换个词，或者开一个新的 Agent</p>
              </div>
              <button
                type="button"
                onClick={() => {
                  onClose();
                  onNewAgent();
                }}
                className="rounded-control px-2.5 py-1 text-[12.5px] font-medium text-accent-ink hover:bg-accent-tint"
              >
                新建 Agent
              </button>
            </div>
          )}
        </div>
        <div className="flex items-center gap-3 border-t border-line bg-inset px-4 py-2 text-[11px] text-ink-3">
          <span className="flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd> 选择
          </span>
          <span className="flex items-center gap-1">
            <Kbd>↵</Kbd> 打开
          </span>
        </div>
      </div>
    </div>,
    document.body,
  );
}
