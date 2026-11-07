// Feishu read state from what was really seen (docs/assistant/inbox.md: Lark messages count as read once they are on screen; docs/archive/redesign/ux.md 3.5 / 3.6;
// the app's LarkPager.readReport). The page notes the messages that reached the screen; a moment after the last one
// it reports everything up to the newest seen past the read cursor, with the ids of others' unread messages up to
// there, and moves the cursor: only forward, never twice.

import { comparePosition, type LarkMessage } from "./inbox";

/** POST /api/v1/lark/conversations/{id}/read. */
export interface LarkReadReport {
  position: string;
  badge_count?: number;
  message_ids?: string[];
}

function isSystem(m: LarkMessage) {
  return m.type === "SYSTEM" || (m.parts ?? []).some((p) => p.kind === "text" && p.metadata?.system);
}

/** The position just before `p` (a Feishu position is a number): the cursor that reports `p` itself once seen. */
export function before(p: string): string {
  if (/^\d+$/.test(p)) {
    try {
      return (BigInt(p) - 1n).toString();
    } catch {
      return "0";
    }
  }
  return "0";
}

export class LarkReadTracker {
  /** Everything at or before this position has been reported (or was read before the chat opened). */
  cursor: string | null;
  /** Positions that reached the screen. */
  private seen = new Set<string>();

  constructor(cursor: string | null) {
    this.cursor = cursor;
  }

  /** A message reached the screen; false if it had already. */
  see(position: string): boolean {
    if (this.seen.has(position)) return false;
    this.seen.add(position);
    return true;
  }

  /** What to report now, moving the cursor; null when nothing new was seen. */
  report(messages: LarkMessage[]): LarkReadReport | null {
    if (this.cursor === null) return null;
    const cursor = this.cursor;
    const past = (m: LarkMessage) => comparePosition(cursor, m.position) < 0;
    let top: LarkMessage | undefined;
    for (const m of messages) if (this.seen.has(m.position) && past(m) && (!top || comparePosition(top.position, m.position) < 0)) top = m;
    if (!top) return null;
    const ids = messages
      .filter((m) => !m.is_mine && m.is_read !== true && !isSystem(m) && past(m) && comparePosition(m.position, top.position) <= 0)
      .map((m) => m.id);
    this.cursor = top.position;
    return { position: top.position, ...(top.badge_count !== undefined ? { badge_count: top.badge_count } : {}), ...(ids.length ? { message_ids: ids } : {}) };
  }

  /** The report did not go through: back to where it was, so the next look reports it again. */
  undo(report: LarkReadReport, previous: string | null) {
    if (this.cursor === report.position) this.cursor = previous;
  }
}

/** Where reporting starts when a chat opens: an open inbox item reports from its first new message on (the first
 * screen counts); otherwise from after what is loaded now, so only what arrives while it is open is reported. */
export function initialCursor(focus: string | undefined, messages: LarkMessage[], topic: boolean): string | null {
  if (topic) return null; // a topic group's read state is per topic: the app does not report it either
  if (focus) return before(focus);
  return messages.length ? messages[messages.length - 1].position : "0";
}
