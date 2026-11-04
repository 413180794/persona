// A plugin's View (docs/plugins.md「声明式 UI」「UI 插槽」), drawn with the web's design system: a plugin page in
// 设置 › 插件 or from the sidebar, a section on a launcher's or a machine's page, a card on 总览. Every kind of item
// (text, metric, progress, row, button, link, field, divider, chart, stat_grid, segmented); an unknown one is left out.
// A tone becomes a role color; a button's confirm asks first, submit sends the fields' values, a segmented acts when
// chosen, and what the press answers (a new view, a word, a link) is carried out. A view with refresh_after is drawn
// again while it is on screen. Logic in lib/plugin-ui.ts.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, errorText } from "../../lib/api";
import { openLink } from "../../lib/links";
import {
  actUI, blocks, fieldValues, isHttps, isUnsupported, numberProblem, outcome, percent, refreshDelay, renderUI, submitValues, toneOf, uiPlaces,
  type UIAction, type UIContext, type UIItem, type UIPlace, type UISection, type UITone, type UIView,
} from "../../lib/plugin-ui";
import { PluginChart } from "../plugins/PluginChart";
import { Button, cx, Dialog, EASE, Loading, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Pill, Switch } from "./SettingsParts";

const TEXT: Record<UITone, string> = { neutral: "text-ink", positive: "text-green", caution: "text-orange", danger: "text-red" };
const BAR: Record<UITone, string> = { neutral: "bg-accent", positive: "bg-green", caution: "bg-orange", danger: "bg-red" };
const BADGE: Record<UITone, "green" | "orange" | "red" | undefined> = { neutral: undefined, positive: "green", caution: "orange", danger: "red" };

const inputClass =
  "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

/**
 * Draws again while the view is on screen (docs/plugins.md「刷新」): `delay` ms after it was last drawn, never while the
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
 * Loads and draws one of a plugin's views. `embedded`: inside another page (a launcher's), lighter, and gone when the
 * plugin says the launcher is not its (404 unsupported) instead of showing an error. `compact`: a card (总览), no
 * chrome of its own, small charts. `bare`: no heading of its own (the page around it has one). `onFailed`: the first
 * draw failed (no view yet), and the page around it shows something else instead of the error. `revision`: what it is
 * drawn for changed (an inbox item took new messages): drawn again, the fields typed into kept. `onOpenSource`: a press
 * opened the inbox item's own source page (persona://inbox/<context.item>/source).
 */
export function PluginUI({
  plugin, ui, context, embedded, compact, bare, title, onUnsupported, onMissing, onFailed, revision, onOpenSource,
}: {
  plugin: string; ui: string; context?: UIContext; embedded?: boolean; compact?: boolean; bare?: boolean; title?: string;
  onUnsupported?: () => void; onMissing?: () => void; onFailed?: (e: unknown) => void; revision?: string; onOpenSource?: () => void;
}) {
  const [view, setView] = useState<UIView | null>(null);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [loading, setLoading] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [confirming, setConfirming] = useState<{ action: UIAction; destructive: boolean } | null>(null);
  const [drawnAt, setDrawnAt] = useState(0);
  const stateRef = useRef<string | undefined>(undefined);
  const drawnRef = useRef(false); // a view has been drawn
  const ctxKey = `${context?.launcher ?? ""}\n${context?.machine ?? ""}\n${context?.item ?? ""}\n${context?.size ?? ""}`;

  // keep: drawn again for the same thing, the fields typed into stay as typed
  const show = (v: UIView, keep = false) => {
    setView(v);
    const start = fieldValues(v);
    setValues((old) => (keep ? Object.fromEntries(Object.entries(start).map(([k, x]) => [k, old[k] ?? x])) : start));
    stateRef.current = v.state;
    drawnRef.current = true;
    setError("");
    setDrawnAt(Date.now());
  };
  const load = useCallback((keep?: boolean) => {
    setLoading(true);
    renderUI(plugin, ui, context, stateRef.current)
      .then((v) => show(v, keep === true))
      .catch((e) => {
        if (isUnsupported(e)) (setUnsupported(true), onUnsupported?.());
        else if (onMissing && e instanceof ApiError && e.status === 404) onMissing();
        else if (onFailed && !drawnRef.current) onFailed(e);
        else {
          setError(errorText(e));
          setDrawnAt(Date.now()); // a failed refresh waits a whole interval too
        }
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin, ui, ctxKey]);
  useEffect(() => load(), [load]);
  useRefresh(refreshDelay(view), drawnAt, () => {
    if (!busy) load();
  });
  const drawnFor = useRef(revision);
  useEffect(() => {
    if (drawnFor.current === revision) return;
    drawnFor.current = revision;
    if (drawnRef.current) load(true);
  }, [revision, load]);

  const run = async (action: UIAction, extra?: Record<string, string>) => {
    const sent = action.submit ? { ...submitValues(view, values), ...extra } : extra;
    if (sent && view) {
      const bad = view.sections.flatMap((s) => s.items).find((it) => it.type === "field" && it.kind === "number" && it.name && numberProblem(sent[it.name] ?? ""));
      if (bad) {
        toast(`「${bad.label || bad.name}」要是一个数`, "error");
        return;
      }
    }
    setBusy(action.id);
    try {
      const r = outcome(await actUI(plugin, ui, action.id, { context, state: stateRef.current, values: sent }), context?.item);
      if (r.view) show(r.view);
      if (r.toast) toast(r.toast, "ok");
      if (r.url) openLink(r.url);
      if (r.source) onOpenSource?.();
      if (r.reload) load();
    } catch (e) {
      if (isUnsupported(e)) (setUnsupported(true), onUnsupported?.());
      else toast(`没有完成：${errorText(e)}`, "error");
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

  if (unsupported) {
    return embedded || compact ? null : (
      <p className="rounded-card bg-surface px-4 py-6 text-center text-[12.5px] text-ink-3 shadow-card">这个插件在这里没有要显示的内容。</p>
    );
  }

  const heading = view?.title || title;
  const light = embedded || compact;
  return (
    <div className={cx("flex flex-col", compact ? "gap-1" : embedded ? "gap-3" : "gap-6")}>
      {!light && !bare && heading && (
        <div className="flex items-center gap-2 px-1">
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{heading}</h2>
          {loading && view && <Spinner />}
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
            {view ? "没有更新：" : "插件没画出来："}
            {error}
          </p>
          <Button tone="quiet" icon="retry" busy={loading} onClick={() => load()}>
            重试
          </Button>
        </div>
      )}
      {error && !compact && (
        <div className={cx("flex flex-col items-center gap-2 px-6 text-center", embedded ? "rounded-control bg-inset py-4 shadow-hairline" : "rounded-card bg-surface py-8 shadow-card")}>
          <span className="flex size-9 items-center justify-center rounded-[10px] bg-red-tint text-red">
            <Icon name="plugin" size={16} />
          </span>
          <p className="text-[13px] font-medium text-ink">{view ? "没有更新" : "插件没画出来"}</p>
          <p className="max-w-[420px] text-[12px] leading-relaxed break-words text-ink-3 select-text">{error}</p>
          <Button tone="secondary" icon="retry" busy={loading} onClick={() => load()}>
            重试
          </Button>
        </div>
      )}
      {view?.sections.map((s, i) => (
        <Section key={i} section={s} embedded={embedded} compact={compact} values={values} setValue={(k, v) => setValues((x) => ({ ...x, [k]: v }))} busy={busy} onPress={press} onChoose={choose} />
      ))}
      {confirming && (
        <Dialog
          open
          onClose={() => setConfirming(null)}
          width={400}
          title="确认"
          footer={
            <>
              <Button tone="quiet" onClick={() => setConfirming(null)}>
                取消
              </Button>
              <Button
                tone={confirming.destructive ? "danger" : "primary"}
                onClick={() => {
                  const a = confirming.action;
                  setConfirming(null);
                  void run(a);
                }}
              >
                确定
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

interface SectionProps {
  section: UISection;
  embedded?: boolean;
  compact?: boolean;
  values: Record<string, string>;
  setValue: (name: string, v: string) => void;
  busy: string;
  onPress: (a: UIAction, destructive?: boolean) => void;
  onChoose: (item: UIItem, value: string) => void;
}

function Section({ section, embedded, compact, ...rest }: SectionProps) {
  const bs = blocks(section.items ?? []);
  return (
    <section style={{ animation: `fade-up 280ms ${EASE} both` }}>
      {section.title && <h3 className={cx("font-medium text-ink-3", compact ? "px-4 pt-1 text-[11.5px]" : embedded ? "mb-1.5 px-1 text-[11.5px]" : "mb-1.5 px-1 text-[12px]")}>{section.title}</h3>}
      <div className={cx("overflow-hidden", compact ? "" : embedded ? "rounded-control bg-inset shadow-hairline" : "rounded-card bg-surface shadow-card")}>
        {bs.map((b, i) => {
          // rows that follow each other read as a list, and charts one under another as separate charts: a hairline between them
          const prev = i > 0 && bs[i - 1].kind === "item" ? (bs[i - 1] as { item: UIItem }).item.type : "";
          const afterRow = b.kind === "item" && ((b.item.type === "row" && prev === "row") || (b.item.type === "chart" && prev === "chart"));
          if (b.kind === "metrics") return <Metrics key={i} items={b.items} />;
          if (b.kind === "buttons")
            return (
              <div key={i} className="flex flex-wrap items-center gap-2 px-4 py-2.5">
                {b.items.map((it, j) => (
                  <ItemButton key={j} item={it} busy={rest.busy} onPress={rest.onPress} />
                ))}
              </div>
            );
          return (
            <div key={i} className={cx(afterRow && "border-t border-line")}>
              <Item item={b.item} compact={compact} {...rest} />
            </div>
          );
        })}
      </div>
      {section.footer && <p className={cx("leading-relaxed text-ink-3", compact ? "px-4 pb-1 text-[11px]" : "mt-1.5 px-1 text-[11.5px]")}>{section.footer}</p>}
    </section>
  );
}

function Metrics({ items }: { items: UIItem[] }) {
  return (
    <div className={cx("grid gap-x-4 gap-y-3 px-4 py-3", items.length === 1 ? "grid-cols-1" : items.length === 2 ? "grid-cols-2" : "grid-cols-2 sm:grid-cols-3")}>
      {items.map((it, i) => (
        <div key={i} className="min-w-0">
          <div className="truncate text-[11.5px] text-ink-3">{it.label}</div>
          <div className={cx("mt-0.5 truncate text-[20px] leading-tight font-semibold tabular-nums", TEXT[toneOf(it.tone)])}>{it.value}</div>
          {it.detail && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{it.detail}</div>}
        </div>
      ))}
    </div>
  );
}

function ItemButton({ item, busy, onPress }: { item: UIItem; busy: string; onPress: SectionProps["onPress"] }) {
  if (!item.action) return null;
  const destructive = item.role === "destructive";
  return (
    <Button
      tone={item.action.submit ? "primary" : "secondary"}
      busy={busy === item.action.id}
      disabled={!!busy && busy !== item.action.id}
      onClick={() => onPress(item.action!, destructive)}
      className={cx(destructive && "text-red")}
    >
      {item.title}
    </Button>
  );
}

function Item({ item: it, compact, values, setValue, busy, onPress, onChoose }: Omit<SectionProps, "section" | "embedded"> & { item: UIItem }) {
  switch (it.type) {
    case "chart":
      return <PluginChart item={it} compact={compact} />;
    case "stat_grid":
      return <StatGrid item={it} compact={compact} />;
    case "segmented":
      return <Segmented item={it} value={values[it.name ?? ""] ?? it.value ?? ""} busy={!!busy} pending={busy === it.action?.id} onChoose={(v) => onChoose(it, v)} />;
    case "text": {
      const style = it.style ?? "body";
      if (style === "code")
        return (
          <div className="px-4 py-2.5">
            <pre className="overflow-x-auto rounded-[8px] bg-inset px-2.5 py-2 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap break-all text-ink-2 shadow-hairline select-text">{it.text}</pre>
          </div>
        );
      return (
        <p className={cx("px-4 py-2.5 leading-relaxed whitespace-pre-wrap break-words select-text", style === "secondary" ? "text-[12.5px] text-ink-2" : style === "caption" ? "text-[11.5px] text-ink-3" : "text-[13px] text-ink")}>
          {it.text}
        </p>
      );
    }
    case "metric":
      return <Metrics items={[it]} />;
    case "progress": {
      const pct = percent(it.ratio);
      const tone = toneOf(it.tone);
      return (
        <div className="px-4 py-2.5">
          <div className="flex items-baseline gap-3">
            <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{it.label}</span>
            {it.detail && <span className={cx("shrink-0 truncate text-[12px] tabular-nums", tone === "neutral" ? "text-ink-3" : TEXT[tone])}>{it.detail}</span>}
          </div>
          <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-hover-2" role="progressbar" aria-label={it.label} aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className={cx("h-full rounded-full transition-[width] duration-300", BAR[tone])} style={{ width: `${pct}%`, transitionTimingFunction: EASE }} />
          </div>
        </div>
      );
    }
    case "row": {
      const body = (
        <>
          <div className="min-w-0 flex-1">
            <div className="truncate text-[13px] text-ink">{it.title}</div>
            {it.subtitle && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{it.subtitle}</div>}
          </div>
          {it.value && <span className="shrink-0 truncate text-[12.5px] text-ink-2 tabular-nums">{it.value}</span>}
          {it.badge?.text && <Pill tone={BADGE[toneOf(it.badge.tone)]}>{it.badge.text}</Pill>}
          {it.action && (busy === it.action.id ? <Spinner /> : <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />)}
        </>
      );
      return it.action ? (
        <button type="button" disabled={!!busy} onClick={() => onPress(it.action!)} className="flex w-full items-center gap-3 px-4 py-2.5 text-left whitespace-nowrap transition-colors enabled:hover:bg-hover">
          {body}
        </button>
      ) : (
        <div className="flex items-center gap-3 px-4 py-2.5 whitespace-nowrap">{body}</div>
      );
    }
    case "button":
      return (
        <div className="px-4 py-2.5">
          <ItemButton item={it} busy={busy} onPress={onPress} />
        </div>
      );
    case "link": {
      if (!isHttps(it.url)) return null;
      const host = new URL(it.url!).host;
      return (
        <a
          href={it.url}
          target="_blank"
          rel="noreferrer noopener"
          onClick={(e) => {
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
            e.preventDefault();
            openLink(it.url!, it.title);
          }}
          className="flex items-center gap-3 px-4 py-2.5 whitespace-nowrap transition-colors hover:bg-hover"
        >
          <span className="min-w-0 flex-1 truncate text-[13px] text-accent-ink">{it.title || host}</span>
          <span className="min-w-0 shrink truncate text-[12px] text-ink-3">{host}</span>
          <Icon name="external" size={12} className="shrink-0 text-ink-3" />
        </a>
      );
    }
    case "field":
      return <Field item={it} value={values[it.name ?? ""] ?? ""} onChange={(v) => it.name && setValue(it.name, v)} />;
    case "divider":
      return <div role="separator" className="mx-4 my-1 border-t border-line" />;
    default:
      return null;
  }
}

// stat_grid: 2 a row when narrow, 3 when there is room, all in one row when wide (a container query: a card is narrow
// on a wide page)
const STAT_COLS: Record<number, string> = {
  1: "grid-cols-1",
  2: "grid-cols-2",
  3: "grid-cols-2 @xs:grid-cols-3",
  4: "grid-cols-2 @lg:grid-cols-4",
  5: "grid-cols-2 @xs:grid-cols-3 @2xl:grid-cols-5",
  6: "grid-cols-2 @xs:grid-cols-3 @2xl:grid-cols-6",
};

function StatGrid({ item, compact }: { item: UIItem; compact?: boolean }) {
  const stats = (item.stats ?? []).slice(0, 6);
  return (
    <div className="@container">
      <div className={cx("grid gap-x-4 gap-y-3 px-4", compact ? "py-2" : "py-3", STAT_COLS[stats.length] ?? STAT_COLS[6])}>
        {stats.map((st, i) => {
          const tone = toneOf(st.tone);
          return (
            <div key={i} className="min-w-0">
              <div className="truncate text-[11.5px] text-ink-3">{st.label}</div>
              <div className={cx("mt-0.5 truncate leading-tight font-semibold text-ink", compact ? "text-[18px]" : "text-[20px]")} title={st.value}>
                {st.value}
              </div>
              {st.detail && <div className={cx("mt-0.5 truncate text-[11.5px] tabular-nums", tone === "neutral" ? "text-ink-3" : TEXT[tone])}>{st.detail}</div>}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** segmented: one of a few, acting as soon as it is chosen (the choice shows at once, the view follows). */
function Segmented({ item, value, busy, pending, onChoose }: { item: UIItem; value: string; busy: boolean; pending: boolean; onChoose: (v: string) => void }) {
  const choices = item.choices ?? [];
  return (
    <div className="flex items-center gap-2 px-4 py-2.5">
      <div role="radiogroup" aria-label={item.label || item.name} className="flex max-w-full items-center gap-0.5 overflow-x-auto rounded-control bg-inset p-0.5 shadow-hairline">
        {choices.map((c) => {
          const on = c.value === value;
          return (
            <button
              key={c.value}
              type="button"
              role="radio"
              aria-checked={on}
              disabled={busy && !on}
              onClick={() => !on && onChoose(c.value)}
              className={cx(
                "flex h-7 shrink-0 items-center whitespace-nowrap rounded-[7px] px-2.5 text-[12px] transition-[background-color,color] duration-150 disabled:opacity-60",
                on ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
              )}
            >
              {c.title || c.value}
            </button>
          );
        })}
      </div>
      {pending && <Spinner />}
    </div>
  );
}

function Field({ item: it, value, onChange }: { item: UIItem; value: string; onChange: (v: string) => void }) {
  const id = `plugin-ui-${it.name}`;
  if (it.kind === "toggle")
    return (
      <div className="flex items-center gap-3 px-4 py-2.5">
        <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{it.label}</span>
        <Switch on={value === "true"} onChange={(on) => onChange(on ? "true" : "false")} label={it.label || it.name || ""} />
      </div>
    );
  const problem = it.kind === "number" ? numberProblem(value) : "";
  let control: React.ReactNode;
  if (it.kind === "choice") {
    const choices = it.choices ?? [];
    control =
      choices.length <= 4 ? (
        <div id={id} role="radiogroup" aria-label={it.label} className="flex flex-wrap gap-1.5">
          {choices.map((c) => {
            const on = value === c.value;
            return (
              <button
                key={c.value}
                type="button"
                role="radio"
                aria-checked={on}
                onClick={() => onChange(c.value)}
                className={cx(
                  "flex h-7 items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 text-[12px] transition-[background-color,box-shadow,color] duration-150",
                  on ? "bg-accent-tint font-medium text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "text-ink-2 shadow-hairline hover:bg-hover",
                )}
              >
                {on && <Icon name="check" size={12} stroke={2.2} />}
                {c.title || c.value}
              </button>
            );
          })}
        </div>
      ) : (
        <select id={id} value={value} onChange={(e) => onChange(e.target.value)} className={cx(inputClass, "max-w-[320px]")}>
          {choices.map((c) => (
            <option key={c.value} value={c.value}>
              {c.title || c.value}
            </option>
          ))}
        </select>
      );
  } else {
    control = (
      <input
        id={id}
        type={it.kind === "secret" ? "password" : "text"}
        inputMode={it.kind === "number" ? "decimal" : undefined}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={it.placeholder}
        spellCheck={false}
        autoComplete={it.kind === "secret" ? "new-password" : "off"}
        autoCapitalize="off"
        className={cx(inputClass, it.kind === "number" && "max-w-[200px] font-mono", it.kind === "secret" && "font-mono", problem && "shadow-[inset_0_0_0_1px_var(--red)]")}
      />
    );
  }
  return (
    <div className="px-4 py-2.5">
      <label htmlFor={id} className="mb-1.5 block text-[12px] font-medium text-ink-2">
        {it.label}
      </label>
      {control}
      {problem && (
        <p role="alert" className="mt-1 text-[11.5px] text-red">
          {problem}
        </p>
      )}
    </div>
  );
}

/**
 * The plugins' sections on a launcher's page (placement launcher_section): one per plugin whose gateway matches. A
 * plugin that says this launcher is not its gateway (404 unsupported) leaves no trace; nothing listed, nothing drawn.
 */
export function LauncherPluginSections({ launcher, machine }: { launcher: string; machine?: string }) {
  const [places, setPlaces] = useState<UIPlace[]>([]);
  useEffect(() => {
    let live = true;
    setPlaces([]);
    uiPlaces("launcher_section", launcher, machine).then(
      (l) => live && setPlaces(l),
      () => {}, // an older hub, or no plugins: no section
    );
    return () => {
      live = false;
    };
  }, [launcher, machine]);
  return (
    <>
      {places.map((u) => (
        <LauncherSection key={`${u.plugin}/${u.id}`} place={u} launcher={launcher} machine={machine} />
      ))}
    </>
  );
}

function LauncherSection({ place, launcher, machine }: { place: UIPlace; launcher: string; machine?: string }) {
  const [gone, setGone] = useState(false);
  const context = useMemo(() => ({ launcher, machine }), [launcher, machine]);
  if (gone) return null;
  return (
    <div data-plugin-section={`${place.plugin}/${place.id}`}>
      <div className="mb-1 flex items-center gap-1.5 text-[11.5px] font-medium text-ink-3">
        <Icon name="plugin" size={11} className="shrink-0" />
        {place.title}
      </div>
      <PluginUI plugin={place.plugin} ui={place.id} context={context} embedded onUnsupported={() => setGone(true)} />
    </div>
  );
}
