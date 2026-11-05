// @vitest-environment jsdom
// Pictures put in the box and never sent free their object URLs when the box goes away or shows another
// conversation (#2378), including ones still being read or a send that fails after the box moved on.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { sendDraft } from "../../lib/drafts";
import { Composer, type Outgoing } from "./Composer";

// imagePayload decodes on a canvas, which jsdom has not: each read waits on a promise the test settles
const reads: ((p: { mime: string; data: string }) => void)[] = [];
vi.mock("../../lib/attachments", async (orig) => ({
  ...(await orig<typeof import("../../lib/attachments")>()),
  imagePayload: () => new Promise<{ mime: string; data: string }>((resolve) => reads.push(resolve)),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let urls = 0;
const create = vi.fn(() => `blob:test/${++urls}`);
const revoke = vi.fn();
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  urls = 0;
  reads.length = 0;
  create.mockClear();
  revoke.mockClear();
  URL.createObjectURL = create;
  URL.revokeObjectURL = revoke;
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const render = (draftKey: string, onSend: (m: Outgoing) => Promise<boolean> = async () => true) =>
  act(() => root.render(<Composer draftKey={draftKey} placeholder="" onSend={onSend} />));

// an edited picture from the chat: no canvas on this path
const putImage = (draftKey: string) =>
  act(() => {
    sendDraft(draftKey, { type: "image", image: { name: "a.png", mime: "image/png", data: btoa("png") } });
  });

const pickFile = (file: File) =>
  act(() => {
    const input = host.querySelector<HTMLInputElement>('input[type="file"]')!;
    Object.defineProperty(input, "files", { value: [file], configurable: true });
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });

const trayImages = () => host.querySelectorAll("img").length;

describe("Composer image object URLs", () => {
  it("are freed when the box unmounts", () => {
    render("a");
    putImage("a");
    expect(create).toHaveBeenCalledTimes(1);
    expect(revoke).not.toHaveBeenCalled();
    act(() => root.unmount());
    expect(revoke).toHaveBeenCalledWith("blob:test/1");
    root = createRoot(host); // afterEach unmounts again
  });

  it("are freed when the box switches to another conversation", () => {
    render("a");
    putImage("a");
    render("b");
    expect(revoke).toHaveBeenCalledWith("blob:test/1");
    expect(trayImages()).toBe(0);
  });

  it("are freed, not put back into the other conversation, when a send fails after the switch", async () => {
    let finish!: (ok: boolean) => void;
    render("a", () => new Promise<boolean>((resolve) => (finish = resolve)));
    putImage("a");
    act(() => host.querySelector<HTMLButtonElement>('button[aria-label="发送"]')!.click());
    render("b", async () => true);
    expect(revoke).not.toHaveBeenCalled(); // the picture is in flight, not in the box
    await act(async () => finish(false));
    expect(revoke).toHaveBeenCalledWith("blob:test/1");
    expect(trayImages()).toBe(0);
  });

  it("are put back when a send fails in the same conversation", async () => {
    render("a", async () => false);
    putImage("a");
    await act(async () => host.querySelector<HTMLButtonElement>('button[aria-label="发送"]')!.click());
    expect(revoke).not.toHaveBeenCalled();
    expect(trayImages()).toBe(1);
  });

  it("are freed for a picked file whose read finishes after the switch", async () => {
    render("a");
    await pickFile(new File(["png"], "b.png", { type: "image/png" }));
    render("b");
    await act(async () => reads[0]({ mime: "image/png", data: btoa("png") }));
    expect(create).toHaveBeenCalledTimes(1);
    expect(revoke).toHaveBeenCalledWith("blob:test/1");
    expect(trayImages()).toBe(0);
  });

  it("are freed for a picked file whose read finishes after the box unmounted", async () => {
    render("a");
    await pickFile(new File(["png"], "b.png", { type: "image/png" }));
    act(() => root.unmount());
    await act(async () => reads[0]({ mime: "image/png", data: btoa("png") }));
    expect(revoke).toHaveBeenCalledWith("blob:test/1");
    root = createRoot(host);
  });
});
