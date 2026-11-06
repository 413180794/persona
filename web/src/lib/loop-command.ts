// /loop typed in an agent's composer (#1586): Claude Code's own command, `/loop [interval] <what to do>`, which re-runs
// the prompt on a schedule. The composer only helps write it; the node types it into Claude as it is.

import { t } from "../i18n";

/** The command word, as the "/" menu puts it in the box (the interval and the task follow). */
export const LOOP_TEXT = "/loop ";

/** Claude Code's interval forms: a number and s / m / h / d (seconds round up to a minute). */
const INTERVAL = /^(\d+)([smhd])$/i;

const UNIT: Record<string, string> = { s: "lib-loop-command.unit_seconds", m: "lib-loop-command.unit_minutes", h: "lib-loop-command.unit_hours", d: "lib-loop-command.unit_days" };

/** The draft is a /loop command: the word alone or followed by a space (not "/looping"). */
export function isLoopDraft(draft: string): boolean {
  return /^\/loop(?:\s|$)/i.test(draft);
}

export interface LoopHint {
  /** One line under the box: what it will do, or what is still missing. */
  text: string;
  /** Nothing to repeat yet: sending would only start Claude's own /loop help. */
  missing: boolean;
}

/** What a /loop draft will do, in words; null for any other draft. */
export function loopHint(draft: string): LoopHint | null {
  if (!isLoopDraft(draft)) return null;
  const words = draft.slice(5).trim().split(/\s+/).filter(Boolean);
  const m = words[0] ? INTERVAL.exec(words[0]) : null;
  const task = (m ? words.slice(1) : words).join(" ");
  if (!task) {
    return {
      missing: true,
      text: m
        ? t("lib-loop-command.every_no_task", { every: every(m) })
        : t("lib-loop-command.usage"),
    };
  }
  return { missing: false, text: m ? t("lib-loop-command.every_task", { every: every(m), task }) : t("lib-loop-command.self_paced_task", { task }) };
}

function every(m: RegExpExecArray): string {
  const n = Number(m[1]);
  const unit = m[2].toLowerCase();
  return t(UNIT[unit], { count: n });
}

/** Claude Code has /loop; Codex, a compatible CLI and other runtimes do not. */
export function hasLoop(s: { agent?: string; source_kind?: string } | undefined): boolean {
  return !!s && (!s.agent || s.agent === "claude") && !s.source_kind?.startsWith("codex");
}
