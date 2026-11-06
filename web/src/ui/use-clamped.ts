// Whether a line-clamped text is cut, for a 展开全文 under it (#4715, #4742). Measured after every render (the text may
// change), whenever the element's box changes (a side panel opens, the window narrows, a hidden container shows), and
// once the web fonts are in, as LarkTopicCard's Fold does. While `open` the last answer stays, so 收起 stays under the text.
import { useCallback, useLayoutEffect, useState } from "react";

export function useClamped<T extends HTMLElement>(open: boolean): { ref: (el: T | null) => void; cut: boolean } {
  const [el, setEl] = useState<T | null>(null);
  const [cut, setCut] = useState(false);
  const measure = useCallback(() => {
    if (el && !open) setCut(el.scrollHeight > el.clientHeight + 1);
  }, [el, open]);
  useLayoutEffect(measure);
  useLayoutEffect(() => {
    if (!el) return;
    let live = true;
    void document.fonts?.ready.then(() => live && measure());
    const ro = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(() => measure());
    ro?.observe(el);
    return () => {
      live = false;
      ro?.disconnect();
    };
  }, [el, measure]);
  return { ref: setEl, cut };
}
