// Elements registered under keys by ref callbacks (the sidebar's gliding highlight, Sidebar.tsx useGlide).

/** A ref callback maker over `rows`: `reg(k)` is the ref of the element registered under `k`. Letting go (React calls
 *  the old ref with null) takes the key out only while it is still that element's: when another row has registered
 *  under the same key since (a folded tab's row taking its open pane's key, #2320), its entry stays. */
export function keyedRefs<T>(rows: Map<string, T>): (k: string) => (el: T | null) => void {
  return (k) => {
    let mine: T | null = null;
    return (el) => {
      if (el) {
        mine = el;
        rows.set(k, el);
      } else {
        if (mine !== null && rows.get(k) === mine) rows.delete(k);
        mine = null;
      }
    };
  };
}
