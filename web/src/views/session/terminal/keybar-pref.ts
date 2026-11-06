// Whether the terminal shows its key bar, kept in this browser (both surfaces read it).
export const KEYBAR_KEY = "persona.terminal.keyBar";

/** The key bar: as chosen in this browser, else up where the pointer is a finger (a phone's keyboard has no Esc / Ctrl). */
export function savedKeyBar(): boolean {
  const v = localStorage.getItem(KEYBAR_KEY);
  return v === null ? matchMedia("(pointer: coarse)").matches : v === "1";
}

export function saveKeyBar(on: boolean) {
  localStorage.setItem(KEYBAR_KEY, on ? "1" : "0");
}
