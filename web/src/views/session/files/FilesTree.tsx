// The Files panel's tree view (#2439, Moshi's form): folders open in place under their parent, folders first, what Git
// ignores dimmed. Listings are fetched folder by folder when one opens, kept in a small cache (lib/files-tree.ts) and
// kept fresh while the panel shows them (lib/fs-live.ts). The open folders are remembered per session.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText, isAbort } from "../../../lib/api";
import { t } from "../../../i18n";
import { entryPayload, ENTRY_DRAG_TYPE } from "../../../lib/entry-drag";
import { gitMark, type FsListing, type GitStatus } from "../../../lib/files";
import { expandedKey, listingCache, readExpanded, toggleExpanded, treeRows, visibleDirs, withExpanded, writeExpanded, type Listed, type TreeRow } from "../../../lib/files-tree";
import { useFsLive } from "../../../lib/use-fs-live";
import { Button, copyText, cx, IconButton, Loading, Spinner } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { Empty, FileGlyph, GitBadge, GitDot } from "./parts";

export function FilesTree({
  mid,
  sid,
  cwd,
  root,
  reveal,
  showHidden,
  git,
  reloads,
  onOpenFile,
  onShowHidden,
}: {
  mid: string;
  sid: string;
  cwd: string;
  /** The tree's top folder. */
  root: string;
  /** Folders to open so a path shows (the panel was opened at a path inside the root). */
  reveal: string[];
  showHidden: boolean;
  git: GitStatus | null;
  /** The panel's refresh button: every folder shown is listed again. */
  reloads: number;
  onOpenFile: (path: string) => void;
  onShowHidden: () => void;
}) {
  const cache = useMemo(() => listingCache(mid, cwd), [mid, cwd]);
  const storeKey = expandedKey(mid, sid);
  const [open, setOpen] = useState<string[]>(() => withExpanded(readExpanded(localStorage, storeKey), reveal));
  useEffect(() => writeExpanded(localStorage, storeKey, open), [storeKey, open]);
  useEffect(() => setOpen((o) => withExpanded(o, reveal)), [reveal]);

  // What was listed before shows at once; each folder is listed again once per mount (stale while revalidating).
  const [lists, setLists] = useState<Record<string, Listed>>(() => {
    const seed: Record<string, Listed> = {};
    for (const d of [root, ...open]) {
      const l = cache.get(d);
      if (l) seed[d] = { listing: l };
    }
    return seed;
  });
  const asked = useRef(new Set<string>());
  const aborts = useRef(new Set<AbortController>());
  const generation = useRef(new Map<string, number>());
  useEffect(() => {
    const live = aborts.current;
    return () => live.forEach((a) => a.abort());
  }, []);

  const fetchDir = useCallback(
    (dir: string) => {
      asked.current.add(dir);
      const n = (generation.current.get(dir) ?? 0) + 1;
      generation.current.set(dir, n);
      const ac = new AbortController();
      aborts.current.add(ac);
      api<FsListing>(`/machines/${enc(mid)}/fs?path=${enc(dir)}&cwd=${enc(cwd)}`, { signal: ac.signal })
        .then((l) => {
          if (generation.current.get(dir) !== n) return;
          cache.set(dir, l);
          setLists((m) => ({ ...m, [dir]: { listing: l } }));
        })
        .catch((e) => {
          if (isAbort(e) || generation.current.get(dir) !== n) return;
          setLists((m) => ({ ...m, [dir]: { listing: m[dir]?.listing, error: errorText(e) } }));
        })
        .finally(() => aborts.current.delete(ac));
    },
    [mid, cwd, cache],
  );

  const openSet = useMemo(() => new Set(open), [open]);
  const at = useCallback((d: string) => lists[d], [lists]);
  const visible = useMemo(() => visibleDirs(root, openSet, at), [root, openSet, at]);
  const rows = useMemo(() => treeRows(root, openSet, at, showHidden), [root, openSet, at, showHidden]);

  // A folder that shows and was not listed in this mount is listed.
  useEffect(() => {
    for (const d of visible) if (!asked.current.has(d)) fetchDir(d);
  }, [visible, fetchDir]);

  const reloadAll = useCallback(() => visible.forEach(fetchDir), [visible, fetchDir]);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) first.current = false;
    else reloadAll();
    // only the refresh button: `reloadAll` changes whenever a folder opens
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloads]);

  // The folders shown are watched; the ones that change are listed again (a folder closed meanwhile is not). The Git
  // half is the panel's (it holds the status the marks come from).
  useFsLive(mid, cwd, visible, false, (c) => {
    for (const d of c.dirs) if (visible.includes(d)) fetchDir(d);
  });

  const toggle = (dir: string) => setOpen((o) => toggleExpanded(o, dir));

  const top = rows[0];
  if (top?.depth === 0 && top.kind === "loading") {
    return (
      <div className="flex h-40 items-center justify-center">
        <Loading label={t("files-tree.loading")} />
      </div>
    );
  }
  if (top?.depth === 0 && top.kind === "error") {
    return <Empty icon="folder" title={t("files-tree.openFailed")} detail={top.error} action={<Button icon="retry" onClick={() => fetchDir(root)}>{t("files-tree.retry")}</Button>} />;
  }
  if (top?.depth === 0 && top.kind === "empty") {
    const hiddenOnly = (lists[root]?.listing?.entries.length ?? 0) > 0;
    return <Empty icon="folder" title={hiddenOnly ? t("files-tree.hiddenOnly") : t("files-tree.emptyFolder")} action={hiddenOnly ? <Button onClick={onShowHidden}>{t("files-tree.showHidden")}</Button> : undefined} />;
  }

  return (
    <div role="tree" aria-label={t("files-tree.tree")} className="py-1" style={{ animation: "fade-in 160ms ease-out both" }}>
      {rows.map((r) => (
        <Row key={r.key} r={r} mid={mid} git={git} onToggle={toggle} onOpenFile={onOpenFile} onRetry={fetchDir} />
      ))}
    </div>
  );
}

/** Where a row's content starts: one step in per level (the design system's spacing unit). */
const indent = (depth: number, extra = 0) => ({ paddingLeft: `calc(var(--spacing) * ${3 + depth * 4 + extra})` });

function Row({
  r,
  mid,
  git,
  onToggle,
  onOpenFile,
  onRetry,
}: {
  r: TreeRow;
  mid: string;
  git: GitStatus | null;
  onToggle: (dir: string) => void;
  onOpenFile: (path: string) => void;
  onRetry: (dir: string) => void;
}) {
  if (r.kind === "loading") {
    return (
      <div role="none" style={indent(r.depth, 7)} className="flex h-8 items-center gap-2 text-[12px] text-ink-3">
        <Spinner />
        {t("files-tree.loading")}
      </div>
    );
  }
  if (r.kind === "empty") {
    return (
      <div role="none" style={indent(r.depth, 7)} className="flex h-8 items-center text-[12px] text-ink-3">
        {t("files-tree.emptyFolder")}
      </div>
    );
  }
  if (r.kind === "more") {
    return (
      <div role="none" style={indent(r.depth, 7)} className="py-1.5 text-[11.5px] text-ink-3">
        {t("files-tree.tooMany")}
      </div>
    );
  }
  if (r.kind === "error") {
    return (
      <div role="none" style={indent(r.depth, 7)} className="flex min-h-8 items-center gap-2 pr-3 text-[12px] text-ink-3">
        <span className="min-w-0 flex-1 truncate" title={r.error}>
          {t("files-tree.openFailedInline", { error: r.error })}
        </span>
        <Button onClick={() => onRetry(r.path)}>{t("files-tree.retry")}</Button>
      </div>
    );
  }
  const e = r.entry;
  const mark = gitMark(git, r.path, e.dir);
  const hidden = e.name.startsWith(".");
  return (
    <div
      role="treeitem"
      aria-level={r.depth + 1}
      aria-expanded={e.dir ? r.expanded : undefined}
      className="group/row relative flex items-center transition-colors hover:bg-hover"
      // dragged into a terminal or the chat box it becomes `@path ` (lib/entry-drag.ts)
      draggable
      onDragStart={(ev) => {
        ev.dataTransfer.setData(ENTRY_DRAG_TYPE, entryPayload({ mid, path: r.path, dir: e.dir }));
        ev.dataTransfer.effectAllowed = "copy";
      }}
    >
      <button
        type="button"
        onClick={() => (e.dir ? onToggle(r.path) : onOpenFile(r.path))}
        style={indent(r.depth)}
        className="flex min-w-0 flex-1 items-center gap-2 py-1.5 pr-2 text-left"
        title={e.ignored ? t("files-tree.gitIgnored", { path: r.path }) : r.path}
      >
        {e.dir ? <Icon name={r.expanded ? "chevron" : "chevronRight"} size={12} stroke={2.2} className="shrink-0 text-ink-3" /> : <span aria-hidden className="size-3 shrink-0" />}
        <span className={cx("flex shrink-0", e.ignored && "opacity-50")}>
          <FileGlyph name={e.name} dir={e.dir} />
        </span>
        <span className={cx("truncate text-[13px]", e.ignored ? "text-ink-3" : hidden ? "text-ink-2" : "text-ink")}>{e.name}</span>
        {e.symlink && <Icon name="link" size={11} className="shrink-0 text-ink-3" />}
        <span className="flex-1" />
        {mark && (e.dir ? <GitDot letter={mark} /> : <GitBadge letter={mark} />)}
      </button>
      {/* hover devices only: on a touch screen it would be an invisible target at the row's end */}
      <span className="hidden pr-2 opacity-0 transition-opacity group-hover/row:opacity-100 focus-within:opacity-100 [@media(hover:hover)]:block">
        <IconButton icon="copy" label={t("files-tree.copyPath")} onClick={() => copyText(r.path)} size={26} iconSize={13} />
      </span>
    </div>
  );
}
