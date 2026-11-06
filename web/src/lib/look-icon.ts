// The look's icon (#3200, docs/extensions.md "Looks"): the picture the page's mark (AppMark: the sidebar's header, the
// sign-in page) and the tab's favicon show for the chosen look. Mochi, the default, is the app icon itself (the files
// in public/); another look is its avatar (a package's PNG or SVG, light and dark) or the built-in face drawn, made
// into a 128 px PNG. The last one is kept in localStorage, so the sign-in page and the first paint show it before the
// hub answers. The installed app's icon (the PWA's manifest) stays the app icon: it is fixed when the app is installed.
import { useSyncExternalStore } from "react";
import { createElement } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { getToken } from "./api";
import { setFaviconBase } from "./attention-signals";
import { avatarFile, BUILTIN_PERSONA, personaFileURL, type PersonaInfo } from "./persona";

const KEY = "persona.look-icon";
const SIDE = 128;

export interface LookIcon {
  /** What it is made from (the look and its rev), so the same look is not drawn again. */
  key: string;
  light: string;
  dark: string;
}

let icon: LookIcon | null = read();
const listeners = new Set<() => void>();
let seq = 0;

function read(): LookIcon | null {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "null");
    return v && typeof v.light === "string" && typeof v.dark === "string" && v.light.startsWith("data:image/") && v.dark.startsWith("data:image/") ? v : null;
  } catch {
    return null;
  }
}

function set(next: LookIcon | null) {
  icon = next;
  try {
    if (next) localStorage.setItem(KEY, JSON.stringify(next));
    else localStorage.removeItem(KEY);
  } catch {
    // private mode: this page only
  }
  if (typeof document !== "undefined") setFaviconBase(next && { light: next.light, dark: next.dark });
  listeners.forEach((l) => l());
}

/** The picture at src as a SIDE px PNG (an SVG drawn at that size). */
function rasterize(src: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      const c = document.createElement("canvas");
      c.width = c.height = SIDE;
      const g = c.getContext("2d");
      if (!g) return reject(new Error("canvas"));
      g.drawImage(img, 0, 0, SIDE, SIDE);
      try {
        resolve(c.toDataURL("image/png"));
      } catch (e) {
        reject(e);
      }
    };
    img.onerror = () => reject(new Error("icon"));
    img.src = src;
  });
}

/** A built-in face as an SVG data URL, the page's accent written in (a picture outside the page has no CSS variables). */
async function builtinSVG(id: string, dark: boolean): Promise<string> {
  const { BuiltinLookFace } = await import("../views/assistant/Persona");
  const div = document.createElement("div");
  if (dark) div.className = "dark";
  const root = createRoot(div);
  flushSync(() => root.render(createElement(BuiltinLookFace, { id, size: SIDE })));
  let svg = div.innerHTML;
  root.unmount();
  const css = getComputedStyle(document.documentElement);
  const accent = css.getPropertyValue("--accent").trim() || "#4f6bed";
  // On its own (an image, not in the page) an SVG needs its namespace, which React leaves out.
  svg = svg.replace("<svg ", '<svg xmlns="http://www.w3.org/2000/svg" ').replaceAll("var(--accent)", accent).replaceAll("var(--mochi-needs-you)", "#e5484d");
  return "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
}

/**
 * The icon for the look on screen: a package (`info`), else the built-in one (`builtin`). Mochi is the app icon's files:
 * nothing to draw. A picture that does not load leaves the last icon as it is.
 */
export function setLookIcon(info: PersonaInfo | null, builtin: string): void {
  if (typeof document === "undefined" || typeof Image === "undefined") return;
  // A built-in face is in the theme's accent: another accent, another icon.
  const accent = getComputedStyle(document.documentElement).getPropertyValue("--accent").trim();
  const key = info ? `${info.id}@${info.rev}` : `${builtin}@${accent}`;
  if (!info && builtin === BUILTIN_PERSONA) {
    if (icon) set(null);
    return;
  }
  if (icon?.key === key) return;
  const mine = ++seq;
  const made = info
    ? Promise.all([false, true].map((dark) => {
        const f = avatarFile(info.manifest, SIDE, dark);
        return f ? rasterize(personaFileURL(info, f, getToken())) : Promise.reject(new Error("no avatar"));
      }))
    : Promise.all([false, true].map((dark) => builtinSVG(builtin, dark).then(rasterize)));
  made.then(
    ([light, dark]) => mine === seq && set({ key, light, dark }),
    () => undefined,
  );
}

export function useLookIcon(): LookIcon | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => icon,
    // a static render (the row tests) draws the app icon
    () => null,
  );
}

// The page starts with the last look's icon in the tab (index.html names the app icon's files).
if (typeof document !== "undefined" && icon) setFaviconBase({ light: icon.light, dark: icon.dark });
