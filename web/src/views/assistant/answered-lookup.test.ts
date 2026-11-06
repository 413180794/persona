// A card answered here stops its agent's "Needs you" at once (#4186), and a new card makes it wait again.
import { describe, expect, it } from "vitest";
import { stateOf } from "../../lib/format";
import { todoStatus, type Todo } from "../../lib/todos";
import type { AnsweredCard } from "../../lib/store";
import type { Interaction, Session } from "../../lib/types";
import { dockAgents } from "../../lib/todos";
import { answeredLookup, answeredSession, answeredSessions } from "./answered-lookup";

const s = { machine_id: "m1", session_id: "s1", title: "a", state: "needs_you", status: "blocked" } as Session;
const other = { machine_id: "m1", session_id: "s2", title: "b", state: "needs_you", status: "blocked" } as Session;
const ix = (sid: string, id = "ix"): Interaction => ({ id, machine_id: "m1", session_id: sid, kind: "permission" });
// withdrawn: the hub has taken the card away already (a card it has not is #4877's case, below)
const answered = (sid: string): Record<string, AnsweredCard> => ({ old: { ix: ix(sid, "old"), resolution: { allow: true }, at: Date.now(), withdrawn: true } as AnsweredCard });
const todo = { id: "t", title: "t", created_at: "", updated_at: "", agents: [{ machine_id: "m1", session_id: "s1" }] } as Todo;

describe("answeredLookup", () => {
  it("counts an agent whose card was just answered here as at work", () => {
    const look = answeredLookup([s], [], answered("s1"));
    expect(stateOf(look("m1", "s1")!)).toBe("working");
    expect(todoStatus(todo, look)).toBe("working");
  });

  it("waits again as soon as a new card comes for it", () => {
    const look = answeredLookup([s], [ix("s1", "new")], answered("s1"));
    expect(stateOf(look("m1", "s1")!)).toBe("needs_you");
    expect(todoStatus(todo, look)).toBe("needs_you");
  });

  it("leaves agents with nothing answered here alone", () => {
    const look = answeredLookup([s, other], [], answered("s1"));
    expect(stateOf(look("m1", "s2")!)).toBe("needs_you");
    expect(stateOf(answeredLookup([s], [], {})("m1", "s1")!)).toBe("needs_you");
  });

  it("gives back the same object for the same session (store selectors compare it)", () => {
    const a = answeredLookup([s], [], answered("s1"))("m1", "s1");
    const b = answeredLookup([s], [], answered("s1"))("m1", "s1");
    expect(a).toBe(b);
  });
});

describe("the review's cases (#4186)", () => {
  it("takes a manual agent's pill off 等你 too (dockAgents over answeredSessions)", () => {
    const manual = { ...s, todo: undefined } as Session;
    const raw = dockAgents([manual], [], "main", "main");
    expect(raw.map((a) => a.state)).toEqual(["needs_you"]);
    const fixed = dockAgents(answeredSessions([manual], [], answered("s1")), [], "main", "main");
    expect(fixed.map((a) => a.state)).toEqual(["working"]);
    expect(dockAgents(answeredSessions([manual], [ix("s1", "new")], answered("s1")), [], "main", "main").map((a) => a.state)).toEqual(["needs_you"]);
  });

  it("lets a needs_you the hub sent after the answer win at once", () => {
    const later = { ...s, state_since: new Date(Date.now() + 60_000).toISOString() } as Session;
    expect(stateOf(answeredLookup([later], [], answered("s1"))("m1", "s1")!)).toBe("needs_you");
    const earlier = { ...s, state_since: new Date(Date.now() - 60_000).toISOString() } as Session;
    expect(stateOf(answeredLookup([earlier], [], answered("s1"))("m1", "s1")!)).toBe("working");
  });

  it("gives back the same list when nothing was answered here", () => {
    const list = [s, other];
    expect(answeredSessions(list, [], {})).toBe(list);
  });
});

describe("the answer's time (#4186 re-review)", () => {
  it("is when the page answered, so a needs_you that came later wins even if the dock mounts afterwards", () => {
    const t = Date.now() - 10_000;
    const card = { old: { ix: ix("s1", "old"), resolution: { allow: true }, at: t, withdrawn: true } as AnsweredCard };
    const later = { ...s, state_since: new Date(t + 5_000).toISOString() } as Session;
    expect(stateOf(answeredLookup([later], [], card)("m1", "s1")!)).toBe("needs_you");
    const before = { ...s, state_since: new Date(t - 5_000).toISOString() } as Session;
    expect(stateOf(answeredLookup([before], [], card)("m1", "s1")!)).toBe("working");
  });
});

describe("answeredSession, one row (#4286)", () => {
  it("does one row by the same rule, and waits again with a new card", () => {
    const at = Date.now();
    expect(stateOf(answeredSession(s, false, at))).toBe("working");
    expect(answeredSession(s, true, at)).toBe(s);
    expect(answeredSession(s, false, undefined)).toBe(s);
  });
  it("lets a later needs_you win", () => {
    const later = { ...s, state_since: new Date(Date.now() + 60_000).toISOString() } as Session;
    expect(answeredSession(later, false, Date.now())).toBe(later);
  });
});

describe("a push ahead of the card's withdrawal (#4877)", () => {
  const later = { ...s, state_since: new Date(Date.now() + 60_000).toISOString() } as Session;
  const card = (withdrawn?: boolean): Record<string, AnsweredCard> => ({ old: { ix: ix("s1", "old"), resolution: { allow: true }, at: Date.now(), withdrawn } as AnsweredCard });

  it("does not bring 等你 back before the hub has withdrawn the card", () => {
    expect(stateOf(answeredLookup([later], [], card())("m1", "s1")!)).toBe("working");
    expect(stateOf(answeredSessions([later], [], card())[0])).toBe("working");
  });

  it("lets it win once the hub has withdrawn the card", () => {
    expect(stateOf(answeredLookup([later], [], card(true))("m1", "s1")!)).toBe("needs_you");
  });

  it("still waits again at once for a new card", () => {
    expect(stateOf(answeredLookup([later], [ix("s1", "new")], card())("m1", "s1")!)).toBe("needs_you");
  });
});
