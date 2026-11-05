// An extension's View (docs/extensions.md「声明式 UI」「UI 插槽」「页面跳转」), loaded and carried out: an extension page in
// 设置 › 扩展 or from the sidebar, a section on a launcher's or a machine's page, a card on 总览, an inbox item's body.
// How the items look is views/extensions/ViewBody.tsx. Here: a button's confirm asks first, submit sends the fields'
// values, a segmented acts when chosen, and what the press answers (a new view, a word, a link, a page pushed or popped)
// is carried out. UI version 3: a page can push another one (list → detail → edit), kept on a stack here (8 deep, the
// browser's back pops it on a full page); the view's search box and filter and a section's 加载更多 go to the
// extension; extension_ui {invalidate} draws the view on screen again (one off screen when it comes back). A view with
// refresh_after is drawn again while it is on screen. Logic in lib/extension-ui.ts.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, errorText } from "../../lib/api";
import { extIdOf } from "../../lib/extension-api";
import { openLink } from "../../lib/links";
import {
  actUI, fieldValues, isUnsupported, numberProblem, outcome, pushPage, refreshDelay, renderUI, submitValues, uiPlaces,
  type UIAction, type UIContext, type UIItem, type UIMore, type UIPage, type UIPlace, type UIPush, type UIView,
} from "../../lib/extension-ui";
import { navigate } from "../../lib/router";
import { onHubEvent } from "../../lib/store";
import { ViewSections } from "../extensions/ViewBody";
import { Button, cx, Dialog, Loading, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

const inputClass =
  "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

/**
 * Draws again while the view is on screen (docs/extensions.md「刷新」): `delay` ms after it was last drawn, never while the
 * page is hidden; coming back after the delay draws at once.
 */
function useRefresh(delay: number, drawnAt: number, draw: () => void) {
  const drawRef = useRef(draw);
  drawRef.current = draw;
  useEffect(() => {
    if (!delay || !drawnAt) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const arm = () => {
      clearTimeout(timer);
      if (document.visibilityState !== "visible") return;
      const left = drawnAt + delay - Date.now();
      if (left <= 0) drawRef.current();
      else timer = setTimeout(() => drawRef.current(), left);
    };
    arm();
    document.addEventListener("visibilitychange", arm);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", arm);
    };
  }, [delay, drawnAt]);
}


/**
 * extension_ui {invalidate} for this extension (and this ui, or all of its): `draw` now when the page is visible, else
 * once it is visible again. At most once a second.
 */
function useInvalidate(extension: string, ui: string, draw: () => void) {
  const drawRef = useRef(draw);
  drawRef.current = draw;
  useEffect(() => {
    let dirty = false;
    let last = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const fire = () => {
      dirty = false;
      last = Date.now();
      drawRef.current();
    };
    const now = () => {
      if (document.visibilityState !== "visible") {
        dirty = true;
        return;
      }
      const wait = last + 1000 - Date.now();
      clearTimeout(timer);
      if (wait <= 0) fire();
      else timer = setTimeout(fire, wait);
    };
    const off = onHubEvent("extension_ui", (msg) => {
      if (msg.invalidate !== true || extIdOf(msg) !== extension) return;
      const id = typeof msg.id === "string" ? msg.id : "";
      if (id && id !== ui) return;
      now();
    });
    const shown = () => {
      if (dirty && document.visibilityState === "visible") now();
    };
    document.addEventListener("visibilitychange", shown);
    return () => {
      off();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", shown);
    };
  }, [extension, ui]);
}

/** Where a pushed page opens when the view cannot hold a stack (a card, a section on another page): the full page. */
export function pageHref(extension: string, ui: string, state?: string): string {
  return `/extension/${encodeURIComponent(extension)}/${encodeURIComponent(ui)}${state ? `?state=${encodeURIComponent(state)}` : ""}`;
}

/**
 * Loads and draws one of an extension's views. `embedded`: inside another page (a launcher's), lighter, and gone when the
 * extension says the launcher is not its (404 unsupported) instead of showing an error. `compact`: a card (总览), no
 * chrome of its own, small charts. `bare`: no heading of its own (the page around it has one). `onFailed`: the first
 * draw failed (no view yet), and the page around it shows something else instead of the error. `revision`: what it is
 * drawn for changed (an inbox item took new messages): drawn again, the fields typed into kept. `onOpenSource`: a press
 * opened the inbox item's own source page (persona://inbox/<context.item>/source). `pages`: where a pushed page goes —
 * on this view's own stack (`stack`, the default; with `history`, the browser's back pops it), or the full page of it
 * (`open`, the default for a card and a section inside another page). `state`: the first page's state (a full page opened by a push).
 */
export function ExtensionUI({
  extension, ui, context, embedded, compact, bare, title, onUnsupported, onMissing, onFailed, revision, onOpenSource, pages, history: withHistory, state: firstState,
}: {
  extension: string; ui: string; context?: UIContext; embedded?: boolean; compact?: boolean; bare?: boolean; title?: string;
  onUnsupported?: () => void; onMissing?: () => void; onFailed?: (e: unknown) => void; revision?: string; onOpenSource?: () => void;
  pages?: "stack" | "open"; history?: boolean; state?: string;
}) {
  const [stack, setStack] = useState<UIPage[]>(() => [{ ui, ...(firstState ? { state: firstState } : {}) }]);
  const top = stack[stack.length - 1];
  const [view, setView] = useState<UIView | null>(null);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [loading, setLoading] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [confirming, setConfirming] = useState<{ action: UIAction; destructive: boolean } | null>(null);
  const [drawnAt, setDrawnAt] = useState(0);
  const [query, setQuery] = useState(""); // the search box as typed
  const stateRef = useRef<string | undefined>(top.state);
  const drawnRef = useRef(false); // a view has been drawn
  const ctxKey = `${context?.launcher ?? ""}\n${context?.machine ?? ""}\n${context?.item ?? ""}\n${context?.size ?? ""}`;
  const mode = pages ?? (compact || embedded ? "open" : "stack");
  const depth = stack.length;
  const pageKey = `${depth}\n${top.ui}`;

  // the first page follows the props (a different extension or page opened in the same place)
  useEffect(() => {
    setStack([{ ui, ...(firstState ? { state: firstState } : {}) }]);
  }, [extension, ui, firstState]);

  // keep: drawn again for the same thing, the fields typed into stay as typed
  const show = (v: UIView, keep = false) => {
    setView(v);
    const start = fieldValues(v);
    setValues((old) => (keep ? Object.fromEntries(Object.entries(start).map(([k, x]) => [k, old[k] ?? x])) : start));
    stateRef.current = v.state;
    drawnRef.current = true;
    if (!keep) setQuery(v.search?.value ?? "");
    setError("");
    setDrawnAt(Date.now());
  };
  const load = useCallback((keep?: boolean) => {
    setLoading(true);
    renderUI(extension, top.ui, context, stateRef.current)
      .then((v) => show(v, keep === true))
      .catch((e) => {
        if (isUnsupported(e)) (setUnsupported(true), onUnsupported?.());
        else if (onMissing && e instanceof ApiError && e.status === 404 && depth === 1) onMissing();
        else if (onFailed && !drawnRef.current) onFailed(e);
        else {
          setError(errorText(e));
          setDrawnAt(Date.now()); // a failed refresh waits a whole interval too
        }
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [extension, pageKey, ctxKey]);
  // a new page (pushed, popped, another one): its own state, a fresh view
  useEffect(() => {
    stateRef.current = top.state;
    drawnRef.current = false;
    setView(null);
    setError("");
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);
  useRefresh(refreshDelay(view), drawnAt, () => {
    if (!busy) load(true);
  });
  useInvalidate(extension, top.ui, () => {
    if (!busy) load(true);
  });
  const drawnFor = useRef(revision);
  useEffect(() => {
    if (drawnFor.current === revision) return;
    drawnFor.current = revision;
    if (drawnRef.current) load(true);
  }, [revision, load]);

  // the browser's back pops a page pushed here (a full page only: a section or a card never holds one)
  const historied = withHistory ?? (!embedded && !compact && mode === "stack");
  useEffect(() => {
    if (!historied) return;
    const onPop = (e: PopStateEvent) => {
      const at = typeof (e.state as { extPage?: unknown } | null)?.extPage === "number" ? (e.state as { extPage: number }).extPage : 1;
      setStack((s) => (at < s.length ? s.slice(0, Math.max(1, at)) : s));
    };
    addEventListener("popstate", onPop);
    return () => removeEventListener("popstate", onPop);
  }, [historied]);

  const push = (p: UIPush) => {
    if (mode === "open") {
      navigate(pageHref(extension, p.ui || top.ui, p.state));
      return;
    }
    const next = pushPage(stack, p, top.ui);
    if (historied && next.length > stack.length) history.pushState({ ...(history.state ?? {}), extPage: next.length }, "", location.href);
    setStack(next);
  };
  const pop = () => {
    if (stack.length <= 1) return;
    if (historied) history.back(); // the popstate above takes the page off
    else setStack((s) => s.slice(0, -1));
  };

  const run = async (action: UIAction, extra?: Record<string, string>) => {
    if (action.push) {
      push(action.push);
      return;
    }
    const sent = action.submit ? { ...submitValues(view, values), ...extra } : extra;
    if (sent && view) {
      const bad = view.sections.flatMap((s) => s.items).find((it) => it.type === "field" && it.kind === "number" && it.name && numberProblem(sent[it.name] ?? ""));
      if (bad) {
        toast(t("settings-extensionview.not_a_number", { field: bad.label || bad.name || "" }), "error");
        return;
      }
    }
    setBusy(action.id);
    try {
      const r = outcome(await actUI(extension, top.ui, action.id, { context, state: stateRef.current, values: sent }), context?.item);
      if (r.view) show(r.view, !!(extra && "q" in extra)); // a search keeps what is typed
      if (r.toast) toast(r.toast, "ok");
      if (r.url) openLink(r.url);
      if (r.source) onOpenSource?.();
      if (r.push) push(r.push);
      if (r.pop) pop();
      if (r.reload) load();
    } catch (e) {
      if (isUnsupported(e)) (setUnsupported(true), onUnsupported?.());
      else toast(t("settings-extensionview.action_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy("");
    }
  };
  const press = (action: UIAction, destructive = false) => {
    if (busy) return;
    if (action.confirm?.trim()) setConfirming({ action, destructive });
    else void run(action);
  };
  // a segmented acts as soon as another choice is made: its own value goes along (the rest only when it submits)
  const choose = (item: UIItem, value: string) => {
    if (busy || !item.name || !item.action) return;
    setValues((x) => ({ ...x, [item.name!]: value }));
    void run(item.action, { [item.name]: value });
  };
  // the search box, the filter and 加载更多 send what the list is narrowed to: the text typed and the filter's choice
  const listValues = (): Record<string, string> => ({
    ...(view?.search ? { q: query } : {}),
    ...(view?.filter ? { [view.filter.name]: values[view.filter.name] ?? view.filter.value ?? view.filter.choices[0]?.value ?? "" } : {}),
  });
  const more = useCallback((m: UIMore) => {
    if (!busy) void run(m.action, listValues());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [busy, view, query, values]);
  // typed into the search box: asked 0.3 s after the last key
  const searched = useRef(query);
  useEffect(() => {
    const s = view?.search;
    if (!s || query === searched.current) return;
    const timer = setTimeout(() => {
      searched.current = query;
      void run(s.action, listValues());
    }, 300);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);
  useEffect(() => {
    searched.current = view?.search?.value ?? "";
  }, [pageKey, view?.search?.value]);

  if (unsupported) {
    return embedded || compact ? null : (
      <p className="rounded-card bg-surface px-4 py-6 text-center text-[12.5px] text-ink-3 shadow-card">{t("settings-extensionview.nothing_here")}</p>
    );
  }

  const heading = view?.title || top.title || (depth === 1 ? title : undefined);
  const below = depth > 1 ? stack[depth - 2].title || (depth === 2 ? title : undefined) || t("settings-extensionview.back") : "";
  const light = embedded || compact;
  return (
    <div className={cx("flex flex-col", compact ? "gap-1" : embedded ? "gap-3" : "gap-6")}>
      {depth > 1 && (
        <div className="-mb-3 flex items-center px-1">
          <Button tone="quiet" icon="chevronLeft" onClick={pop} className="-ml-2 max-w-full">
            <span className="truncate">{below}</span>
          </Button>
        </div>
      )}
      {!light && (!bare || depth > 1) && heading && (
        <div className="flex items-center gap-2 px-1">
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{heading}</h2>
          {loading && view && <Spinner />}
        </div>
      )}
      {view && (view.search || view.filter) && (
        <div className={cx("flex items-center gap-2", compact ? "px-4" : "px-0")}>
          {view.search && (
            <label className="relative min-w-0 flex-1">
              <Icon name="search" size={13} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={view.search.placeholder || t("settings-extensionview.search")}
                aria-label={view.search.placeholder || t("settings-extensionview.search")}
                spellCheck={false}
                autoComplete="off"
                className={cx(inputClass, "pl-8")}
              />
            </label>
          )}
          {view.filter && (
            <select
              aria-label={view.filter.title || view.filter.name}
              value={values[view.filter.name] ?? view.filter.value ?? view.filter.choices[0]?.value ?? ""}
              disabled={!!busy}
              onChange={(e) => {
                const f = view.filter!;
                setValues((x) => ({ ...x, [f.name]: e.target.value }));
                void run(f.action, { ...listValues(), [f.name]: e.target.value });
              }}
              className={cx(inputClass, "w-auto max-w-[200px] shrink-0")}
            >
              {view.filter.choices.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.title || c.value}
                </option>
              ))}
            </select>
          )}
          {busy && <Spinner />}
        </div>
      )}
      {!view && !error && (
        <div className={cx(compact ? "px-4 py-3" : embedded ? "py-2" : "rounded-card bg-surface px-4 py-3 shadow-card")}>
          <Loading />
        </div>
      )}
      {error && compact && (
        <div className="flex items-center gap-2 px-4 py-2.5">
          <p className="min-w-0 flex-1 truncate text-[12px] text-red" title={error}>
            {view ? t("settings-extensionview.update_failed_inline", { error }) : t("settings-extensionview.draw_failed_inline", { error })}
          </p>
          <Button tone="quiet" icon="retry" busy={loading} onClick={() => load()}>
            {t("settings-extensionview.retry")}
          </Button>
        </div>
      )}
      {error && !compact && (
        <div className={cx("flex flex-col items-center gap-2 px-6 text-center", embedded ? "rounded-control bg-inset py-4 shadow-hairline" : "rounded-card bg-surface py-8 shadow-card")}>
          <span className="flex size-9 items-center justify-center rounded-[10px] bg-red-tint text-red">
            <Icon name="extension" size={16} />
          </span>
          <p className="text-[13px] font-medium text-ink">{view ? t("settings-extensionview.update_failed") : t("settings-extensionview.draw_failed")}</p>
          <p className="max-w-[420px] text-[12px] leading-relaxed break-words text-ink-3 select-text">{error}</p>
          <Button tone="secondary" icon="retry" busy={loading} onClick={() => load()}>
            {t("settings-extensionview.retry")}
          </Button>
        </div>
      )}
      {view && (
        <ViewSections
          view={view}
          embedded={embedded}
          compact={compact}
          values={values}
          setValue={(k, v) => setValues((x) => ({ ...x, [k]: v }))}
          busy={busy}
          onPress={press}
          onChoose={choose}
          onMore={more}
        />
      )}
      {confirming && (
        <Dialog
          open
          onClose={() => setConfirming(null)}
          width={400}
          title={t("settings-extensionview.confirm_title")}
          footer={
            <>
              <Button tone="quiet" onClick={() => setConfirming(null)}>
                {t("common.cancel")}
              </Button>
              <Button
                tone={confirming.destructive ? "danger" : "primary"}
                onClick={() => {
                  const a = confirming.action;
                  setConfirming(null);
                  void run(a);
                }}
              >
                {t("settings-extensionview.ok")}
              </Button>
            </>
          }
        >
          <p className="text-[13px] leading-relaxed break-words text-ink-2">{confirming.action.confirm}</p>
        </Dialog>
      )}
    </div>
  );
}

/**
 * The extensions' sections on a launcher's page (placement launcher_section): one per extension whose gateway matches. A
 * extension that says this launcher is not its gateway (404 unsupported) leaves no trace; nothing listed, nothing drawn.
 */
export function LauncherExtensionSections({ launcher, machine }: { launcher: string; machine?: string }) {
  const [places, setPlaces] = useState<UIPlace[]>([]);
  useEffect(() => {
    let live = true;
    setPlaces([]);
    uiPlaces("launcher_section", launcher, machine).then(
      (l) => live && setPlaces(l),
      () => {}, // an older hub, or no extensions: no section
    );
    return () => {
      live = false;
    };
  }, [launcher, machine]);
  return (
    <>
      {places.map((u) => (
        <LauncherSection key={`${u.extension}/${u.id}`} place={u} launcher={launcher} machine={machine} />
      ))}
    </>
  );
}

function LauncherSection({ place, launcher, machine }: { place: UIPlace; launcher: string; machine?: string }) {
  const [gone, setGone] = useState(false);
  const context = useMemo(() => ({ launcher, machine }), [launcher, machine]);
  if (gone) return null;
  return (
    <div data-extension-section={`${place.extension}/${place.id}`}>
      <div className="mb-1 flex items-center gap-1.5 text-[11.5px] font-medium text-ink-3">
        <Icon name="extension" size={11} className="shrink-0" />
        {place.title}
      </div>
      <ExtensionUI extension={place.extension} ui={place.id} context={context} embedded onUnsupported={() => setGone(true)} />
    </div>
  );
}
