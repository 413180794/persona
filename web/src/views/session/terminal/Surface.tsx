// One of the terminal panel's two surfaces while the panel switches between them: the shown one slides in (a few pixels
// from the side the switch came from) and fades up, the one leaving slides out the other way and fades down; both are
// plain transitions with the page's easing, so the system's reduced-motion rule (styles.css) cuts them to nothing. At
// rest a surface has no transform at all (a transformed box would hold fixed-position menus inside it).
import { useEffect, useState } from "react";
import { SURFACE_MS } from "../../../lib/terminal-mode";

const SLIDE_PX = 14;

export function Surface({
  leaving,
  enter,
  dir,
  children,
}: {
  /** On its way out: not clickable, not focusable, then gone. */
  leaving: boolean;
  /** Comes in with a transition (it replaced another surface); false: it was there from the start. */
  enter: boolean;
  /** 1 when the shown surface came from the right, -1 from the left. */
  dir: 1 | -1;
  children: React.ReactNode;
}) {
  const [entered, setEntered] = useState(!enter);
  useEffect(() => {
    if (entered) return;
    // two frames: the first paint has the surface out, so the transition has somewhere to go from
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => setEntered(true));
    });
    return () => cancelAnimationFrame(frame);
  }, [entered]);

  const at = !leaving && entered;
  return (
    <div
      inert={leaving || undefined}
      aria-hidden={leaving || undefined}
      data-surface={leaving ? "leaving" : entered ? "shown" : "entering"}
      className="absolute inset-0 flex min-h-0 flex-col"
      style={{
        opacity: at ? 1 : 0,
        transform: at ? "none" : `translateX(${(leaving ? -dir : dir) * SLIDE_PX}px)`,
        transition: `opacity ${SURFACE_MS}ms var(--ease-out-strong), transform ${SURFACE_MS}ms var(--ease-out-strong)`,
        pointerEvents: leaving ? "none" : undefined,
      }}
    >
      {children}
    </div>
  );
}
