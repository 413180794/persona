// Reading a Feishu document for the panel (docs/feishu.md「文档」, the phone's FeishuDocReader / FeishuDocsModels):
// the Markdown export first (fast, faithful lists and tables); when the owner forbids export, the ProseMirror-style
// JSON tree. Bases, whiteboards and files say what they are and offer the tab. Reads are cached and can start early
// (a pointer resting on the link), so the panel usually opens on a finished page.
import { ApiError, api, enc, getToken } from "../../lib/api";
import { docKind, DOC_LABEL, setLinkPrefetcher, type DocKind } from "../../lib/links";

export interface DocNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: DocNode[];
  marks?: { type: string; attrs?: Record<string, unknown> }[];
  text?: string;
}

interface Authority {
  copy?: boolean;
}

export type DocContent =
  | { kind: "markdown"; title: string; markdown: string; copy: boolean; partial: boolean }
  | { kind: "tree"; title: string; doc: DocNode; copy: boolean; partial: boolean }
  | { kind: "unsupported"; title: string; doc: DocKind; message: string; image?: string; file?: string };

export interface DocInfo {
  owner?: string;
  updatedAt?: string;
  views?: number;
  comments?: number;
}

/** A failed read: lark-connector's `{"error":{"code"}}` or the hub's `{"error":"…"}`. */
export class DocError extends Error {
  status: number;
  code?: string;
  constructor(status: number, code: string | undefined, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

/** What to tell the user about a failed read (FeishuDocsFailure, plus the hub's own reasons). */
export function docErrorText(status: number, code?: string, raw?: string): string {
  switch (code) {
    case "DOCUMENT_EXPORT_FORBIDDEN":
      return "作者禁止导出这篇文档";
    case "DOCUMENT_FORMAT_UNSUPPORTED":
      return "这种文档暂不支持在 Jarvis 中阅读";
    case "DOCUMENT_NOT_FOUND":
      return "文档不存在或已被删除";
    case "DOCUMENT_FORBIDDEN":
    case "DOCUMENT_PERMISSION_DENIED":
      return "没有这篇文档的访问权限";
    case "DOCUMENT_TIMEOUT":
      return "飞书响应超时，请稍后重试";
    case "DOCUMENT_INVALID_URL":
      return "无法识别的文档链接";
  }
  if (code?.startsWith("AUTH_")) return "hub 上的 lark-connector 没有登录飞书，登录后再试";
  const text = raw ?? "";
  if (/not configured|not enabled/i.test(text)) return "hub 还没有接入飞书（lark-connector），读不了文档";
  switch (status) {
    case 0:
      return "连不上 hub";
    case 401:
      return "飞书登录已失效";
    case 403:
      return "没有访问权限";
    case 404:
      return "内容不存在或已被删除";
    case 502:
    case 503:
    case 504:
      return /lark|lark-connector|飞书/i.test(text) ? "连不上飞书（lark-connector），请稍后重试" : "飞书服务暂时不可用，请稍后重试";
  }
  return code || text || `HTTP ${status}`;
}

export function docErrorFromBody(status: number, body: Record<string, unknown>): DocError {
  const e = body.error;
  const obj = e && typeof e === "object" ? (e as { code?: unknown; message?: unknown }) : undefined;
  // lark-connector: {"error":{"code","message"}}; the hub's own errors: {"error":"…","code":<its generic code>}
  const code = typeof obj?.code === "string" ? obj.code : typeof e === "string" && /^[A-Z][A-Z_]+$/.test(e) ? e : undefined;
  const raw = typeof e === "string" ? e : typeof obj?.message === "string" ? obj.message : undefined;
  return new DocError(status, code, docErrorText(status, code, raw));
}

export type Get = (path: string) => Promise<Record<string, unknown>>;

// Through the hub's read-only lark-connector window. The token is passed explicitly: an lark-connector 401 (Feishu signed out)
// must not sign this browser out of the hub (api() does that on any 401 when it picks the token itself).
const hubGet: Get = async (path) => {
  try {
    return await api<Record<string, unknown>>(`/lark/raw/${path}`, { token: getToken() });
  } catch (e) {
    if (e instanceof ApiError) throw docErrorFromBody(e.status, e.body);
    throw new DocError(0, undefined, docErrorText(0));
  }
};

function bool(v: unknown, fallback: boolean) {
  return typeof v === "boolean" ? v : fallback;
}

function fromTree(r: Record<string, unknown>, url: string, title?: string): DocContent {
  const kind = docKind(url);
  const name = (typeof r.title === "string" && r.title) || title || DOC_LABEL[kind];
  const copy = bool((r.authority as Authority | undefined)?.copy, true);
  if ("base" in r) return { kind: "unsupported", title: name, doc: "bitable", message: "多维表格暂不支持在 Jarvis 中查看" };
  const o = r.object as { kind?: string; token?: string; image?: string; empty?: boolean } | undefined;
  if (o?.kind === "file") return { kind: "unsupported", title: name, doc: "file", message: "云空间文件，可以下载后查看", file: o.token };
  if (o?.kind === "whiteboard") return { kind: "unsupported", title: name, doc: "whiteboard", message: o.empty ? "这是一个空白画板" : "画板", image: o.image };
  if (o) return { kind: "unsupported", title: name, doc: kind, message: "这种内容暂不支持在 Jarvis 中查看" };
  const doc = (r.content as DocNode | undefined) ?? { type: "doc" };
  if ("spreadsheet" in r && !(doc.content ?? []).length) return { kind: "unsupported", title: name, doc: "sheet", message: "作者禁止导出这个表格，请在飞书中查看" };
  // the panel draws the title itself: a first heading that repeats it goes
  const blocks = doc.content ?? [];
  const first = blocks[0];
  const body = first?.type === "heading" && plainText(first).trim() === name.trim() ? { ...doc, content: blocks.slice(1) } : doc;
  return { kind: "tree", title: name, doc: body, copy, partial: r.partial === true };
}

/** Markdown when the owner allows export, else the JSON tree; what neither can show becomes `unsupported`. */
export async function readDoc(url: string, title: string | undefined, get: Get = hubGet): Promise<DocContent> {
  const kind = docKind(url);
  if (kind === "bitable") return { kind: "unsupported", title: title || DOC_LABEL.bitable, doc: "bitable", message: "多维表格暂不支持在 Jarvis 中查看" };
  if (kind !== "file" && kind !== "whiteboard") {
    try {
      const r = await get(`documents/read?url=${enc(url)}&format=markdown`);
      if (typeof r.markdown === "string") {
        const name = (typeof r.title === "string" && r.title) || title || DOC_LABEL[kind];
        return { kind: "markdown", title: name, markdown: r.markdown, copy: bool((r.authority as Authority | undefined)?.copy, true), partial: r.partial === true };
      }
      // an lark-connector (or the mock hub) that answers with the tree anyway: no second request
      if (r.content && typeof r.content === "object") return fromTree(r, url, title);
    } catch (e) {
      if (!(e instanceof DocError && (e.code === "DOCUMENT_EXPORT_FORBIDDEN" || e.code === "DOCUMENT_FORMAT_UNSUPPORTED" || e.status === 422))) throw e;
    }
  }
  try {
    return fromTree(await get(`documents/read?url=${enc(url)}&format=json`), url, title);
  } catch (e) {
    if (e instanceof DocError && (e.code === "DOCUMENT_FORMAT_UNSUPPORTED" || e.status === 422)) {
      return { kind: "unsupported", title: title || DOC_LABEL[kind], doc: kind, message: "这种内容暂不支持在 Jarvis 中查看" };
    }
    throw e;
  }
}

export async function readInfo(url: string, get: Get = hubGet): Promise<DocInfo> {
  const r = await get(`documents/reader/info?url=${enc(url)}`);
  const owner = r.owner as { name?: string } | undefined;
  const num = (v: unknown) => (typeof v === "number" ? v : undefined);
  return { owner: owner?.name || undefined, updatedAt: typeof r.updatedAt === "string" ? r.updatedAt : undefined, views: num(r.views), comments: num(r.comments) };
}

// ── Markdown export → what the panel renders ──

/** Stands for a <br> until the line-break plugin turns it into a real break (markdown here renders no raw HTML). */
export const BR = "\u2028";

/**
 * lark-connector's export as the panel shows it (FeishuDocsMarkdownExport.prepare):
 * - drops the leading `# Title` and `[Source](<url>)` lines (the panel draws its own title),
 * - points media at the hub (`http://<lark-connector>/api/documents/…` → `/api/v1/lark/raw/documents/…`), with the token
 *   an <img> cannot send as a header,
 * - turns `<br>` (table cells, mostly) into breaks; code is left alone.
 */
export function prepareMarkdown(md: string, title?: string, token = ""): string {
  let lines = md.replace(/https?:\/\/[A-Za-z0-9.-]+(:\d+)?\/api\/(?!v1\/)/g, "/api/v1/lark/raw/").split("\n");
  const blank = (s?: string) => s !== undefined && !s.trim();
  while (blank(lines[0])) lines.shift();
  const first = lines[0] ?? "";
  if (first.startsWith("# ") && (!title || unescape(first.slice(2)).trim() === title.trim())) lines.shift();
  while (blank(lines[0])) lines.shift();
  if ((lines[0] ?? "").startsWith("[Source](")) lines.shift();
  while (blank(lines[0])) lines.shift();
  let fence = "";
  lines = lines.map((line) => {
    const t = line.trim();
    if (fence) {
      if (t.startsWith(fence)) fence = "";
      return line;
    }
    if (t.startsWith("```") || t.startsWith("~~~")) {
      fence = t.slice(0, 3);
      return line;
    }
    return line.replace(/<br\s*\/?>/gi, BR);
  });
  const withToken = (u: string) => (token ? `${u}${u.includes("?") ? "&" : "?"}token=${enc(token)}` : u);
  return lines.join("\n").replace(/(!\[[^\]]*\]\(<?)(\/api\/v1\/[^)>\s]+)/g, (_, pre: string, u: string) => pre + withToken(u));
}

/** Removes Markdown backslash escapes (`\-`, `\.`…) for plain display. */
export function unescape(s: string): string {
  return s.replace(/\\([\\`*_{}[\]()#+\-.!|<>~])/g, "$1");
}

// ── JSON tree helpers (FeishuDocsTree) ──

export function attr(n: DocNode, key: string): string | undefined {
  const v = n.attrs?.[key];
  return typeof v === "string" ? v : typeof v === "number" ? String(v) : undefined;
}

export function num(n: DocNode, key: string): number | undefined {
  const v = n.attrs?.[key];
  const x = typeof v === "number" ? v : typeof v === "string" ? Number(v) : NaN;
  return Number.isFinite(x) ? x : undefined;
}

/**
 * The number each ordered list shows. Every docx list item arrives as its own one-item `orderedList`; consecutive ones
 * keep counting unless one says `restart` (then it starts at its `start`).
 */
export function orderedNumbers(siblings: DocNode[]): (number | undefined)[] {
  const out: (number | undefined)[] = [];
  let next: number | undefined;
  for (const n of siblings) {
    if (n.type !== "orderedList") {
      out.push(undefined);
      next = undefined;
      continue;
    }
    const start = num(n, "start") ?? 1;
    const restart = n.attrs?.restart === true || n.attrs?.restart === "true";
    const value = restart || next === undefined ? start : next;
    out.push(value);
    next = value + Math.max(1, (n.content ?? []).filter((c) => c.type === "listItem").length);
  }
  return out;
}

/** All text below a node, mentions by their label. */
export function plainText(n: DocNode): string {
  if (n.text !== undefined) return n.text;
  if (n.type === "mention") return attr(n, "label") ?? "";
  if (n.type === "hardBreak") return "\n";
  if (n.type === "equation") return attr(n, "latex") ?? "";
  return (n.content ?? []).map(plainText).join("");
}

/** Media under the hub (`/api/v1/…`) needs the token on the URL; an lark-connector path is moved under the hub first. */
export function mediaURL(src: string, token = getToken()): string {
  const u = src.replace(/^https?:\/\/[A-Za-z0-9.-]+(:\d+)?\/api\/(?!v1\/)/, "/api/v1/lark/raw/");
  if (!u.startsWith("/api/v1/")) return u;
  return `${u}${u.includes("?") ? "&" : "?"}token=${enc(token)}`;
}

// ── cache ──

const MAX = 12;
const docs = new Map<string, Promise<DocContent>>();

/** The document, from memory when it was read (or started) before; `fresh` reads it again. */
export function loadDoc(url: string, title?: string, fresh = false): Promise<DocContent> {
  const have = docs.get(url);
  if (have && !fresh) {
    docs.delete(url);
    docs.set(url, have); // most recently used last
    return have;
  }
  const p = readDoc(url, title);
  docs.set(url, p);
  p.catch(() => docs.get(url) === p && docs.delete(url)); // a failure is not kept
  while (docs.size > MAX) docs.delete(docs.keys().next().value!);
  return p;
}

setLinkPrefetcher((url) => {
  if (!docs.has(url)) void loadDoc(url).catch(() => {});
});
