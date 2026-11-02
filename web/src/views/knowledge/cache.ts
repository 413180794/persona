// What the knowledge page keeps between visits: entries it has seen (a detail shows its row at once while it
// loads), the lists by kind and tag, and the search results by query (coming back to a search shows it again).
import type { KBEntry, KBList, KBResult } from "../../lib/kb";

const entries = new Map<string, KBEntry>();

export function remember(list: (KBEntry | undefined | null)[] | null | undefined) {
  for (const e of list ?? []) if (e) entries.set(`${e.kind}/${e.id}`, e);
}

export function known(kind: string, id: string): KBEntry | undefined {
  return entries.get(`${kind}/${id}`);
}

export const lists = new Map<string, KBList>();
export const listKey = (kind: string, tag?: string) => `${kind}|${tag ?? ""}`;

export const searches = new Map<string, KBResult[]>();
