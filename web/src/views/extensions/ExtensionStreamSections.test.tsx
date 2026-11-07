// @vitest-environment jsdom
// Pure native/injected-adapter evidence only; this does not launch Deno or a backend.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExtensionUIStreamSession } from "../../lib/extension-ui-stream";
import { AppUIStreamPage } from "../../lib/extension-ui-app-stream";
import type { UIStreamDelta, UIStreamRequest, UIStreamSnapshot } from "../../lib/extension-wire";
import { readView } from "../../lib/extension-ui";
import { ExtensionStreamSections } from "./ExtensionStreamSections";
import { INERT, ViewSections } from "./ViewBody";

vi.hoisted(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  (globalThis as Record<string, unknown>).matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {} });
});
let root: Root | undefined;
let host: HTMLDivElement | undefined;
let session: ExtensionUIStreamSession | undefined;
afterEach(() => { act(() => root?.unmount()); session?.close(); root = undefined; session = undefined; host?.remove(); host = undefined; });
const identity = { owner: "owner-a", hubGroup: "group-a", extension: "example", contribution: "page", navigation: "entry-a" };
const snapshot: UIStreamSnapshot = {
  protocol: 1, session: "session-a", epoch: "epoch-a", version: 1, root_key: "root",
  view: { sections: [{ key: "main", items: [
    { type: "text", key: "text", text: "Live" },
    { type: "field", key: "field", kind: "text", label: "Draft", name: "draft", value: "Initial" },
    { type: "composer", key: "composer", name: "body", buttons: [{ title: "Send", action: { id: "same" } }] },
    { type: "button", key: "button", title: "Run", action: { id: "same" } },
  ] }, { key: "other", items: [] }] },
};
async function mount(confirm?: (action: { id: string }) => Promise<boolean>, initial = snapshot) {
  const sent: UIStreamRequest[] = [];
  let n = 0;
  session = new ExtensionUIStreamSession({ identity, id: () => `id-${++n}`, context: { platform: "web", ui_version: 5, stream: { protocol: 1, components: ["text", "field", "composer", "button"] } }, transport: { send: async (r) => { sent.push(r); } } });
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  await act(async () => {
    root!.render(<ExtensionStreamSections identity={identity} availableAdapter={session} fallback={<p>HTTP fallback</p>} confirm={confirm} />);
  });
  await act(async () => { expect(session!.receiveSnapshot(initial)).toBe(true); });
  return { session: session!, host, sent };
}
async function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    const prototype = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
function d(operations: UIStreamDelta["operations"]): UIStreamDelta { return { protocol: 1, session: "session-a", epoch: "epoch-a", base_version: 1, version: 2, operations }; }

describe("explicit available native renderer guard", () => {
  it.each(["ambiguous", "rejected"] as const)("rolls back an optimistic switch after an actual adapter %s receipt with unchanged canonical value", async (status) => {
    const sent: UIStreamRequest[] = [];
    let receive: (raw: string) => void = () => {};
    const page = new AppUIStreamPage({ identity, context: { platform: "web", ui_version: 7, stream: { protocol: 1, components: ["row"] } } }, "manager", {
      connected: () => true, send: (r) => { sent.push(r); }, connection: () => () => {}, frames: (fn) => { receive = fn; return () => {}; },
    }, vi.fn(), vi.fn());
    session = page.session;
    host = document.createElement("div"); document.body.append(host); root = createRoot(host);
    const frame = (body: object) => receive(JSON.stringify({ type: "extension_ui_stream", manager: "manager", ...body }));
    try {
      await act(async () => root!.render(<ExtensionStreamSections identity={identity} availableAdapter={session} fallback={null} readResult={page.readResult} />));
      const initial: UIStreamSnapshot = { ...snapshot, view: { sections: [{ key: "main", items: [{ type: "row", key: "row", title: "Power", control: { kind: "switch", name: "power", on: false, action: { id: "toggle" } } }] }] } };
      await act(async () => frame({ kind: "snapshot", request_id: sent[0].request_id, snapshot: initial }));
      const control = host.querySelector<HTMLButtonElement>('[role="switch"]')!;
      expect(control.getAttribute("aria-checked")).toBe("false");
      await act(async () => control.click());
      expect(control.getAttribute("aria-checked")).toBe("true");
      const request = sent.at(-1)!;
      if (request.request !== "event") throw new Error("event expected");
      await act(async () => {
        if (status === "ambiguous") frame({ kind: "delta", delta: d([]) });
        frame({ kind: "receipt", request_id: request.request_id, receipt: { session: initial.session, epoch: initial.epoch, event_id: request.interaction.event_id, version: status === "ambiguous" ? 2 : 1, status } });
      });
      expect(control.getAttribute("aria-checked")).toBe("false");
      expect(control.disabled).toBe(false);
      expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
    } finally { await act(async () => page.dispose()); }
  });
  it("rejects an old DOM interaction before a newer snapshot has rendered", async () => {
    const { session, host, sent } = await mount();
    const button = [...host.querySelectorAll("button")].find((b) => b.textContent === "Run")!;
    await act(async () => {
      expect(session.receiveDelta(d([{ op: "replace", key: "button", item: { type: "button", key: "button", title: "Changed", action: { id: "same", values: { target: "new" } } } }]))).toBe(true);
      button.click(); // The old render is still mounted; it must not borrow the newer callback/version.
    });
    expect(sent.filter((r) => r.request === "event")).toHaveLength(0);
    await act(async () => { [...host.querySelectorAll("button")].find((b) => b.textContent === "Changed")!.click(); });
    expect(sent.filter((r) => r.request === "event")).toMatchObject([{ interaction: { version: 2, value: { target: "new" } } }]);
  });
  it("keeps secret controls unfilled and uses native field defaults for display and submit", async () => {
    const initial = structuredClone(snapshot);
    initial.view.sections[0].items = [
      { type: "field", key: "secret", kind: "secret", name: "credential", label: "Secret", value: "must not prefill" },
      { type: "field", key: "choice", kind: "choice", name: "choice", label: "Choice", choices: ["a", "b", "c", "d", "e"].map((value) => ({ value, title: value })) },
      { type: "field", key: "toggle", kind: "toggle", name: "enabled", label: "Enabled" },
      { type: "field", key: "prototype", kind: "choice", name: "__proto__", label: "Prototype-like name", choices: ["safe", "other", "c", "d", "e"].map((value) => ({ value, title: value })) },
      { type: "button", key: "submit", title: "Submit", action: { id: "submit", submit: true } },
    ];
    const { host, sent } = await mount(undefined, initial);
    expect(host.querySelector<HTMLInputElement>('input[type="password"]')!.value).toBe("");
    expect(host.querySelector("select")!.value).toBe("a");
    expect([...host.querySelectorAll("select")].at(-1)!.value).toBe("safe");
    await act(async () => { [...host.querySelectorAll("button")].find((b) => b.textContent === "Submit")!.click(); });
    expect(sent.filter((r) => r.request === "event")).toMatchObject([{ interaction: { value: JSON.parse('{"choice":"a","enabled":"false","__proto__":"safe"}') } }]);
  });
  it("does not replay an unchanged load-more observer after unrelated deltas", async () => {
    const observers: Array<() => void> = [];
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: (entries: Array<{ isIntersecting: boolean }>) => void) { observers.push(() => callback([{ isIntersecting: true }])); }
      observe() {} disconnect() {}
    });
    try {
      const initial = structuredClone(snapshot);
      initial.view.sections[0].more = { action: { id: "more" }, title: "More" };
      const { session, sent } = await mount(undefined, initial);
      await act(async () => { observers.forEach((trigger) => trigger()); });
      expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
      const count = observers.length;
      await act(async () => { session.receiveDelta(d([{ op: "replace", key: "text", item: { type: "text", key: "text", text: "Updated" } }])); });
      expect(observers).toHaveLength(count);
      expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
      await act(async () => { session.receiveDelta({ ...d([{ op: "move", key: "text", parent: "other" }]), base_version: 2, version: 3 }); });
      expect(observers.length).toBeGreaterThan(count); // Its last Run remounted; the session must remember the observation.
      await act(async () => { observers.forEach((trigger) => trigger()); });
      expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
      await act(async () => { [...host!.querySelectorAll("button")].find((b) => b.textContent === "More")!.click(); });
      expect(sent.filter((r) => r.request === "event")).toHaveLength(2); // Explicit presses are not automatic retries.
    } finally { vi.unstubAllGlobals(); }
  });
  it("returns exactly the original native HTTP markup without an adapter", () => {
    const fallback = <ViewSections view={readView(snapshot.view)} {...INERT} />;
    expect(renderToStaticMarkup(<ExtensionStreamSections identity={identity} fallback={fallback} />)).toBe(renderToStaticMarkup(fallback));
  });
  it("rejects an adapter for another authenticated group before drawing its data", async () => {
    const { session } = await mount();
    expect(() => renderToStaticMarkup(<ExtensionStreamSections identity={{ ...identity, hubGroup: "group-b" }} availableAdapter={session} fallback={null} />)).toThrow("identity mismatch");
  });
  it("retains field/composer drafts across text deltas, section moves and native remounts", async () => {
    const { session, host, sent } = await mount();
    const input = host.querySelector("input")!;
    const box = host.querySelector("textarea")!;
    await type(input, "Local field draft"); await type(box, "Local composer draft");
    expect(session.getDraft("field")).toBe("Local field draft");
    expect(session.getDraft("composer")).toBe("Local composer draft");
    host.scrollTop = 90;
    await act(async () => { expect(session.receiveDelta(d([
      { op: "replace", key: "text", item: { type: "text", key: "text", text: "Updated live text" } },
      { op: "move", key: "field", parent: "other" },
      { op: "move", key: "composer", parent: "other" },
    ]))).toBe(true); });
    expect(host.querySelector("input")!.value).toBe("Local field draft");
    expect([...host.querySelectorAll("textarea")].at(-1)!.value).toBe("Local composer draft");
    expect(host.scrollTop).toBe(90); // jsdom state check, not browser geometry/screenshot evidence.
    const send = [...host.querySelectorAll("button")].find((b) => b.textContent === "Send")!;
    await act(async () => { send.click(); });
    expect(sent.filter((r) => r.request === "event")).toMatchObject([{ interaction: { key: "composer", action: "same", event: "submit", value: { body: "Local composer draft" } } }]);
  });
  it("binds identical callback IDs to their native node and event slot", async () => {
    const { host, sent } = await mount();
    await act(async () => { [...host.querySelectorAll("button")].find((b) => b.textContent === "Run")!.click(); });
    expect(sent.filter((r) => r.request === "event")).toMatchObject([{ interaction: { key: "button", action: "same", event: "press", value: {} } }]);
  });
  it("requires caller-native confirmation and never sends from stale confirmed versions", async () => {
    let answer!: (yes: boolean) => void;
    const { session, host, sent } = await mount(async () => new Promise<boolean>((r) => { answer = r; }));
    const confirmed = structuredClone(snapshot);
    const button = confirmed.view.sections[0].items[3];
    if (button.type !== "button") throw new Error("Expected button");
    button.action.confirm = "Confirm operation";
    confirmed.version = 2;
    await act(async () => { session.setConnected(false); session.setConnected(true); session.receiveSnapshot(confirmed); });
    await act(async () => { [...host.querySelectorAll("button")].find((b) => b.textContent === "Run")!.click(); });
    await act(async () => { session.receiveDelta({ ...d([{ op: "replace", key: "text", item: { type: "text", key: "text", text: "New version" } }]), base_version: 2, version: 3 }); answer(true); });
    expect(sent.filter((r) => r.request === "event")).toHaveLength(0);
  });
});
