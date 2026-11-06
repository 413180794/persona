// The sidebar's agent rows say under their title what they need from you or what they did (#3195), so the owner
// does not have to hover or open each one: the same words as the to-dos page's agent rows (todos.json kicker_*).
import { t } from "../i18n";
import { sessionPreview, stateOf } from "./format";
import { interactionAsk } from "./inbox";
import { plainMarkdown } from "./plain-markdown";
import type { Interaction, Session } from "./types";

/** What kind of card waits, in a word: "Asking you" / "Plan to approve" / "Needs permission" / "Terminal dialog" / "MCP request". */
export function interactionKicker(ix: Pick<Interaction, "kind"> | undefined): string {
  switch (ix?.kind) {
    case "plan":
      return t("todos.kicker_plan");
    case "permission":
      return t("todos.kicker_permission");
    case "screen":
      return t("todos.kicker_screen");
    case "elicitation":
      return t("todos.kicker_elicitation");
    default:
      return t("todos.kicker_question");
  }
}

// a row shows one line of it; the rest is cut by the row anyway
const MAX = 160;

/**
 * The second line of an agent's sidebar row: for one that needs you, its card's kind and what it asks
 * ("Asking you: which test framework?"), else why it waits (sessionPreview: the host's reason, the error, the hub's triage, its
 * last words); for one that finished unseen, what it did ("✓ conclusion" or its last words, as plain text). Undefined for
 * every other state, and when there is nothing to say. `lead` is the red part, colon included when text follows.
 */
export function rowLine(s: Session, ix?: Interaction): { lead?: string; text: string } | undefined {
  const st = stateOf(s);
  if (st !== "needs_you" && st !== "unread") return undefined;
  if (st === "needs_you" && ix) {
    const kicker = interactionKicker(ix);
    const text = plainMarkdown(interactionAsk(ix)).slice(0, MAX);
    return text ? { lead: t("todos.kicker_line", { kicker }), text } : { lead: kicker, text: "" };
  }
  const text = plainMarkdown(sessionPreview(s)).slice(0, MAX);
  return text ? { text } : undefined;
}
