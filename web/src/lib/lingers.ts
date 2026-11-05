// Something that stays a moment after it is off, for its way out (#2102): the drawer's scrim fades out, then leaves.
import { useEffect, useState } from "react";

/** How long the drawer's scrim takes to fade out (its duration-200, and a little more). */
export const SCRIM_FADE = 220;

/** `on`, staying true `ms` after it goes off. */
export function useLingers(on: boolean, ms: number): boolean {
  const [shown, setShown] = useState(on);
  useEffect(() => {
    if (on) {
      setShown(true);
      return;
    }
    const t = setTimeout(() => setShown(false), ms);
    return () => clearTimeout(t);
  }, [on, ms]);
  return on || shown;
}
