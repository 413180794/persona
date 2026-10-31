import { useSyncExternalStore } from "react";

export type Theme = "system" | "light" | "dark";

const listeners = new Set<() => void>();
const media = matchMedia("(prefers-color-scheme: dark)");

function current(): Theme {
  return (localStorage.getItem("jarvis.theme") as Theme) || "system";
}

function apply() {
  const t = current();
  document.documentElement.classList.toggle("dark", t === "dark" || (t === "system" && media.matches));
}

media.addEventListener("change", apply);

export function setTheme(t: Theme) {
  localStorage.setItem("jarvis.theme", t);
  apply();
  listeners.forEach((l) => l());
}

export function useTheme(): Theme {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    current,
  );
}
