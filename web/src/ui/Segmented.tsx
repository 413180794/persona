import { useLayoutEffect, useRef, useState } from "react";
import { cx } from "./atoms";

/** Where the chosen segment sits inside the track, in the track's own pixels. */
export type ThumbBox = { x: number; y: number; w: number; h: number };

/** The box of the segment marked `data-selected="true"` among the track's direct children, or null when none is. */
export function selectedBox(track: HTMLElement): ThumbBox | null {
  const el = track.querySelector<HTMLElement>(':scope > [data-selected="true"]');
  if (!el || !el.offsetWidth) return null;
  return { x: el.offsetLeft, y: el.offsetTop, w: el.offsetWidth, h: el.offsetHeight };
}

/** The track every segmented control draws (#3559, after Moshi): a ~7% ink bed with one thumb that slides under the
 *  chosen segment (a transform, 200 ms on the design system's ease; none under reduced motion). The segments are the
 *  children, each a button marked `data-selected` and `relative z-[1]`; roles and keyboard stay the callers'. The thumb
 *  starts where it belongs, it does not slide in on the first paint. */
export function SegmentedTrack({ role, label, className, thumbClassName, children, ...rest }: { role: "radiogroup" | "tablist"; label: string; className?: string; thumbClassName: string; children: React.ReactNode } & Omit<React.HTMLAttributes<HTMLDivElement>, "role" | "className">) {
  const ref = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<ThumbBox | null>(null);
  const [ready, setReady] = useState(false);

  // Measure after every render (the chosen segment, the labels, a wrapped row may all have changed) and whenever the
  // track or a segment is resized (fonts arriving, a hidden panel being shown).
  useLayoutEffect(() => {
    const track = ref.current;
    if (!track) return;
    const measure = () => {
      const next = selectedBox(track);
      setBox((prev) => (prev && next && prev.x === next.x && prev.y === next.y && prev.w === next.w && prev.h === next.h ? prev : next));
    };
    measure();
  });
  // One ResizeObserver per mounted track. Segments that come and go are followed through a MutationObserver on the
  // track's child list, so a parent re-render (new `children` elements, same segments) no longer disconnects and
  // re-observes everything: each observe() delivers an initial callback that forces layout, and the layout effect
  // above already measures after every render.
  useLayoutEffect(() => {
    const track = ref.current;
    if (!track || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      const next = selectedBox(track);
      setBox((prev) => (prev && next && prev.x === next.x && prev.y === next.y && prev.w === next.w && prev.h === next.h ? prev : next));
    });
    ro.observe(track);
    for (const c of Array.from(track.children)) ro.observe(c);
    const mo = typeof MutationObserver === "undefined" ? null : new MutationObserver((records) => {
      for (const r of records) {
        for (const n of Array.from(r.removedNodes)) if (n instanceof Element) ro.unobserve(n);
        for (const n of Array.from(r.addedNodes)) if (n instanceof Element && n.parentNode === track) ro.observe(n);
      }
    });
    mo?.observe(track, { childList: true });
    return () => {
      mo?.disconnect();
      ro.disconnect();
    };
  }, []);
  useLayoutEffect(() => {
    if (!box || ready) return;
    const id = requestAnimationFrame(() => setReady(true));
    return () => cancelAnimationFrame(id);
  }, [box, ready]);

  return (
    <div ref={ref} role={role} aria-label={label} className={cx("relative bg-seg-track", className)} {...rest}>
      {box && <span aria-hidden data-testid="seg-thumb" data-ready={ready} className={cx("seg-thumb", thumbClassName)} style={{ width: box.w, height: box.h, transform: `translate(${box.x}px, ${box.y}px)` }} />}
      {children}
    </div>
  );
}
