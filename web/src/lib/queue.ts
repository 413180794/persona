// Messages typed while the agent works and not yet taken (Session.queued): "Pull back to edit" pulls them all back out of
// Claude and re-queues the others, in order (POST …/queue {keep}, node editQueue; iOS QueueStrip / editQueued).

/** The order to re-queue in when message `index` moves `by` places (−1 up, +1 down; #6088): every message, that one moved.
 *  Null when it cannot move that way. */
export function moved(count: number, index: number, by: number): number[] | null {
  const to = index + by;
  if (index < 0 || index >= count || to < 0 || to >= count || by === 0) return null;
  const order = Array.from({ length: count }, (_, i) => i);
  order.splice(index, 1);
  order.splice(to, 0, index);
  return order;
}

/** The indexes to re-queue when message `index` is taken out: every other one, in their order. */
export function keepAllBut(count: number, index: number): number[] {
  return Array.from({ length: count }, (_, i) => i).filter((i) => i !== index);
}

function normalize(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}

/** Two texts are the same message once normalized, or one contains the other when both are long enough
 *  (Claude adds placeholders, the phone adds notes; a short "ok" must not match anything with "ok" in it). */
export function sameMessage(a: string, b: string): boolean {
  const x = normalize(a),
    y = normalize(b);
  if (!x || !y) return false;
  return x === y || (Math.min([...x].length, [...y].length) >= 8 && (x.includes(y) || y.includes(x)));
}
