// The default face's loops (MochiFace): the blink, the working arc, the needs-you rock and the pulses.
// They run as Web Animations, not CSS animations: a looping CSS animation sends an animationiteration event every cycle,
// React listens for those at the root, and each one cost the page a style recalc plus (an SVG's) a layout. Five faces
// in the persona picker's status strip kept /settings busy while nothing happened, off-screen too. A Web Animation
// sends no such events. The keyframes and timing are the CSS ones they replace, eased per keyframe as CSS does; the
// pivots stay in styles.css (.mochi-blink, .mochi-spin) or on the element. Reduced motion: none of them runs.
import { useEffect, useLayoutEffect, useRef, useState } from "react";

const REDUCE = "(prefers-reduced-motion: reduce)";

type Loop = { keyframes: Keyframe[]; duration: number };

/** Each loop by the class that marks its element. A `data-loop-delay` (ms) on the element starts it later. */
export const FACE_LOOPS: Record<string, Loop> = {
  "mochi-blink": {
    keyframes: [
      { offset: 0, transform: "scaleY(1)", easing: "ease" },
      { offset: 0.94, transform: "scaleY(1)", easing: "ease" },
      { offset: 0.97, transform: "scaleY(.12)", easing: "ease" },
      { offset: 1, transform: "scaleY(1)" },
    ],
    duration: 3600,
  },
  "mochi-spin": { keyframes: [{ transform: "rotate(0turn)" }, { transform: "rotate(1turn)" }], duration: 1600 },
  "mochi-rock": {
    keyframes: [
      { transform: "rotate(-5deg)", easing: "ease-in-out" },
      { transform: "rotate(5deg)", easing: "ease-in-out" },
      { transform: "rotate(-5deg)" },
    ],
    duration: 900,
  },
  "mochi-pulse": { keyframes: [{ opacity: 1, easing: "ease-in-out" }, { opacity: 0.35, easing: "ease-in-out" }, { opacity: 1 }], duration: 1200 },
};

/** Whether the system asks for reduced motion, following changes. */
export function useReducedMotion(): boolean {
  const [reduce, setReduce] = useState(() => typeof matchMedia === "function" && matchMedia(REDUCE).matches);
  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const q = matchMedia(REDUCE);
    const on = () => setReduce(q.matches);
    q.addEventListener("change", on);
    return () => q.removeEventListener("change", on);
  }, []);
  return reduce;
}

/** A ref for a face's <svg>: runs the loops of the elements in it that carry a FACE_LOOPS class. `drawn` names what
 *  decides which elements carry one (the state, the compact size); the loops restart when it changes, as a CSS
 *  animation restarts when its class comes back. Started before paint, so the first frame is the loop's first. */
export function useFaceMotion<T extends Element>(drawn: string) {
  const ref = useRef<T>(null);
  const reduce = useReducedMotion();
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root || reduce) return;
    const running: Animation[] = [];
    for (const [name, loop] of Object.entries(FACE_LOOPS)) {
      root.querySelectorAll<SVGElement>(`.${name}`).forEach((el) => {
        if (typeof el.animate !== "function") return; // jsdom
        running.push(el.animate(loop.keyframes, { duration: loop.duration, delay: Number(el.dataset.loopDelay ?? 0), iterations: Infinity }));
      });
    }
    return () => running.forEach((a) => a.cancel());
  }, [drawn, reduce]);
  return ref;
}
