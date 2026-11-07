// One of the two views of an agent's page (chat, terminal), stacked in the same box (docs/web/web.md "Terminal ⇄ chat"). The one
// not shown stays mounted, at the same size, only unseen and out of reach: the terminal keeps its stream (coming back is
// instant) and is not resized (display:none would make xterm measure 100px and resize the pane, as in TerminalPage);
// the chat keeps its scroll position and what was typed. Switching crossfades with a short slide.
import { cx, EASE } from "../../ui/atoms";

const MS = 200;

export function ViewLayer({
  on,
  from,
  enter,
  bare,
  layerRef,
  children,
}: {
  on: boolean;
  /** The side it leaves to and comes from. */
  from: "start" | "end";
  /** Fade in when it first appears (the terminal's layer is added on the first switch to it). */
  enter?: boolean;
  /** No switching here: the children as they are, in no box. */
  bare?: boolean;
  layerRef?: React.Ref<HTMLDivElement>;
  children: React.ReactNode;
}) {
  if (bare) return <>{children}</>;
  return (
    <div
      ref={layerRef}
      inert={!on || undefined}
      aria-hidden={!on || undefined}
      // visibility flips at once when the layer comes in (so the keyboard can go to it) and after the fade when it leaves
      style={{ transition: `opacity ${MS}ms ${EASE}, translate ${MS}ms ${EASE}, visibility 0s linear ${on ? 0 : MS}ms` }}
      className={cx(
        "absolute inset-0 flex min-h-0 flex-col motion-reduce:translate-none",
        on ? cx("translate-none opacity-100", enter && "starting:translate-x-3 starting:opacity-0") : cx("pointer-events-none invisible opacity-0", from === "start" ? "-translate-x-3" : "translate-x-3"),
      )}
    >
      {children}
    </div>
  );
}
