// 交给 Jarvis (the Agents list's row menu, iOS AgentHandoff): what goes to Jarvis' main conversation about one agent —
// where it runs and what it last said as a context card, then the ask.
import { baseName, stateOf } from "./format";
import { contextXml } from "./kb";
import type { Session } from "./types";

export function agentHandoffText(s: Session, machineName?: string): string {
  const title = s.title || baseName(s.cwd);
  let text = `机器：${machineName || s.machine_id}（${s.machine_id}）\n会话：${s.session_id}\n目录：${s.cwd}\n状态：${stateOf(s)}`;
  if (s.last_text) text += `\n最后说的：${[...s.last_text].slice(0, 800).join("")}`;
  return `${contextXml({ kind: "context", title: `Agent · ${title}`, url: "", text })}\n\n看一下这个 Agent（agent_read）：它在做什么、做到哪了、有没有在等我。它属于哪个项目就把进展记进项目（kb_update，没登记目录的顺便登记 dirs）；需要我拍板的列出来，能替我推进的直接推进。`;
}
