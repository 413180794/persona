// @vitest-environment jsdom
// Downloading a paged text file: the node sends text a page at a time, so a Blob of the pages read here could be a
// truncated copy (a read cap, or a page that fails). The download goes through the hub's file/download instead,
// which serves the whole file; a Blob is built only from content that is fully loaded.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { t } from "../../../i18n";
import type { FileContent } from "../../../lib/files";

const pages: ((path: string) => Promise<unknown>)[] = [];
const requested: string[] = [];
vi.mock("../../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../../lib/api")>()),
  api: (path: string) => {
    requested.push(path);
    const next = pages.shift();
    return next ? next(path) : Promise.reject(new Error("unexpected request " + path));
  },
}));
vi.mock("../../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../../ui/atoms")>()), toast: () => {} }));

const { FileView } = await import("./FileView");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const text = (over: Partial<FileContent>): FileContent => ({ path: "/w/big.log", name: "big.log", size: 300 << 20, mime: "text/plain", truncated: false, is_dir: false, ...over });
const byLabel = (label: string) =>
  [...document.querySelectorAll("button")].find((b) => b.getAttribute("aria-label") === label || b.textContent?.trim() === label) as HTMLButtonElement | undefined;
const flush = async () => {
  for (let i = 0; i < 10; i++) await act(async () => { await Promise.resolve(); });
};

// jsdom's Blob has no text(); FileReader reads it.
const readBlob = (b: Blob) =>
  new Promise<string>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result));
    r.onerror = () => reject(r.error);
    r.readAsText(b);
  });

let host: HTMLDivElement;
let root: Root;
let clicked: { href: string; name: string }[] = [];
let blobs: Blob[] = [];

beforeEach(() => {
  pages.length = 0;
  requested.length = 0;
  clicked = [];
  blobs = [];
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  URL.createObjectURL = (b: Blob | MediaSource) => {
    blobs.push(b as Blob);
    return "blob:x";
  };
  URL.revokeObjectURL = () => {};
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    clicked.push({ href: this.getAttribute("href") ?? "", name: this.download });
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function openAndDownload() {
  await act(async () => root.render(<FileView mid="m1" path="/w/big.log" cwd="/w" onClose={() => {}} onOpenPath={() => {}} />));
  await flush();
  const button = byLabel(t("session-files-fileview.download"));
  expect(button, "download button").toBeTruthy();
  await act(async () => button!.click());
  await flush();
}

describe("FileView download", () => {
  it("saves a paged text file through file/download, not a Blob of the pages read", async () => {
    pages.push(async () => text({ text: "page one\n", next: 2 << 20, revision: "r1" }));
    pages.push(async () => text({ text: "page two\n", next: 0, revision: "r1" }));
    await openAndDownload();
    expect(blobs).toEqual([]);
    expect(clicked).toHaveLength(1);
    expect(clicked[0].href).toContain("/api/v1/machines/m1/file/download?path=%2Fw%2Fbig.log&cwd=%2Fw");
    expect(clicked[0].name).toBe("big.log");
    expect(requested).toHaveLength(1); // the rest is not read into the page first
  });

  it("does not save a partial copy when a later page would fail", async () => {
    pages.push(async () => text({ text: "page one\n", next: 2 << 20, revision: "r1" }));
    pages.push(async () => Promise.reject(new Error("boom")));
    await openAndDownload();
    expect(blobs).toEqual([]);
    expect(clicked.map((c) => c.href)).toEqual([expect.stringContaining("/file/download?")]);
  });

  it("keeps the Blob of the pages for an older node without file.chunk (no revision)", async () => {
    pages.push(async () => text({ text: "page one\n", next: 9 }));
    pages.push(async () => text({ text: "page two\n", offset: 9, next: 0 }));
    await openAndDownload();
    expect(clicked.map((c) => c.href)).toEqual(["blob:x"]); // file/download would answer 409 node_outdated there
    expect(blobs).toHaveLength(1);
    expect(await readBlob(blobs[0])).toBe("page one\npage two\n");
  });

  it("still saves a fully loaded text file from what it holds", async () => {
    pages.push(async () => text({ text: "all of it\n", size: 10, next: 0 }));
    await openAndDownload();
    expect(blobs).toHaveLength(1);
    expect(await readBlob(blobs[0])).toBe("all of it\n");
    expect(clicked).toEqual([{ href: "blob:x", name: "big.log" }]);
  });
});
