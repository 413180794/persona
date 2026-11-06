// A single visible-page timer. Returning to the page refreshes immediately; hiding or disposal leaves no timer.
export function visiblePoll(refresh: () => void, period: number, page: { visible: () => boolean; subscribe: (fn: () => void) => () => void }): () => void {
  let timer: ReturnType<typeof setInterval> | undefined;
  const clear = () => { if (timer !== undefined) clearInterval(timer); timer = undefined; };
  const change = () => {
    clear();
    if (!page.visible()) return;
    refresh();
    if (period > 0) timer = setInterval(() => { if (page.visible()) refresh(); }, period);
  };
  const off = page.subscribe(change);
  change();
  return () => { off(); clear(); };
}

// The browser tab as visiblePoll's page: shown while document.visibilityState is "visible".
export const documentPage = {
  visible: () => document.visibilityState === "visible",
  subscribe: (fn: () => void) => {
    document.addEventListener("visibilitychange", fn);
    return () => document.removeEventListener("visibilitychange", fn);
  },
};

// Both successes and errors belong to the latest live request, never to an older response or an unmounted page.
export function latestRequest<T>(apply: (result: T) => void, failed: (error: unknown) => void) {
  let live = true, generation = 0;
  return {
    run: async (fetch: () => Promise<T>) => {
      if (!live) return;
      const n = ++generation;
      try { const value = await fetch(); if (live && n === generation) apply(value); }
      catch (e) { if (live && n === generation) failed(e); }
    },
    dispose: () => { live = false; generation++; },
  };
}
