// A value that may change between two draws (#2757, #3434): its numbers count to the new ones when only they changed
// ("1.2M" → "8.4M", "2/8" → "3/8", "169/185+" → "170/186+"), any other change fades the new text in ("Working" → "Needs you").
// At rest it is the value's own text, nothing around it, so a view that doesn't change looks exactly as before.
// Extension views (views/extensions) and the workflow cards (views/chat/WorkflowCard.tsx) both draw with it.
import { useMemo } from "react";
import { fadeInStyle, formatParts, numberParts, useFlash, useTween } from "./motion";

export function Rolling({ value }: { value: string }) {
  const parts = useMemo(() => numberParts(value), [value]);
  const now = useTween(parts.nums, parts.shape);
  const flash = useFlash(parts.shape);
  // tabular figures only while counting, so the digits don't jitter (at rest the value is drawn as it always was)
  if (now && now.length === parts.nums.length) return <span data-rolling="" className="tabular-nums">{formatParts(parts, now)}</span>;
  if (flash) return <span key={flash} data-fading="" style={fadeInStyle()}>{value}</span>;
  return <>{value}</>;
}

/** The text of a badge (or any short label): a new text fades in. */
export function Fading({ text }: { text: string }) {
  const flash = useFlash(text);
  if (flash) return <span key={flash} data-fading="" style={fadeInStyle()}>{text}</span>;
  return <>{text}</>;
}
