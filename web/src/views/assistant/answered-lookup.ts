// A card answered here goes at once (store endInteraction), while the agent's own state says needs_you until the
// node's next sessions push: rows built from the session (the dock's pill, a task card's status, a 待办 row, a manual
// agent's pill and its 全部 row) kept saying 等你 for that while (#4186). Until the hub catches up, an agent that needs
// you, has no card waiting and has a card just answered here counts as at work; a new card for it makes it wait again
// at once, and so does a needs_you the hub sent after the answer (a second card still on its way, a TUI dialog).
// A deny shows 工作中 too until the next push: the agent goes on either way.
import { stateOf } from "../../lib/format";
import { makeSessionLookup, type SessionLookup } from "../../lib/todos";
import type { AnsweredCard } from "../../lib/store";
import type { Interaction, Session } from "../../lib/types";

const key = (mid: string, sid: string) => `${mid}/${sid}`;

// One answered copy per session object: store selectors compare what they return, so the same session gives back the
// same copy until the hub sends a new one.
const copies = new WeakMap<Session, Session>();

function answeredCopy(s: Session): Session {
  let c = copies.get(s);
  if (!c) {
    c = { ...s, state: "working" };
    copies.set(s, c);
  }
  return c;
}

/** Per agent, the latest answer this page sent it (ms, the card's `at` from endInteraction; 0 without one, so any
 *  needs_you with a time counts as newer). */
function answeredBy(answered: Record<string, AnsweredCard>): Map<string, number> {
  const out = new Map<string, number>();
  for (const a of Object.values(answered)) {
    const k = key(a.ix.machine_id, a.ix.session_id);
    out.set(k, Math.max(out.get(k) ?? 0, a.at ?? 0));
  }
  return out;
}

/** Whether an agent's needs_you is only the hub not having caught up with a card answered here. */
export function answeredHere(s: Session, waiting: ReadonlySet<string>, answered: ReadonlyMap<string, number>): boolean {
  const k = key(s.machine_id, s.session_id);
  const at = answered.get(k);
  if (at === undefined || stateOf(s) !== "needs_you" || waiting.has(k)) return false;
  // a needs_you that started after the answer is a new one, not the old one lingering. state_since is the hub's
  // clock and `at` this browser's: a skew of a few seconds only moves where the 20 s window cuts over.
  const since = s.state_since ? Date.parse(s.state_since) : NaN;
  return Number.isNaN(since) || since <= at;
}

/** The sessions lookup with that applied. */
export function answeredLookup(sessions: Session[] | Record<string, Session[]>, interactions: Interaction[], answered: Record<string, AnsweredCard>): SessionLookup {
  const base = makeSessionLookup(sessions);
  const done = answeredBy(answered);
  if (!done.size) return base;
  const waiting = new Set(interactions.map((ix) => key(ix.machine_id, ix.session_id)));
  return (mid, sid) => {
    const s = base(mid, sid);
    return s && answeredHere(s, waiting, done) ? answeredCopy(s) : s;
  };
}

/** The sessions with that applied, for lists that take them whole (the dock's and 全部's manual agents). Same array
 *  when nothing changes. */
export function answeredSessions(sessions: Session[], interactions: Interaction[], answered: Record<string, AnsweredCard>): Session[] {
  const done = answeredBy(answered);
  if (!done.size) return sessions;
  const waiting = new Set(interactions.map((ix) => key(ix.machine_id, ix.session_id)));
  let changed = false;
  const out = sessions.map((s) => {
    if (!answeredHere(s, waiting, done)) return s;
    changed = true;
    return answeredCopy(s);
  });
  return changed ? out : sessions;
}
