// Persona's persona in the page (docs/persona.md, the app's PersonaStore / PersonaViews / PersonaPicker): the packages the
// hub offers, the one chosen in this browser, and its face (a still avatar) and dot (the state's APNG, which the
// browser animates by itself). The default is 团子 (MochiFace), also while a chosen package is not there (an older hub,
// an empty list); with the page's mark chosen (the default before 2026-11-03) every place keeps what it showed before:
// the views take that as `fallback`.
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api, ApiError, errorText, getToken } from "../../lib/api";
import {
  avatarFile, BUILTIN_PERSONA, dotFile, isBuiltinPersona, MARK_PERSONA, PERSONA_STATES, personaFileURL, personaFiles, usablePersonas,
  withChosenReady, type PersonaInfo, type PersonaState,
} from "../../lib/persona";
import { onHubEvent } from "../../lib/store";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { MochiFace } from "./MochiFace";
import { t } from "../../i18n";

const CHOSEN_KEY = "jarvis.persona";

interface PersonaStoreState {
  list: PersonaInfo[];
  loaded: boolean;
  error: string;
  chosen: string;
}

let ps: PersonaStoreState = { list: [], loaded: false, error: "", chosen: readChosen() };
const listeners = new Set<() => void>();
let loading: Promise<void> | null = null;

function readChosen() {
  try {
    return localStorage.getItem(CHOSEN_KEY) || BUILTIN_PERSONA;
  } catch {
    return BUILTIN_PERSONA;
  }
}

function setPS(patch: Partial<PersonaStoreState>) {
  ps = { ...ps, ...patch };
  listeners.forEach((l) => l());
}

/** What the hub offers now (each time the picker opens, once per page load, and when the hub says the list changed). */
export function refreshPersonas(): Promise<void> {
  if (loading) return loading;
  loading = api<{ personas: PersonaInfo[] }>("/personas")
    .then(async (r) => {
      const list = usablePersonas(r.personas);
      const next = list.find((p) => p.id === ps.chosen);
      const shown = ps.list.find((p) => p.id === ps.chosen);
      const ready = !next || !shown || next.rev === shown.rev || (await preload(next));
      setPS({ list: withChosenReady(list, ps.list, ps.chosen, ready), loaded: true, error: "" });
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

// A tiered look moved to another tier (docs/persona.md): list again, the new tier loaded before it shows.
onHubEvent("personas", () => void refreshPersonas());

/** Choosing one keeps it when its package leaves the hub (the built-in shows meanwhile), as on the phone. */
export function choosePersona(id: string) {
  try {
    if (id === BUILTIN_PERSONA) localStorage.removeItem(CHOSEN_KEY);
    else localStorage.setItem(CHOSEN_KEY, id);
  } catch {
    // private mode: this page only
  }
  setPS({ chosen: id });
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
    if (!ps.loaded && !isBuiltinPersona(ps.chosen)) void refreshPersonas();
  }, []);
  return s;
}

export interface CurrentPersona {
  /** The package on screen; null: a built-in. */
  info: PersonaInfo | null;
  /** 团子 is on screen (the default, or a chosen package that is not there); false with a package or the mark. */
  mochi: boolean;
  name: string;
  chosen: string;
}

/** The persona on screen: the chosen package once the hub listed it, else the built-in named Persona. */
export function usePersona(): CurrentPersona {
  const s = usePersonaStore();
  const info = isBuiltinPersona(s.chosen) ? null : (s.list.find((p) => p.id === s.chosen) ?? null);
  return { info, mochi: !info && s.chosen !== MARK_PERSONA, name: info?.manifest.name ?? "Persona", chosen: s.chosen };
}

function useReducedMotion(): boolean {
  const [reduce, setReduce] = useState(() => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches);
  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const q = matchMedia("(prefers-reduced-motion: reduce)");
    const on = () => setReduce(q.matches);
    q.addEventListener("change", on);
    return () => q.removeEventListener("change", on);
  }, []);
  return reduce;
}

/** The page's own mark at a size (the same as 设置 › 助手): the built-in before 团子, still a choice. */
export function BuiltinFace({ size }: { size: number }) {
  return (
    <span className="flex shrink-0 items-center justify-center rounded-full bg-ink text-surface" style={{ width: size, height: size }}>
      <Icon name="spark" size={Math.round(size * 0.4)} />
    </span>
  );
}

/**
 * A still face in a circle: the package's avatar at the size it needs; 团子 when it is the one on screen; else `fallback`
 * (what the place showed before, for the page's mark).
 */
export function PersonaAvatar({ size, persona, fallback, className }: { size: number; persona?: PersonaInfo | null; fallback: React.ReactNode; className?: string }) {
  const current = usePersona();
  const p = persona === undefined ? current.info : persona;
  const [broken, setBroken] = useState(false);
  const file = p ? avatarFile(p.manifest, size * Math.max(2, Math.ceil(typeof devicePixelRatio === "number" ? devicePixelRatio : 2))) : undefined;
  useEffect(() => setBroken(false), [p?.id, p?.rev]);
  if (!p || !file || broken) return persona === undefined && current.mochi ? <MochiFace size={size} className={className} /> : <>{fallback}</>;
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
 * which says the state by itself (a canvas draws an animated image's first frame). 团子 for the default, `fallback`
 * for the page's mark.
 */
export function PersonaDot({ size, state, persona, fallback, className }: { size: number; state: PersonaState; persona?: PersonaInfo | null; fallback: React.ReactNode; className?: string }) {
  const current = usePersona();
  const p = persona === undefined ? current.info : persona;
  const still = useReducedMotion();
  const [broken, setBroken] = useState(false);
  const file = p ? dotFile(p.manifest, state) : undefined;
  const url = p && file ? personaFileURL(p, file, getToken()) : "";
  useEffect(() => setBroken(false), [url]);
  if (!p || !file || broken) return persona === undefined && current.mochi ? <MochiFace size={size} state={state} className={className} /> : <>{fallback}</>;
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
 * 形象 (设置 › 通用 and the profile page; the app's 设置 › 外观 › 形象): 团子 (the default), the page's mark, then the hub's
 * packages, and the chosen one's dot in each state. `builtin` is the mark's face. Choosing changes the name, face and dot in this browser; the page's colors stay.
 */
export function PersonaPicker({ builtin }: { builtin: React.ReactNode }) {
  const s = usePersonaStore();
  const current = usePersona();
  useEffect(() => {
    void refreshPersonas();
  }, []);
  const cards: { id: string; title: string; sub: React.ReactNode; face: React.ReactNode }[] = [
    { id: BUILTIN_PERSONA, title: t("assistant-persona.mochi"), sub: <span className="text-[11px] text-ink-3">{t("assistant-persona.default_avatar")}</span>, face: <MochiFace size={56} /> },
    { id: MARK_PERSONA, title: t("assistant-persona.mark"), sub: <span className="text-[11px] text-ink-3">{t("assistant-persona.mark_sub")}</span>, face: builtin },
    ...s.list.map((p) => ({
      id: p.id,
      title: p.manifest.name,
      sub: p.manifest.about ? <span className="max-w-full truncate text-[11px] whitespace-nowrap text-ink-3">{p.manifest.about}</span> : <Swatches p={p} />,
      face: <PersonaAvatar size={56} persona={p} fallback={<MochiFace size={56} />} />,
    })),
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
              aria-label={c.id === BUILTIN_PERSONA ? t("assistant-persona.mochi_default") : c.title}
              onClick={() => choosePersona(c.id)}
              className={cx(
                "flex flex-col items-center gap-2 rounded-card px-2 pt-3.5 pb-2.5 transition-[background-color,box-shadow] duration-150",
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
      {(current.info || current.mochi) && (
        <div className="grid grid-cols-5 gap-1 rounded-card bg-inset py-2.5 shadow-hairline" aria-label={t("assistant-persona.states_label", { name: current.name })} role="img">
          {PERSONA_STATES.map((st) => (
            <span key={st.id} className="flex flex-col items-center gap-1">
              <PersonaDot size={40} state={st.id} fallback={null} />
              <span className="text-[11px] whitespace-nowrap text-ink-3">{st.title}</span>
            </span>
          ))}
        </div>
      )}
      {missing && <p className="px-1 text-[11.5px] text-ink-3">{t("assistant-persona.missing")}</p>}
      {s.error && <p role="alert" className="px-1 text-[11.5px] text-orange">{s.error}</p>}
      {s.loaded && !s.error && !s.list.length && <p className="px-1 text-[11.5px] leading-relaxed text-ink-3">{t("assistant-persona.none")}</p>}
    </div>
  );
}
