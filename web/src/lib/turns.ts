// Items → what the chat shows (docs/ios.md「展示逻辑」): every user message opens a segment; the segment shows
// the message, one work row folding the tools / thoughts / notices / intermediate replies, and the agent's last
// words before the next message as the answer.

import { errorSignature, isRetryText, parseAPIError, type APIErrorInfo } from "./api-errors";
import { foldCompactions, isCompaction } from "./compaction";
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
  /** Sent by another Claude session (cross-session messaging): its name. */
  from?: string;
  /** A finished exchange that took several messages: each message's text, shown in one bubble. */
  parts?: string[];
  /** The message sent from here it came from, as the hub tied it (Item.client_msg_id). */
  client_msg_id?: string;
  /** Files it carries as <jarvis-file> tags (videos sent from the phone…), drawn as cards. */
  files?: AttachedFile[];
}

/** A <jarvis-file kind="video" path="…" name="…" size="…" duration="…"/> in a message (docs/protocol.md「发送视频」). */
export interface AttachedFile {
  kind: string; // video | audio | file
  path: string; // on the session's machine
  name: string;
  size?: number;
  duration?: number; // seconds
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

const FILE_RE = /<jarvis-file\b([^>]*?)\/?>(?:\s*<\/jarvis-file>)?/g;

const VIDEO = ["mp4", "m4v", "mov", "webm", "mkv"];
const AUDIO = ["mp3", "m4a", "aac", "wav", "flac", "ogg", "opus"];
const IMAGE = ["png", "jpg", "jpeg", "gif", "webp", "heic", "svg"];
const DOCS = ["pdf", "docx", "pptx", "xlsx", "csv", "ipynb", "html", "zip", "key", "pages", "numbers"];

/** What a file is for its card: video / audio play in place, the rest open in the viewer. */
export function mediaKindOf(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return VIDEO.includes(ext) ? "video" : AUDIO.includes(ext) ? "audio" : IMAGE.includes(ext) ? "image" : "file";
}

/**
 * Absolute paths (/… or ~/…) to media and documents an answer names, in order, at most 8: shown as cards under it when
 * they exist on the machine. Code files are left to the path links in the text.
 */
export function replyPaths(text: string): string[] {
  const exts = [...VIDEO, ...AUDIO, ...IMAGE, ...DOCS].join("|");
  const re = new RegExp(`(?:^|[\\s\`'"(（「:：])((?:~/|/(?!/))[^\\s\`'"()（）<>「」，。；！？]*\\.(?:${exts}))(?=$|[\\s\`'")）」，。；！？.,:;!?])`, "gim");
  const out: string[] = [];
  for (const m of text.matchAll(re)) if (!out.includes(m[1]) && out.length < 8) out.push(m[1]);
  return out;
}

/** 1:05, 12:00:03 */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds)); // down, as players show it (2.5 s is 0:02)
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`;
}

export function parseUserText(raw: string): { text: string; contexts: Context[]; files: AttachedFile[] } {
  const contexts: Context[] = [];
  const files: AttachedFile[] = [];
  let text = raw.replace(CONTEXT_RE, (_, attrs: string, body: string) => {
    const a: Record<string, string> = {};
    for (const m of attrs.matchAll(ATTR_RE)) a[m[1]] = unescapeAttr(m[2]);
    contexts.push({ kind: a.kind ?? "context", title: a.title, url: a.url, text: body.trim() });
    return "";
  });
  text = text.replace(FILE_RE, (_, attrs: string) => {
    const a: Record<string, string> = {};
    for (const m of attrs.matchAll(ATTR_RE)) a[m[1]] = unescapeAttr(m[2]);
    if (!a.path) return "";
    const num = (v: string | undefined) => (v !== undefined && Number.isFinite(Number(v)) ? Number(v) : undefined);
    files.push({ kind: a.kind || "file", path: a.path, name: a.name || a.path.split("/").pop() || a.path, size: num(a.size), duration: num(a.duration) });
    return "";
  });
  text = text
    .replace(/<pasted_content\b[^>]*>([\s\S]*?)<\/pasted_content>/g, "$1")
    // the paths of images sent from the phone (the images themselves show as images), like the app
    .replace(/<attached_images>[\s\S]*?<\/attached_images>/g, "")
    .replace(/\[Image #\d+\]\s?/g, "")
    .trim();
  return { text, contexts, files };
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
        cur.user = { id: it.id, text: "", images: [], queued: it.queued, ts: it.ts, contexts: [], from: it.from || undefined };
        cur.start = it.ts;
      }
      lastUserEntry = entry;
      cur.user!.client_msg_id ??= it.client_msg_id || undefined;
      if (it.kind === "image") cur.user!.images.push(it);
      else {
        const p = parseUserText(it.text ?? "");
        cur.user!.text = cur.user!.text ? `${cur.user!.text}\n${p.text}` : p.text;
        cur.user!.contexts.push(...p.contexts);
        if (p.files.length) cur.user!.files = [...(cur.user!.files ?? []), ...p.files];
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
        // an empty thought (Claude Code mostly writes them without words) still shows as 「思考 Ns」
        cur.steps.push({ kind: "thinking", id: it.id, item: it });
        if (it.text?.trim()) cur.thoughts++;
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

/** The agent's words in a pushed batch that the chat did not have yet: they just came in (their words fade in once). */
export function freshWords(have: Item[], pushed: Item[]): string[] {
  const known = new Set(have.map((i) => i.id));
  return pushed.filter((i) => i.role === "assistant" && i.kind === "text" && !known.has(i.id)).map((i) => i.id);
}

/** A segment's steps with its answer back in its place: what lib/work-groups.ts groups. */
export function exchangeSteps(s: Segment): Step[] {
  if (!s.answer) return s.steps;
  const a = s.answer;
  const at = s.steps.findIndex((st) => blockOrder(st.kind === "tool" ? st.use : st.item, a) > 0);
  const step: Step = { kind: "text", id: a.id, item: a };
  return at < 0 ? [...s.steps, step] : [...s.steps.slice(0, at), step, ...s.steps.slice(at)];
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
  // mcp__<server>__<tool>, and the server may have underscores itself (mcp__plugin_x_y__tool), like the apps
  if (name.startsWith("mcp__")) return name.slice(name.lastIndexOf("__") + 2).replace(/_/g, " ");
  return name;
}

// ── exchanges and lines (iOS ChatBlock.build + ChatLines) ──────────────────────────
// Items group into exchanges (`Turn`): what you said (messages typed while the agent worked join the exchange they were
// sent during), everything the agent did, and its answer; recaps stand between them. A background task that wakes the
// idle agent stays in the exchange; the lines are then what the chat draws, one per row.

export interface Turn {
  id: string;
  /** Your messages and their pictures. */
  users: Item[];
  /** Everything the agent did (no input of yours), the answer included. */
  process: Item[];
  answer?: Item;
  /** Notices that close the exchange visibly ("已中断"). */
  trailing: Item[];
  /** Background tasks that woke the agent during this exchange (their notices). */
  wakes: Item[];
  /** Everything from the first wake on, for the 后台任务完成 drawer. */
  afterWake: Item[];
  /** What came after the answer (a follow-up once a background task reported back): only in the drawer. */
  tail: Item[];
  /** Messages and steps in arrival order, up to the answer (how the exchange is drawn). */
  ordered: Item[];
}

export type Block = { kind: "turn"; turn: Turn } | { kind: "row"; item: Item };

/** One of your messages, or a picture sent with one. */
export function isUserInput(i: Item): boolean {
  return i.role === "user" && (i.kind === "text" || (i.kind === "image" && !i.tool_use_id));
}

function isAnswerText(i: Item): boolean {
  return i.role === "assistant" && i.kind === "text" && Boolean(i.text?.trim());
}

const isWake = (i: Item) => i.kind === "notice" && i.wake === true;

function newTurn(id: string): Turn {
  return { id, users: [], process: [], trailing: [], wakes: [], afterWake: [], tail: [], ordered: [] };
}

function cloneTurn(t: Turn): Turn {
  return { ...t, users: [...t.users], process: [...t.process], trailing: [...t.trailing], wakes: [...t.wakes], afterWake: [...t.afterWake], tail: [...t.tail], ordered: [...t.ordered] };
}

/** Background tasks split an exchange into runs (before the first wake, after each one), each ending in the agent's
 *  words. The answer is the longest of those; what comes after it shows only in the drawer. */
function pickAnswerAcrossWakes(t: Turn) {
  const all = t.ordered;
  const candidates: number[] = [];
  let last: number | undefined;
  all.forEach((r, i) => {
    if (isWake(r)) {
      if (last !== undefined) candidates.push(last);
      last = undefined;
    } else if (isAnswerText(r)) last = i;
  });
  if (last !== undefined) candidates.push(last);
  const len = (i: number) => all[i].text?.length ?? 0;
  let pick: number | undefined;
  for (const c of candidates) if (pick === undefined || len(c) >= len(pick)) pick = c; // ties: the later one
  // what you sent stays with the exchange
  const first = all.findIndex(isWake);
  if (first >= 0) t.afterWake = all.slice(first).filter((x) => !isUserInput(x));
  if (pick === undefined) return;
  t.tail = all.slice(pick + 1).filter((x) => !isUserInput(x));
  t.ordered = [...all.slice(0, pick + 1), ...all.slice(pick + 1).filter(isUserInput)];
  t.process = t.ordered.filter((x) => !isUserInput(x));
}

export function buildBlocks(items: Item[]): Block[] {
  const blocks: Block[] = [];
  let cur: Turn | null = null;
  // the last exchange as it was before closing, and where it sits: a background task that reports back after a recap
  // (Claude Code writes one when a finished session sits idle) still belongs to that exchange
  // (a holder: the closure assigns these, which the loop's narrowing would not see)
  const last: { raw: Turn | null; index: number; reopenedAt: number | null } = { raw: null, index: 0, reopenedAt: null };
  let lastUserEntry = "";

  const close = () => {
    if (!cur) return;
    const t: Turn = cur;
    last.raw = cloneTurn(t);
    if (t.wakes.length) pickAnswerAcrossWakes(t);
    t.answer = [...t.process].reverse().find(isAnswerText);
    if (last.reopenedAt !== null) {
      blocks.splice(last.reopenedAt, 0, { kind: "turn", turn: t }); // back in its place, the recap still after it
      last.index = last.reopenedAt;
    } else {
      last.index = blocks.length;
      blocks.push({ kind: "turn", turn: t });
    }
    last.reopenedAt = null;
    cur = null;
  };
  const add = (t: Turn, it: Item, process = true) => {
    if (process) t.process.push(it);
    t.ordered.push(it);
  };

  for (const [i, it] of items.entries()) {
    if (isUserInput(it)) {
      const entry = it.id.split("#")[0];
      const prev: Item | undefined = cur ? cur.ordered[cur.ordered.length - 1] : undefined;
      // the blocks of one line (a text and its pictures) are one message; input typed mid-turn joins the exchange
      if (cur && ((prev && isUserInput(prev) && entry === lastUserEntry) || it.queued)) {
        cur.users.push(it);
        add(cur, it, false);
      } else {
        close();
        cur = newTurn(it.id);
        cur.users.push(it);
        add(cur, it, false);
      }
      lastUserEntry = entry;
      continue;
    }
    // a compaction is a divider of its own after the exchange; in the middle of work (more of it follows) it stays a
    // step of that work, so an auto-compaction does not split the exchange
    if (isCompaction(it)) {
      const next = items[i + 1];
      if (cur && next && !isUserInput(next)) {
        add(cur, it);
        continue;
      }
      close();
      blocks.push({ kind: "row", item: it });
      continue;
    }
    if (it.kind === "recap") {
      close();
      blocks.push({ kind: "row", item: it });
      continue;
    }
    if (it.kind === "notice") {
      const raw = last.raw;
      const closed = blocks[last.index];
      if (isWake(it) && !cur && raw && closed?.kind === "turn" && closed.turn.id === raw.id) {
        blocks.splice(last.index, 1); // reopen the exchange the recap closed
        last.reopenedAt = last.index;
        cur = cloneTurn(raw);
      }
      if (isWake(it) && cur) {
        // a background task woke the idle agent: still the same exchange (the answer is picked at close)
        cur.wakes.push(it);
        add(cur, it);
      } else if (!cur) {
        // e.g. a background command finishing after a recap: part of the (next) work, never its own line
        cur = newTurn(it.id);
        add(cur, it);
      } else if (!it.task && (it.text ?? "").includes("中断")) {
        cur.trailing.push(it);
      } else {
        add(cur, it);
      }
      continue;
    }
    if (!cur) cur = newTurn(it.id);
    add(cur, it);
  }
  close();
  return blocks;
}

export type ChatLine =
  /** A message (or none), one work row for what the agent did after it, the agent's last words. `group`: a finished
   *  exchange of several messages drawn as one (opening its work splits it, `ChatLinesOptions.split`). */
  | { kind: "segment"; id: string; seg: Segment; live: boolean; group?: string }
  /** Above the split exchange's messages: 已按 N 条消息拆开 · 收起. */
  | { kind: "split"; id: string; group: string; count: number }
  /** A notice of its own line: a wake that opens new work, "已中断" after an exchange. */
  | { kind: "notice"; id: string; item: Item }
  /** Under an answer: background tasks reported back; what the agent did next opens in a drawer. */
  | { kind: "wakes"; id: string; turn: Turn; live: boolean }
  /** Where the context was compacted: a divider across the chat (lib/compaction.ts). */
  | { kind: "compact"; id: string; item: Item }
  /** One or more "while you were away" recaps in a row. */
  | { kind: "recaps"; id: string; items: Item[] }
  /** A model API error, `count` times in a row (retries between folded in). */
  | { kind: "apiError"; id: string; item: Item; info: APIErrorInfo; count: number };

export interface ChatLinesOptions {
  /** The agent is at work: the last exchange is the live one. */
  live?: boolean;
  /** Finished exchanges of several messages the user split open (their `group` ids). */
  split?: ReadonlySet<string>;
}

/** The work of these items as one segment without a message: every step, the agent's words included (no answer). */
export function workSegment(id: string, items: Item[]): Segment {
  const segs = segments(items.filter((i) => !isUserInput(i)));
  const out: Segment = { id, steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] };
  for (const s of segs) {
    out.steps.push(...s.steps);
    if (s.answer) out.steps.push({ kind: "text", id: s.answer.id, item: s.answer });
    out.tools += s.tools;
    out.thoughts += s.thoughts;
    out.folds.push(...s.folds);
    out.start ??= s.start;
    out.end = s.end ?? out.end;
  }
  out.replies = out.steps.filter((x) => x.kind === "text").length;
  return out;
}

function turnLines(t: Turn, live: boolean, split: ReadonlySet<string>): ChatLine[] {
  const out: ChatLine[] = [];
  let ordered = t.ordered;
  let wokeAt: string | undefined;
  // work a background task woke the agent for opens with that notice, where a message would be
  if (ordered[0] && isWake(ordered[0])) {
    out.push({ kind: "notice", id: ordered[0].id, item: ordered[0] });
    wokeAt = ordered[0].ts;
    ordered = ordered.slice(1);
  }
  const segs = segments(ordered);
  const messages = segs.filter((s) => s.user);
  const group = `wt-${t.id}`;
  const after = () => {
    if (t.wakes.length) out.push({ kind: "wakes", id: `wk-${t.id}`, turn: t, live: live && t.tail.length > 0 });
    for (const n of t.trailing) out.push({ kind: "notice", id: n.id, item: n });
  };
  // a finished exchange that took several messages reads as one: the messages in one bubble, one work row, the answer
  if (!live && messages.length > 1) {
    if (!split.has(group)) {
      const seg = segments(ordered.filter((i) => !isUserInput(i)));
      const merged: Segment = { id: `mu-${t.id}`, steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] };
      seg.forEach((s, k) => {
        merged.steps.push(...s.steps);
        if (s.answer && k < seg.length - 1) merged.steps.push({ kind: "text", id: s.answer.id, item: s.answer });
        merged.tools += s.tools;
        merged.thoughts += s.thoughts;
        merged.folds.push(...s.folds);
        merged.end = s.end ?? merged.end;
      });
      merged.answer = seg[seg.length - 1]?.answer;
      merged.replies = merged.steps.filter((x) => x.kind === "text").length;
      const users = messages.map((m) => m.user!);
      merged.start = users[0].ts;
      merged.user = {
        id: users[0].id,
        text: users.map((u) => u.text).filter(Boolean).join("\n\n"),
        parts: users.map((u) => u.text).filter(Boolean),
        images: users.flatMap((u) => u.images),
        contexts: users.flatMap((u) => u.contexts),
        files: users.flatMap((u) => u.files ?? []),
        ts: users[0].ts,
      };
      out.push({ kind: "segment", id: merged.id, seg: merged, live: false, group });
      after();
      return out;
    }
    out.push({ kind: "split", id: `${group}-h`, group, count: messages.length });
  }
  segs.forEach((seg, k) => {
    // once the answer is in and a background task woke the agent, the new work runs behind the chip
    const isLive = live && k === segs.length - 1 && !t.tail.length;
    if (k === 0 && !seg.user && wokeAt) seg.start = wokeAt;
    out.push({ kind: "segment", id: seg.id, seg, live: isLive });
  });
  after();
  return out;
}

/** The chat as lines (iOS ChatLines.lines): exchanges, their wakes and notices, recaps, API errors as cards. */
export function chatLines(items: Item[], opts: ChatLinesOptions = {}): ChatLine[] {
  const blocks = buildBlocks(foldCompactions(items));
  const split = opts.split ?? new Set<string>();
  const last = blocks[blocks.length - 1];
  const liveId = opts.live && last?.kind === "turn" ? last.turn.id : undefined;
  const out: ChatLine[] = [];
  for (const b of blocks) {
    if (b.kind === "row") out.push(isCompaction(b.item) ? { kind: "compact", id: b.item.id, item: b.item } : { kind: "recaps", id: b.item.id, items: [b.item] });
    else out.push(...turnLines(b.turn, b.turn.id === liveId, split));
  }
  // working with nothing of this turn yet (the last thing is a recap): the work row still shows
  if (opts.live && !liveId) out.push({ kind: "segment", id: "live", seg: { id: "live", steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] }, live: true });
  return foldRecaps(foldAPIErrors(out));
}

/** The API error a line ends in: an answer, or a notice of its own line. `rest`: the line without it (null: nothing). */
function lineError(l: ChatLine): { item: Item; info: APIErrorInfo; rest: ChatLine | null } | null {
  if (l.kind === "notice") {
    const info = parseAPIError(l.item.text);
    return info ? { item: l.item, info, rest: null } : null;
  }
  if (l.kind !== "segment" || !l.seg.answer) return null;
  const info = parseAPIError(l.seg.answer.text);
  if (!info) return null;
  const seg = { ...l.seg, answer: undefined };
  const empty = !seg.user && !seg.steps.length && !l.live;
  return { item: l.seg.answer, info, rest: empty ? null : { ...l, seg } };
}

/** A line that only asked to try again: "继续" with nothing else. */
function isRetryLine(l: ChatLine): boolean {
  return l.kind === "segment" && !l.live && !l.seg.steps.length && !l.seg.answer && Boolean(l.seg.user) && !l.seg.user!.images.length && isRetryText(l.seg.user!.text);
}

/** Model API errors become cards; the same error again right after, with nothing but a retry between, folds into the
 *  first with a count (iOS ChatLines.foldAPIErrors). */
export function foldAPIErrors(lines: ChatLine[]): ChatLine[] {
  const out: ChatLine[] = [];
  for (const line of lines) {
    const e = lineError(line);
    if (!e) {
      out.push(line);
      continue;
    }
    if (e.rest) out.push(e.rest);
    let last = -1;
    for (let i = out.length - 1; i >= 0; i--) if (out[i].kind === "apiError") { last = i; break; }
    const prev = last >= 0 ? out[last] : undefined;
    if (prev?.kind === "apiError" && errorSignature(prev.info) === errorSignature(e.info) && out.slice(last + 1).every(isRetryLine)) {
      out.splice(last + 1);
      out[last] = { ...prev, info: e.info, count: prev.count + 1 };
      continue;
    }
    out.push({ kind: "apiError", id: `err-${e.item.id}`, item: e.item, info: e.info, count: 1 });
  }
  return out;
}

/** Several recaps in a row (nothing else between) are one line, keeping the first one's id. */
export function foldRecaps(lines: ChatLine[]): ChatLine[] {
  const out: ChatLine[] = [];
  for (const l of lines) {
    const prev = out[out.length - 1];
    if (l.kind === "recaps" && prev?.kind === "recaps") out[out.length - 1] = { ...prev, items: [...prev.items, ...l.items] };
    else out.push(l);
  }
  return out;
}

/** The newest recap shown open, in full, as the chat's last line (#1360): in the flow with the agent's words, so it
 *  scrolls with them and never covers them. Only while it is the last line and `pending` (not closed); the older
 *  recaps folded into the same line stay a one-line chip above it. */
export type OpenRecap = { line: string; earlier: Item[]; newest: Item };

export function openRecap(lines: ChatLine[], pending?: Item): OpenRecap | undefined {
  const last = lines[lines.length - 1];
  if (!pending || last?.kind !== "recaps" || last.items[last.items.length - 1]?.id !== pending.id) return undefined;
  return { line: last.id, earlier: last.items.slice(0, -1), newest: pending };
}

/** The drawer behind a wakes line: each wake (its task reports, then what the agent answered to them), newest wake
 *  first; the steps in between fold into one work row at the end. */
export function wakeDrawer(t: Turn): { groups: { id: string; notices: Item[]; answer?: Item }[]; process: Item[] } {
  const groups: { id: string; notices: Item[]; answer?: Item }[] = [];
  const process: Item[] = [];
  let chunk: Item[] = [];
  const flush = () => {
    let answer = -1;
    for (let i = chunk.length - 1; i >= 0; i--) if (isAnswerText(chunk[i])) { answer = i; break; }
    const notices: Item[] = [];
    chunk.forEach((r, i) => {
      if (i === answer) return;
      if (r.kind === "notice" && (r.task || r.wake)) notices.push(r);
      else process.push(r);
    });
    if (notices.length || answer >= 0) groups.push({ id: (notices[0] ?? chunk[answer]).id, notices, answer: answer >= 0 ? chunk[answer] : undefined });
    chunk = [];
  };
  for (const r of t.afterWake) {
    if (isWake(r)) flush(); // each wake starts its own answer
    chunk.push(r);
  }
  flush();
  return { groups: groups.reverse(), process };
}

/** A /loop wakeup (the transcript records each as the "/loop …" prompt again): a quiet line, not a bubble. */
export function isLoopTick(msg: UserMessage): boolean {
  return msg.text.startsWith("/loop") && !msg.parts;
}
