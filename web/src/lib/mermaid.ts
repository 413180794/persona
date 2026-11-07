// Mermaid diagrams in Markdown (docs/web/web.md "Markdown", #1376): Mermaid (MIT) loads the first time a diagram is
// drawn, in strict mode (its own DOMPurify pass: no scripts, no click callbacks, no HTML in labels that runs). Renders
// go one at a time (Mermaid keeps global state while it draws), and each diagram is kept per theme, so a conversation
// re-rendering or scrolling back never draws it twice.

import { t } from "../i18n";

type Mermaid = (typeof import("mermaid"))["default"];

let loader: Promise<Mermaid> | null = null;
let queue: Promise<unknown> = Promise.resolve();
let theme: "dark" | "default" | null = null;
let seq = 0;
const cache = new Map<string, string>();
const CACHE_MAX = 60;

const FONT = `-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Noto Sans CJK SC",sans-serif`;

// Mermaid's own pie colors are near-black in its dark theme and pastel in the light one: the app's chart hues instead.
const PIE = {
  pie1: "#1f6feb", pie2: "#ed7d31", pie3: "#2da44e", pie4: "#bf8700", pie5: "#8250df", pie6: "#cf222e", pie7: "#1b7c83", pie8: "#9a6700",
  pieSectionTextColor: "#ffffff", pieStrokeColor: "#ffffff", pieOuterStrokeColor: "transparent", pieStrokeWidth: "1.5px", pieOpacity: "1",
};

function load(): Promise<Mermaid> {
  loader ??= import("mermaid").then((m) => m.default);
  return loader;
}

/** A diagram already drawn for this theme, or undefined. */
export function cachedMermaid(code: string, dark: boolean): string | undefined {
  return cache.get(`${dark ? 1 : 0}\n${code}`);
}

/** Draws a diagram as SVG markup (sanitized by Mermaid); rejects with Mermaid's own message for a syntax error. */
export function renderMermaid(code: string, dark: boolean): Promise<string> {
  const key = `${dark ? 1 : 0}\n${code}`;
  const hit = cache.get(key);
  if (hit) return Promise.resolve(hit);
  const job = queue.then(async () => {
    const again = cache.get(key);
    if (again) return again;
    const m = await load();
    const want = dark ? "dark" : "default";
    if (theme !== want) {
      m.initialize({ startOnLoad: false, securityLevel: "strict", theme: want, fontFamily: FONT, suppressErrorRendering: true, themeVariables: PIE });
      theme = want;
    }
    await m.parse(code); // throws a readable syntax error before render leaves anything behind
    const { svg } = await m.render(`persona-mermaid-${++seq}`, code);
    if (cache.size >= CACHE_MAX) cache.delete(cache.keys().next().value!);
    cache.set(key, svg);
    return svg;
  });
  queue = job.catch(() => undefined);
  return job;
}

/** A Mermaid error for people: a Chinese line saying where, and Mermaid's own message kept for the details. */
export function mermaidProblem(e: unknown): { summary: string; detail: string } {
  const raw = (e instanceof Error ? e.message : String(e)).trim();
  const line = /\bline (\d+)/i.exec(raw)?.[1];
  const syntax = /parse error|syntax|expecting|lexical/i.test(raw);
  const summary = syntax ? (line ? t("lib-mermaid.syntax_error_line", { line }) : t("lib-mermaid.syntax_error")) : t("lib-mermaid.render_failed");
  return { summary, detail: raw || summary };
}
