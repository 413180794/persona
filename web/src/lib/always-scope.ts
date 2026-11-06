// Keep the card's promise tied to the node's actual permission keys, including older cards without keys.
import { t } from "../i18n";
import type { Interaction } from "./types";

export function alwaysScope(ix: Pick<Interaction, "agent" | "tool_name" | "always_label" | "always_keys">): string {
  if (ix.always_label) return t("chat-interactions.always_scope_named", { scope: ix.always_label });
  if (ix.agent && ix.agent !== "claude") return "";
  const keys = ix.always_keys ?? [];
  if (keys.length && keys.every((key) => /^tool:(Read|Edit)\(\/\/.*\/\*\*\)$/.test(key))) {
    return t("chat-interactions.always_scope_directory");
  }
  if (!keys.length || keys.every((key) => key === `tool:${ix.tool_name}`)) {
    return ix.tool_name === "Bash"
      ? t("chat-interactions.always_scope_commands")
      : t("chat-interactions.always_scope_tool", { tool: ix.tool_name ?? "" });
  }
  return t("chat-interactions.always_scope_saved");
}
