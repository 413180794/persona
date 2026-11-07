// Which agent a row, a header or a launcher is: the `agent` string the node reports (herdr's own name for the program in
// the pane, a runtime's id, "" for Claude Code) turned into one of the kinds that have a mark (ui/AgentMark.tsx), or
// left as it is for any other. Nothing here decides what the mark looks like.
import { t } from "../i18n";
import type { Session } from "./types";

/** The agents whose own mark is drawn (ui/AgentMark.tsx). A compatible CLI has none: it has no public mark, it draws the letter. */
export const MARKED_AGENTS = ["claude", "codex", "opencode", "pi", "gemini", "cursor", "trae"] as const;
export type MarkedAgent = (typeof MARKED_AGENTS)[number];

/** Other names for the same program (the CLI's binary, the product's own name), by the lowercased `agent` string. */
const ALIASES: Record<string, string> = {
  "claude-code": "claude",
  claudecode: "claude",
  "codex-cli": "codex",
  "gemini-cli": "gemini",
  "cursor-agent": "cursor",
  "pi-web": "pi",
  "trae-cli": "trae",
  traecli: "trae",
};

/** The agent a reported `agent` string names: lowercase, aliases folded in, "" (Claude Code reports nothing) is Claude. */
export function agentId(raw: string | undefined | null): string {
  const id = (raw ?? "").trim().toLowerCase();
  return ALIASES[id] ?? (id || "claude");
}

export function isMarked(id: string): id is MarkedAgent {
  return (MARKED_AGENTS as readonly string[]).includes(id);
}

/** What the agent is called: the product's name for the ones the app knows, else the runtime's own label, else the string as reported. */
export function agentName(raw: string | undefined | null, label?: string): string {
  const id = agentId(raw);
  if (isMarked(id) || id === "traex") return t(`agent-mark.${id}`);
  return label?.trim() || (raw ?? "").trim() || t("agent-mark.unknown");
}

/** The agent of a session, the same way `agentLabel` (lib/mentions.ts) tells: a Codex thread read off the app-server or history is Codex whatever it reports. */
export function sessionAgent(s: Pick<Session, "agent" | "agent_label" | "source_kind" | "agent_surface">): { agent: string; label?: string; surface?: AgentSurface } {
  const codex = s.source_kind === "codex_app_server" || s.source_kind === "codex_history";
  return { agent: codex ? "codex" : s.agent ?? "", label: s.agent_label, surface: agentSurface(s.agent_surface) };
}

/** The client an agent runs in (the node's `agent_surface`, #6390): its desktop app, or its command line in a terminal. */
export type AgentSurface = "desktop" | "cli";

/** A reported surface the app knows, else none: an older node, an SDK, a runtime say nothing. */
export function agentSurface(raw: string | undefined | null): AgentSurface | undefined {
  return raw === "desktop" || raw === "cli" ? raw : undefined;
}

/** The letter drawn for an agent with no mark of its own: the first letter or digit of its name, upper case. */
export function agentLetter(name: string): string {
  const first = Array.from(name.trim()).find((c) => /[\p{L}\p{N}]/u.test(c));
  return first ? first.toUpperCase() : "?";
}
