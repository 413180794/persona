// Opening a node's menu from a row: a right click opens it at the pointer, the menu key and Shift+F10 on the row itself
// (a browser that sends no contextmenu event for them is covered by the key handler). Opening twice is harmless.
import { isContextKey, menuAtFromContext, type MenuAt } from "./menu-model";

/** The two handlers a row (or its header) gets. `open` is given where the menu goes and the element it is for. */
export function contextMenuProps(open: (at: MenuAt, el: HTMLElement) => void) {
  return {
    onContextMenu: (e: React.MouseEvent<HTMLElement>) => {
      e.preventDefault();
      open(menuAtFromContext(e), e.currentTarget);
    },
    onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
      if (!isContextKey(e)) return;
      e.preventDefault();
      open({ anchor: e.currentTarget }, e.currentTarget);
    },
  };
}
