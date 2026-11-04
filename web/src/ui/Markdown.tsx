import { memo } from "react";
import ReactMarkdown, { defaultUrlTransform, type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { clickLink, linkHref, linkRoute, prefetchLink } from "../lib/links";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
// \ce{…} / \pu{…} for chemistry; registers its macros on the katex that rehype-katex uses.
import "katex/contrib/mhchem";
import { copyText } from "./atoms";
import { Icon } from "./icons";
import { MermaidBlock } from "./Mermaid";

// A link ends at whitespace or any non-ASCII character (Chinese text, full-width punctuation), without trailing
// .,;:!? or an unbalanced ) ] (docs/lessons/ios.md: where a link ends). GFM's autolinks only end at whitespace, so
// bare URLs are written as <url> before markdown sees them. Code is left alone.
const BARE = /(^|[^\x21-\x7e]|[\s(])(https?:\/\/[\x21-\x7e]+)/g;

export function trimURL(url: string): string {
  let u = url.replace(/[.,;:!?'"*_~]+$/, "");
  for (const [open, close] of [
    ["(", ")"],
    ["[", "]"],
  ]) {
    while (u.endsWith(close) && u.split(open).length < u.split(close).length) u = u.slice(0, -1).replace(/[.,;:!?]+$/, "");
  }
  return u;
}

// Fenced code (``` or ~~~, also inside lists and quotes and on a list item's first line; an unclosed one runs to the
// end of its quote or list item, or of the text while streaming), indented code (4 spaces after a blank line, outside
// lists) and code spans (a run of n backticks up to the next run of n). Returns text and code alternately, so odd
// indices are code: left alone by normalizeMath and linkify.
const QUOTES = /^(?:[ \t]*>[ \t]?)*/;
const FENCE = /^((?:[ \t]*>[ \t]?)*)([ \t]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)?[ \t]*)(`{3,}|~{3,})/;
const LIST_ITEM = /^(?:[ \t]*>)*[ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]/;
const INDENTED = /^( {4}|\t)/;

const quoteDepth = (line: string) => (QUOTES.exec(line)![0].match(/>/g) ?? []).length;

// A line without its first `depth` quote markers.
function unquote(line: string, depth: number): string {
  for (let k = 0; k < depth; k++) {
    const m = /^[ \t]*>[ \t]?/.exec(line);
    if (!m) break;
    line = line.slice(m[0].length);
  }
  return line;
}

export function splitCode(md: string): string[] {
  const parts = [""];
  const add = (s: string, code: boolean) => {
    if (parts.length % 2 === 0 === code) parts[parts.length - 1] += s;
    else parts.push(s);
  };
  let text = "";
  const flush = () => {
    for (let i = 0; i < text.length; ) {
      const j = text.indexOf("`", i);
      if (j !== i) {
        const end = j < 0 ? text.length : j;
        add(text.slice(i, end), false);
        i = end;
        continue;
      }
      const n = /^`+/.exec(text.slice(i))![0].length;
      const close = new RegExp(`(?<!\`)\`{${n}}(?!\`)`, "g");
      close.lastIndex = i + n;
      const m = close.exec(text);
      add(text.slice(i, m ? m.index + n : i + n), !!m);
      i = m ? m.index + n : i + n;
    }
    text = "";
  };
  const lines = md.split(/(?<=\n)/);
  let blank = true;
  let inList = false;
  for (let i = 0; i < lines.length; ) {
    const line = lines[i];
    if (line.trim() && !/^[ \t]/.test(line)) inList = LIST_ITEM.test(line) || (inList && !blank);
    const fence = FENCE.exec(line);
    if (fence && !(fence[3][0] === "`" && line.slice(fence[0].length).includes("`"))) {
      // The fence ends at a closing fence, or where its container does: a line with fewer quote markers, or (in a
      // list item) a line indented less than the item's content.
      const depth = quoteDepth(fence[1]);
      const indent = inList || LIST_ITEM.test(line) ? fence[2].length : 0;
      const end = new RegExp(`^[ \\t]*\\${fence[3][0]}{${fence[3].length},}[ \\t]*$`);
      let j = i + 1;
      let closed = false;
      for (; j < lines.length; j++) {
        const l = lines[j].replace(/\n$/, "");
        if (quoteDepth(l) < depth) break;
        const rest = unquote(l, depth);
        if (indent && rest.trim() && /^[ \t]*/.exec(rest)![0].length < indent) break;
        if (end.test(rest)) {
          closed = true;
          break;
        }
      }
      const stop = closed ? j + 1 : j;
      flush();
      add(lines.slice(i, stop).join(""), true);
      i = stop;
      blank = false;
      continue;
    }
    // A whitespace-only line is blank, not code: every block of indented code starts with a line that has text.
    if (blank && !inList && INDENTED.test(line) && line.trim()) {
      let j = i;
      while (j < lines.length && (INDENTED.test(lines[j]) || !lines[j].trim())) j++;
      while (j > i && !lines[j - 1].trim()) j--;
      flush();
      add(lines.slice(i, j).join(""), true);
      i = j;
      continue;
    }
    blank = !line.trim();
    text += line;
    i++;
  }
  flush();
  return parts;
}

// What starts a line inside a quote or list item, and what a new line needs to stay in it: quote markers are kept,
// a list marker becomes spaces.
const PREFIX = /^(?:[ \t]*>[ \t]?)*[ \t]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)?/;
const continuation = (prefix: string) => prefix.replace(/[-*+]|\d{1,9}[.)]/, (m) => " ".repeat(m.length));

// Bare URLs and <autolinks>: a $ in them (https://…/odata/$metadata) is part of the address, not math.
const URL_TEXT = /<https?:\/\/[^\s<>]*>|(^|[^\x21-\x7e]|[\s(])(https?:\/\/[\x21-\x7e]+)/g;
// What normalizeMath holds out of the $ rules while it works: formulas written with \( \) / \[ \] (explicit, so "the
// \(n\)th term" stays math) and URLs with a $.
const HELD = /\uE000(\d+)\uE001/g;
// Stands in for code and held URLs when looking for table rows and their math.
const MASK = "\uE002";

// remark-math only knows $…$ / $$…$$, and CommonMark turns \( \) \[ \] into plain brackets, so the LaTeX
// delimiters models often use are rewritten to dollars first. remark-math renders $$…$$ on one line as inline
// math, so a line holding only that is split into a display block. Code is left alone.
export function normalizeMath(md: string): string {
  let done = "";
  let mask = "";
  const held: { s: string; url?: boolean }[] = [];
  const hold = (s: string, url?: boolean) => `\uE000${held.push({ s, url }) - 1}\uE001`;
  const restore = (s: string, masked = false): string =>
    s.replace(HELD, (_, n: string) => {
      const h = held[+n];
      return masked && h.url ? MASK.repeat(h.s.length) : restore(h.s, masked);
    });
  splitCode(md).forEach((part, i) => {
    if (i % 2) {
      done += part;
      mask += part.replace(/[^\n]/g, MASK);
      return;
    }
    const midLine = done !== "" && !done.endsWith("\n");
    part = part
      .replace(URL_TEXT, (m, pre: string | undefined, url: string | undefined) => {
        if (url === undefined) return m.includes("$") ? hold(m, true) : m;
        const u = trimURL(url).replace(/[$}]+$/, "");
        return u.includes("$") ? `${pre}${hold(u, true)}${url.slice(u.length)}` : m;
      })
      .replace(
        /^((?:[ \t]*>[ \t]?)*[ \t]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)?)\$\$([^$\n]+)\$\$[ \t]*$/gm,
        (m, pre: string, tex: string, off: number) =>
          off === 0 && midLine ? m : `${pre}$$\n${continuation(pre)}${tex.trim()}\n${continuation(pre)}$$`,
      )
      .replace(/(?<!\\)\\\[([\s\S]+?)\\\]/g, (m, tex: string, off: number, all: string) => {
        const before = done + all.slice(0, off);
        const line = before.slice(before.lastIndexOf("\n") + 1);
        const pre = PREFIX.exec(line)![0];
        const c = continuation(pre);
        // Lines after the first carry the quote / list prefix already: each output line gets it exactly once.
        const depth = (pre.match(/>/g) ?? []).length;
        const body = tex
          .split("\n")
          .map((l) => unquote(l, depth).trim())
          .filter(Boolean)
          .join(`\n${c}`);
        if (!body) return m;
        const head = pre.length === line.length ? "" : `\n${c}`;
        const next = all[off + m.length];
        const tail = next === undefined || next === "\n" ? "" : `\n${c}`;
        return hold(`${head}$$\n${c}${body}\n${c}$$${tail}`);
      })
      .replace(/(?<!\\)\\\(([\s\S]+?)\\\)/g, (m, tex: string) => (tex.trim() ? hold(`$${dollarsInMath(tex.trim())}$`) : m));
    const out = escapeLoneDollars(part);
    done += restore(out);
    mask += restore(out, true);
  });
  return pipesInTableMath(done, mask);
}

// remark-math ends $…$ at a \$ too, so KaTeX's \$ is written as a character code.
const dollarsInMath = (tex: string) => tex.replace(/\\([\s\S])/g, (m, c: string) => (c === "$" ? '{\\char"24}' : m));

// Pandoc's rule for single dollars, plus no letter or digit right outside: $…$ is math only when the opening $
// follows no letter/digit and is followed by a non-space, and the closing $ follows a non-space and is followed by no
// letter/digit. Any other $ is escaped, so "$5 和 $10", "US$5" and "$HOME/$USER" stay text and an unclosed $ doesn't
// pair with the next formula's $.
const DISPLAY = /(\$\$[\s\S]*?\$\$)/g;
const ALNUM = /[A-Za-z0-9]/;

function escapeLoneDollars(md: string): string {
  return md
    .split(DISPLAY)
    .map((part, n) => {
      if (n % 2) return part;
      let out = "";
      for (let i = 0; i < part.length; ) {
        if (part[i] === "\\") {
          out += part.slice(i, i + 2);
          i += 2;
          continue;
        }
        if (part[i] !== "$") {
          out += part[i++];
          continue;
        }
        const close = closingDollar(part, i + 1);
        if (
          close > i + 1 &&
          /\S/.test(part[i + 1]) &&
          /\S/.test(part[close - 1]) &&
          !ALNUM.test(part[i - 1] ?? "") &&
          !ALNUM.test(part[close + 1] ?? "")
        ) {
          out += `$${dollarsInMath(part.slice(i + 1, close))}$`;
          i = close + 1;
        } else {
          out += "\\$";
          i++;
        }
      }
      return out;
    })
    .join("");
}

// The $ ending a formula that starts before `from`: on the same line, skipping \-escaped characters (LaTeX's \$).
function closingDollar(s: string, from: number): number {
  for (let i = from; i < s.length && s[i] !== "\n"; i++) {
    if (s[i] === "\\") i++;
    else if (s[i] === "$") return i;
  }
  return -1;
}

// GFM splits table cells on every |, even inside $…$, so | in math on a table row is written as \vert. A table is a
// line with a | followed by a delimiter row (outer pipes optional), and runs to a blank line; a line starting with |
// counts too (a table still streaming in). `mask` is `md` with code and URLs blanked out, so neither a code span
// splitting the row nor a $ in code or a URL changes what is math.
const DELIMITER_ROW = /^[ \t]*\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/;

function pipesInTableMath(md: string, mask: string): string {
  const masked = mask.split("\n");
  const row = (k: number) => masked[k].replace(QUOTES, "");
  let table = false;
  return md
    .split("\n")
    .map((line, k) => {
      const r = row(k);
      if (!r.trim()) table = false;
      else if (!table && r.includes("|") && k + 1 < masked.length && row(k + 1).includes("|") && DELIMITER_ROW.test(row(k + 1)))
        table = true;
      return table || /^[ \t]*\|/.test(r) ? pipesInMath(line, masked[k]) : line;
    })
    .join("\n");
}

function pipesInMath(line: string, mask: string): string {
  let out = "";
  for (let i = 0; i < line.length; ) {
    if (mask[i] === "\\") {
      out += line.slice(i, i + 2);
      i += 2;
      continue;
    }
    const close = mask[i] === "$" ? closingDollar(mask, i + 1) : -1;
    if (close < 0) {
      out += line[i++];
      continue;
    }
    out += `$${line.slice(i + 1, close).replace(/\\\|/g, "\\Vert{}").replace(/\|/g, "\\vert{}")}$`;
    i = close + 1;
  }
  return out;
}

// With `math` (text normalizeMath wrote, where every $ left unescaped delimits a formula) URLs inside formulas are
// left alone; a URL that starts before a $ is matched first, so https://…/$metadata stays whole. Without it (renderers
// that don't parse math) a $ is plain text.
const URL_OR_MATH = /(?<!\\)\$\$[\s\S]*?\$\$|(?<!\\)\$(?:\\[\s\S]|[^\\$])+?\$|(^|[^\x21-\x7e]|[\s(])(https?:\/\/[\x21-\x7e]+)/g;

export function linkify(md: string, { math = false }: { math?: boolean } = {}): string {
  return splitCode(md)
    .map((part, i) => {
      if (i % 2) return part;
      return part.replace(math ? URL_OR_MATH : BARE, (m, pre: string | undefined, url: string | undefined, off: number, all: string) => {
        if (url === undefined) return m; // a formula
        if (pre === "(" && all.slice(Math.max(0, off - 1), off + 1) === "](") return m; // [text](url)
        const u = trimURL(url);
        return `${pre}<${u}>${url.slice(u.length)}`;
      });
    })
    .join("");
}

// jarvis://session links survive (they move to that conversation); everything else as react-markdown decides
// (javascript: and other apps' schemes are dropped).
export function markdownUrl(url: string): string {
  return linkRoute(url).kind === "session" ? url : defaultUrlTransform(url);
}

// A link opens where it belongs (lib/links.ts): Feishu documents in the side panel, conversations in place, pages in
// a new tab. ⌘ / Ctrl / middle click is always the browser's new tab.
function Link({ href, children }: { href?: string; children?: React.ReactNode }) {
  const title = textOf(children).trim();
  return (
    <a
      href={href ? linkHref(href) : undefined}
      target="_blank"
      rel="noreferrer noopener"
      onClick={(e) => clickLink(e, href, title && title !== href ? title : undefined)}
      onPointerEnter={() => prefetchLink(href)}
    >
      {children}
    </a>
  );
}

export const markdownComponents: Components = {
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  pre: ({ children }) => (codeLanguage(children) === "mermaid" ? <MermaidBlock code={textOf(children).replace(/\n$/, "")} /> : <CodeBlock>{children}</CodeBlock>),
};

/** The language a fenced block names (```mermaid → "mermaid"), from its <code>'s class. */
function codeLanguage(children: React.ReactNode): string {
  const code = Array.isArray(children) ? children[0] : children;
  const cls = code && typeof code === "object" && "props" in code ? String((code as { props: { className?: string } }).props.className ?? "") : "";
  return /(?:^|\s)language-([\w-]+)/.exec(cls)?.[1]?.toLowerCase() ?? "";
}

function textOf(node: React.ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (node && typeof node === "object" && "props" in node) return textOf((node as { props: { children?: React.ReactNode } }).props.children);
  return "";
}

function CodeBlock({ children }: { children: React.ReactNode }) {
  return (
    <div className="group/code relative">
      <pre>{children}</pre>
      <button
        type="button"
        aria-label="拷贝代码"
        title="拷贝"
        onClick={() => copyText(textOf(children).replace(/\n$/, ""))}
        className="absolute top-1.5 right-1.5 flex size-6 items-center justify-center rounded-[6px] bg-surface text-ink-3 opacity-0 shadow-btn transition-opacity duration-150 group-hover/code:opacity-100 hover:text-ink focus-visible:opacity-100"
      >
        <Icon name="copy" size={13} />
      </button>
    </div>
  );
}

// `compact`: a few lines inside a card (a task's receipt): body size in the card's color, headings as bold body text,
// tight blocks, so a parent's line-clamp shows as much as it can.
export const Markdown = memo(function Markdown({ text, small, compact }: { text: string; small?: boolean; compact?: boolean }) {
  return (
    <div className={`prose-j ${small ? "prose-sm" : ""} ${compact ? "prose-compact" : ""}`}>
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkMath]} rehypePlugins={[rehypeKatex]} components={markdownComponents} urlTransform={markdownUrl}>
        {linkify(normalizeMath(text), { math: true })}
      </ReactMarkdown>
    </div>
  );
});
