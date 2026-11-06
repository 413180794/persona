// @vitest-environment jsdom
// Handing a card back to the computer (dismiss) leaves its agent waiting there, so it is never counted as answered
// here: not when the hub takes it, and not when the hub says it was handed back already (#4877). A card the hub says
// was answered already (someone answered it) does count: that answer took effect and the agent goes on.
import { afterEach, describe, expect, it, vi } from "vitest";
const reply = vi.hoisted(() => ({ value: {} as Record<string, unknown> }));
vi.mock("../../lib/ops", () => ({ apiOp: vi.fn(async () => reply.value) }));
import { resolve } from "./Interactions";
import { getState, reset, upsertSession } from "../../lib/store";
import { stateOf } from "../../lib/format";
import { answeredLookup } from "../assistant/answered-lookup";
import type { Interaction, Session } from "../../lib/types";

const s = { machine_id: "m1", session_id: "s1", title: "a", state: "needs_you", status: "blocked" } as Session;
const ix: Interaction = { id: "ix1", machine_id: "m1", session_id: "s1", kind: "permission" };
const shown = () => stateOf(answeredLookup(getState().sessions, getState().interactions, getState().answered)("m1", "s1")!);

afterEach(() => reset());

describe("handing a card back (#4877)", () => {
  it("keeps the agent waiting when the hub takes it", async () => {
    upsertSession(s);
    reply.value = { ok: true };
    await resolve(ix, { dismiss: true });
    expect(shown()).toBe("needs_you");
  });

  it("keeps the agent waiting when the hub says it was handed back already", async () => {
    upsertSession(s);
    reply.value = { ok: true, already: true, status: "dismissed" };
    await resolve(ix, { dismiss: true });
    expect(shown()).toBe("needs_you");
  });

  it("counts it answered when the hub says someone answered it already", async () => {
    upsertSession(s);
    reply.value = { ok: true, already: true, status: "resolved", resolution: { allow: true } };
    await resolve(ix, { dismiss: true });
    expect(shown()).toBe("working");
  });
});
