import { beforeEach, describe, expect, it, vi } from "vitest";

const uploads: { file: File; resolve: (path: string) => void; reject: (e: unknown) => void }[] = [];
vi.mock("./clipboard", () => ({
  uploadToMachine: (_mid: string, file: File) => new Promise<string>((resolve, reject) => uploads.push({ file, resolve, reject })),
  uploadFailure: (name: string) => `failed: ${name}`,
}));
const toasts: string[] = [];
vi.mock("../../../ui/atoms", () => ({ toast: (m: string) => void toasts.push(m) }));

import { ENTRY_DRAG_TYPE } from "../../../lib/entry-drag";
import { FileTransfer, type TransferHost } from "./transfer";

const words = { notDriving: "not live", changed: "changed", released: "released" };

function host(over: Partial<TransferHost> = {}) {
  const state = { uploading: [] as (string | null)[], dropping: [] as boolean[], pasted: [] as string[], driving: true, disposed: false };
  const h: TransferHost = {
    mid: "m1",
    cwd: "",
    get driving() {
      return state.driving;
    },
    get disposed() {
      return state.disposed;
    },
    setUploading: (t) => void state.uploading.push(t),
    setDropping: (on) => void state.dropping.push(on),
    paste: (t) => void state.pasted.push(t),
    ...over,
  };
  return { h, state };
}

const file = (name: string, type = "image/png", size = 4) => new File([new Uint8Array(size)], name, { type });
const tick = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  uploads.length = 0;
  toasts.length = 0;
});

describe("FileTransfer.uploadFiles", () => {
  it("uploads, then types the escaped paths and a space as one paste", async () => {
    const { h, state } = host();
    const done = new FileTransfer(h, words).uploadFiles([file("shot one.png"), file("b.png")]);
    expect(state.uploading.at(-1)).toContain("shot one.png");
    uploads[0].resolve("/tmp/shot one.png");
    uploads[1].resolve("/tmp/b.png");
    await done;
    expect(state.uploading.at(-1)).toBeNull();
    expect(state.pasted).toEqual(["/tmp/shot\\ one.png /tmp/b.png "]);
  });
  it("pastes nothing when the terminal is not driven, and says so", async () => {
    const { h, state } = host();
    state.driving = false;
    await new FileTransfer(h, words).uploadFiles([file("a.png")]);
    expect(uploads).toHaveLength(0);
    expect(toasts).toEqual(["not live"]);
    expect(state.pasted).toEqual([]);
  });
  it("pastes nothing when the terminal stopped being driven during the upload", async () => {
    const { h, state } = host();
    const done = new FileTransfer(h, words).uploadFiles([file("a.png")]);
    state.driving = false;
    uploads[0].resolve("/tmp/a.png");
    await done;
    expect(toasts).toEqual(["released"]);
    expect(state.pasted).toEqual([]);
  });
  it("pastes nothing into whatever is open when the surface is gone", async () => {
    const { h, state } = host();
    const done = new FileTransfer(h, words).uploadFiles([file("a.png")]);
    state.disposed = true;
    uploads[0].resolve("/tmp/a.png");
    await done;
    expect(toasts).toEqual(["changed"]);
    expect(state.pasted).toEqual([]);
  });
  it("types the paths that went up and reports the one that did not", async () => {
    const { h, state } = host();
    const done = new FileTransfer(h, words).uploadFiles([file("a.png"), file("b.png")]);
    uploads[0].resolve("/tmp/a.png");
    uploads[1].reject(new Error("boom"));
    await done;
    expect(toasts).toEqual(["failed: b.png"]);
    expect(state.pasted).toEqual(["/tmp/a.png "]);
  });
  it("refuses an empty file without uploading it", async () => {
    const { h } = host();
    await new FileTransfer(h, words).uploadFiles([file("e.png", "image/png", 0)]);
    expect(uploads).toHaveLength(0);
    expect(toasts).toHaveLength(1);
  });
});

function dragEvent(types: string[], data: Record<string, string> = {}, files: File[] = []) {
  const e = {
    dataTransfer: { types, files, items: [], getData: (t: string) => data[t] ?? "", dropEffect: "none" },
    prevented: false,
    stopped: false,
    preventDefault() {
      this.prevented = true;
    },
    stopPropagation() {
      this.stopped = true;
    },
  };
  return e as unknown as DragEvent & { prevented: boolean; stopped: boolean };
}

describe("FileTransfer drops", () => {
  it("lights the drop only while driven and only for files or a Files entry", () => {
    const { h, state } = host();
    const t = new FileTransfer(h, words);
    t.dragEnter(dragEvent(["text/plain"]));
    expect(state.dropping).toEqual([]);
    t.dragEnter(dragEvent(["Files"]));
    expect(state.dropping).toEqual([true]);
    t.dragLeave(dragEvent(["Files"]));
    expect(state.dropping).toEqual([true, false]);
    const over = dragEvent(["Files"]);
    t.dragOver(over);
    expect(over.prevented).toBe(true);
    expect(over.dataTransfer!.dropEffect).toBe("copy");
  });
  it("uploads dropped files and consumes the drop", async () => {
    const { h, state } = host();
    const t = new FileTransfer(h, words);
    const e = dragEvent(["Files"], {}, [file("a.png")]);
    t.drop(e);
    expect(e.prevented && e.stopped).toBe(true);
    uploads[0].resolve("/tmp/a.png");
    await tick();
    expect(state.pasted).toEqual(["/tmp/a.png "]);
    expect(state.dropping.at(-1)).toBe(false);
  });
  it("types a Files panel entry as @path (absolute where no folder is known), uploading nothing", () => {
    const { h, state } = host();
    const e = dragEvent([ENTRY_DRAG_TYPE], { [ENTRY_DRAG_TYPE]: JSON.stringify({ mid: "m1", path: "/home/me/my proj/a b.ts", dir: false }) });
    new FileTransfer(h, words).drop(e);
    expect(uploads).toHaveLength(0);
    expect(state.pasted).toEqual(["@/home/me/my\\ proj/a\\ b.ts "]);
  });
  it("names the entry relative to the folder when the host has one", () => {
    const { h, state } = host({ cwd: "/home/me/proj" });
    const e = dragEvent([ENTRY_DRAG_TYPE], { [ENTRY_DRAG_TYPE]: JSON.stringify({ mid: "m1", path: "/home/me/proj/src/a.ts", dir: false }) });
    new FileTransfer(h, words).drop(e);
    expect(state.pasted).toEqual(["@src/a.ts "]);
  });
  it("refuses an entry of another computer, and any entry while not driven", () => {
    const { h, state } = host();
    new FileTransfer(h, words).drop(dragEvent([ENTRY_DRAG_TYPE], { [ENTRY_DRAG_TYPE]: JSON.stringify({ mid: "other", path: "/x", dir: false }) }));
    expect(state.pasted).toEqual([]);
    expect(toasts).toHaveLength(1);
    state.driving = false;
    new FileTransfer(h, words).drop(dragEvent([ENTRY_DRAG_TYPE], { [ENTRY_DRAG_TYPE]: JSON.stringify({ mid: "m1", path: "/x", dir: false }) }));
    expect(toasts.at(-1)).toBe("not live");
  });
});
