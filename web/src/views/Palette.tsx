// ⌘K: Beautiful UI's Search, over every agent and Persona conversation — and the terminal panes of the computers
// (lib/terminals.ts, the /terminal page's data). `>` first (or ⌘⇧P) is command mode: the registered commands with their
// shortcuts, and whatever other modules add through registerContextCommands (lib/palette.ts). docs/web.md「快捷键」.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { enc, api } from "../lib/api";
import { machineWithHub } from "../lib/mesh";
import { createPortal } from "react-dom";
import { ago, sessionTitle, shortPath, stateOf, STATE_LABEL, tabLabel } from "../lib/format";
import { appCommands, commandKeys, contextCommands, filterCommands, parseQuery, setPaletteQuery, terminalHits, usePalette, type MachinePanes, type PaletteCommand, type TerminalHit } from "../lib/palette";
import { href, navigate, type Route } from "../lib/router";
import { useAgentSessions } from "../lib/sessions";
import { handlerOf, runShortcut, useBindings } from "../lib/shortcuts";
import { useStore } from "../lib/store";
import { buildTerminalTree, type PanesResponse } from "../lib/terminals";
import type { Session } from "../lib/types";
import { EASE, Kbd, StatusDot, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

interface Hit {
  key: string;
  s: Session;
  to: Route;
  assistant?: boolean;
  score: number;
}

/** One selectable row of the list: a conversation, a terminal pane, or (command mode) a command. */
type Row = { kind: "session"; key: string; hit: Hit } | { kind: "pane"; key: string; hit: TerminalHit } | { kind: "command"; key: string; cmd: PaletteCommand };

function score(s: Session, q: string, machine: string): number {
  if (!q) return 1;
  const title = sessionTitle(s).toLowerCase();
  if (title.startsWith(q)) return 100;
  if (title.includes(q)) return 80;
  const hay = [s.cwd, s.space_name, s.slot_name, machine, s.last_text].join(" ").toLowerCase();
  return q.split(/\s+/).every((w) => hay.includes(w) || title.includes(w)) ? 40 : 0;
}

const HOME: Route = { name: "home" };

export function Palette({ open, onClose, onNewAgent, pane, route = HOME }: { open: boolean; onClose: () => void; onNewAgent: () => void; pane?: { mid: string; sid: string }; route?: Route }) {
  // what is typed lives in the store (lib/palette.ts): the shortcuts open it in a mode (`>` for commands) and read it
  const q = usePalette().query;
  const setQ = setPaletteQuery;
  const { mode, text } = parseQuery(q);
  const [active, setActive] = useState(0);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  const sessions = useAgentSessions();
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const bySession = useStore((s) => s.sessions);
  const panesOn = useStore((s) => s.features.includes("terminal_panes"));
  const bindings = useBindings();
  const rows = useRef<(HTMLButtonElement | null)[]>([]);
  const list = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) setActive(0);
  }, [open]);

  // The terminal panes of the online computers, read when the palette opens (the /terminal page's endpoint).
  const onlineKey = machines.filter((m) => m.online).map((m) => m.id).join("\n");
  const [trees, setTrees] = useState<Record<string, PanesResponse>>({});
  useEffect(() => {
    if (!open || !panesOn || !onlineKey) return;
    let live = true;
    for (const mid of onlineKey.split("\n")) {
      api<PanesResponse>(`/machines/${enc(mid)}/panes`).then(
        (res) => live && setTrees((t) => ({ ...t, [mid]: res })),
        () => {}, // a computer that cannot be read just has no panes here
      );
    }
    return () => {
      live = false;
    };
  }, [open, panesOn, onlineKey]);
  const machinePanes = useMemo<MachinePanes[]>(
    () =>
      machines.flatMap((m) =>
        trees[m.id] ? [{ mid: m.id, machine: machineWithHub(m.id, machines), spaces: buildTerminalTree(trees[m.id], bySession[m.id] ?? []), sessionIds: new Set((bySession[m.id] ?? []).map((x) => x.session_id)) }] : [],
      ),
    [machines, trees, bySession],
  );

  const hits = useMemo((): Row[] => {
    if (mode !== "jump") return [];
    const query = text.toLowerCase();
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
    const all: (Row & { score: number; at: string })[] = [
      ...out.map((h) => ({ kind: "session" as const, key: h.key, hit: h, score: h.score, at: h.s.updated_at ?? "" })),
      ...terminalHits(machinePanes, text).map((h) => ({ kind: "pane" as const, key: h.key, hit: h, score: h.score, at: "" })),
    ];
    all.sort((a, b) => b.score - a.score || b.at.localeCompare(a.at));
    return all.slice(0, 40);
  }, [mode, text, sessions, assistant, machines, machinePanes]);

  // command mode: what other modules offer for here, then the registered commands with their shortcuts
  const commands = useMemo((): Row[] => {
    if (!open || mode !== "command") return [];
    const run = (id: string, digit?: number) => {
      if (!runShortcut(id, { digit })) toast("这里用不了这个命令");
    };
    const extra: PaletteCommand[] = [
      { id: "app:new-agent", title: "新建 Agent", section: "全局", icon: "plus", keywords: "new agent", run: onNewAgent },
      { id: "app:new-conversation", title: "新对话", section: "全局", icon: "chat", keywords: "persona new", run: () => navigate({ name: "persona-new" }) },
    ];
    const all = [...contextCommands({ route, pane }), ...appCommands({ bindings, available: (id) => Boolean(handlerOf(id)), run, machines: machines.map((m) => ({ id: m.id, name: m.name })), extra })];
    return filterCommands(all, text).map((cmd) => ({ kind: "command" as const, key: cmd.id, cmd }));
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `route` is parsed anew each render; its path says it all
  }, [open, mode, text, bindings, machines, pane?.mid, pane?.sid, href(route), onNewAgent]);
  const shown = mode === "command" ? commands : hits;

  useEffect(() => setActive(0), [q]);
  useLayoutEffect(() => {
    const el = rows.current[active];
    setBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, shown]);

  if (!open) return null;
  const pick = (r: Row) => {
    onClose();
    if (r.kind === "command") r.cmd.run();
    else navigate(r.hit.to);
  };
  const command = mode === "command";

  return createPortal(
    <div className="fixed inset-0 z-40 flex items-start justify-center bg-black/20 px-4 pt-[14vh] backdrop-blur-[2px] dark:bg-black/45" style={{ animation: "fade-in 120ms ease-out both" }} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" data-shortcut-layer aria-label={command ? "命令面板" : "搜索会话"} className="w-full max-w-[560px] overflow-hidden rounded-[14px] bg-surface shadow-overlay" style={{ animation: `slide-up 200ms ${EASE} both` }}>
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
                if (shown.length) setActive((a) => (a + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
              } else if (e.key === "Enter") {
                e.preventDefault();
                if (shown[active]) pick(shown[active]);
              } else if (e.key === "Escape") onClose();
            }}
            placeholder={command ? "输入命令…" : "搜索会话、终端、目录、空间、机器…"}
            className="min-w-0 flex-1 bg-transparent text-[14px] text-ink outline-none placeholder:text-ink-3"
          />
          {/* the key hints are for a keyboard: a touch screen has none (#1996) */}
          <span className="contents [@media(pointer:coarse)]:hidden">
            <Kbd>Esc</Kbd>
          </span>
        </label>
        <div ref={list} className="relative max-h-[52vh] overflow-y-auto p-1.5" onMouseLeave={() => {}}>
          <span aria-hidden className="pointer-events-none absolute inset-x-1.5 rounded-[8px] bg-hover" style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 180ms ${EASE}, height 180ms ${EASE}` }} />
          {shown.map((r, i) => {
            const ref = (el: HTMLButtonElement | null) => {
              rows.current[i] = el;
            };
            const rowClass = "relative z-10 flex w-full items-center gap-3 rounded-[8px] px-2.5 py-2 text-left";
            if (r.kind === "command") {
              const c = r.cmd;
              const heading = c.section && c.section !== (shown[i - 1] as Extract<Row, { kind: "command" }> | undefined)?.cmd.section;
              return (
                <div key={r.key}>
                  {heading && <div className="px-2.5 pt-2 pb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">{c.section}</div>}
                  <button ref={ref} type="button" onMouseMove={() => setActive(i)} onClick={() => pick(r)} className={rowClass}>
                    <span className="flex size-5 shrink-0 items-center justify-center">{c.icon && <Icon name={c.icon} size={13} className={c.danger ? "text-red" : "text-ink-3"} />}</span>
                    <span className={`min-w-0 flex-1 truncate text-[13px] ${c.danger ? "text-red" : "text-ink"}`}>{c.title}</span>
                    {c.chord && (
                      <span className="flex shrink-0 items-center gap-1 [@media(pointer:coarse)]:hidden">
                        {commandKeys(c).map((k, n) => (
                          <Kbd key={n}>{k}</Kbd>
                        ))}
                      </span>
                    )}
                  </button>
                </div>
              );
            }
            if (r.kind === "pane") {
              const h = r.hit;
              return (
                <button key={r.key} ref={ref} type="button" onMouseMove={() => setActive(i)} onClick={() => pick(r)} className={rowClass}>
                  <span className="flex size-5 shrink-0 items-center justify-center">{h.state === "shell" ? <Icon name="terminal" size={13} className="text-ink-3" /> : <StatusDot state={h.state} />}</span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13px] font-medium text-ink">{h.title}</span>
                    <span className="block truncate text-[11.5px] text-ink-3">{[h.trail, h.detail].filter(Boolean).join(" · ")}</span>
                  </span>
                  <span className="shrink-0 text-[11px] text-ink-3">终端</span>
                </button>
              );
            }
            const h = r.hit;
            const st = stateOf(h.s);
            const meta = h.assistant ? "Persona" : [machineWithHub(h.s.machine_id, machines), h.s.space_name, tabLabel(h.s)].filter(Boolean).join(" › ");
            return (
              <button key={h.key} ref={ref} type="button" onMouseMove={() => setActive(i)} onClick={() => pick(r)} className={rowClass}>
                <span className="flex size-5 shrink-0 items-center justify-center">{h.assistant ? <Icon name="spark" size={13} className="text-ink-3" /> : <StatusDot state={st} />}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-ink">{sessionTitle(h.s)}</span>
                  <span className="block truncate text-[11.5px] text-ink-3">{[meta, shortPath(h.s.cwd)].filter(Boolean).join(" · ")}</span>
                </span>
                <span className="shrink-0 text-[11px] text-ink-3">{st === "offline" || h.assistant ? ago(h.s.updated_at) : STATE_LABEL[st]}</span>
              </button>
            );
          })}
          {shown.length === 0 && command && (
            <div className="px-4 py-10 text-center">
              <p className="text-[13px] font-medium text-ink">没有匹配的命令</p>
              <p className="mt-0.5 text-[12px] text-ink-3">删掉「&gt;」可以搜索会话和终端</p>
            </div>
          )}
          {shown.length === 0 && !command && (
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
        <div className="flex items-center gap-3 border-t border-line bg-inset px-4 py-2 text-[11px] text-ink-3 [@media(pointer:coarse)]:hidden">
          <span className="flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd> 选择
          </span>
          <span className="flex items-center gap-1">
            <Kbd>↵</Kbd> {command ? "执行" : "打开"}
          </span>
          {!command && (
            <span className="flex items-center gap-1">
              <Kbd>&gt;</Kbd> 命令
            </span>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
