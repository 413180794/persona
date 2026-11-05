import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useDeclaredResult } from "../lib/extension-declared";
import { allowedTheme, extensionThemeTokens, themeKey, type ExtensionTheme } from "./extension-theme";

const KEY = "persona.extension-theme";
const AVAILABILITY_CHECK_MS = 30_000;
interface PaletteState { available: ExtensionTheme[]; selected: string; select: (id: string) => void }
const PaletteContext = createContext<PaletteState>({ available: [], selected: "", select: () => {} });

export function ExtensionThemeProvider({ children }: { children: ReactNode }) {
  const [selected, setSelected] = useState(() => typeof localStorage === "undefined" ? "" : localStorage.getItem(KEY) ?? "");
  const result = useDeclaredResult<ExtensionTheme>("themes", "themes", selected ? AVAILABILITY_CHECK_MS : 0);
  const available = useMemo(() => result.items.filter(allowedTheme), [result.items]);
  const select = (id: string) => {
    if (id && !available.some((t) => themeKey(t) === id)) return;
    if (id) localStorage.setItem(KEY, id); else localStorage.removeItem(KEY);
    setSelected(id);
  };
  useEffect(() => {
    if (result.loaded && selected && !available.some((t) => themeKey(t) === selected)) select("");
  }, [result.loaded, selected, available]);
  const current = available.find((t) => themeKey(t) === selected);
  useLayoutEffect(() => {
    if (!current) return;
    const root = document.documentElement;
    const tokens = { light: extensionThemeTokens(current, "light"), dark: extensionThemeTokens(current, "dark") };
    const keys = new Set([...Object.keys(tokens.light), ...Object.keys(tokens.dark)]);
    const before = [...keys].map((key) => ({ key, value: root.style.getPropertyValue(key), priority: root.style.getPropertyPriority(key) }));
    const restore = () => before.forEach(({ key, value, priority }) => value ? root.style.setProperty(key, value, priority) : root.style.removeProperty(key));
    const apply = () => {
      restore();
      Object.entries(tokens[root.classList.contains("dark") ? "dark" : "light"]).forEach(([key, value]) => root.style.setProperty(key, value));
      root.dataset.extensionTheme = themeKey(current);
    };
    apply();
    const observer = new MutationObserver(apply);
    observer.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => { observer.disconnect(); restore(); delete root.dataset.extensionTheme; };
  }, [current]);
  return <PaletteContext.Provider value={{ available, selected, select }}>{children}</PaletteContext.Provider>;
}

export function ExtensionThemePicker() {
  const { available, selected, select } = useContext(PaletteContext);
  if (!available.length) return null;
  return <div className="flex items-center gap-3 px-4 py-2.5">
    <span className="min-w-0 flex-1 text-[13px] text-ink">主题色</span>
    <select aria-label="主题色" value={selected} onChange={(e) => select(e.target.value)} className="h-8 max-w-[60%] rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]">
      <option value="">默认</option>
      {available.map((t) => <option key={themeKey(t)} value={themeKey(t)}>{t.name} · {t.extension_name}</option>)}
    </select>
  </div>;
}
