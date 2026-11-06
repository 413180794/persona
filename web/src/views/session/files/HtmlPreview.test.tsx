// @vitest-environment jsdom
// An HTML preview's reads of the files beside the page: one the machine never answers gives up after READ_TIMEOUT_MS
// and counts as missing, so the page still shows; closing the file or pressing reload aborts the reads still pending.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../../i18n";
import { READ_TIMEOUT_MS } from "../../../lib/api";
import { HtmlPreview } from "./HtmlPreview";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const PAGE = '<html><head><link rel="stylesheet" href="style.css"></head><body>hi</body></html>';
const flush = async () => {
  for (let i = 0; i < 10; i++) await act(async () => { await Promise.resolve(); });
};
const frame = () => document.querySelector("iframe");

let host: HTMLDivElement;
let root: Root;
let signals: AbortSignal[];

/** A fetch that never answers until its signal aborts. */
const stalled = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
  const signal = init?.signal ?? undefined;
  if (signal) signals.push(signal);
  return new Promise<Response>((_resolve, reject) => {
    signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
  });
});

function render(props: Partial<{ html: string; path: string }> = {}) {
  act(() => root.render(<HtmlPreview html={props.html ?? PAGE} title="page" mid="m1" path={props.path ?? "/w/index.html"} />));
}

beforeEach(() => {
  signals = [];
  stalled.mockClear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("HtmlPreview sibling files", () => {
  it("shows the page with the file missing once a stalled read times out", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", stalled);
    render();
    await flush();
    expect(stalled).toHaveBeenCalledTimes(1);
    expect(frame()).toBeNull();
    expect(host.textContent).toContain(t("session-files-htmlpreview.loading_local", { count: 1 }));

    await act(async () => { vi.advanceTimersByTime(READ_TIMEOUT_MS - 1); });
    await flush();
    expect(frame(), "still waiting just before the time").toBeNull();

    await act(async () => { vi.advanceTimersByTime(1); });
    await flush();
    expect(signals[0].aborted).toBe(true);
    expect(frame(), "page shown after the timeout").not.toBeNull();
    expect(host.textContent).toContain(t("session-files-htmlpreview.missing", { count: 1 }));
  });

  it("aborts the pending read when the preview closes", async () => {
    vi.stubGlobal("fetch", stalled);
    render();
    await flush();
    expect(signals).toHaveLength(1);
    expect(signals[0].aborted).toBe(false);
    act(() => root.unmount());
    expect(signals[0].aborted).toBe(true);
    root = createRoot(host);
  });

  it("starts no new read after the preview closes", async () => {
    // style.css answers with two imports, read one after the other: a.css stalls until the close aborts it, and
    // the inliner then moves on to b.css, which must not be fetched on an already-closed preview
    const css = '@import "a.css"; @import "b.css";';
    const sheetThenStall = vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      String(input).includes(encodeURIComponent("/w/style.css"))
        ? Promise.resolve({ ok: true, blob: async () => ({ size: css.length, text: async () => css }) } as unknown as Response)
        : stalled(input, init),
    );
    vi.stubGlobal("fetch", sheetThenStall);
    render();
    await flush();
    expect(sheetThenStall).toHaveBeenCalledTimes(2);
    expect(String(sheetThenStall.mock.calls[1]?.[0])).toContain(encodeURIComponent("/w/a.css"));
    act(() => root.unmount());
    await flush();
    expect(signals[0].aborted).toBe(true);
    expect(sheetThenStall, "no read of b.css after the close").toHaveBeenCalledTimes(2);
    root = createRoot(host);
  });

  it("aborts the pending read on reload and reads again", async () => {
    vi.stubGlobal("fetch", stalled);
    render();
    await flush();
    const reload = host.querySelector(`button[aria-label="${t("session-files-htmlpreview.reload")}"]`) as HTMLButtonElement;
    expect(reload, "reload button").toBeTruthy();
    await act(async () => reload.click());
    await flush();
    expect(signals).toHaveLength(2);
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });

  it("still inlines a sibling the machine answers", async () => {
    // jsdom's Blob has no text(), so the answer is a Response-like stub with a Blob-like body
    const css = "body { color: red }";
    const ok = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({ ok: true, blob: async () => ({ size: css.length, text: async () => css }) }) as unknown as Response);
    vi.stubGlobal("fetch", ok);
    render();
    await flush();
    expect(ok).toHaveBeenCalledTimes(1);
    expect(String(ok.mock.calls[0]?.[0])).toContain(encodeURIComponent("/w/style.css"));
    const f = frame();
    expect(f).not.toBeNull();
    expect(f!.getAttribute("srcdoc")).toContain("color: red");
    expect(host.textContent).toContain(t("session-files-htmlpreview.loaded", { count: 1 }));
    expect(host.textContent).not.toContain(t("session-files-htmlpreview.missing", { count: 1 }));
  });
});
