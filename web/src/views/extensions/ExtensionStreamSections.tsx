// Shared native renderer; only an explicitly supplied, authenticated app adapter activates it.
import { useEffect, useLayoutEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from "react";
import type { Action, UIStreamEvent, UIStreamSnapshot, UIStreamValue } from "../../lib/extension-wire";
import { ExtensionUIStreamSession, nativeStreamGap, nativeStreamSlots, same, streamPageKey, type StreamPageIdentity } from "../../lib/extension-ui-stream";
import { fieldValues, type UIAction, type UIActionResult } from "../../lib/extension-ui";
import { INERT, ViewSections, type ViewHandlers } from "./ViewBody";
import { scopeLocal, useNativeState, type NativeLocal } from "./native-state";
import { nativeRenderGap, streamItemRenderer, type NativeMedia, type NativeNavigation, type DisplayedNativeAction } from "./NativeStreamItems";
import { Box } from "./CodeViews";
import { KeysHelp, ReviewKeys, ReviewProvider, ReviewToolbar, reviewing } from "./DiffReview";
import { Button } from "../../ui/atoms";
import { NATIVE, NativeTabs } from "../../ui/ExtensionNative";
import { LineCache } from "../../lib/code-review";
import { NativeMediaContext } from "./native-media";
import { t } from "../../i18n";
const reviewCaches = new WeakMap<ExtensionUIStreamSession, { epoch: string; lines: Map<string, LineCache> }>();

export interface ExtensionStreamSectionsProps {
  identity: StreamPageIdentity;
  availableAdapter?: ExtensionUIStreamSession;
  /** Existing HTTP content: mounted exactly as before when no adapter is explicitly supplied. */
  fallback: ReactNode;
  loading?: ReactNode;
  active?: boolean;
  embedded?: boolean;
  compact?: boolean;
  confirm?: (action: Action, displayed: Readonly<DisplayedNativeAction>, value: UIStreamValue) => Promise<boolean>;
  onError?: (error: unknown) => void;
  navigate?: NativeNavigation;
  media?: NativeMedia;
  /** Manager-owned event correlation, not a new socket envelope. Returns an already validated native projection. */
  readResult?: (event: Readonly<UIStreamEvent>) => Promise<Pick<UIActionResult, "lines"> | null>;
  onGap?: (gap: string) => void;
}
export function ExtensionStreamSections(props: ExtensionStreamSectionsProps) {
  if (!props.availableAdapter) return props.fallback;
  if (props.availableAdapter.pageKey !== streamPageKey(props.identity)) throw new Error("Native stream page identity mismatch");
  return <AvailableSections key={props.availableAdapter.pageKey} {...props} session={props.availableAdapter} />;
}
function localIdentity(session: ExtensionUIStreamSession, key: string): string | undefined {
  const s = session.getState().snapshot;
  if (!s) return;
  if (s.root_key === key) return JSON.stringify([s.epoch, "root"]);
  for (const section of s.view.sections) {
    if (section.key === key) return JSON.stringify([s.epoch, "section"]);
    for (const item of section.items) {
      if (item.key === key) return JSON.stringify([s.epoch, item.type, "name" in item ? item.name : null, item.type === "field" ? item.kind : null, item.type === "diff" ? [item.path, item.revision ?? null] : null]);
      if ((item.type === "grid" || item.type === "carousel") && item.tiles.some((tile) => tile.key === key)) return JSON.stringify([s.epoch, "tile"]);
    }
  }
}
function localFor(session: ExtensionUIStreamSession, key: string): NativeLocal {
  const identity = localIdentity(session, key);
  const available = () => identity !== undefined && identity === localIdentity(session, key);
  return {
    identity,
    get: (name) => available() ? session.getLocal<Record<string, unknown>>(key)?.[name] as never : undefined,
    set: (name, value) => { if (available()) session.setLocal(key, { ...session.getLocal<Record<string, unknown>>(key), [name]: value }); },
    clear: (name) => { if (!available()) return; const values = { ...session.getLocal<Record<string, unknown>>(key) }; delete values[name]; session.setLocal(key, values); },
  };
}
function AvailableSections(props: ExtensionStreamSectionsProps & { session: ExtensionUIStreamSession }) {
  const { session, active = true, loading = null, embedded, compact, confirm, onError, media, navigate, readResult } = props;
  const state = useSyncExternalStore(session.subscribe, session.getState, session.getState);
  const gap = state.snapshot ? nativeStreamGap(state.snapshot.view) ?? nativeRenderGap(state.snapshot.view, media, navigate, !!readResult) : undefined;
  useEffect(() => { if (gap) props.onGap?.(gap); }, [gap, props.onGap]);
  useEffect(() => {
    const visibility = () => { session.setVisible(active && !gap && document.visibilityState !== "hidden"); session.open(); };
    visibility();
    document.addEventListener("visibilitychange", visibility);
    return () => { document.removeEventListener("visibilitychange", visibility); session.setVisible(false); };
  }, [session, active, gap]);
  const nodeHandlers = useMemo(() => {
    const before = state.snapshot;
    const defaults = new Map<string, string>();
    for (const section of before?.view.sections ?? []) for (const item of section.items) {
      if (item.type === "field" || item.type === "segmented") defaults.set(item.key!, item.type === "field" && item.kind === "multichoice" ? item.value ?? "[]" : fieldValues({ sections: [{ items: [{ ...item, name: "value" }] }] }).value);
    }
    const initialValue = (item: { type: string; name: string; key?: string; value?: string }) => item.type === "composer" ? item.value ?? "" : defaults.get(item.key!) ?? "";
    const slots = before ? nativeStreamSlots(before) : [];
    const send = async (key: string, action: UIAction, preferred: "press" | "change" | "submit", extra: Record<string, string> = {}): Promise<UIStreamEvent | null> => {
      try {
        if (!before || session.getState().snapshot !== before || session.getState().phase !== "ready" || session.getState().pending || session.getLocal<Record<string, unknown>>(before.root_key)?.confirming) throw new Error("Displayed version changed or control unavailable");
        const registered = slots.find((s) => s.key === key && same(s.action, action) && s.event === preferred)
          ?? slots.find((s) => s.key === key && same(s.action, action));
        if (!registered) throw new Error("Native callback not available on this node");
        const narrowed = () => {
          const root = before && session.getLocal<Record<string, string>>(before.root_key);
          return { ...(before?.view.search ? { q: root?.search ?? before.view.search.value ?? "" } : {}), ...(before?.view.filter ? { [before.view.filter.name]: root?.[`filter/${before.view.filter.name}`] ?? before.view.filter.value ?? before.view.filter.choices[0]?.value ?? "" } : {}), ...(before?.view.tabs ? { [before.view.tabs.name]: root?.[`tab/${before.view.tabs.name}`] ?? before.view.tabs.value } : {}) };
        };
        if (same(before?.view.sections.find((s) => s.key === key)?.more?.action, action)) extra = { ...narrowed(), ...extra };
        const fields: Record<string, string> = Object.create(null);
        if (registered.action.submit) for (const section of before.view.sections) for (const item of section.items) {
          if ((item.type === "field" && item.kind !== "secret" || item.type === "segmented" || item.type === "composer") && "name" in item) fields[item.name] = session.getDraft(item.key!) ?? initialValue(item);
        }
        // Freeze carried fields BEFORE asking for confirmation, never collect a newer draft after awaiting it.
        // Native SDK callbacks receive a string-value record, including an empty record for a
        // plain press. Protocol scalar/null values remain available only to explicit adapters.
        const value: UIStreamValue = Object.freeze({ ...fields, ...extra, ...registered.action.values });
        const secretNames = before.view.sections.flatMap((s) => s.items).filter((it) => it.type === "field" && it.kind === "secret").map((it) => (it as { name: string }).name);
        if (value && typeof value === "object" && secretNames.some((name) => Object.prototype.hasOwnProperty.call(value, name))) throw new Error("Secret action values forbidden");
        const displayed = Object.freeze({ session: before.session, epoch: before.epoch, version: before.version, key, action: action.id });
        if (registered.action.confirm?.trim()) {
          if (!confirm) return null;
          const local = localFor(session, before.root_key);
          local.set("confirming", true);
          try { if (!await confirm(registered.action, displayed, value)) return null; }
          finally { if (session.getState().snapshot?.root_key === before.root_key) local.set("confirming", false); }
          if (session.getState().snapshot !== before) throw new Error("Displayed version changed during confirmation");
        }
        if (registered.action.push) { navigate?.(registered.action, displayed); return null; }
        return await session.eventDisplayed(before, key, action.id, registered.event, value);
      } catch (error) { onError?.(error); return null; }
    };
    const get = (key: string): ViewHandlers => {
      const current = session.getState();
      const item = before?.view.sections.flatMap((s) => s.items).find((it) => it.key === key);
      const named = item && "name" in item ? item : undefined;
      const value = item?.type === "field" && item.kind === "secret" ? "" : session.getDraft(key) ?? (named ? initialValue(named) : "");
      const confirming = before && session.getLocal<Record<string, unknown>>(before.root_key)?.confirming;
      const enabled = current.phase === "ready" && current.pending === 0 && !confirming;
      const local = localFor(session, key);
      return {
        ...INERT, local, nodeHandlers: get, values: named ? { [named.name]: value } : {},
        busy: enabled ? "" : "stream-pending", locked: !enabled,
        canPress: (a) => !!enabled && slots.some((s) => s.key === key && s.action.id === a.id),
        setValue: (_name, v) => { if (enabled && session.getState().snapshot === before && !(item?.type === "field" && item.kind === "secret")) session.setDraft(key, v); },
        onPress: (a) => { void send(key, a, "press"); },
        onChoose: (it, v) => { if (it.action && enabled && session.getState().snapshot === before) { session.setDraft(key, v); void send(key, it.action, "change", { [it.name ?? "value"]: v }); } },
        onMore: (more) => { void send(key, more.action, "press"); },
        onObserveMore: (more) => { if (before && session.getState().snapshot === before && enabled && !session.getState().pending && session.observeMore(key)) { void send(key, more.action, "press"); return true; } return false; },
        onAct: (a, values) => { void send(key, a, "press", values); },
        onNativeAct: async (a, values) => !!await send(key, a, "change", values),
        nativeAvailable: () => session.getState().snapshot === before && session.getState().phase === "ready" && !session.getState().pending && !session.getLocal<Record<string, unknown>>(before!.root_key)?.confirming,
        nativeFetch: async (a, values) => {
          const event = await send(key, a, "press", values);
          if (!event) return null;
          try {
            const result = readResult ? await readResult(event) : {};
            const current = session.getState().snapshot;
            const currentItem = current?.view.sections.flatMap((s) => s.items).find((it) => it.key === key);
            if (current?.session !== before?.session || current?.epoch !== before?.epoch || item?.type === "diff" && (currentItem?.type !== "diff" || item.path !== currentItem.path || item.revision !== currentItem.revision)) return null;
            if (result?.lines && (result.lines.path !== values?.path || !Number.isSafeInteger(result.lines.start) || result.lines.start < 1 || !Array.isArray(result.lines.lines) || !result.lines.lines.every((line) => typeof line === "string"))) return null;
            return result ?? {}; // Delivery succeeded; a mutation need not have a lines projection.
          } catch (error) { onError?.(error); return null; }
        },
        ...(item?.type === "composer" ? { draft: { value, setValue: (v: string) => enabled && session.getState().snapshot === before && session.setDraft(key, v) } } : {}),
      };
    };
    return get;
  }, [session, state.snapshot, confirm, onError, navigate, readResult]);
  if (gap) return props.fallback;
  if (!state.display || !state.snapshot) return loading;
  const root = nodeHandlers(state.snapshot.root_key);
  return <NativeMediaContext.Provider value={media}><StreamPage key={session.pageKey} session={session} displayed={state.snapshot} handlers={root} active={active} embedded={embedded} compact={compact} media={media} /></NativeMediaContext.Provider>;
}
function StreamPage({ session, displayed, handlers: h, active, embedded, compact, media }: { session: ExtensionUIStreamSession; displayed: UIStreamSnapshot; handlers: ViewHandlers; active: boolean; embedded?: boolean; compact?: boolean; media?: NativeMedia }) {
  const view = session.getState().display!;
  const ref = useRef<HTMLDivElement>(null);
  const restoringFocus = useRef(false);
  const caches = useMemo(() => {
    let cache = reviewCaches.get(session);
    if (!cache || cache.epoch !== displayed.epoch) { cache = { epoch: displayed.epoch, lines: new Map() }; reviewCaches.set(session, cache); }
    return cache.lines;
  }, [session, displayed.epoch]);
  const local = h.local!;
  const [q, setQ] = useNativeState(local, "search", view.search?.value ?? "");
  const [filter, setFilter] = useNativeState(local, `filter/${view.filter?.name}`, view.filter?.value ?? view.filter?.choices[0]?.value ?? "");
  const [tab, setTab] = useNativeState(local, `tab/${view.tabs?.name}`, view.tabs?.value ?? "");
  const [help, setHelp] = useNativeState(local, "keys-help", false);
  const [summary, setSummary] = useNativeState(scopeLocal(local, `review/${view.review?.name}`), "summary", "");
  const queryValues = { ...(view.search ? { q } : {}), ...(view.filter ? { [view.filter.name]: filter } : {}), ...(view.tabs ? { [view.tabs.name]: tab } : {}) };
  const searchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(searchTimer.current), [displayed]);
  useLayoutEffect(() => {
    const saved = local.get<{ key: string; index: number; start: number | null; end: number | null }>("focus");
    if (active && saved && document.activeElement === document.body) {
      const node = [ref.current!, ...ref.current!.querySelectorAll<HTMLElement>("[data-native-key]")].find((n) => n.dataset.nativeKey === saved.key);
      const input = node?.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>("input,textarea,select")[saved.index];
      if (input && !input.disabled) {
        restoringFocus.current = true;
        try { input.focus({ preventScroll: true }); if ("setSelectionRange" in input && saved.start !== null) { try { input.setSelectionRange(saved.start, saved.end); } catch { /* date/range inputs have no text selection */ } } }
        finally { restoringFocus.current = false; }
      }
    }
    for (const position of local.get<Array<{ key: string; index: number; top: number; left: number }>>("scroll") ?? []) {
      const node = [ref.current!, ...ref.current!.querySelectorAll<HTMLElement>("[data-native-key]")].find((n) => n.dataset.nativeKey === position.key);
      const target = position.index === -1 ? node : node?.querySelectorAll<HTMLElement>("*")[position.index];
      if (target) { target.scrollTop = position.top; target.scrollLeft = position.left; }
    }
  }, [displayed, active]); // restore only on a keyed tree/remount, not every keystroke
  const rememberFocus = (target: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement) => {
    if (restoringFocus.current || target instanceof HTMLInputElement && target.type === "password") return;
    const node = target.closest<HTMLElement>("[data-native-key]");
    if (node) local.set("focus", { key: node.dataset.nativeKey!, index: [...node.querySelectorAll("input,textarea,select")].indexOf(target), start: "selectionStart" in target ? target.selectionStart : null, end: "selectionEnd" in target ? target.selectionEnd : null });
  };
  const rememberAfterInput = (target: HTMLInputElement) => {
    // A browser checks microtasks between native listeners, so queueMicrotask can still
    // publish before the bubbling onChange. Use the next task, after the draft commits.
    setTimeout(() => { if (target.isConnected) rememberFocus(target); }, 0);
  };
  const isReview = reviewing(view);
  return <div ref={ref} className={NATIVE.page} data-native-key={displayed.root_key}
    onFocusCapture={(e) => rememberFocus(e.target as HTMLInputElement)}
    onInputCapture={(e) => rememberAfterInput(e.target as HTMLInputElement)}
    onSelectCapture={(e) => rememberAfterInput(e.target as HTMLInputElement)}
    onBlurCapture={(e) => { if (e.relatedTarget && !ref.current?.contains(e.relatedTarget as Node)) local.set("focus", null); }}
    onScrollCapture={(e) => {
      const target = e.target as HTMLElement, node = target.closest<HTMLElement>("[data-native-key]");
      if (!node) return;
      const index = target === node ? -1 : [...node.querySelectorAll("*")].indexOf(target);
      const saved = local.get<Array<{ key: string; index: number; top: number; left: number }>>("scroll") ?? [];
      local.set("scroll", [...saved.filter((p) => p.key !== node.dataset.nativeKey || p.index !== index), { key: node.dataset.nativeKey!, index, top: target.scrollTop, left: target.scrollLeft }]);
    }}>
    {!compact && !embedded && view.title !== undefined && <h2 className="text-xl font-semibold text-ink">{view.title}</h2>}
    {view.tabs && <NativeTabs name={view.tabs.name} items={view.tabs.items} value={tab} disabled={h.locked} onChoose={(value) => { setTab(value); void h.onNativeAct?.(view.tabs!.action, { ...queryValues, [view.tabs!.name]: value }); }} />}
    {(view.search || view.filter) && <div className="flex flex-wrap items-center gap-2">
      {view.search && <input type="search" aria-label={view.search.placeholder ?? t("settings-extensionview.search")} placeholder={view.search.placeholder} className={NATIVE.field} value={q} disabled={h.locked} onChange={(e) => {
        const value = e.target.value; setQ(value); clearTimeout(searchTimer.current);
        const action = view.search!.action, values = { ...queryValues, q: value };
        searchTimer.current = setTimeout(() => { void h.onNativeAct?.(action, values); }, NATIVE.searchDelay);
      }} />}
      {view.filter && <select aria-label={view.filter.title ?? view.filter.name} className={NATIVE.field} value={filter} disabled={h.locked} onChange={(e) => { const value = e.target.value; setFilter(value); void h.onNativeAct?.(view.filter!.action, { ...queryValues, [view.filter!.name]: value }); }}>{view.filter.choices.map((it) => <option key={it.value} value={it.value}>{it.title ?? it.value}</option>)}</select>}
    </div>}
    <ReviewProvider view={isReview ? view : null} page={session.pageKey} fetch={h.nativeFetch!} local={local} nativeCaches={caches}>
      {isReview && <><ReviewToolbar view={view} onHelp={() => setHelp(true)} />{active && !h.locked && <ReviewKeys onHelp={() => setHelp(true)} />}<KeysHelp open={help} onClose={() => setHelp(false)} /></>}
      <ViewSections view={view} embedded={embedded} compact={compact} {...INERT} local={local} nodeHandlers={h.nodeHandlers} renderItem={streamItemRenderer(media)} />
    </ReviewProvider>
    {view.review && <div className={NATIVE.card}><div className="p-3"><p className={NATIVE.detail}>{t("extensions-review.drafts", { count: view.review.count })}</p><Box value={summary} onChange={setSummary} placeholder={view.review.placeholder ?? t("extensions-review.summary")} disabled={h.locked} /><div className="flex flex-wrap gap-2 pt-2">{view.review.discard && <Button disabled={h.locked} tone="quiet" onClick={() => h.onPress(view.review!.discard!)}>{t("extensions-review.discard")}</Button>}{view.review.buttons.map((b) => <Button key={b.action.id} disabled={h.locked} onClick={() => h.onAct?.(b.action, { [view.review!.name]: summary })}>{b.title}</Button>)}</div></div></div>}
  </div>;
}
