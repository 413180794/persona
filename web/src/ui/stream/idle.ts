// Work a reply can put off (the swap of a faded-in block back to plain Markdown, loading the highlighter): run when the
// page is idle, so it doesn't land in the same frame as a keystroke or the first draw of the chat. At most `timeout` ms
// late; a short timer where there is no requestIdleCallback (Safari). Returns the cancel.
interface IdleWindow {
  requestIdleCallback?: (run: () => void, opts?: { timeout: number }) => number;
  cancelIdleCallback?: (id: number) => void;
}

export function whenIdle(run: () => void, timeout = 1000, w: IdleWindow = globalThis as IdleWindow): () => void {
  if (w.requestIdleCallback && w.cancelIdleCallback) {
    const id = w.requestIdleCallback(run, { timeout });
    return () => w.cancelIdleCallback!(id);
  }
  const t = setTimeout(run, 50);
  return () => clearTimeout(t);
}
