// The context-menu items of a node in a tree of machines (#2329): a machine, a workspace, a tab or a pane, in the
// sidebar's Agents tree and on the 终端 page. One builder for all of them, so a menu offers the same things wherever the
// node is listed (and the command palette can ask it later). What changes a computer goes through routes only a full
// device may call (server/internal/hub/panes.go); copying and folding are local.
import { api, enc, errorText, sessionPath } from "./api";
import type { TermPane, TermSpace, TermTab } from "./terminals";
import type { Launcher, Machine, Session } from "./types";
import { copyText, toast } from "../ui/atoms";
import { openNewAgent } from "./new-agent";
import type { MenuItem } from "../ui/menu-model";

export type NodeKind = "machine" | "workspace" | "tab" | "pane";

export interface MenuNode {
  kind: NodeKind;
  mid: string;
  /** The machine is online; a node of one that is away is only copied from. */
  online?: boolean;
  /** The kind of terminal host the node is on ("herdr"); "" or unset: herdr, the one most machines have. */
  host?: string;
  /** What the node is called: a workspace's or tab's name is what 重命名 starts from. */
  label?: string;
  workspaceId?: string;
  tabId?: string;
  /** The pane the node is (kind "pane"), or one of a tab's or workspace's panes: the one a split or a git command is about. */
  paneId?: string;
  /** The agent session in the pane. */
  sessionId?: string;
  /** The agent running in the pane ("claude", "codex"…); "" a plain shell; unset when the tree does not say. */
  agent?: string;
  /** An agent session in the tab: a tab with one is renamed through it (what 重命名… already does). */
  tabSessionId?: string;
  cwd?: string;
  branch?: string;
  /** The node's fold: the row it hangs under folds and opens (the sidebar's and the terminal page's `fold:` keys). */
  fold?: { folded: boolean; toggle: () => void };
  /** 新建终端 in this workspace (the terminal page). */
  onOpenShell?: () => void;
  /** Read the list again (the terminal page). */
  onRefresh?: () => void;
}

/** What the items do. The defaults call the hub; a test (or the palette) passes its own. */
export interface NodeActionDeps {
  /** POST /machines/{mid}/<path> with a JSON body. */
  post: (mid: string, path: string, body: object) => Promise<unknown>;
  renameWorkspace: (node: MenuNode, name: string) => Promise<unknown>;
  renameTab: (node: MenuNode, name: string) => Promise<unknown>;
  launchers: (mid: string) => Promise<Launcher[]>;
  /** 新建 Agent… in a folder of this machine (the 新建 Agent dialog, filled in). */
  newAgent: (mid: string, cwd: string) => void;
  copy: (text: string) => void;
  notify: (text: string, tone: "ok" | "error" | "info") => void;
  /** The tree changed on the computer: read it again. */
  changed: () => void;
}

export function defaultNodeDeps(changed: () => void = () => {}): NodeActionDeps {
  return {
    post: (mid, path, body) => api(`/machines/${enc(mid)}/${path}`, { method: "POST", json: body }),
    renameWorkspace: (node, name) => api(`/machines/${enc(node.mid)}/spaces/${enc(node.workspaceId ?? "")}/rename`, { method: "POST", json: { name } }),
    renameTab: (node, name) =>
      node.tabSessionId
        ? api(`${sessionPath(node.mid, node.tabSessionId)}/rename`, { method: "POST", json: { tab_name: name } })
        : api(`/machines/${enc(node.mid)}/tabs/rename`, { method: "POST", json: { tab_id: node.tabId, label: name, ...(node.host ? { kind: node.host } : {}) } }),
    launchers: (mid) => api<{ launchers: Launcher[] }>(`/machines/${enc(mid)}/launchers`).then((r) => r.launchers ?? []),
    newAgent: (mid, cwd) => openNewAgent({ machineId: mid, cwd }),
    copy: (text) => void copyText(text),
    notify: (text, tone) => toast(text, tone),
    changed,
  };
}

export interface NodeActionOptions {
  /** Items to leave out, by key (a row whose own menu already has them: AgentRowMenu's 拷贝目录 is the path). */
  omit?: readonly string[];
}

/** Closing, splitting, renaming and git are herdr's: another host (Orca, herdr-core, botmux) is not asked to do them. */
export const canChangeHost = (host: string | undefined) => !host || host === "herdr";

/** Only the agents a launcher starts the way 新建 Agent does without a first message: Claude Code. */
export const launchableHere = (l: Launcher) => (l.agent_type || l.agent || "claude") === "claude";

export const GIT_OPS = [
  { op: "pull", label: "拉取", accel: "p", done: "拉取" },
  { op: "push", label: "推送", accel: "u", done: "推送" },
  { op: "fetch", label: "获取", accel: "f", done: "获取" },
] as const;

/** The menu items of a node, in groups (a hairline between them): fold and open, rename, make and run, copy, close. */
export function nodeActions(node: MenuNode, deps: NodeActionDeps, opts: NodeActionOptions = {}): MenuItem[] {
  const { mid } = node;
  const online = node.online !== false;
  const writes = online && canChangeHost(node.host);
  const kind = node.host ? { kind: node.host } : {};
  const fail = (what: string, e: unknown) => new Error(`${what}：${errorText(e)}`);

  // where a git command runs: a pane's own directory, else the workspace's
  const gitTarget = node.paneId ? { pane: node.paneId } : node.workspaceId ? { workspace_id: node.workspaceId, ...kind } : null;
  const isShell = (node.kind === "pane" || node.kind === "tab") && node.agent === "" && !node.sessionId;

  const groups: MenuItem[][] = [];
  const group = (...items: (MenuItem | false | null | undefined)[]) => {
    const list = items.filter(Boolean) as MenuItem[];
    if (list.length) groups.push(list);
  };
  const noop = () => {};
  const copy = (key: string, label: string, accel: string, text: string | undefined): MenuItem | false =>
    Boolean(text) && { key, label, icon: "copy", accel, onSelect: () => deps.copy(text!) };

  // ── fold and open ──
  group(
    node.fold && { key: "node-fold", label: node.fold.folded ? "展开" : "折叠", icon: node.fold.folded ? "arrowDown" : "chevron", accel: "z", onSelect: node.fold.toggle },
    node.kind === "workspace" && online && Boolean(node.cwd) && { key: "node-new-agent", label: "新建 Agent…", icon: "agent", accel: "n", onSelect: () => deps.newAgent(mid, node.cwd!) },
    node.kind === "workspace" && online && node.onOpenShell && { key: "node-shell", label: "新建终端", icon: "terminal", accel: "t", onSelect: node.onOpenShell },
    node.kind === "machine" && node.onRefresh && { key: "node-refresh", label: "刷新", icon: "retry", accel: "r", onSelect: node.onRefresh },
  );

  // ── rename ──
  const renameValue = node.label ?? "";
  group(
    node.kind === "workspace" && writes && Boolean(node.workspaceId) && {
      key: "node-rename",
      label: "重命名…",
      icon: "pencil",
      accel: "r",
      onSelect: noop,
      rename: {
        value: renameValue,
        placeholder: "工作区名称",
        commit: async (name) => {
          await deps.renameWorkspace(node, name).catch((e) => Promise.reject(fail("没改成", e)));
          deps.notify("已重命名工作区", "ok");
          deps.changed();
        },
      },
    },
    node.kind === "tab" && writes && Boolean(node.tabId) && {
      key: "node-rename",
      label: "重命名…",
      icon: "pencil",
      accel: "r",
      onSelect: noop,
      rename: {
        value: renameValue,
        placeholder: "标签页名称",
        commit: async (name) => {
          await deps.renameTab(node, name).catch((e) => Promise.reject(fail("没改成", e)));
          deps.notify("已重命名标签页", "ok");
          deps.changed();
        },
      },
    },
  );

  // ── make and run: split, an agent here, git ──
  const split = (direction: "right" | "down", label: string, accel: string): MenuItem => ({
    key: `node-split-${direction}`,
    label,
    accel,
    inline: true,
    onSelect: async () => {
      await deps.post(mid, "panes/split", { pane: node.paneId, direction }).catch((e) => Promise.reject(fail("新建窗格失败", e)));
      deps.notify("已新建窗格", "ok");
      deps.changed();
    },
  });
  const git = (op: (typeof GIT_OPS)[number]): MenuItem => ({
    key: `node-git-${op.op}`,
    label: op.label,
    accel: op.accel,
    inline: true,
    onSelect: async () => {
      const res = (await deps.post(mid, "panes/git", { op: op.op, ...gitTarget }).catch((e) => Promise.reject(new Error(errorText(e))))) as { message?: string } | undefined;
      deps.notify(`${op.done}：${res?.message ?? "完成"}`, "ok");
      deps.changed();
    },
  });
  const onHere = (l: Launcher): MenuItem => ({
    key: `node-agent-${l.name}`,
    label: l.name,
    hint: l.model,
    inline: true,
    onSelect: async () => {
      await deps.post(mid, "panes/agent", { pane: node.paneId, launcher: l.name }).catch((e) => Promise.reject(fail("启动失败", e)));
      deps.notify(`已在窗格里启动 ${l.name}`, "ok");
      deps.changed();
    },
  });
  const gitMenu = writes && node.kind !== "machine" && Boolean(gitTarget);
  group(
    writes && Boolean(node.paneId) && node.kind !== "machine" && node.kind !== "workspace" && {
      key: "node-split",
      label: "新建窗格",
      icon: "layers",
      accel: "n",
      onSelect: noop,
      children: [split("right", "向右拆分", "r"), split("down", "向下拆分", "d")],
    },
    writes && isShell && Boolean(node.paneId) && {
      key: "node-agent",
      label: "在这里运行 Agent…",
      icon: "agent",
      accel: "a",
      onSelect: noop,
      children: async () => (await deps.launchers(mid)).filter(launchableHere).map(onHere),
    },
    gitMenu && {
      key: "node-git",
      label: "Git",
      icon: "git",
      accel: "g",
      hint: node.branch,
      onSelect: noop,
      children: [...GIT_OPS.map(git), ...(node.branch ? [copy("node-copy-branch", "复制分支名", "b", node.branch) as MenuItem] : [])],
    },
  );

  // ── copy ──
  const herdrId = node.kind === "pane" ? node.paneId : node.kind === "tab" ? node.tabId : node.kind === "workspace" ? node.workspaceId : undefined;
  group(
    node.kind !== "machine" && copy("node-copy-path", "复制路径", "c", node.cwd),
    node.kind !== "machine" && node.kind !== "workspace" && copy("node-copy-session", "复制会话 id", "s", node.sessionId),
    node.kind !== "machine" && copy("node-copy-id", "复制 herdr id", "i", herdrId),
    node.kind !== "machine" && !gitMenu && copy("node-copy-branch", "复制分支名", "b", node.branch),
    node.kind === "machine" && copy("node-copy-machine", "复制机器 id", "i", mid),
  );

  // ── close ──
  group(
    node.kind === "tab" && writes && Boolean(node.tabId) && {
      key: "node-close-tab",
      label: "关闭标签页",
      icon: "x",
      danger: true,
      closes: true,
      confirm: "确认关闭",
      inline: true,
      onSelect: async () => {
        await deps.post(mid, "tabs/close", { tab_id: node.tabId, ...kind }).catch((e) => Promise.reject(fail("关闭失败", e)));
        deps.notify("已关闭标签页", "ok");
        deps.changed();
      },
    },
    node.kind === "pane" && writes && Boolean(node.paneId) && {
      key: "node-close-pane",
      label: "关闭窗格",
      icon: "x",
      danger: true,
      closes: true,
      confirm: "确认关闭",
      inline: true,
      onSelect: async () => {
        await deps.post(mid, "panes/close", { pane: node.paneId }).catch((e) => Promise.reject(fail("关闭失败", e)));
        deps.notify("已关闭窗格", "ok");
        deps.changed();
      },
    },
  );

  const omit = new Set(opts.omit);
  return groups
    .map((g) => g.filter((it) => !omit.has(it.key)))
    .filter((g) => g.length)
    .flatMap((g, i) => g.map((it, j) => (i > 0 && j === 0 ? { ...it, divider: true } : it)));
}

// ── the nodes of the two trees ──────────────────────────────

/** A machine's header. */
export function machineNode(m: Machine, extra: Partial<MenuNode> = {}): MenuNode {
  return { kind: "machine", mid: m.id, online: m.online, label: m.name, ...extra };
}

/** The pane an agent session runs in (the sidebar's agent rows). */
export function sessionNode(s: Session, online: boolean, extra: Partial<MenuNode> = {}): MenuNode {
  return {
    kind: "pane",
    mid: s.machine_id,
    online,
    host: s.space_kind,
    workspaceId: s.workspace_id ?? s.space_id,
    tabId: s.tab_id ?? s.slot_id,
    paneId: s.pane_id,
    sessionId: s.session_id,
    agent: s.agent || "claude",
    cwd: s.cwd,
    label: s.slot_name,
    ...extra,
  };
}

/** A workspace of the 终端 page. */
export function termSpaceNode(mid: string, sp: TermSpace, online: boolean, extra: Partial<MenuNode> = {}): MenuNode {
  return { kind: "workspace", mid, online, host: sp.host, label: sp.label, workspaceId: sp.id, cwd: sp.cwd, branch: sp.branch, ...extra };
}

/** A tab of the 终端 page; a tab with one pane is that pane too (its agent or shell, its session). */
export function termTabNode(mid: string, sp: TermSpace, tab: TermTab, online: boolean, extra: Partial<MenuNode> = {}): MenuNode {
  const lone = tab.panes.length === 1 ? tab.panes[0] : undefined;
  const first = tab.panes[0];
  return {
    kind: "tab",
    mid,
    online,
    host: sp.host,
    label: tab.label,
    workspaceId: sp.id,
    tabId: tab.id,
    paneId: first?.pane,
    cwd: lone?.cwd || sp.cwd,
    branch: sp.branch,
    ...(lone ? { sessionId: lone.sessionId, agent: lone.agent } : {}),
    ...extra,
  };
}

/** One pane of a tab with several, on the 终端 page. */
export function termPaneNode(mid: string, sp: TermSpace, tab: TermTab, pane: TermPane, online: boolean, extra: Partial<MenuNode> = {}): MenuNode {
  return {
    kind: "pane",
    mid,
    online,
    host: sp.host,
    workspaceId: sp.id,
    tabId: tab.id,
    paneId: pane.pane,
    sessionId: pane.sessionId,
    agent: pane.agent,
    cwd: pane.cwd || sp.cwd,
    branch: sp.branch,
    ...extra,
  };
}
