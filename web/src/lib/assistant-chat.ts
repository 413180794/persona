// A Persona conversation's own controls (the app's AssistantTopBar): what 拷贝信息 copies, and switching its launcher
// (the model) from the conversation itself (the 启动器 menu). A pick holds for that conversation's next messages, sent
// as `launcher` with each (POST …/messages), and the hub resumes the conversation on it. It lasts for this page's life,
// like the app's `assistantLaunchers`: after a reload the conversation's own launcher (the one the hub ran it on) shows.
// The new-conversation page has its own pick under NEW_CONVERSATION, sent with POST /assistant/conversations.
import { useSyncExternalStore } from "react";

export const NEW_CONVERSATION = "new";

/** A launcher as GET /machines/{mid}/launchers lists it (the fields the assistant needs). */
export interface AssistantLauncher {
  name: string;
  model?: string;
  agent?: string;
  base_url?: string;
}

/** Runs Claude Code or Codex: what the assistant can use (Launcher.runsAssistant). TRAEX and other agents are left out. */
export function runsAssistant(l: AssistantLauncher): boolean {
  return l.base_url !== "(traex)" && (!l.agent || l.agent === "claude" || l.agent === "codex");
}

/**
 * The launcher a conversation's next message uses (AssistantTopBar.launcher): the main conversation follows 设置 › 助手
 * (the hub resumes it there) unless picked here; a side one keeps its own; the new-conversation page the default.
 */
export function effectiveLauncher(o: { isNew?: boolean; isMain?: boolean; picked?: string; session?: string; config?: string }): string {
  if (o.isNew) return o.picked || o.config || "claude";
  if (o.isMain) return o.picked || o.config || o.session || "claude";
  return o.picked || o.session || o.config || "claude";
}

/** The model a launcher names, shortened (Fmt.model): the part after the last "/". */
export function shortModel(model?: string): string {
  return model ? model.split("/").pop() ?? model : "";
}

/** What 拷贝信息 copies (AssistantCopyText.info): enough for another agent, or a terminal, to find the conversation. */
export function assistantInfoText(o: { title?: string; isMain: boolean; sessionId: string; machine: string; machineId: string; cwd: string; launcher: string; model?: string }): string {
  const name = [o.isMain ? "主对话" : "", o.title ? `「${o.title}」` : ""].filter(Boolean).join(" ");
  return [
    `Persona 助手的对话${name ? ` ${name}` : ""}`,
    `会话 ID：${o.sessionId}`,
    `所在机器：${o.machine && o.machine !== o.machineId ? `${o.machine}（${o.machineId}）` : o.machineId}`,
    `工作目录：${o.cwd}`,
    `启动器：${o.launcher}${o.model ? ` · 模型：${o.model}` : ""}`,
    `在那台机器上接着聊：cd ${o.cwd} && claude --resume ${o.sessionId}`,
  ].join("\n");
}

const picks = new Map<string, string>();
/** The model picked for the next messages, with the launcher pick (`pickedModel`); absent: the launcher's default. */
const models = new Map<string, string>();
const listeners = new Set<() => void>();

export function conversationKey(mid: string, sid: string) {
  return `${mid}/${sid}`;
}

/** This conversation's pick, if any (key: conversationKey, or NEW_CONVERSATION). */
export function pickedLauncher(key: string): string | undefined {
  return picks.get(key);
}

export function pickLauncher(key: string, name: string | undefined) {
  if (name) picks.set(key, name);
  else picks.delete(key);
  models.delete(key); // another launcher: back to its default model
  listeners.forEach((l) => l());
}

/** The model picked for key, if any. */
export function pickedModel(key: string): string | undefined {
  return models.get(key);
}

/** Pick a model for key (undefined: the launcher's default); the launcher pick stays. */
export function pickModel(key: string, model: string | undefined) {
  if (model) models.set(key, model);
  else models.delete(key);
  listeners.forEach((l) => l());
}

export function usePickedModel(key: string): string | undefined {
  return useSyncExternalStore(subscribe, () => models.get(key));
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export function usePickedLauncher(key: string): string | undefined {
  return useSyncExternalStore(subscribe, () => picks.get(key));
}
