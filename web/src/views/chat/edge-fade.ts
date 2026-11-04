// A row that scrolls sideways fades at the edge that has more (the turn's picture strip, a diff's long lines on a phone).
import { useCallback, useEffect, useLayoutEffect, useState, type RefObject } from "react";

export interface Edges {
  left: boolean;
  right: boolean;
}

/** The mask for `edges`: a fade of 28 px on the left, 44 on the right, where there is more to scroll to. */
export function edgeMask(edges: Edges): string | undefined {
  if (edges.left && edges.right) return "linear-gradient(90deg,transparent,#000 28px,#000 calc(100% - 44px),transparent)";
  if (edges.right) return "linear-gradient(90deg,#000 calc(100% - 44px),transparent)";
  if (edges.left) return "linear-gradient(90deg,transparent,#000 28px)";
  return undefined;
}

/** Which edges of `ref`'s element have more to scroll to; `measure` goes on its onScroll, and runs again when it resizes
 *  and when any of `deps` changes (its content). */
export function useEdgeFade(ref: RefObject<HTMLElement | null>, deps: unknown[]): { edges: Edges; measure: () => void } {
  const [edges, setEdges] = useState<Edges>({ left: false, right: false });
  const measure = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const left = el.scrollLeft > 2;
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 2;
    setEdges((e) => (e.left === left && e.right === right ? e : { left, right }));
  }, [ref]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useLayoutEffect(measure, [measure, ...deps]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref, measure]);
  return { edges, measure };
}
