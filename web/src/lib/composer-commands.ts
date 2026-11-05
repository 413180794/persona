// What an agent's composer offers and refuses, by what the session can do (iOS ChatView canPickModel / canClear,
// Android canTypeClaudeCommands): the "/" list and the cap on pictures.
import { canSwitchModel } from "./model-switch";
import { hasLoop, LOOP_TEXT } from "./loop-command";
import { sessionCan, stateOf } from "./format";
import type { Session } from "./types";

/** Pictures one message takes (iOS ChatView, MacComposerDrop.maxImages, Android ChatComposer). */
export const MAX_IMAGES = 4;

/** Claude Code in a herdr pane: it takes `/model` and `/clear` typed into its terminal (Codex, TRAEX, the assistant
 *  and the read-only sources have no such commands here). */
export function paneClaude(s: Session | undefined): boolean {
  if (!s || s.assistant || s.source_kind || !s.pane_id) return false;
  if (s.agent && s.agent !== "claude") return false;
  return sessionCan(s, "send") && sessionCan(s, "terminal");
}

/** The agent commands the "/" list shows for `s`: /model where the model can switch, /clear for pane Claude while it
 *  neither works nor waits for you, /loop for Claude agents. */
export function agentCommands<C extends { text: string }>(all: C[], s: Session | undefined): C[] {
  const st = s ? stateOf(s) : "offline";
  return all.filter((c) => {
    const word = c.text.trim();
    if (word === "/model") return paneClaude(s) && canSwitchModel(s);
    if (word === "/clear") return paneClaude(s) && st !== "working" && st !== "needs_you";
    if (c.text === LOOP_TEXT) return hasLoop(s);
    return true;
  });
}
