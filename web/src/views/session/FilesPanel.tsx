// A session's files (docs/ios.md「文件」, iOS FilesPane): from the session's folder (or `path`) folder by folder, a
// breadcrumb back to any level; folders first with size, time and Git marks; hidden files off unless asked for. A
// file opens in the viewer inside the panel (back returns to the list where it was).
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText, isAbort } from "../../lib/api";
import { entryPayload, ENTRY_DRAG_TYPE } from "../../lib/entry-drag";
import { below, gitMark, joinPath, type FsEntry, type FsListing, type GitStatus } from "../../lib/files";
import { ago, baseName, bytes, shortPath } from "../../lib/format";
import { openPanel } from "../../lib/panel";
import { Button, copyText, cx, IconButton, Loading, Menu, useMenu } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { PanelHeader } from "../SidePanel";
import { FileView } from "./files/FileView";
import { Empty, FileGlyph, GitDot, GitBadge, useStoredFlag } from "./files/parts";

type Listed = { at: string; listing?: FsListing; error?: string };

export function FilesPanel({ mid, sid, cwd, path, onClose }: { mid: string; sid: string; cwd: string; path?: string; onClose: () => void }) {
  // The root is the session's folder; a `path` inside it opens there with the folders above it in the breadcrumb.
  const start = useMemo(() => {
    const inside = path && cwd ? below(cwd, path) : null;
    return { root: inside || !path ? cwd : path, stack: inside ?? [] };
  }, [cwd, path]);
  const [stack, setStack] = useState<string[]>(start.stack);
  useEffect(() => setStack(start.stack), [start]);
  const dir = stack.reduce(joinPath, start.root);

  const [showHidden, setShowHidden] = useStoredFlag("jarvis.files.showHidden", false);
  const [listed, setListed] = useState<Record<string, Listed>>({});
  const [reloads, setReloads] = useState(0);
  const [git, setGit] = useState<GitStatus | null>(null);
  const [open, setOpen] = useState<string[]>([]); // files opened from the list, newest last
  const menu = useMenu();

  useEffect(() => {
    if (!dir) return;
    const ac = new AbortController();
    api<FsListing>(`/machines/${enc(mid)}/fs?path=${enc(dir)}&cwd=${enc(cwd)}`, { signal: ac.signal })
      .then((l) => setListed((m) => ({ ...m, [dir]: { at: dir, listing: l } })))
      .catch((e) => {
        if (!isAbort(e)) setListed((m) => ({ ...m, [dir]: { at: dir, listing: m[dir]?.listing, error: errorText(e) } }));
      });
    return () => ac.abort();
  }, [mid, cwd, dir, reloads]);

  useEffect(() => {
    if (!cwd) return;
    const ac = new AbortController();
    api<GitStatus>(`/machines/${enc(mid)}/git/status?cwd=${enc(cwd)}`, { signal: ac.signal })
      .then(setGit)
      .catch(() => {}); // marks are extra: without Git the list is the same
    return () => ac.abort();
  }, [mid, cwd, reloads]);

  const go = useCallback((level: number) => setStack((s) => s.slice(0, level)), []);
  const enter = (e: FsEntry) => (e.dir ? setStack((s) => [...s, e.name]) : setOpen([joinPath(dir, e.name)]));

  // A path the viewer links to: a folder under the root opens in the list, anything else in the viewer.
  const openPath = (p: string) => {
    const rel = below(start.root, p);
    if (rel && listed[p]?.listing) {
      setOpen([]);
      setStack(rel);
    } else setOpen((o) => [...o, p]);
  };

  const here = listed[dir];
  const entries = (here?.listing?.entries ?? []).filter((e) => showHidden || !e.name.startsWith("."));
  const changes = git?.is_repo ? git.files.length : 0;
  const viewing = open[open.length - 1];

  return (
    <>
      <div className={cx("flex min-h-0 flex-1 flex-col", viewing && "hidden")}>
        <PanelHeader
          icon="folder"
          title="文件"
          subtitle={shortPath(dir)}
          onClose={onClose}
          actions={
            <>
              <button
                type="button"
                title="Git"
                aria-label={changes ? `Git，${changes} 处改动` : "Git"}
                onClick={() => openPanel({ kind: "git", mid, sid, cwd })}
                className="flex h-7 shrink-0 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 transition-colors hover:bg-hover hover:text-ink"
              >
                <Icon name="git" size={14} />
                Git
                {changes > 0 && <span className="rounded-full bg-field px-1.5 text-[11px] text-ink-2 tabular-nums">{changes}</span>}
              </button>
              <IconButton icon="more" label="更多" onClick={menu.open} active={Boolean(menu.anchor)} />
            </>
          }
        />
        {menu.anchor && (
          <Menu
            anchor={menu.anchor}
            onClose={menu.close}
            align="right"
            items={[
              { key: "hidden", label: "显示隐藏文件", checked: showHidden, onSelect: () => setShowHidden(!showHidden) },
              { key: "copy", label: "拷贝路径", icon: "copy", onSelect: () => copyText(dir) },
              { key: "reload", label: "刷新", icon: "retry", onSelect: () => setReloads((n) => n + 1) },
            ]}
          />
        )}
        <Crumbs root={start.root} stack={stack} go={go} />
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto" key={dir}>
          {here?.listing ? (
            <div className="py-1" style={{ animation: "fade-in 160ms ease-out both" }}>
              {entries.length === 0 && (
                <Empty
                  icon="folder"
                  title={here.listing.entries.length ? "只有隐藏文件" : "空文件夹"}
                  action={here.listing.entries.length ? <Button onClick={() => setShowHidden(true)}>显示隐藏文件</Button> : undefined}
                />
              )}
              {entries.map((e) => (
                <EntryRow key={e.name} mid={mid} e={e} full={joinPath(dir, e.name)} mark={gitMark(git, joinPath(dir, e.name), e.dir)} onOpen={() => enter(e)} />
              ))}
              {here.listing.truncated && <p className="px-4 py-2 text-[11.5px] text-ink-3">文件太多，只列出了前 2000 个</p>}
            </div>
          ) : here?.error ? (
            <Empty icon="folder" title="打不开这个文件夹" detail={here.error} action={<Button icon="retry" onClick={() => setReloads((n) => n + 1)}>重试</Button>} />
          ) : !dir ? (
            <Empty icon="folder" title="不知道这个会话的目录" />
          ) : (
            <div className="flex h-40 items-center justify-center">
              <Loading label="正在读取" />
            </div>
          )}
        </div>
      </div>
      {viewing && (
        <FileView
          key={viewing}
          mid={mid}
          path={viewing}
          cwd={cwd}
          onBack={() => setOpen((o) => o.slice(0, -1))}
          onClose={onClose}
          onOpenPath={openPath}
        />
      )}
    </>
  );
}

function Crumbs({ root, stack, go }: { root: string; stack: string[]; go: (level: number) => void }) {
  const bar = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    bar.current?.scrollTo({ left: bar.current.scrollWidth, behavior: "smooth" });
  }, [stack.length]);
  const names = [baseName(root) || "/", ...stack];
  return (
    <div className="flex h-10 shrink-0 items-center gap-1 border-b border-line px-2">
      <IconButton icon="chevronLeft" label="上一级" onClick={() => go(stack.length - 1)} disabled={!stack.length} iconSize={16} />
      <div ref={bar} className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto scrollbar-none" aria-label="位置">
        {names.map((n, i) => {
          const current = i === stack.length;
          return (
            <span key={i} className="flex shrink-0 items-center gap-0.5">
              {i > 0 && <Icon name="chevronRight" size={11} stroke={2.2} className="text-ink-3" />}
              <button
                type="button"
                onClick={() => !current && go(i)}
                aria-current={current ? "location" : undefined}
                title={i === 0 ? root : undefined}
                className={cx(
                  "h-6 rounded-full px-2 text-[12px] whitespace-nowrap transition-colors",
                  current ? "bg-field font-semibold text-ink" : "text-ink-2 hover:bg-hover hover:text-ink",
                )}
              >
                {n}
              </button>
            </span>
          );
        })}
      </div>
    </div>
  );
}

function EntryRow({ mid, e, full, mark, onOpen }: { mid: string; e: FsEntry; full: string; mark: string | null; onOpen: () => void }) {
  const hidden = e.name.startsWith(".");
  return (
    <div
      className="group/row relative flex items-center transition-colors hover:bg-hover"
      // dragged into a terminal or the chat box it becomes `@path ` (lib/entry-drag.ts)
      draggable
      onDragStart={(ev) => {
        ev.dataTransfer.setData(ENTRY_DRAG_TYPE, entryPayload({ mid, path: full, dir: e.dir }));
        ev.dataTransfer.effectAllowed = "copy";
      }}
    >
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 py-2 pr-2 pl-4 text-left" title={full}>
        <FileGlyph name={e.name} dir={e.dir} />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1">
            <span className={cx("truncate text-[13px]", hidden ? "text-ink-2" : "text-ink")}>{e.name}</span>
            {e.symlink && <Icon name="link" size={11} className="shrink-0 text-ink-3" />}
          </span>
          <span className="flex gap-2 text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums">
            {!e.dir && <span>{bytes(e.size)}</span>}
            {e.mtime && <span title={new Date(e.mtime).toLocaleString("zh-CN")}>{ago(e.mtime)}</span>}
          </span>
        </span>
        {mark && (e.dir ? <GitDot letter={mark} /> : <GitBadge letter={mark} />)}
        {e.dir && <Icon name="chevronRight" size={13} stroke={2.2} className="shrink-0 text-ink-3" />}
      </button>
      {/* hover devices only: on a touch screen it would be an invisible target at the row's end */}
      <span className="hidden pr-2 opacity-0 transition-opacity group-hover/row:opacity-100 focus-within:opacity-100 [@media(hover:hover)]:block">
        <IconButton icon="copy" label="拷贝路径" onClick={() => copyText(full)} size={26} iconSize={13} />
      </span>
    </div>
  );
}
