import { memo } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { copyText } from "./atoms";
import { Icon } from "./icons";

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

export function linkify(md: string): string {
  return md
    .split(/(```[\s\S]*?```|`[^`\n]*`)/g)
    .map((part, i) => {
      if (i % 2) return part;
      return part.replace(BARE, (m, pre: string, url: string, off: number, all: string) => {
        if (pre === "(" && all.slice(Math.max(0, off - 1), off + 1) === "](") return m; // [text](url)
        const u = trimURL(url);
        return `${pre}<${u}>${url.slice(u.length)}`;
      });
    })
    .join("");
}

const components: Components = {
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  ),
  pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
};

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

export const Markdown = memo(function Markdown({ text, small }: { text: string; small?: boolean }) {
  return (
    <div className={`prose-j ${small ? "prose-sm" : ""}`}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {linkify(text)}
      </ReactMarkdown>
    </div>
  );
});
