// @vitest-environment jsdom
// "Hand to AI" with several managers (docs/managers.md "Clients"): the conversation starts with the item's manager and
// its next requests go there too; an item stamped with the default (a hub with one manager stamps every item so) sends
// no header at all.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../lib/router", () => ({ navigate: vi.fn() }));
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: vi.fn() }));

import type { InboxItem } from "../../lib/inbox";
import { chosenManager, managerHeaderFor, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { HandOffDialog } from "./InboxDialogs";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const item = (manager: string): InboxItem => ({
  id: "lark:f1:1", manager, source: "lark", kind: "dm", account: "feishu", account_name: "Lark", title: "Example", preview: "text", count: 1,
  updated_at: "2026-11-01T09:00:00Z", tier: "action", lark: { feed_id: "f1", is_group: false },
});

let host: HTMLDivElement;
let root: Root;
const calls: { url: string; method: string; header: string | null }[] = [];

beforeEach(() => {
  calls.length = 0;
  resetManagerChoice();
  setRosterDefault("assistant");
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? "GET", header: new Headers(init?.headers).get("X-Persona-Manager") });
    const body = url.endsWith("/assistant/conversations") ? { session: { machine_id: "mac", session_id: "s-new" } } : url.includes("/inbox/") ? item("assistant") : {};
    return { ok: true, status: 200, text: async () => JSON.stringify(body), headers: new Headers() };
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetManagerChoice();
});

const send = async () => {
  const button = [...document.body.querySelectorAll("button")].find((b) => b.textContent?.trim() === "发送")!;
  await act(async () => button.click());
  await vi.waitFor(() => expect(calls.some((c) => c.url.endsWith("/assistant/conversations"))).toBe(true));
};
const started = () => calls.find((c) => c.url.endsWith("/assistant/conversations"))!;

describe("Hand to AI", () => {
  it("names nothing for an item stamped with the default and nothing picked (one manager)", async () => {
    act(() => root.render(<HandOffDialog item={item("assistant")} onClose={() => {}} />));
    await send();
    expect(calls.every((c) => c.header === null)).toBe(true);
    expect(chosenManager()).toBeNull();
  });

  it("starts with the item's manager, shows its page and sends the conversation's requests there", async () => {
    act(() => root.render(<HandOffDialog item={item("m-1")} onClose={() => {}} />));
    await send();
    expect(calls[0]).toMatchObject({ url: "/api/v1/inbox/lark:f1:1", header: "m-1" });
    expect(started().header).toBe("m-1");
    expect(chosenManager()).toBe("m-1");
    expect(managerHeaderFor("/assistant/conversations/s-new/feed")).toBe("m-1");
  });
});
