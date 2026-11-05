import { t } from "../i18n";
import type { Todo } from "./todos";
import type { Injected, Item } from "./types";

// Messages Persona types into a conversation for the model (a task's notice, a scheduled run, a wake-up, the
// background tasks a node restart stopped): one line instead of a user bubble, never the instructions inside
// (docs/protocol.md「Persona 注入的消息」). The hub marks them (`injected`); an older hub does not, so the text is read
// the same way it does (prompts.ParseInjected; spec/fixtures/injected is shared with it).

const NOTICE_HEAD = /^🔔 任务「(.+)」(做完了|在等用户|被模型服务出错打断了|没开起来)(?:（(.+)）)?$/u; // i18n-ignore: matches the hub's injected text
const FOLLOWUP_HEAD = /^⏰ (.+)（(你之前安排的跟进|定时任务(?:，每 [^）]+)?)）$/u; // i18n-ignore: matches the hub's injected text
const WAKE_HEAD = /^⏰（Persona 叫醒你，第 \d+ 次）你上一轮停下时说在等「(.*?)」/u; // i18n-ignore: matches the hub's injected text
// <persona-context>, or <jarvis-context> as older nodes and apps wrote it (#3182)
const LEAD_CONTEXT = /^\s*<(?:persona|jarvis)-context\b[^>]*>[\s\S]*?<\/(?:persona|jarvis)-context>/;
const NOTICE_MARKER = "这是 Persona 的任务通知"; // i18n-ignore: matches the hub's injected text
const LOST_TASKS_HEAD = "⚠️ 运行你的 node 刚重启过"; // i18n-ignore: matches the hub's injected text
const KB_HINT_HEAD = "（知识库"; // i18n-ignore: matches the hub's injected text

const NOTICE_STATES: Record<string, string> = { 做完了: "done", 在等用户: "needs", 被模型服务出错打断了: "interrupted", 没开起来: "failed" }; // i18n-ignore: matches the hub's injected text
const NOTICE_LINES: Record<string, string> = { done: "lib-injected.task_done", needs: "lib-injected.task_needs", interrupted: "lib-injected.task_interrupted", failed: "lib-injected.task_failed" };

/** What text is when it is one of Persona's messages for the model; null for anything else. */
export function parseInjected(raw: string): Injected | null {
  let text = raw;
  for (let m = LEAD_CONTEXT.exec(text); m; m = LEAD_CONTEXT.exec(text)) text = text.slice(m[0].length);
  text = text.trim();
  const nl = text.indexOf("\n");
  const head = nl < 0 ? text : text.slice(0, nl);
  const rest = nl < 0 ? "" : text.slice(nl + 1);
  if (head.startsWith("🔔 任务「")) { // i18n-ignore: matches the hub's injected text
    const m = NOTICE_HEAD.exec(head);
    if (!m || !rest.includes(NOTICE_MARKER)) return null;
    const state = NOTICE_STATES[m[2]];
    return { kind: "task_notice", state, title: m[1], machine: m[3] || undefined, line: t(NOTICE_LINES[state] ?? "lib-injected.task_done", { title: m[1] }) };
  }
  if (head.startsWith("⏰（")) { // i18n-ignore: matches the hub's injected text
    const m = WAKE_HEAD.exec(head);
    return m ? { kind: "followup", state: "wake", title: m[1], line: t("lib-injected.wake", { title: m[1] }) } : null;
  }
  if (head.startsWith("⏰ ")) {
    const m = FOLLOWUP_HEAD.exec(head);
    if (!m) return null;
    const detail = followupDetail(rest) || undefined;
    // i18n-ignore: matches the hub's injected text
    if (m[2] === "你之前安排的跟进") return { kind: "followup", state: "once", title: m[1], line: t("lib-injected.followup_due", { title: m[1] }), detail };
    const state = m[2].startsWith("定时任务，每 ") ? "interval" : "daily"; // i18n-ignore: matches the hub's injected text
    return { kind: "followup", state, title: m[1], line: t("lib-injected.scheduled_started", { title: m[1] }), detail };
  }
  if (head.startsWith(LOST_TASKS_HEAD)) {
    const n = rest.split("\n").filter((l) => l.startsWith("- ")).length;
    return { kind: "system", state: "lost_tasks", line: n ? t("lib-injected.lost_tasks_count", { count: n }) : t("lib-injected.lost_tasks") };
  }
  return null;
}

function followupDetail(rest: string): string {
  const s = "\n" + rest;
  const at = s.indexOf("\n\n");
  if (at < 0) return "";
  let body = s.slice(at + 2).trim();
  const hint = body.lastIndexOf("\n\n" + KB_HINT_HEAD);
  if (hint >= 0) body = body.slice(0, hint).trim();
  else if (body.startsWith(KB_HINT_HEAD)) body = "";
  return body;
}

/** The item as one of Persona's messages for the model: the hub's mark, else read off the text. A line sent from an
 *  app (client_msg_id) or by another session is never one. */
export function injectedOf(it: Item): Injected | null {
  if (it.role !== "user" || it.kind !== "text") return null;
  if (it.injected) return it.injected;
  if (it.client_msg_id || it.from) return null;
  return parseInjected(it.text ?? "");
}

/** The task a task notice is about: the hub's todo id, else the task of that title opened from this conversation
 *  (the newest); undefined when none is known. */
export function taskOfInjected(n: Injected, todos: readonly Todo[], conversation: string): string | undefined {
  if (n.kind !== "task_notice") return undefined;
  if (n.todo_id) return n.todo_id;
  let best: Todo | undefined;
  for (const t of todos) {
    if (t.title !== n.title || t.origin !== conversation) continue;
    if (!best || t.created_at > best.created_at) best = t;
  }
  return best?.id;
}
