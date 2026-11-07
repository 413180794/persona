// The assistant page before the roster is read (docs/managers.md "Clients"): what it read with no header is the
// default's, whichever manager that turns out to be. Once the roster says (another than the hub's own), the page stays
// as it is (not drawn empty and read again), and that answer is kept as the default's, not as the hub's own assistant's.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chooseManager, resetManagerChoice, setRosterDefault } from "./manager-choice";
import { getState, refreshAssistant, reset } from "./store";

let fetcher: ReturnType<typeof vi.fn>;
const asked = () => fetcher.mock.calls.filter((c) => String(c[0]).endsWith("/api/v1/assistant")).map((c) => new Headers(c[1]?.headers).get("X-Persona-Manager"));

beforeEach(() => {
  resetManagerChoice();
  vi.stubGlobal("localStorage", { getItem: () => "tok", setItem: () => {}, removeItem: () => {} });
  fetcher = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const named = new Headers(init?.headers).get("X-Persona-Manager");
    // the hub sends no header to the default (m-1 here); the hub's own assistant is named
    const body = String(url).endsWith("/api/v1/assistant") ? { machine_id: named === "assistant" ? "hub-own" : "work-mac", main: named === "assistant" ? "c-own" : "c-work", conversations: [] } : {};
    return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
  });
  vi.stubGlobal("fetch", fetcher);
});

afterEach(() => {
  reset();
  resetManagerChoice();
  vi.unstubAllGlobals();
});

describe("the assistant page and the roster's default", () => {
  it("keeps the default's page when the roster says which manager that is", async () => {
    await refreshAssistant();
    expect(getState().assistant?.main).toBe("c-work");
    setRosterDefault("m-1");
    // the same page: not emptied, not read again
    expect(getState().assistant?.main).toBe("c-work");
    expect(asked()).toEqual([null]);
  });

  it("does not show the default's page as the hub's own assistant's when that one is picked", async () => {
    await refreshAssistant();
    setRosterDefault("m-1");
    chooseManager("assistant");
    // nothing kept of the hub's own: drawn empty until its own answer comes, which it asks for by name
    expect(getState().assistant).toBeNull();
    await vi.waitFor(() => expect(getState().assistant?.main).toBe("c-own"));
    expect(asked()).toEqual([null, "assistant"]);
    // back to the default: its page at once, from what was kept
    chooseManager("m-1");
    expect(getState().assistant?.main).toBe("c-work");
  });
});
