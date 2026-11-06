// Each online machine's terminal panes (`GET /machines/{mid}/panes`): read when the consumer mounts, every few seconds
// while the page is in front, and on demand. The "Terminal" page and the sidebar's terminal tree (when turned on) read them.
import { useCallback, useEffect, useState } from "react";
import { api, enc, errorText } from "./api";
import type { PanesResponse } from "./terminals";
import type { Machine } from "./types";

/** How often the layout is read again while the page is in front (agents' states are live already, from the sessions). */
export const REFRESH_MS = 8000;

export interface Listing {
  data?: PanesResponse;
  error?: string;
}

/** `enabled` false reads nothing (the sidebar's tree, off). */
export function usePanes(machines: Machine[], enabled = true) {
  const [listing, setListing] = useState<Record<string, Listing>>({});
  const [busy, setBusy] = useState(false);
  const online = enabled ? machines.filter((m) => m.online).map((m) => m.id) : [];
  const key = online.join("\n");

  const load = useCallback(async (ids: string[]) => {
    await Promise.all(
      ids.map((mid) =>
        api<PanesResponse>(`/machines/${enc(mid)}/panes`).then(
          (data) => setListing((l) => ({ ...l, [mid]: { data } })),
          // a failed read keeps what was seen: the list does not blink out
          (e) => setListing((l) => ({ ...l, [mid]: { data: l[mid]?.data, error: errorText(e) } })),
        ),
      ),
    );
  }, []);

  useEffect(() => {
    const ids = key ? key.split("\n") : [];
    if (!ids.length) return;
    let dead = false;
    setBusy(true);
    void load(ids).finally(() => !dead && setBusy(false));
    const t = setInterval(() => document.visibilityState === "visible" && void load(ids), REFRESH_MS);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [key, load]);

  const refresh = useCallback(async () => {
    setBusy(true);
    await load(key ? key.split("\n") : []);
    setBusy(false);
  }, [key, load]);
  return { listing, busy, refresh, reload: load };
}
