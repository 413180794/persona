// A fold kept per browser (`fold:<key>` is "1" or "0"): the sidebar's sections and the 终端 page's tree share it.
import { useEffect, useState } from "react";
import { foldedNow } from "./fold";
import { onFoldAll } from "./sidebar-api";

// follow: until the user folds or opens it, the section takes its default as it is now, not as it was on the first
// render (未归属 is folded only once the todos above it have loaded, #2249); other sections keep their first default.
// holds: the section has the open conversation's row (#2283); it opens when it comes to hold it (opening the
// conversation from anywhere shows its row), without storing that, and can still be folded by hand after.
export function useFolded(key: string, initial = false, { follow = false, holds = false } = {}): [boolean, () => void] {
  const [stored, setStored] = useState(() => localStorage.getItem(`fold:${key}`));
  const [first] = useState(initial);
  const [opened, setOpened] = useState(holds);
  const [held, setHeld] = useState(holds);
  if (held !== holds) {
    // adjusted while rendering, so the row is laid out (and scrolled to) in the same frame
    setHeld(holds);
    if (holds) setOpened(true);
  }
  const folded = !opened && foldedNow(stored, follow ? initial : first);
  // 折叠 / 展开全部 (the keyboard shortcut, lib/sidebar-api.ts)
  useEffect(() => onFoldAll((all) => {
    const next = all ? "1" : "0";
    localStorage.setItem(`fold:${key}`, next);
    setStored(next);
    setOpened(false);
  }), [key]);
  return [
    folded,
    () => {
      const next = folded ? "0" : "1";
      localStorage.setItem(`fold:${key}`, next);
      setStored(next);
      setOpened(false);
    },
  ];
}
