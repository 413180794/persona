// Carried over from #1974 (by @tzh476): lazy pages preload when the browser is idle, and a failed load is asked again.
import { afterEach, describe, expect, it, vi } from "vitest";
import { Suspense } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { lazyPage, preloadPages } from "./lazy-page";

describe("lazyPage", () => {
  afterEach(() => vi.useRealTimers());

  it("preloads once the browser is idle, and asks again for a chunk that failed", async () => {
    vi.useFakeTimers();
    const Page = () => <p>page</p>;
    const load = vi.fn<() => Promise<typeof Page>>().mockRejectedValueOnce(new TypeError("Failed to fetch dynamically imported module")).mockResolvedValue(Page);
    const Lazy = lazyPage(load);
    preloadPages({}); // no requestIdleCallback (Safari): a timer
    expect(load).not.toHaveBeenCalled();
    await vi.runAllTimersAsync();
    expect(load).toHaveBeenCalledTimes(1);
    preloadPages({});
    await vi.runAllTimersAsync();
    expect(load).toHaveBeenCalledTimes(2); // the failed one again
    preloadPages({});
    await vi.runAllTimersAsync();
    expect(load).toHaveBeenCalledTimes(2); // the loaded one not
    // loaded: drawn at once, not through the fallback
    expect(renderToStaticMarkup(<Suspense fallback={<p>loading</p>}><Lazy /></Suspense>)).toBe("<p>page</p>");
  });

  it("waits for idle where there is requestIdleCallback, can be cancelled, and skips Data Saver", () => {
    const idle = vi.fn((run: () => void) => (void run, 7));
    const cancel = vi.fn();
    preloadPages({ requestIdleCallback: idle, cancelIdleCallback: cancel })();
    expect(idle).toHaveBeenCalledWith(expect.any(Function), { timeout: 5000 });
    expect(cancel).toHaveBeenCalledWith(7);
    idle.mockClear();
    preloadPages({ requestIdleCallback: idle, cancelIdleCallback: cancel, navigator: { connection: { saveData: true } } });
    expect(idle).not.toHaveBeenCalled();
  });

  it("shows the fallback while the chunk is on its way", () => {
    const Lazy = lazyPage(() => new Promise<() => null>(() => {}));
    expect(renderToStaticMarkup(<Suspense fallback={<p>loading</p>}><Lazy /></Suspense>)).toBe("<p>loading</p>");
  });
});
