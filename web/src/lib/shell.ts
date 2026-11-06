// The shell's own decisions (App.tsx), pure so they are tested without a DOM.
import type { Conn } from "./store";

/** Whether the page shows "Offline" instead of loading (#1996): the hub has not been reached since the page opened and the
 *  socket has given up for now. It stays through a retry (`connecting`) so the page doesn't flicker back to loading,
 *  and goes once anything has loaded. A drop after loading keeps the page and shows the small banner instead. */
export function unreachable(was: boolean, conn: Conn, loaded: boolean): boolean {
  if (loaded) return false;
  return conn === "offline" || (was && conn === "connecting");
}
