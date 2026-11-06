// A value that the extension may change between two draws (#2757): its numbers count to the new ones when only they
// changed ("1.2M" → "8.4M", "2/8" → "3/8", "¥1.20" → "¥3.05"), any other change fades the new text in ("在做" → "等你").
// At rest it is the value's own text, nothing around it, so a view that doesn't change looks exactly as before.
import { useMemo } from "react";
import { EASE } from "../../ui/atoms";
import { ENTER_MS, formatParts, numberParts, useFlash, useTween } from "./motion";

export function Rolling({ value }: { value: string }) {
  const parts = useMemo(() => numberParts(value), [value]);
  const now = useTween(parts.nums, parts.shape);
  const flash = useFlash(parts.shape);
  // tabular figures only while counting, so the digits don't jitter (at rest the value is drawn as it always was)
  if (now && now.length === parts.nums.length) return <span data-rolling="" className="tabular-nums">{formatParts(parts, now)}</span>;
  if (flash) return <span key={flash} data-fading="" style={{ animation: `fade-in ${ENTER_MS}ms ${EASE} both` }}>{value}</span>;
  return <>{value}</>;
}

/** The text of a badge (or any short label): a new text fades in. */
export function Fading({ text }: { text: string }) {
  const flash = useFlash(text);
  if (flash) return <span key={flash} data-fading="" style={{ animation: `fade-in ${ENTER_MS}ms ${EASE} both` }}>{text}</span>;
  return <>{text}</>;
}
