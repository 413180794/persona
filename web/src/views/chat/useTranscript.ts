// A session's items: the newest page over HTTP, then live over the WebSocket (docs/protocol.md「items 的连续性」).
import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorText, isAbort, sessionPath } from "../../lib/api";
import { sessionLoadError } from "../../lib/session-link";
import { listenSession, send, type ItemsEvent } from "../../lib/store";
import { afterPull, afterPush, FRESH_TALLY, newestItemAt, notLive as tallyNotLive, shouldPull, type StaleTally } from "../../lib/stale-page";
import { mergeItems } from "../../lib/turns";
import type { Item, ItemsPage } from "../../lib/types";
import { createTextThrottle, type TextThrottle } from "../../ui/stream/throttle";

const PAGE = "turns=2&fold_work=1&lazy_images=1";
/** How often a page checks whether it is behind (a pure comparison; nothing is asked unless it is, lib/stale-page.ts). */
const STALE_TICK_MS = 2000;
/** When each session was last asked for what its stream may have lost: kept across remounts, so reopening a page does not ask again at once. */
const lastPulls = new Map<string, number>();

export interface Transcript {
  items: Item[];
  loading: boolean;
  error: string;
  hasMore: boolean;
  loadingEarlier: boolean;
  loadEarlier: () => Promise<void>;
  expandFold: (fold: Item) => Promise<void>;
  streaming: string; // the headless assistant's reply as it is written
  /** The stream keeps losing items (the page got them by asking): say so, with a reload. */
  notLive: boolean;
  reload: () => void;
}

/** One delta: the reply so far is throttled; "" (the turn's end) goes out at once and drops what was pending. */
export function applyDelta(t: TextThrottle, text: string): void {
  if (text) t.push(text);
  else t.flush("");
}

/** `updatedAt`: the session's `updated_at` from the list (a subagent's from its parent's), what the stale-page check compares. */
export function useTranscript(mid: string, sid: string, opts: { starting?: boolean; updatedAt?: string } = {}): Transcript {
  const [items, setItems] = useState<Item[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingEarlier, setLoadingEarlier] = useState(false);
  const [streaming, setStreaming] = useState("");
  const [nonce, setNonce] = useState(0);
  const [notLive, setNotLive] = useState(false);
  const updatedAt = useRef(opts.updatedAt);
  updatedAt.current = opts.updatedAt;
  const covered = useRef(0); // the last transcript line we have without a gap
  const history = useRef("");
  const itemsRef = useRef<Item[]>([]);
  itemsRef.current = items;
  const gapTimer = useRef<number | undefined>(undefined);
  // The assistant's delta (the whole reply so far, about every 80 ms; "" when it ends) re-renders the chat at most once
  // per throttle interval (ui/stream/throttle.ts), the last one never dropped; the end and the reply's item at once.
  const deltas = useRef<TextThrottle>(null!);
  deltas.current ??= createTextThrottle(setStreaming);

  const apply = useCallback((batch: Item[]) => {
    if (!batch.length) return;
    setItems((have) => mergeItems(have, batch));
    if (batch.some((i) => i.role === "assistant" && i.kind === "text")) deltas.current.flush("");
  }, []);

  useEffect(() => {
    let dead = false;
    const ctl = new AbortController();
    setItems([]);
    setLoading(true);
    setError("");
    setHasMore(false);
    deltas.current.flush("");
    covered.current = 0;
    history.current = "";
    setNotLive(false);
    let tally: StaleTally = FRESH_TALLY;
    let lastArrival = Date.now();
    let ready = false; // the first page is in
    let pulling = false;
    let checkedFor: string | undefined;
    const pullKey = `${mid}/${sid}`;

    const subscribe = () => send({ type: "subscribe", machine_id: mid, session_id: sid, since_seq: covered.current || undefined, lazy_images: true, fold_work: true });

    // Items after what we have, a page at a time (past 8 the newest page is cheaper); how many of them were new.
    const pull = async (): Promise<number> => {
      let fresh = 0;
      for (let n = 0; n < 8 && !dead; n++) {
        const p = await api<ItemsPage>(`${sessionPath(mid, sid)}/items?after_seq=${covered.current}&lazy_images=1`, { signal: ctl.signal });
        if (dead) return fresh;
        if (p.history && history.current && p.history !== history.current) {
          setNonce((x) => x + 1); // the file was rewritten: start over
          return fresh;
        }
        const got = p.items ?? [];
        // Not what the stream brought while this was on its way: those are merged as they are, and not a miss.
        fresh += got.filter((i) => i.seq > covered.current).length;
        apply(got);
        if (p.to_seq) covered.current = Math.max(covered.current, p.to_seq);
        lastArrival = Date.now();
        if (!p.more_after) return fresh;
      }
      if (!dead) setNonce((x) => x + 1);
      return fresh;
    };
    const fillGap = async () => void (await pull());

    // The safety net (lib/stale-page.ts): the list says the transcript grew past what we hold and no push came.
    const checkStale = () => {
      if (dead || !ready) return;
      const now = Date.now();
      const asked = updatedAt.current;
      if (!shouldPull({ updatedAt: asked, lastItemAt: newestItemAt(itemsRef.current), lastArrival, lastPull: lastPulls.get(pullKey) ?? 0, checkedFor, now, visible: document.visibilityState === "visible", pulling })) return;
      lastPulls.set(pullKey, now);
      pulling = true;
      pull()
        .then((fresh) => {
          checkedFor = asked;
          tally = afterPull(tally, fresh > 0 ? "found" : "empty");
        })
        .catch((e) => {
          if (!isAbort(e)) tally = afterPull(tally, "failed");
        })
        .finally(() => {
          pulling = false;
          if (!dead) setNotLive(tallyNotLive(tally));
        });
    };
    const staleTimer = window.setInterval(checkStale, STALE_TICK_MS);
    document.addEventListener("visibilitychange", checkStale);

    const onItems = (e: ItemsEvent) => {
      if (e.history && history.current && e.history !== history.current) {
        setNonce((x) => x + 1); // the file was rewritten: start over
        return;
      }
      if (e.history) history.current = e.history;
      lastArrival = Date.now();
      if (e.items?.length && tally !== FRESH_TALLY) {
        tally = afterPush(tally); // the stream works
        setNotLive(false);
      }
      if (e.from_seq !== undefined && covered.current && e.from_seq > covered.current) {
        apply(e.items ?? []);
        clearTimeout(gapTimer.current);
        gapTimer.current = window.setTimeout(() => void fillGap().catch(() => {}), 1500);
        return;
      }
      apply(e.items ?? []);
      if (e.to_seq) covered.current = Math.max(covered.current, e.to_seq);
    };

    const off = listenSession(mid, sid, {
      items: onItems,
      delta: (text) => applyDelta(deltas.current, text),
      reconnected: subscribe,
    });

    (async () => {
      try {
        const p = await api<ItemsPage>(`${sessionPath(mid, sid)}/items?${PAGE}`, { signal: ctl.signal });
        if (dead) return;
        apply(p.items ?? []);
        setHasMore(Boolean(p.has_more));
        if (p.history) history.current = p.history;
        covered.current = p.to_seq ?? Math.max(0, ...(p.items ?? []).map((i) => i.seq));
      } catch (e) {
        if (dead) return;
        // a session that is still starting has no transcript yet
        if (!opts.starting) setError(sessionLoadError(errorText(e)));
      } finally {
        if (!dead) setLoading(false);
        lastArrival = Date.now();
        ready = true;
      }
      if (!dead) subscribe();
    })();

    return () => {
      dead = true;
      ctl.abort();
      off();
      deltas.current.cancel();
      clearTimeout(gapTimer.current);
      clearInterval(staleTimer);
      document.removeEventListener("visibilitychange", checkStale);
      send({ type: "unsubscribe", machine_id: mid, session_id: sid });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, sid, nonce]);

  const loadEarlier = useCallback(async () => {
    const first = itemsRef.current[0];
    if (!first || loadingEarlier || !hasMore) return;
    setLoadingEarlier(true);
    try {
      const p = await api<ItemsPage>(`${sessionPath(mid, sid)}/items?${PAGE}&before_seq=${first.seq}`);
      apply(p.items ?? []);
      setHasMore(Boolean(p.has_more));
    } catch (e) {
      setError(sessionLoadError(errorText(e)));
    } finally {
      setLoadingEarlier(false);
    }
  }, [mid, sid, hasMore, loadingEarlier, apply]);

  const expandFold = useCallback(
    async (fold: Item) => {
      if (!fold.fold) return;
      let from = fold.seq;
      const out: Item[] = [];
      for (let n = 0; n < 20; n++) {
        const p = await api<ItemsPage>(`${sessionPath(mid, sid)}/items?from_seq=${from}&through_seq=${fold.fold.through_seq}&lazy_images=1`);
        out.push(...(p.items ?? []));
        if (!p.more || !p.items?.length) break;
        from = p.items[p.items.length - 1].seq + 1;
      }
      apply(out);
    },
    [mid, sid, apply],
  );

  return { items, loading, error, hasMore, loadingEarlier, loadEarlier, expandFold, streaming, notLive, reload: () => setNonce((x) => x + 1) };
}
