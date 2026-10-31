// Items → what the chat shows (docs/ios.md「展示逻辑」): every user message opens a segment; the segment shows
// the message, one work row folding the tools / thoughts / notices / intermediate replies, and the agent's last
// words before the next message as the answer.

import type { Item } from "./types";

export type Step =
  | { kind: "tool"; id: string; use: Item; result?: Item; images: Item[] }
  | { kind: "thinking"; id: string; item: Item }
  | { kind: "text"; id: string; item: Item }
  | { kind: "notice"; id: string; item: Item }
  | { kind: "image"; id: string; item: Item }
  | { kind: "fold"; id: string; item: Item };

export interface UserMessage {
  id: string;
  text: string;
  images: Item[];
  queued?: boolean;
  ts?: string;
  contexts: Context[];
}

export interface Context {
  kind: string;
  title?: string;
  url?: string;
  text: string;
}

export interface Segment {
  id: string;
  user?: UserMessage;
  steps: Step[]; // the work, in order (the answer is not among them)
  answer?: Item;
  start?: string;
  end?: string;
  tools: number;
  thoughts: number;
  replies: number;
  folds: Item[];
}

function blockOrder(a: Item, b: Item) {
  if (a.seq !== b.seq) return a.seq - b.seq;
  const ia = Number(a.id.split("#")[1] ?? 0);
  const ib = Number(b.id.split("#")[1] ?? 0);
  return ia - ib;
}

export function sortItems(items: Item[]): Item[] {
  return [...items].sort(blockOrder);
}

// Merge a batch into what we have, by id. A fold stands for a range of work: once any real step of that range
// is here (the fold was expanded, or a later page overlaps it) the fold goes.
export function mergeItems(have: Item[], batch: Item[]): Item[] {
  const byId = new Map(have.map((i) => [i.id, i]));
  for (const it of batch) byId.set(it.id, it);
  const all = [...byId.values()];
  const work = all.filter((i) => i.kind === "tool_use" || i.kind === "tool_result" || i.kind === "thinking");
  const kept = all.filter((f) => f.kind !== "fold" || !f.fold || !work.some((i) => i.seq >= f.seq && i.seq <= f.fold!.through_seq));
  return sortItems(kept);
}

const CONTEXT_RE = /<jarvis-context\b([^>]*)>([\s\S]*?)<\/jarvis-context>/g;
const ATTR_RE = /(\w+)="([^"]*)"/g;

function unescapeAttr(s: string) {
  return s.replace(/&quot;/g, '"').replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
}

export function parseUserText(raw: string): { text: string; contexts: Context[] } {
  const contexts: Context[] = [];
  let text = raw.replace(CONTEXT_RE, (_, attrs: string, body: string) => {
    const a: Record<string, string> = {};
    for (const m of attrs.matchAll(ATTR_RE)) a[m[1]] = unescapeAttr(m[2]);
    contexts.push({ kind: a.kind ?? "context", title: a.title, url: a.url, text: body.trim() });
    return "";
  });
  text = text
    .replace(/<pasted_content\b[^>]*>([\s\S]*?)<\/pasted_content>/g, "$1")
    .replace(/\[Image #\d+\]\s?/g, "")
    .trim();
  return { text, contexts };
}

function isUserMessage(i: Item) {
  return i.role === "user" && (i.kind === "text" || (i.kind === "image" && !i.tool_use_id));
}

export function segments(items: Item[]): Segment[] {
  const out: Segment[] = [];
  let cur: Segment | null = null;
  let lastUserEntry = "";
  const tools = new Map<string, Extract<Step, { kind: "tool" }>>();

  const open = (id: string): Segment => {
    const s: Segment = { id, steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] };
    out.push(s);
    return s;
  };

  for (const it of items) {
    const entry = it.id.split("#")[0];
    if (isUserMessage(it)) {
      // blocks of one transcript line (text + its images) are one message
      if (!cur || !cur.user || entry !== lastUserEntry || cur.steps.length || cur.answer) {
        cur = open(`seg:${it.id}`);
        cur.user = { id: it.id, text: "", images: [], queued: it.queued, ts: it.ts, contexts: [] };
        cur.start = it.ts;
      }
      lastUserEntry = entry;
      if (it.kind === "image") cur.user!.images.push(it);
      else {
        const p = parseUserText(it.text ?? "");
        cur.user!.text = cur.user!.text ? `${cur.user!.text}\n${p.text}` : p.text;
        cur.user!.contexts.push(...p.contexts);
      }
      continue;
    }
    if (!cur) {
      cur = open(`seg:${it.id}`);
      cur.start = it.ts;
    }
    if (it.ts) cur.end = it.ts;
    switch (it.kind) {
      case "tool_use": {
        const st: Extract<Step, { kind: "tool" }> = { kind: "tool", id: it.id, use: it, images: [] };
        if (it.tool_use_id) tools.set(it.tool_use_id, st);
        cur.steps.push(st);
        cur.tools++;
        break;
      }
      case "tool_result": {
        const st = it.tool_use_id ? tools.get(it.tool_use_id) : undefined;
        if (st) st.result = it;
        break; // a result without its call (the call is on an earlier page) has nothing to show
      }
      case "image": {
        const st = it.tool_use_id ? tools.get(it.tool_use_id) : undefined;
        if (st) st.images.push(it);
        else cur.steps.push({ kind: "image", id: it.id, item: it });
        break;
      }
      case "thinking":
        if (it.text?.trim()) {
          cur.steps.push({ kind: "thinking", id: it.id, item: it });
          cur.thoughts++;
        }
        break;
      case "text":
        if (it.text?.trim()) cur.steps.push({ kind: "text", id: it.id, item: it });
        break;
      case "notice":
        cur.steps.push({ kind: "notice", id: it.id, item: it });
        break;
      case "fold":
        cur.steps.push({ kind: "fold", id: it.id, item: it });
        cur.folds.push(it);
        if (it.fold) {
          cur.tools += it.fold.tools;
          cur.thoughts += it.fold.thoughts;
          if (it.fold.until) cur.end = it.fold.until;
        }
        break;
    }
  }

  // the answer: the last thing the agent said in the segment
  for (const s of out) {
    for (let i = s.steps.length - 1; i >= 0; i--) {
      const st = s.steps[i];
      if (st.kind === "text") {
        s.answer = st.item;
        s.steps.splice(i, 1);
        break;
      }
    }
    s.replies = s.steps.filter((x) => x.kind === "text").length;
  }
  return out;
}

export function workSummary(s: Segment): string {
  const parts: string[] = [];
  if (s.tools) parts.push(`工具 ${s.tools}`);
  if (s.thoughts) parts.push(`思考 ${s.thoughts}`);
  if (s.replies) parts.push(`回复 ${s.replies}`);
  const notices = s.steps.filter((x) => x.kind === "notice").length;
  if (notices) parts.push(`通知 ${notices}`);
  return parts.join(" · ");
}

// The first line worth showing for a tool call: the command, the file, the pattern…
export function toolChip(use: Item): string {
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const pick = (...keys: string[]) => {
    for (const k of keys) {
      const v = inp[k];
      if (typeof v === "string" && v.trim()) return v.trim();
    }
    return "";
  };
  const name = use.tool_name ?? "";
  let v = "";
  if (name === "Bash" || name === "exec_command" || name === "shell") v = pick("description", "command", "cmd");
  else if (/^(Read|Edit|Write|MultiEdit|NotebookEdit)$/.test(name)) v = shortFile(pick("file_path", "notebook_path"));
  else if (name === "Grep" || name === "Glob") v = pick("pattern");
  else if (name === "WebFetch") v = pick("url");
  else if (name === "WebSearch") v = pick("query");
  else if (name === "Task" || name === "Agent") v = pick("description", "prompt");
  else if (name === "TodoWrite") v = "更新待办";
  else v = pick("description", "command", "file_path", "path", "query", "url", "prompt", "pattern", "name");
  if (!v && Array.isArray(inp.command)) v = (inp.command as unknown[]).join(" ");
  return v.split("\n")[0].slice(0, 200);
}

export function shortFile(p: string) {
  const parts = p.split("/");
  return parts.length > 3 ? `…/${parts.slice(-2).join("/")}` : p;
}

export const TOOL_LABEL: Record<string, string> = {
  Bash: "运行",
  exec_command: "运行",
  shell: "运行",
  Read: "读取",
  Edit: "编辑",
  MultiEdit: "编辑",
  Write: "写入",
  Grep: "搜索",
  Glob: "查找文件",
  WebFetch: "打开网页",
  WebSearch: "搜索网页",
  Task: "子 Agent",
  Agent: "子 Agent",
  TodoWrite: "待办",
  ToolSearch: "加载工具",
  Skill: "技能",
  AskUserQuestion: "提问",
  ExitPlanMode: "计划",
  apply_patch: "修改",
};

export function toolLabel(name?: string): string {
  if (!name) return "工具";
  if (TOOL_LABEL[name]) return TOOL_LABEL[name];
  const m = /^mcp__([^_]+)__(.+)$/.exec(name);
  if (m) return m[2].replace(/_/g, " ");
  return name;
}

export function toolIcon(name?: string): "run" | "read" | "write" | "search" | "web" | "agent" | "todo" | "tool" {
  switch (name) {
    case "Bash":
    case "exec_command":
    case "shell":
      return "run";
    case "Read":
      return "read";
    case "Edit":
    case "MultiEdit":
    case "Write":
    case "NotebookEdit":
    case "apply_patch":
      return "write";
    case "Grep":
    case "Glob":
    case "ToolSearch":
      return "search";
    case "WebFetch":
    case "WebSearch":
      return "web";
    case "Task":
    case "Agent":
      return "agent";
    case "TodoWrite":
      return "todo";
  }
  return "tool";
}

// +N −M of an edit, from the old / new strings Claude gives.
export function editStats(use: Item): { add: number; del: number } | null {
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const lines = (s: unknown) => (typeof s === "string" && s ? s.split("\n").length : 0);
  if (use.tool_name === "Edit") return { add: lines(inp.new_string), del: lines(inp.old_string) };
  if (use.tool_name === "Write") return { add: lines(inp.content), del: 0 };
  if (use.tool_name === "MultiEdit" && Array.isArray(inp.edits)) {
    let add = 0,
      del = 0;
    for (const e of inp.edits as Record<string, unknown>[]) {
      add += lines(e.new_string);
      del += lines(e.old_string);
    }
    return { add, del };
  }
  return null;
}
