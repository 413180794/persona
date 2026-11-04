import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { api, enc, errorText } from "../lib/api";
import { sessionTitle, stateOf } from "../lib/format";
import { openNewAgent } from "../lib/new-agent";
import { openPanel } from "../lib/panel";
import { useAgentSessions } from "../lib/sessions";
import { onHubEvent, useStore } from "../lib/store";
import { BoardModel, boundCard, cardSessions, columnCards, sameDirectory, selectedColumn, type BoardCard, type BoardColumn, type BoardMutation, type BoardState, type Workspace } from "../lib/workspace-board";
import type { Machine, Session } from "../lib/types";
import { Button, cx, Dialog, IconButton, Loading, Menu, StatePill, toast, type MenuItem } from "../ui/atoms";
import { Icon } from "../ui/icons";
const field = "w-full rounded-control bg-field px-3 py-2 text-sm text-ink outline-none focus:ring-2 focus:ring-accent";
type Edit = { kind: "column" | "card"; id?: string; title: string };

export function WorkspaceBoard({ onToggleSidebar }: { onToggleSidebar?: () => void }) {
  const [model] = useState(() => new BoardModel());
  const state = useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
  const machines = useStore((s) => s.machines), sessions = useAgentSessions();
  const [search, setSearch] = useState(""), [machine, setMachine] = useState("");
  const [archived, setArchived] = useState(false), [selected, setSelected] = useState<string>(), [adding, setAdding] = useState<string>();
  const [edit, setEdit] = useState<Edit | null>(null), [deleting, setDeleting] = useState<BoardColumn | null>(null), [moveTo, setMoveTo] = useState("");
  const [menu, setMenu] = useState<{ anchor: HTMLElement; card?: BoardCard; column?: BoardColumn } | null>(null);
  const dragging = useRef<string | null>(null);
  const refreshing = useRef(false);
  const [drop, setDrop] = useState("");
  const [columnSelection, setColumnSelection] = useState("");
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "hidden" || refreshing.current) return;
      refreshing.current = true;
      void model.refresh().finally(() => { refreshing.current = false; });
    };
    refresh(); const timer = setInterval(refresh, 10_000);
    document.addEventListener("visibilitychange", refresh);
    const off = onHubEvent("workspace_board", (e) => { if (typeof e.revision !== "number" || e.revision > (model.snapshot().board?.revision ?? -1)) refresh(); });
    return () => { clearInterval(timer); document.removeEventListener("visibilitychange", refresh); off(); };
  }, [model]);
  const board = state.board, disabled = state.busy || state.readonly;
  const activeColumn = board ? selectedColumn(board, columnSelection) : "";
  const card = board?.cards.find((c) => c.id === selected);
  const names = useMemo(() => new Map(machines.map((m) => [m.id, m.name || m.id])), [machines]);
  const machineIDs = [...new Set([...machines.map((m) => m.id), ...(board?.cards.map((c) => c.machine_id) ?? [])])];
  const mutate = async (m: BoardMutation) => { const ok = await model.mutate(m); if (!ok && model.snapshot().error) toast(model.snapshot().error, "error"); return ok; };
  const dropCard = (e: React.DragEvent, column: string, before?: string) => {
    e.preventDefault(); e.stopPropagation(); const id = dragging.current || e.dataTransfer.getData("application/x-persona-workspace"); dragging.current = null; setDrop("");
    if (!disabled && id && id !== before && board?.cards.some((c) => c.id === id)) void mutate({ action: "move_card", id, column_id: column, before_id: before });
  };
  const cardMenu = (c: BoardCard): MenuItem[] => {
    const peers = columnCards(board!, c.column_id, { archived: c.archived }).filter((p) => p.pinned === c.pinned), i = peers.findIndex((p) => p.id === c.id);
    return [
      { key: "rename", label: "重命名卡片", icon: "write", onSelect: () => setEdit({ kind: "card", id: c.id, title: c.title }) },
      { key: "pin", label: c.pinned ? "取消置顶" : "置顶", icon: "pin", onSelect: () => void mutate({ action: "update_card", id: c.id, pinned: !c.pinned }) },
      { key: "up", label: "向上移动", icon: "up", disabled: i <= 0, onSelect: () => void mutate({ action: "move_card", id: c.id, column_id: c.column_id, before_id: peers[i - 1]?.id }) },
      { key: "down", label: "向下移动", icon: "arrowDown", disabled: i >= peers.length - 1, onSelect: () => void mutate({ action: "move_card", id: c.id, column_id: c.column_id, before_id: peers[i + 2]?.id }) },
      ...board!.columns.filter((v) => v.id !== c.column_id).map((v) => ({ key: v.id, label: `移到「${v.title}」`, icon: "layers" as const, onSelect: () => void mutate({ action: "move_card", id: c.id, column_id: v.id }) })),
      { key: "archive", label: c.archived ? "恢复到看板" : "归档卡片", icon: c.archived ? "undo" : "check", divider: true, onSelect: () => void mutate({ action: "update_card", id: c.id, archived: !c.archived }) },
    ];
  };
  const columnMenu = (c: BoardColumn): MenuItem[] => {
    const i = board!.columns.findIndex((v) => v.id === c.id);
    return [
      { key: "rename", label: "重命名列", icon: "write", onSelect: () => setEdit({ kind: "column", id: c.id, title: c.title }) },
      { key: "left", label: "向左移动", icon: "chevronLeft", disabled: i === 0, onSelect: () => void mutate({ action: "move_column", id: c.id, before_id: board!.columns[i - 1]?.id }) },
      { key: "right", label: "向右移动", icon: "chevronRight", disabled: i === board!.columns.length - 1, onSelect: () => void mutate({ action: "move_column", id: c.id, before_id: board!.columns[i + 2]?.id }) },
      { key: "delete", label: "删除列…", icon: "x", danger: true, disabled: board!.columns.length <= 1, onSelect: () => { setDeleting(c); setMoveTo(""); } },
    ];
  };
  return <div className="@container flex h-full flex-col bg-page">
    <header className="flex shrink-0 items-center gap-3 border-b border-line bg-surface px-4 py-3">
      {onToggleSidebar && <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />}
      <div className="min-w-0 flex-1"><h1 className="truncate text-base font-semibold text-ink">工作流看板</h1><p className="truncate text-xs text-ink-3">按工作空间推进任务 · 手动排序</p></div>
      <Button icon="plus" disabled={!board || disabled} onClick={() => setEdit({ kind: "column", title: "" })}>添加列</Button>
    </header>
    <div className="flex flex-wrap items-center gap-2 border-b border-line bg-surface px-4 py-3">
      <label className="flex min-w-0 flex-1 items-center gap-2 rounded-control bg-field px-3 py-2"><Icon name="search" size={14} className="text-ink-3" /><input aria-label="搜索工作空间" placeholder="搜索名称、目录、分支" className="min-w-0 flex-1 bg-transparent text-sm text-ink outline-none" value={search} onChange={(e) => setSearch(e.target.value)} /></label>
      <select aria-label="筛选机器" className="max-w-full rounded-control bg-field px-3 py-2 text-sm text-ink" value={machine} onChange={(e) => setMachine(e.target.value)}><option value="">全部机器</option>{machineIDs.map((id) => <option key={id} value={id}>{names.get(id) || id}</option>)}</select>
      <Button tone={archived ? "primary" : "quiet"} onClick={() => setArchived(!archived)}>{archived ? "返回看板" : "已归档"}</Button><IconButton icon="retry" label="刷新看板" onClick={() => void model.refresh()} disabled={state.busy} />
    </div>
    {board && <div className="border-b border-line bg-surface px-4 pb-3 @3xl:hidden"><select aria-label="选择工作流列" className={field} value={activeColumn} onChange={(e) => setColumnSelection(e.target.value)}>{board.columns.map((c) => <option key={c.id} value={c.id}>{c.title} · {columnCards(board, c.id, { search, machine, archived }).length}</option>)}</select></div>}
    {state.readonly && <p className="border-b border-line bg-inset px-4 py-2 text-xs text-ink-2">只读连接 · 可查看工作空间，编辑需要完整权限。</p>}
    {state.error && <p role="alert" className="border-b border-line bg-red-tint px-4 py-2 text-sm text-red">{state.error}</p>}
    {(state.discoveryError || state.discovery.errors?.length) ? <div role="status" className="border-b border-line bg-orange-tint px-4 py-2 text-xs text-orange">{state.discoveryError && <p>空间发现失败：{state.discoveryError}</p>}{state.discovery.errors?.map((e, index) => <p key={`${e.machine_id}:${index}`}>{names.get(e.machine_id) || e.machine_id}：{e.error}</p>)}<p>已有卡片保留在原列，可继续查看。</p></div> : null}
    {state.loading && !board ? <div className="p-6"><Loading label="正在加载工作流看板" /></div> : board ? <div className="flex min-h-0 flex-1 gap-4 overflow-x-auto p-4" aria-label={archived ? "已归档工作空间" : "工作流列"}>
      {board.columns.map((column) => { const cards = columnCards(board, column.id, { search, machine, archived });
        return <section key={column.id} className={cx("w-full shrink-0 flex-col rounded-card bg-canvas @3xl:flex @3xl:w-72 @3xl:min-w-64 @3xl:flex-1", column.id === activeColumn ? "flex" : "hidden")} aria-label={column.title}>
          <div className="flex items-center gap-2 px-3 py-3"><span className="size-2 rounded-full bg-accent" /><h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink" title={column.title}>{column.title}</h2><span className="text-xs text-ink-3">{cards.length}</span><IconButton icon="more" label={`${column.title}列菜单`} disabled={disabled} onClick={(e) => setMenu({ anchor: e.currentTarget, column })} /></div>
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 pb-3">
            {cards.map((c) => <div key={c.id} className={cx("rounded-card", drop === c.id && "ring-2 ring-accent")} onDragOver={(e) => { if (!disabled && dragging.current) { e.preventDefault(); e.stopPropagation(); setDrop(c.id); } }} onDrop={(e) => dropCard(e, column.id, c.id)}>
              <WorkspaceCard card={c} machine={machines.find((m) => m.id === c.machine_id)} workspace={state.discovery.workspaces.find((w) => w.id === c.workspace_id || w.machine_id === c.machine_id && sameDirectory(w.cwd, c.cwd))} sessions={cardSessions(c, sessions)} disabled={disabled} onOpen={() => setSelected(c.id)} onMenu={(anchor) => setMenu({ anchor, card: c })} onDragStart={(e) => { dragging.current = c.id; e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("application/x-persona-workspace", c.id); }} onDragEnd={() => { dragging.current = null; setDrop(""); }} />
            </div>)}
            <div className={cx("flex min-h-16 items-center justify-center rounded-control border border-dashed px-3 py-4 text-center text-xs text-ink-3", drop === column.id ? "border-accent bg-accent-tint" : "border-line-strong")} onDragOver={(e) => { if (!disabled && dragging.current) { e.preventDefault(); setDrop(column.id); } }} onDrop={(e) => dropCard(e, column.id)}>{cards.length ? "拖到这里放在列末" : search || machine ? "没有匹配的工作空间" : "还没有工作空间"}</div>
          </div>{!archived && <div className="border-t border-line px-3 py-2"><Button tone="quiet" icon="plus" className="w-full" disabled={disabled} onClick={() => setAdding(column.id)}>绑定工作空间</Button></div>}
        </section>;
      })}
    </div> : <div className="p-6 text-sm text-ink-3">看板暂不可用。<Button onClick={() => void model.refresh()}>重试</Button></div>}
    {menu && <Menu anchor={menu.anchor} onClose={() => setMenu(null)} align="right" items={(menu.card ? cardMenu(menu.card) : columnMenu(menu.column!)).map((item) => ({ ...item, disabled: disabled || item.disabled }))} />}
    <Dialog open={Boolean(edit)} onClose={() => setEdit(null)} title={edit?.kind === "card" ? "重命名卡片" : edit?.id ? "重命名列" : "添加列"}>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (!edit?.title.trim()) return; void mutate({ action: edit.kind === "card" ? "update_card" : edit.id ? "update_column" : "add_column", id: edit.id, title: edit.title.trim() }).then((ok) => { if (ok) setEdit(null); }); }}><input autoFocus aria-label="名称" maxLength={120} className={field} value={edit?.title ?? ""} onChange={(e) => setEdit((v) => v && { ...v, title: e.target.value })} /><div className="flex justify-end gap-2"><Button onClick={() => setEdit(null)}>取消</Button><Button type="submit" tone="primary" disabled={disabled || !edit?.title.trim()} busy={state.busy}>保存</Button></div></form>
    </Dialog>
    <Dialog open={Boolean(deleting)} onClose={() => setDeleting(null)} title={`删除「${deleting?.title ?? ""}」列`}>
      <p className="mb-3 text-sm text-ink-2">删除列不会删除工作空间或 Agent。此列中的卡片（含归档）必须先移到其他列。</p>
      {board?.cards.some((c) => c.column_id === deleting?.id) && <select aria-label="将卡片移到" className={field} value={moveTo} onChange={(e) => setMoveTo(e.target.value)}><option value="">请选择接收卡片的列</option>{board.columns.filter((c) => c.id !== deleting?.id).map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}</select>}
      <div className="mt-4 flex justify-end gap-2"><Button onClick={() => setDeleting(null)}>取消</Button><Button tone="danger" disabled={disabled || Boolean(board?.cards.some((c) => c.column_id === deleting?.id) && !moveTo)} busy={state.busy} onClick={() => void mutate({ action: "delete_column", id: deleting?.id, move_to: moveTo || undefined }).then((ok) => { if (ok) setDeleting(null); })}>删除列</Button></div>
    </Dialog>
    {adding && board && <BindWorkspace column={adding} board={board} workspaces={state.discovery.workspaces} machines={machines} busy={state.busy} disabled={state.readonly} onClose={() => setAdding(undefined)} onBind={mutate} />}
    {card && <WorkspaceDetail card={card} column={board!.columns.find((c) => c.id === card.column_id)?.title ?? ""} machine={machines.find((m) => m.id === card.machine_id)} sessions={cardSessions(card, sessions)} readonly={state.readonly} onClose={() => setSelected(undefined)} />}
  </div>;
}

/** Durable workflow binding; availability and Agent activity remain secondary facts. */
export function WorkspaceCard({ card, machine, workspace, sessions, disabled, onOpen, onMenu, onDragStart, onDragEnd }: {
  card: BoardCard; machine?: Machine; workspace?: Workspace; sessions: Session[]; disabled?: boolean;
  onOpen: () => void; onMenu: (anchor: HTMLElement) => void; onDragStart?: (e: React.DragEvent) => void; onDragEnd?: () => void;
}) {
  const availability = machine?.online === false || workspace?.online === false ? "机器离线" : workspace?.available === false ? "空间暂不可用" : !workspace ? "空间未发现" : "";
  return <article draggable={!disabled} onDragStart={onDragStart} onDragEnd={onDragEnd} className="rounded-card bg-surface p-3 shadow-card" data-card-id={card.id}>
    <div className="flex items-start gap-2"><button type="button" className="min-w-0 flex-1 text-left" onClick={onOpen}><span className="flex items-center gap-1.5">{card.pinned && <Icon name="pin" size={12} className="shrink-0 text-accent" />}<span className="truncate text-sm font-semibold text-ink" title={card.title}>{card.title}</span></span><span className="mt-2 block truncate font-mono text-xs text-ink-3" title={card.cwd}>{card.cwd}</span></button><IconButton icon="more" label={`${card.title}卡片菜单`} disabled={disabled} onClick={(e) => onMenu(e.currentTarget)} /></div>
    <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-3"><span className="flex min-w-0 items-center gap-1"><Icon name="machine" size={12} />{machine?.name || card.machine_id}</span>{card.branch && <span className="flex min-w-0 items-center gap-1"><Icon name="git" size={12} /><span className="max-w-32 truncate">{card.branch}</span></span>}</div>
    <div className="mt-3 flex flex-wrap items-center gap-2"><span className="text-xs text-ink-2">{sessions.length ? `${sessions.length} 个 Agent` : "暂无 Agent"}</span>{availability && <span className="rounded-full bg-field px-2 py-1 text-xs text-ink-3">{availability}</span>}{sessions.some((s) => stateOf(s) === "needs_you") && <StatePill state="needs_you" />}{!sessions.some((s) => stateOf(s) === "needs_you") && sessions.some((s) => stateOf(s) === "working") && <StatePill state="working" />}</div>
  </article>;
}
function WorkspaceDetail({ card, column, machine, sessions, readonly, onClose }: { card: BoardCard; column: string; machine?: Machine; sessions: Session[]; readonly: boolean; onClose: () => void }) {
  const open = (kind: "files" | "git") => { onClose(); openPanel({ kind, mid: card.machine_id, sid: sessions[0]?.session_id ?? "", cwd: card.cwd }); };
  return <Dialog open title={card.title} onClose={onClose}><div className="space-y-4"><p className="text-xs text-ink-3">{column}{card.archived ? " · 已归档" : ""} · {machine?.name || card.machine_id}{machine?.online === false ? " · 离线" : ""}</p><p className="break-all rounded-control bg-inset p-3 font-mono text-xs text-ink-2">{card.cwd}</p>{card.branch && <p className="text-xs text-ink-2">分支：{card.branch}</p>}
    <div className="flex flex-wrap gap-2"><Button icon="folder" disabled={!machine?.online} onClick={() => open("files")}>文件</Button><Button icon="git" disabled={!machine?.online} onClick={() => open("git")}>Git</Button><Button icon="plus" disabled={readonly || !machine?.online} onClick={() => { onClose(); openNewAgent({ machineId: card.machine_id, cwd: card.cwd }); }}>新建 Agent</Button></div>
    <div className="border-t border-line pt-3"><h3 className="mb-2 text-xs font-medium text-ink-3">此机器、此目录的 Agents</h3>{sessions.length ? <div className="space-y-1">{sessions.map((s) => <button type="button" key={s.session_id} className="flex w-full items-center gap-2 rounded-control px-2 py-2 text-left hover:bg-hover" onClick={() => { onClose(); openPanel({ kind: "agent", mid: s.machine_id, sid: s.session_id }); }}><Icon name="agent" size={14} className="text-ink-3" /><span className="min-w-0 flex-1 truncate text-sm text-ink">{sessionTitle(s)}</span><StatePill state={stateOf(s)} /></button>)}</div> : <p className="text-sm text-ink-3">此工作空间暂无 Agent。可显式新建 Agent；看板卡片会保留。</p>}</div></div></Dialog>;
}
interface Dirs { path: string; parent?: string; entries: { name: string; path: string }[] }
function BindWorkspace({ column, board, workspaces, machines, busy, disabled, onClose, onBind }: {
  column: string; board: BoardState; workspaces: Workspace[]; machines: Machine[]; busy: boolean; disabled: boolean; onClose: () => void; onBind: (m: BoardMutation) => Promise<boolean>;
}) {
  const [mode, setMode] = useState<"existing" | "directory">("existing"), [mid, setMid] = useState(machines.find((m) => m.online)?.id ?? "");
  const [path, setPath] = useState(""), [browse, setBrowse] = useState(""), [dirs, setDirs] = useState<Dirs | null>(null);
  const [error, setError] = useState(""), [loading, setLoading] = useState(false), [query, setQuery] = useState("");
  useEffect(() => {
    if (mode !== "directory" || !mid) return;
    const controller = new AbortController(); setLoading(true); setError(""); setDirs(null);
    void api<Dirs>(`/machines/${enc(mid)}/dirs?path=${enc(browse)}`, { signal: controller.signal }).then((d) => { if (!controller.signal.aborted) setDirs(d); }).catch((e) => { if (!controller.signal.aborted) setError(errorText(e)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [mid, mode, browse]);
  const bind = async (w?: Workspace) => { if (await onBind({ action: "add_card", column_id: column, machine_id: w?.machine_id ?? mid, workspace_id: w?.id, cwd: w?.cwd ?? path.trim() })) onClose(); };
  return <Dialog open title="绑定工作空间" onClose={onClose}>
    <div className="mb-4 flex gap-2"><Button tone={mode === "existing" ? "primary" : "quiet"} onClick={() => setMode("existing")}>已有工作空间</Button><Button tone={mode === "directory" ? "primary" : "quiet"} onClick={() => setMode("directory")}>选择目录</Button></div>
    {mode === "existing" ? <div className="space-y-3"><input aria-label="搜索可绑定空间" className={field} value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索工作空间" /><div className="max-h-80 space-y-2 overflow-y-auto">{workspaces.filter((w) => `${w.title} ${w.cwd}`.toLocaleLowerCase().includes(query.toLocaleLowerCase())).map((w) => {
      const bound = boundCard(board, w), online = machines.find((m) => m.id === w.machine_id)?.online !== false && w.online !== false;
      return <div key={w.id} className="flex items-center gap-2 rounded-control bg-inset p-3"><div className="min-w-0 flex-1"><p className="truncate text-sm text-ink">{w.title}</p><p className="truncate text-xs text-ink-3" title={w.cwd}>{machines.find((m) => m.id === w.machine_id)?.name || w.machine_id} · {w.cwd || "尚未提供目录"}</p></div>{bound ? <Button disabled={disabled || busy || !bound.archived} onClick={() => void onBind({ action: "update_card", id: bound.id, archived: false }).then((ok) => { if (ok) onClose(); })}>{bound.archived ? "恢复" : "已绑定"}</Button> : <Button disabled={disabled || busy || !w.cwd || !online || w.available === false} onClick={() => void bind(w)}>{online ? "绑定" : "离线"}</Button>}</div>;
    })}{!workspaces.length && <p className="text-sm text-ink-3">暂未发现工作空间。可以选择在线机器的真实目录。</p>}</div></div> : <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void bind(); }}>
      <select aria-label="目录所在机器" className={field} value={mid} onChange={(e) => { setMid(e.target.value); setBrowse(""); setPath(""); }}><option value="">选择在线机器</option>{machines.filter((m) => m.online).map((m) => <option key={m.id} value={m.id}>{m.name || m.id}</option>)}</select><input aria-label="工作空间目录" className={field} value={path} onChange={(e) => setPath(e.target.value)} placeholder="输入真实目录的绝对路径，或在下方选择" />
      {error && <p role="alert" className="text-xs text-red">{error}</p>}{loading && <Loading label="正在读取目录" />}
      {dirs && <div className="max-h-64 overflow-y-auto rounded-control border border-line"><div className="flex items-center gap-2 border-b border-line p-2"><IconButton icon="chevronLeft" label="上一级目录" disabled={dirs.parent === undefined} onClick={() => setBrowse(dirs.parent!)} /><button type="button" className="min-w-0 flex-1 truncate text-left font-mono text-xs text-ink-2" onClick={() => setPath(dirs.path)} title={dirs.path}>{dirs.path}</button><Button tone="quiet" onClick={() => setPath(dirs.path)}>选此目录</Button></div>{dirs.entries.map((d) => <div key={d.path} className="flex items-center gap-2 px-3 py-1"><button type="button" className="flex min-w-0 flex-1 items-center gap-2 py-1 text-left text-sm text-ink" onClick={() => setBrowse(d.path)}><Icon name="folder" size={14} className="text-ink-3" /><span className="truncate">{d.name}</span></button><Button tone="quiet" onClick={() => setPath(d.path)}>选择</Button></div>)}</div>}
      <p className="text-xs text-ink-3">只绑定目录，不会启动 Agent。hub 会验证并规范化目录路径。</p><div className="flex justify-end"><Button type="submit" tone="primary" disabled={disabled || busy || !mid || !path.trim()} busy={busy}>绑定目录</Button></div>
    </form>}
  </Dialog>;
}
