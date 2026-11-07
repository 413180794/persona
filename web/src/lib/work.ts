// A session's work state as the lists show it (docs/assistant/agent-work.md): the hub triages each stopped turn into done or
// blocked on something named, with one line for the row.
import { t } from "../i18n";
import type { Session, WorkState } from "./types";

/** The hub waits it out: it wakes it (wake_at), or it wakes itself (its background tasks, a /loop). */
export function hubWaits(w?: WorkState): boolean {
  if (!w || w.phase !== "blocked" || w.on === "user") return false;
  return Boolean(w.wake_at) || (w.on === "external" && !w.wait && !w.agent);
}

/** The row's line for a stopped session the hub triaged: "✓ conclusion" when done, what it waits for when blocked (with
 *  when the hub acts next); undefined while it works or before the hub triaged its turn. */
export function workLine(s: Pick<Session, "work" | "state" | "status">, now = Date.now()): string | undefined {
  const w = s.work;
  if (!w?.line || w.phase === "working" || s.state === "working") return undefined;
  if (w.phase === "done") return `✓ ${w.line}`;
  if (!w.wake_at) return w.line;
  const ms = Date.parse(w.wake_at) - now;
  const retry = w.on === "transient";
  const when =
    Number.isNaN(ms) || ms < 60_000
      ? t(retry ? "lib-work.retry_soon" : "lib-work.wake_soon")
      : t(retry ? "lib-work.retry_in" : "lib-work.wake_in", { duration: inMinutes(ms) });
  return t("lib-work.line_when", { line: w.line, when });
}

// "12 min", "1 h 5 min": when the hub acts next, minutes are enough (format.ts shortDuration, which imports this).
function inMinutes(ms: number): string {
  const m = Math.round(ms / 60_000);
  if (m < 60) return t("lib-work.minutes", { count: m });
  return m % 60 ? t("lib-work.hours_minutes", { hours: Math.floor(m / 60), minutes: m % 60 }) : t("lib-work.hours", { count: m / 60 });
}
