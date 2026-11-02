// Messages typed while the agent works and not yet taken (Session.queued): 撤回编辑 pulls them all back out of
// Claude and re-queues the others, in order (POST …/queue {keep}, node editQueue; iOS QueueStrip / editQueued).

/** The indexes to re-queue when message `index` is taken out: every other one, in their order. */
export function keepAllBut(count: number, index: number): number[] {
  return Array.from({ length: count }, (_, i) => i).filter((i) => i !== index);
}

function normalize(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}

/** Two texts are the same message once normalized, or one contains the other when both are long enough
 *  (Claude adds placeholders, the phone adds notes; a short 好 must not match anything with 好 in it). */
export function sameMessage(a: string, b: string): boolean {
  const x = normalize(a),
    y = normalize(b);
  if (!x || !y) return false;
  return x === y || (Math.min([...x].length, [...y].length) >= 8 && (x.includes(y) || y.includes(x)));
}
