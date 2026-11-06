// The composer's @ menu (iOS PromptComposer.swift / BeautifulUI PromptBar): files this chat's tools touched and the
// session's folder, then (in the Persona assistant) workspaces on your computers, then other agents' sessions. A query with a slash browses that folder on the session's computer
// (`@src/` lists src); picking a folder keeps browsing, picking a file types `@path `, picking a workspace or a session
// adds it as a context chip.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, enc } from "../../lib/api";
import type { FsListing } from "../../lib/files";
import { browseTarget, byPrefix, displayTitle, nameMatches, otherSessions, relPath, sessionDetail, workspaceDetail, workspaceMatches, workspaceTitle, type MentionToken } from "../../lib/mentions";
import { machineWithHub } from "../../lib/mesh";
import type { Machine, Session } from "../../lib/types";
import type { Workspace, WorkspaceList } from "../../lib/workspace-board";
import { useStore } from "../../lib/store";
import { EASE, Spinner } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { t } from "../../i18n";

export interface MentionSource {
  /** The session's folder: relative paths and the first listing start here. */
  cwd?: string;
  /** This chat's draft key ("mid/sid"), left out of the sessions. */
  self?: string;
  /** Absolute paths the chat's tools read or wrote, newest first (read when the menu opens). */
  files: () => string[];
  /** Offer the hub's workspaces (GET /workspaces): the Persona assistant, which works on any computer. */
  workspaces?: boolean;
}

export type MentionRow =
  | { kind: "file"; id: string; name: string; detail: string; insert: string; dir: boolean }
  | { kind: "workspace"; id: string; name: string; detail: string; workspace: Workspace }
  | { kind: "session"; id: string; name: string; detail: string; session: Session };

const NO_SESSIONS: Record<string, Session[]> = {};
const NO_MACHINES: Machine[] = [];
const MAX_ROWS = 40;

/** The rows for the token being typed; the folder listings come from the session's computer (`/fs`), cached per folder. */
export function useMentionRows(token: MentionToken | null, source: MentionSource | undefined, machineId: string | undefined): { rows: MentionRow[]; loading: boolean } {
  const open = Boolean(token && source);
  const sessions = useStore((s) => (open ? s.sessions : NO_SESSIONS));
  const machines = useStore((s) => (open ? s.machines : NO_MACHINES));
  const workspaces = useWorkspaces(open && Boolean(source?.workspaces));
  const cache = useRef(new Map<string, FsListing | null>());
  const [, setTick] = useState(0);
  const [loading, setLoading] = useState(false);
  // read once per opening (the chat may grow meanwhile)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const files = useMemo(() => (open ? source!.files() : []), [open]);

  const cwd = source?.cwd;
  const target = token ? browseTarget(token.query, cwd) : null;
  const dir = !open || !machineId ? null : target ? target.dir : cwd ?? null;

  // a different session's computer or folder: start the cache over
  useEffect(() => cache.current.clear(), [machineId, cwd]);

  useEffect(() => {
    if (!dir || !machineId || cache.current.has(dir)) return;
    const ac = new AbortController();
    const t = setTimeout(() => {
      setLoading(true);
      api<FsListing>(`/machines/${enc(machineId)}/fs?path=${enc(dir)}&cwd=${enc(cwd ?? "")}`, { signal: ac.signal })
        .then((l) => cache.current.set(dir, l))
        .catch(() => {
          if (!ac.signal.aborted) cache.current.set(dir, null); // not a folder / unreadable: no rows from it
        })
        .finally(() => {
          if (ac.signal.aborted) return;
          setLoading(false);
          setTick((n) => n + 1);
        });
    }, 120);
    return () => {
      clearTimeout(t);
      ac.abort();
      setLoading(false);
    };
  }, [dir, machineId, cwd]);

  const listing = dir ? cache.current.get(dir) : undefined;
  const rows = useMemo<MentionRow[]>(() => {
    if (!token || !source) return [];
    if (target) {
      const entries = (listing?.entries ?? []).filter((e) => nameMatches(e.name, target.filter));
      const ordered = [...byPrefix(entries.filter((e) => e.dir), (e) => e.name, target.filter), ...byPrefix(entries.filter((e) => !e.dir), (e) => e.name, target.filter)];
      return ordered.slice(0, MAX_ROWS).map((e) => ({
        kind: "file",
        id: `fs:${target.typed}${e.name}`,
        name: e.dir ? `${e.name}/` : e.name,
        detail: target.typed,
        insert: `@${target.typed}${e.name}${e.dir ? "/" : " "}`,
        dir: e.dir,
      }));
    }
    const q = token.query.toLowerCase();
    const out: MentionRow[] = [];
    const used = new Set<string>();
    for (const p of files) {
      const rel = relPath(p, cwd);
      if (q && !rel.toLowerCase().includes(q)) continue;
      used.add(rel);
      const slash = rel.lastIndexOf("/");
      out.push({ kind: "file", id: `used:${p}`, name: rel.slice(slash + 1), detail: slash > 0 ? rel.slice(0, slash) : t("chat-mentionmenu.used_files"), insert: `@${rel} `, dir: false });
    }
    const top = (listing?.entries ?? []).filter((e) => nameMatches(e.name, q) && !used.has(e.name));
    for (const e of [...byPrefix(top.filter((x) => x.dir), (x) => x.name, q), ...byPrefix(top.filter((x) => !x.dir), (x) => x.name, q)].slice(0, 12)) {
      out.push({ kind: "file", id: `top:${e.name}`, name: e.dir ? `${e.name}/` : e.name, detail: e.dir ? t("chat-mentionmenu.folder") : t("chat-mentionmenu.session_dir"), insert: `@${e.name}${e.dir ? "/" : " "}`, dir: e.dir });
    }
    if (source.workspaces) {
      const name = (id: string) => machineWithHub(id, machines);
      for (const w of workspaceMatches(workspaces ?? [], q, name)) {
        out.push({ kind: "workspace", id: `workspace:${w.id}`, name: workspaceTitle(w), detail: workspaceDetail(w, name), workspace: w });
      }
    }
    for (const s of otherSessions(Object.values(sessions).flat(), source.self, q)) {
      out.push({ kind: "session", id: `session:${s.machine_id}/${s.session_id}`, name: displayTitle(s), detail: sessionDetail(s), session: s });
    }
    return out.slice(0, MAX_ROWS);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token?.query, source, listing, sessions, cwd, files, workspaces, machines]);

  return { rows, loading: (loading && !listing) || (Boolean(source?.workspaces) && open && workspaces === null) };
}

/** The hub's workspaces, read once each time the menu opens (null while reading; empty when the hub can't say). */
function useWorkspaces(open: boolean): Workspace[] | null {
  const [list, setList] = useState<Workspace[] | null>(null);
  useEffect(() => {
    if (!open) return;
    const ac = new AbortController();
    api<WorkspaceList>("/workspaces", { signal: ac.signal })
      .then((l) => setList(l.workspaces ?? []))
      .catch(() => {
        if (!ac.signal.aborted) setList((cur) => cur ?? []); // an old hub (404) or the manager away (503): no workspace rows
      });
    return () => ac.abort();
  }, [open]);
  return list;
}

function rowIcon(r: MentionRow): IconName {
  if (r.kind === "session") return "chat";
  if (r.kind === "workspace") return "folder";
  return r.dir ? "folder" : "read";
}

export function MentionMenu({ rows, active, loading, browsing, onHover, onPick }: { rows: MentionRow[]; active: number; loading: boolean; browsing: boolean; onHover: (i: number) => void; onPick: (r: MentionRow) => void }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const [box, setBox] = useState<{ top: number; height: number } | null>(null);
  useLayoutEffect(() => {
    const el = refs.current[active];
    setBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, rows.length]);
  // a line above the first row of each group after the files
  const groupStart = (i: number) => i > 0 && rows[i].kind !== "file" && rows[i - 1].kind !== rows[i].kind;
  return (
    <div className="absolute inset-x-0 bottom-full z-10 mb-2 rounded-[10px] bg-surface p-1 shadow-raised" style={{ animation: `pop-in 180ms ${EASE} both`, transformOrigin: "bottom center" }} role="listbox" aria-label={t("chat-mentionmenu.label")}>
      <div className="relative max-h-[min(50vh,320px)] overflow-y-auto">
        <span aria-hidden className="pointer-events-none absolute inset-x-0 rounded-[6px] bg-hover" style={{ top: box?.top ?? 0, height: box?.height ?? 0, opacity: box ? 1 : 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}` }} />
        {rows.map((r, i) => (
          <div key={r.id}>
            {groupStart(i) && <div className="mx-2 my-1 border-t border-line" />}
            <button
              ref={(el) => {
                refs.current[i] = el;
              }}
              type="button"
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => e.preventDefault()}
              onMouseEnter={() => onHover(i)}
              onClick={() => onPick(r)}
              className="relative z-10 flex h-9 w-full items-center gap-2.5 rounded-[6px] px-2 text-left"
            >
              <Icon name={rowIcon(r)} size={14} className="shrink-0 text-ink-3" />
              <span className="min-w-0 shrink truncate text-[12.5px] font-medium text-ink">{r.name}</span>
              <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">{r.detail}</span>
              {r.kind === "session" && <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-3">{t("chat-mentionmenu.as_context")}</span>}
              {r.kind === "workspace" && <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-3">{t("chat-mentionmenu.workspace")}</span>}
            </button>
          </div>
        ))}
        {!rows.length && (
          <div className="flex h-9 items-center gap-2 px-2 text-[12px] text-ink-3">
            {loading ? (
              <>
                <Spinner /> {t("chat-mentionmenu.reading_folder")}
              </>
            ) : browsing ? (
              t("chat-mentionmenu.no_match_in_folder")
            ) : (
              t("chat-mentionmenu.no_match")
            )}
          </div>
        )}
      </div>
      <div className="mt-1 border-t border-line px-2 pt-1.5 pb-1 text-[11px] text-ink-3">{t("chat-mentionmenu.keys_hint")}</div>
    </div>
  );
}
