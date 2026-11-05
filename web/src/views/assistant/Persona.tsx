// Persona's look in the page (形象, docs/persona.md, docs/extensions.md「形象」; the app's PersonaStore / PersonaViews /
// PersonaPicker): the looks the hub offers (GET /looks: the built-in ones, its packages and the extensions'), the one
// chosen — the user's, kept on the hub so every device shows it — and its face (a still avatar) and dot (the state's
// APNG, which the browser animates by itself). The built-in ones the page draws: 团子 (the default, MochiFace), 面罩 and
// 汇流 (BuiltinFaces); the default shows too while a chosen package is not there. An older hub without /looks gets the
// packages from /personas and the choice stays this browser's.
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api, ApiError, errorText, getToken } from "../../lib/api";
import {
  avatarFile, BUILTIN_PERSONA, CIRCUIT_PERSONA, dotFile, isBuiltinPersona, localized, lookPackages, MARK_PERSONA, PERSONA_STATES, personaFileURL,
  personaFiles, usablePersonas, withChosenReady, type LookCurrent, type LookView, type PersonaInfo, type PersonaState,
} from "../../lib/persona";
import { onHubEvent } from "../../lib/store";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { MochiFace } from "./MochiFace";
import { useReducedMotion } from "./face-motion";
import { CircuitFace, VisorFace } from "./BuiltinFaces";
import { personaAccent, setPersonaAccent } from "../../ui/look";
import { currentLanguage, t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { BASE } from "../../lib/base";
import { navigate } from "../../lib/router";
import { settingsHref } from "../settings/nav";
import { setLookIcon } from "../../lib/look-icon";

const CHOSEN_KEY = "jarvis.persona";

interface PersonaStoreState {
  list: PersonaInfo[];
  /** GET /looks's list; null on a hub without it. */
  looks: LookView[] | null;
  loaded: boolean;
  error: string;
  chosen: string;
}

let ps: PersonaStoreState = { list: [], looks: null, loaded: false, error: "", chosen: readChosen() };
const listeners = new Set<() => void>();
let loading: Promise<void> | null = null;
/** Whether the hub keeps the choice (it answered /looks): then choosing goes there. */
let synced = false;

function readChosen() {
  try {
    return localStorage.getItem(CHOSEN_KEY) || BUILTIN_PERSONA;
  } catch {
    return BUILTIN_PERSONA;
  }
}

/** Whether this browser chose a look itself before the hub kept the choice (what moves up to the hub). */
function choseHere(): string | null {
  try {
    return localStorage.getItem(CHOSEN_KEY);
  } catch {
    return null;
  }
}

function saveChosen(id: string) {
  try {
    if (id === BUILTIN_PERSONA) localStorage.removeItem(CHOSEN_KEY);
    else localStorage.setItem(CHOSEN_KEY, id);
  } catch {
    // private mode: this page only
  }
}

function setPS(patch: Partial<PersonaStoreState>) {
  ps = { ...ps, ...patch };
  // 主题色「跟随形象」(ui/look.ts) keeps the chosen persona's accent, from the last list the hub gave; while there is no
  // list yet (or the last load failed) and the chosen one is not in it, the saved accent stays.
  const shown = ps.list.find((p) => p.id === ps.chosen);
  if (isBuiltinPersona(ps.chosen)) setPersonaAccent(null);
  else if (shown || (ps.loaded && !ps.error)) setPersonaAccent(personaAccent(shown?.manifest.colors));
  setLookIcon(shown ?? null, isBuiltinPersona(ps.chosen) ? ps.chosen : BUILTIN_PERSONA);
  listeners.forEach((l) => l());
}

/** The choice to keep given the hub's: the hub's once it has one; else this browser's moves up (once, if unset there). */
async function settleChoice(cur: LookCurrent): Promise<string> {
  if (cur.id) return cur.id;
  const mine = choseHere();
  if (!mine) return BUILTIN_PERSONA;
  try {
    const r = await api<LookCurrent>("/looks/current", { method: "PUT", json: { id: mine, if_unset: true } });
    return r.id ?? BUILTIN_PERSONA;
  } catch {
    // the hub does not know it (the page's old mark, a package gone): the default
    return BUILTIN_PERSONA;
  }
}

/** What the hub offers now (each time the picker opens, once per page load, and when the hub says the list changed). */
export function refreshPersonas(): Promise<void> {
  if (loading) return loading;
  loading = api<{ looks: LookView[]; current: LookCurrent }>("/looks")
    .then(async (r) => {
      synced = true;
      return { list: usablePersonas(lookPackages(r.looks)), looks: r.looks, chosen: await settleChoice(r.current) };
    })
    .catch(async (e) => {
      if (!(e instanceof ApiError && e.status === 404)) throw e;
      synced = false; // an older hub: its packages, this browser's choice
      const r = await api<{ personas: PersonaInfo[] }>("/personas");
      return { list: usablePersonas(r.personas), looks: null, chosen: ps.chosen };
    })
    .then(async ({ list, looks, chosen }) => {
      const next = list.find((p) => p.id === chosen);
      const shown = ps.list.find((p) => p.id === chosen);
      const ready = !next || !shown || next.rev === shown.rev || (await preload(next));
      saveChosen(chosen);
      setPS({ list: withChosenReady(list, ps.list, chosen, ready), looks, chosen, loaded: true, error: "" });
    })
    .catch((e) => setPS({ loaded: true, error: e instanceof ApiError && e.status === 404 ? t("assistant-persona.hub_unsupported") : t("assistant-persona.load_failed", { error: errorText(e) }) }))
    .finally(() => {
      loading = null;
    });
  return loading;
}

/** Every file of the package in the browser's cache before it is shown; false when one did not come. */
function preload(p: PersonaInfo): Promise<boolean> {
  const token = getToken();
  return Promise.all(
    personaFiles(p.manifest).map(
      (f) =>
        new Promise<boolean>((done) => {
          const img = new Image();
          img.onload = () => done(true);
          img.onerror = () => done(false);
          img.src = personaFileURL(p, f, token);
        }),
    ),
  ).then((all) => all.every(Boolean));
}

// The list changed (a tiered look moved, an extension with looks came or went): list again, the new files loaded first.
onHubEvent("personas", () => void refreshPersonas());
// Another device chose a look (or the hub went back to the default when its extension went).
onHubEvent("look", (m) => {
  const id = typeof m.id === "string" ? m.id : "";
  if (!id || id === ps.chosen) return;
  saveChosen(id);
  if (!isBuiltinPersona(id) && !ps.list.some((p) => p.id === id)) void refreshPersonas();
  setPS({ chosen: id });
});

/** Choosing one keeps it when its package leaves the hub (the built-in shows meanwhile), as on the phone; on a hub that
 *  keeps the choice, every device follows. */
export function choosePersona(id: string) {
  saveChosen(id);
  setPS({ chosen: id });
  if (synced) void api("/looks/current", { method: "PUT", json: { id } }).catch(() => undefined);
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

function usePersonaStore(): PersonaStoreState {
  const s = useSyncExternalStore(subscribe, () => ps);
  useEffect(() => {
    if (!ps.loaded && !loading) void refreshPersonas();
  }, []);
  return s;
}

export interface CurrentPersona {
  /** The package on screen; null: a built-in. */
  info: PersonaInfo | null;
  /** The built-in look on screen (the chosen one, or the default while a chosen package is not there); null with a package. */
  builtin: string | null;
  /** 团子 is on screen. */
  mochi: boolean;
  name: string;
  chosen: string;
}

/** The look on screen: the chosen package once the hub listed it, else the built-in (named Persona). */
export function usePersona(): CurrentPersona {
  const s = usePersonaStore();
  const info = isBuiltinPersona(s.chosen) ? null : (s.list.find((p) => p.id === s.chosen) ?? null);
  const builtin = info ? null : isBuiltinPersona(s.chosen) ? s.chosen : BUILTIN_PERSONA;
  const name = info ? localized(info.manifest.name, info.manifest.names, currentLanguage()) : "Persona";
  return { info, builtin, mochi: builtin === BUILTIN_PERSONA, name, chosen: s.chosen };
}

/** Whether the page is dark now (the theme's class on <html>), for the dark avatar. */
export function useDarkPage(): boolean {
  const read = () => typeof document !== "undefined" && document.documentElement.classList.contains("dark");
  const [dark, setDark] = useState(read);
  useEffect(() => {
    if (typeof MutationObserver === "undefined") return;
    const o = new MutationObserver(() => setDark(read()));
    o.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => o.disconnect();
  }, []);
  return dark;
}

/** A built-in look drawn at a size and state. */
export function BuiltinLookFace({ id, size, state = "idle", className }: { id: string; size: number; state?: PersonaState; className?: string }) {
  if (id === MARK_PERSONA) return <VisorFace size={size} state={state} className={className} />;
  if (id === CIRCUIT_PERSONA) return <CircuitFace size={size} state={state} className={className} />;
  return <MochiFace size={size} state={state} className={className} />;
}

/**
 * A still face in a circle: the package's avatar at the size it needs (its SVG, its dark one on a dark page), else the
 * built-in look on screen. With `persona` given (a card in the picker), that package's, or `fallback` when its file
 * does not load.
 */
export function PersonaAvatar({ size, persona, fallback, className }: { size: number; persona?: PersonaInfo | null; fallback?: React.ReactNode; className?: string }) {
  const current = usePersona();
  const dark = useDarkPage();
  const p = persona === undefined ? current.info : persona;
  const [broken, setBroken] = useState(false);
  const file = p ? avatarFile(p.manifest, size * Math.max(2, Math.ceil(typeof devicePixelRatio === "number" ? devicePixelRatio : 2)), dark) : undefined;
  useEffect(() => setBroken(false), [p?.id, p?.rev]);
  if (!p || !file || broken) return persona === undefined ? <BuiltinLookFace id={current.builtin ?? BUILTIN_PERSONA} size={size} className={className} /> : <>{fallback}</>;
  return (
    <img
      src={personaFileURL(p, file, getToken())}
      alt=""
      aria-hidden
      draggable={false}
      onError={() => setBroken(true)}
      width={size}
      height={size}
      className={cx("shrink-0 rounded-full object-cover", className)}
      style={{ width: size, height: size }}
    />
  );
}

/**
 * The face moving for a state (the top bar's dot): the state's frames, looping. With reduced motion only the first frame,
 * which says the state by itself (a canvas draws an animated image's first frame). The built-in look drawn in that
 * state when the look on screen is one; `fallback` for a given package whose frames do not load.
 */
export function PersonaDot({ size, state, persona, fallback, className }: { size: number; state: PersonaState; persona?: PersonaInfo | null; fallback?: React.ReactNode; className?: string }) {
  const current = usePersona();
  const p = persona === undefined ? current.info : persona;
  const still = useReducedMotion();
  const [broken, setBroken] = useState(false);
  const file = p ? dotFile(p.manifest, state) : undefined;
  const url = p && file ? personaFileURL(p, file, getToken()) : "";
  useEffect(() => setBroken(false), [url]);
  if (!p || !file || broken) return persona === undefined ? <BuiltinLookFace id={current.builtin ?? BUILTIN_PERSONA} size={size} state={state} className={className} /> : <>{fallback}</>;
  if (still) return <StillFrame url={url} size={size} className={className} onError={() => setBroken(true)} />;
  return (
    <img
      src={url}
      alt=""
      aria-hidden
      draggable={false}
      onError={() => setBroken(true)}
      width={size}
      height={size}
      className={cx("shrink-0 object-contain", className)}
      style={{ width: size, height: size }}
    />
  );
}

function StillFrame({ url, size, className, onError }: { url: string; size: number; className?: string; onError: () => void }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const img = new Image();
    let live = true;
    img.onload = () => {
      const c = ref.current;
      if (!live || !c) return;
      const k = Math.max(1, Math.ceil(typeof devicePixelRatio === "number" ? devicePixelRatio : 1));
      c.width = size * k;
      c.height = size * k;
      const g = c.getContext("2d");
      g?.clearRect(0, 0, c.width, c.height);
      g?.drawImage(img, 0, 0, c.width, c.height);
    };
    img.onerror = () => live && onError();
    img.src = url;
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, size]);
  return <canvas ref={ref} aria-hidden className={cx("shrink-0", className)} style={{ width: size, height: size }} />;
}

/** The palette's colors as small dots (a card in the picker); they are the package's own, data rather than page tokens. */
function Swatches({ p }: { p: PersonaInfo }) {
  const c = p.manifest.colors?.light ?? p.manifest.colors?.dark;
  const keys = ["primary", "secondary", "accent", "bubble"].filter((k) => c?.[k]);
  if (!keys.length) return <span className="text-[11px] text-ink-3">{t("assistant-persona.no_colors")}</span>;
  return (
    <span className="flex items-center gap-1" aria-hidden>
      {keys.map((k) => (
        <span key={k} className="size-3 rounded-full shadow-hairline" style={{ background: c![k] }} />
      ))}
    </span>
  );
}

/**
 * 形象 (设置 › 通用 and the profile page; the app's 设置 › 外观 › 形象): the one setting for Persona's face, name, the
 * page's mark and icon (#3200): the built-in looks (团子 the default, 面罩, 汇流), then the hub's packages and the
 * extensions' looks, and the chosen one's dot in each state. On a hub that keeps the choice every device follows it; the
 * page's colors follow it only with 主题色「跟随形象」 (ui/look.ts).
 */
/**
 * What the picker says when the hub has no looks beyond the built-in ones: where to get one, with 设置 › 扩展 as a link
 * (#3597). onExtensions opens it in place (设置 keeps its own tab state); without it the link goes to the page.
 */
export function NoOtherLooks({ onExtensions }: { onExtensions?: () => void }) {
  const to = settingsHref("extensions", null, false);
  const open = (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    if (onExtensions) onExtensions();
    else navigate(to);
  };
  return (
    <p className="px-1 text-[11.5px] leading-relaxed text-ink-3">
      {tRich("assistant-persona.none", {
        extensions: (
          <a href={BASE + to} onClick={open} className="text-accent hover:underline">
            {t("assistant-persona.none_extensions")}
          </a>
        ),
      })}
    </p>
  );
}

export function PersonaPicker({ onExtensions }: { onExtensions?: () => void } = {}) {
  const s = usePersonaStore();
  const current = usePersona();
  const lang = currentLanguage();
  useEffect(() => {
    void refreshPersonas();
  }, []);
  const sub = (text: string) => <span className="max-w-full truncate text-[11px] whitespace-nowrap text-ink-3">{text}</span>;
  const builtins: { id: string; title: string; sub: string }[] = [
    { id: BUILTIN_PERSONA, title: t("assistant-persona.mochi"), sub: t("assistant-persona.default_avatar") },
    { id: MARK_PERSONA, title: t("assistant-persona.visor"), sub: t("assistant-persona.follows_theme") },
    { id: CIRCUIT_PERSONA, title: t("assistant-persona.circuit"), sub: t("assistant-persona.follows_theme") },
  ];
  const lookOf = (id: string) => s.looks?.find((l) => l.id === id);
  const cards: { id: string; title: string; label: string; sub: React.ReactNode; face: React.ReactNode }[] = [
    ...builtins.map((b) => ({ id: b.id, title: b.title, label: b.id === BUILTIN_PERSONA ? t("assistant-persona.mochi_default") : b.title, sub: sub(b.sub), face: <BuiltinLookFace id={b.id} size={56} /> })),
    ...s.list.map((p) => {
      const title = localized(p.manifest.name, p.manifest.names, lang);
      const from = lookOf(p.id)?.extension_name ?? p.manifest.source?.extension_name;
      return {
        id: p.id,
        title,
        label: title,
        sub: p.manifest.about ? sub(p.manifest.about) : from ? sub(t("assistant-persona.from_extension", { name: from })) : <Swatches p={p} />,
        face: <PersonaAvatar size={56} persona={p} fallback={<MochiFace size={56} />} />,
      };
    }),
  ];
  const missing = !isBuiltinPersona(s.chosen) && s.loaded && !s.list.some((p) => p.id === s.chosen);
  return (
    <div className="flex flex-col gap-3 p-3">
      <div role="radiogroup" aria-label={t("assistant-persona.avatar")} className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
        {cards.map((c) => {
          const selected = c.id === s.chosen;
          return (
            <button
              key={c.id}
              type="button"
              role="radio"
              aria-checked={selected}
              aria-label={c.label}
              onClick={() => choosePersona(c.id)}
              className={cx(
                "flex min-w-0 flex-col items-center gap-2 rounded-card px-2 pt-3.5 pb-2.5 transition-[background-color,box-shadow] duration-150",
                selected ? "bg-accent-tint shadow-[inset_0_0_0_1.5px_var(--accent)]" : "bg-inset shadow-hairline hover:bg-hover",
              )}
            >
              <span className="flex h-14 items-center justify-center">{c.face}</span>
              <span className="flex max-w-full items-center gap-1 text-[12.5px] font-medium whitespace-nowrap text-ink">
                {selected && <Icon name="check" size={12} stroke={2.6} className="shrink-0 text-accent-ink" />}
                <span className="truncate">{c.title}</span>
              </span>
              {c.sub}
            </button>
          );
        })}
      </div>
      <div className="grid grid-cols-5 gap-1 rounded-card bg-inset py-2.5 shadow-hairline" aria-label={t("assistant-persona.states_label", { name: current.name })} role="img">
        {PERSONA_STATES.map((st) => (
          <span key={st.id} className="flex flex-col items-center gap-1">
            <PersonaDot size={40} state={st.id} />
            <span className="text-[11px] whitespace-nowrap text-ink-3">{st.title}</span>
          </span>
        ))}
      </div>
      {missing && <p className="px-1 text-[11.5px] text-ink-3">{t("assistant-persona.missing")}</p>}
      {s.error && <p role="alert" className="px-1 text-[11.5px] text-orange">{s.error}</p>}
      {s.loaded && !s.error && !s.list.length && <NoOtherLooks onExtensions={onExtensions} />}
    </div>
  );
}
