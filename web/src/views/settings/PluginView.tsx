// A plugin's View (docs/plugins.md「声明式 UI」), drawn with the web's design system: a plugin page in 设置 › 插件, or a
// section on a launcher's page. Every kind of item (text, metric, progress, row, button, link, field, divider); a tone
// becomes a role color; a button's confirm asks first, submit sends the fields' values, and what the press answers
// (a new view, a word, a link) is carried out. Logic in lib/plugin-ui.ts.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import { openLink } from "../../lib/links";
import {
  actUI, blocks, fieldValues, isHttps, isUnsupported, numberProblem, outcome, percent, renderUI, submitValues, toneOf, uiPlaces,
  type UIAction, type UIContext, type UIItem, type UIPlace, type UISection, type UITone, type UIView,
} from "../../lib/plugin-ui";
import { Button, cx, Dialog, EASE, Loading, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Pill, Switch } from "./SettingsParts";

const TEXT: Record<UITone, string> = { neutral: "text-ink", positive: "text-green", caution: "text-orange", danger: "text-red" };
const BAR: Record<UITone, string> = { neutral: "bg-accent", positive: "bg-green", caution: "bg-orange", danger: "bg-red" };
const BADGE: Record<UITone, "green" | "orange" | "red" | undefined> = { neutral: undefined, positive: "green", caution: "orange", danger: "red" };

const inputClass =
  "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

/**
 * Loads and draws one of a plugin's views. `embedded`: inside another page (a launcher's), lighter, and gone when the
 * plugin says the launcher is not its (404 unsupported) instead of showing an error.
 */
export function PluginUI({ plugin, ui, context, embedded, title, onUnsupported }: { plugin: string; ui: string; context?: UIContext; embedded?: boolean; title?: string; onUnsupported?: () => void }) {
  const [view, setView] = useState<UIView | null>(null);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [loading, setLoading] = useState(false);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [confirming, setConfirming] = useState<{ action: UIAction; destructive: boolean } | null>(null);
  const stateRef = useRef<string | undefined>(undefined);
  const ctxKey = `${context?.launcher ?? ""}\n${context?.machine ?? ""}`;

  const show = (v: UIView) => {
    setView(v);
    setValues(fieldValues(v));
    stateRef.current = v.state;
    setError("");
  };
  const load = useCallback(() => {
    setLoading(true);
    renderUI(plugin, ui, context, stateRef.current)
      .then(show)
      .catch((e) => (isUnsupported(e) ? (setUnsupported(true), onUnsupported?.()) : setError(errorText(e))))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin, ui, ctxKey]);
  useEffect(load, [load]);

  const run = async (action: UIAction) => {
    const sent = action.submit ? submitValues(view, values) : undefined;
    if (sent && view) {
      const bad = view.sections.flatMap((s) => s.items).find((it) => it.type === "field" && it.kind === "number" && it.name && numberProblem(sent[it.name] ?? ""));
      if (bad) {
        toast(`「${bad.label || bad.name}」要是一个数`, "error");
        return;
      }
    }
    setBusy(action.id);
    try {
      const r = outcome(await actUI(plugin, ui, action.id, { context, state: stateRef.current, values: sent }));
      if (r.view) show(r.view);
      if (r.toast) toast(r.toast, "ok");
      if (r.url) openLink(r.url);
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

  if (unsupported) {
    return embedded ? null : (
      <p className="rounded-card bg-surface px-4 py-6 text-center text-[12.5px] text-ink-3 shadow-card">这个插件在这里没有要显示的内容。</p>
    );
  }

  const heading = view?.title || title;
  return (
    <div className={cx("flex flex-col", embedded ? "gap-3" : "gap-6")}>
      {!embedded && heading && (
        <div className="flex items-center gap-2 px-1">
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{heading}</h2>
          {loading && view && <Spinner />}
        </div>
      )}
      {!view && !error && (
        <div className={cx(embedded ? "py-2" : "rounded-card bg-surface px-4 py-3 shadow-card")}>
          <Loading />
        </div>
      )}
      {error && (
        <div className={cx("flex flex-col items-center gap-2 px-6 text-center", embedded ? "rounded-control bg-inset py-4 shadow-hairline" : "rounded-card bg-surface py-8 shadow-card")}>
          <span className="flex size-9 items-center justify-center rounded-[10px] bg-red-tint text-red">
            <Icon name="plugin" size={16} />
          </span>
          <p className="text-[13px] font-medium text-ink">{view ? "没有更新" : "插件没画出来"}</p>
          <p className="max-w-[420px] text-[12px] leading-relaxed break-words text-ink-3 select-text">{error}</p>
          <Button tone="secondary" icon="retry" busy={loading} onClick={load}>
            重试
          </Button>
        </div>
      )}
      {view?.sections.map((s, i) => (
        <Section key={i} section={s} embedded={embedded} values={values} setValue={(k, v) => setValues((x) => ({ ...x, [k]: v }))} busy={busy} onPress={press} />
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
  values: Record<string, string>;
  setValue: (name: string, v: string) => void;
  busy: string;
  onPress: (a: UIAction, destructive?: boolean) => void;
}

function Section({ section, embedded, ...rest }: SectionProps) {
  const bs = blocks(section.items ?? []);
  return (
    <section style={{ animation: `fade-up 280ms ${EASE} both` }}>
      {section.title && <h3 className={cx("mb-1.5 px-1 font-medium text-ink-3", embedded ? "text-[11.5px]" : "text-[12px]")}>{section.title}</h3>}
      <div className={cx("overflow-hidden", embedded ? "rounded-control bg-inset shadow-hairline" : "rounded-card bg-surface shadow-card")}>
        {bs.map((b, i) => {
          // rows that follow each other read as a list: a hairline between them
          const afterRow = i > 0 && b.kind === "item" && b.item.type === "row" && bs[i - 1].kind === "item" && (bs[i - 1] as { item: UIItem }).item.type === "row";
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
              <Item item={b.item} {...rest} />
            </div>
          );
        })}
      </div>
      {section.footer && <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">{section.footer}</p>}
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

function Item({ item: it, values, setValue, busy, onPress }: Omit<SectionProps, "section" | "embedded"> & { item: UIItem }) {
  switch (it.type) {
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
