// Where a link opens (CLAUDE.md「链接在 App 内打开」, docs/web.md「链接」). In a browser the browser is the app:
// ordinary pages open in a new tab (most sites refuse to be framed and the page cannot tell it was refused, so a
// framed panel would just be blank), Feishu documents and 公司圈 posts open in the side panel's reader (the same links
// the phone reads natively, FeishuLinks.route) when the hub reads Feishu (feature `lark`; otherwise they are ordinary
// pages), and jarvis://session links move to that conversation.
//
// Also the hand-off between the reader and the composers: text picked in the panel goes to the composer the user is
// writing in, as <jarvis-context> tags in the next message (docs/protocol.md「消息里的标签」).
import { t } from "../i18n";
import { useSyncExternalStore } from "react";
import { uuid } from "./format";
import { openPanel } from "./panel";
import { navigate } from "./router";
import { larkOn } from "./store";

/** What a Feishu document link points at, from the link alone (FeishuDocsKind.from). A wiki link does not say. */
export type DocKind = "doc" | "sheet" | "bitable" | "mindnote" | "file" | "slides" | "wiki" | "page" | "whiteboard" | "other";

export type LinkRoute =
  | { kind: "doc"; doc: DocKind } // a Feishu cloud document: the panel's reader
  | { kind: "moments"; id: string } // a 公司圈 post: the panel
  | { kind: "session"; mid: string; sid: string } // jarvis://session/<machine>/<session>: that conversation
  | { kind: "web" } // any other http(s) page: a new tab
  | { kind: "system" } // mailto: / tel:: the browser hands it to the system
  | { kind: "none" }; // anything else (javascript:, other apps' schemes, not a URL): not followed

const FEISHU_HOSTS = ["larkoffice.com", "feishu.cn", "larksuite.com"];
// First path segments the reader opens (FeishuLinks.isDoc).
const DOC_PATHS = ["docx", "wiki", "docs", "sheets", "base", "mindnotes", "file", "slides"];

function parse(url: string): URL | null {
  try {
    return new URL(url);
  } catch {
    return null;
  }
}

function isFeishuHost(host: string) {
  const h = host.toLowerCase();
  return FEISHU_HOSTS.some((d) => h === d || h.endsWith(`.${d}`));
}

export function docKind(url: string): DocKind {
  const parts = (parse(url)?.pathname ?? "").split("/").filter(Boolean);
  switch (parts[0] ?? "") {
    case "docx":
    case "docs":
    case "doc":
      return "doc";
    case "sheets":
    case "sheet":
      return "sheet";
    case "base":
    case "bitable":
      return "bitable";
    case "mindnotes":
    case "mindnote":
      return "mindnote";
    case "file":
      return "file";
    case "slides":
      return "slides";
    case "wiki":
      return "wiki";
    case "page":
      return "page";
    case "board":
    case "whiteboard":
      return "whiteboard";
    default:
      return "other";
  }
}

export const DOC_LABEL: Record<DocKind, string> = {
  doc: t("lib-links.doc"),
  sheet: t("lib-links.sheet"),
  bitable: t("lib-links.bitable"),
  mindnote: t("lib-links.mindnote"),
  file: t("lib-links.file"),
  slides: t("lib-links.slides"),
  wiki: t("lib-links.wiki"),
  page: t("lib-links.page"),
  whiteboard: t("lib-links.whiteboard"),
  other: t("lib-links.doc"),
};

/** A 公司圈 share link's post id (LarkMoments.postID). */
function momentsPostID(u: URL): string | null {
  if (!u.pathname.endsWith("/moments/detail")) return null;
  const id = u.searchParams.get("postId") ?? "";
  return /^\d+$/.test(id) ? id : null;
}

/** Where `url` opens. `lark`: whether the hub reads Feishu (store.larkOn); without it Feishu links are web pages. */
export function linkRoute(url: string, lark = larkOn()): LinkRoute {
  const u = parse(url.trim());
  if (!u) return { kind: "none" };
  const scheme = u.protocol.toLowerCase();
  if (scheme === "jarvis:" || scheme === "persona:") {
    // jarvis://session/<mid>/<sid>: the host is "session"
    try {
      const parts = `${u.host}${u.pathname}`.split("/").filter(Boolean).map(decodeURIComponent);
      if (parts[0] === "session" && parts[1] && parts[2]) return { kind: "session", mid: parts[1], sid: parts[2] };
    } catch {
      // A malformed percent escape in model output must not crash the transcript render.
      return { kind: "none" };
    }
    return { kind: "none" };
  }
  if (scheme === "mailto:" || scheme === "tel:") return { kind: "system" };
  if (scheme !== "http:" && scheme !== "https:") return { kind: "none" };
  if (lark && isFeishuHost(u.hostname)) {
    const first = u.pathname.split("/").filter(Boolean)[0] ?? "";
    if (DOC_PATHS.includes(first)) return { kind: "doc", doc: docKind(url) };
    const post = momentsPostID(u);
    if (post) return { kind: "moments", id: post };
  }
  return { kind: "web" };
}

/**
 * Whether a link opens inside the page (panel or route) rather than in a new tab. A Base never does: the reader can't
 * show one (the phone says so and offers Feishu), so the tab is one click shorter.
 */
export function opensInApp(url: string): boolean {
  const r = linkRoute(url);
  return (r.kind === "doc" && r.doc !== "bitable") || r.kind === "moments" || r.kind === "session";
}

/** The href to put on an <a>: jarvis://session links become the in-app path, so ⌘-click opens a tab of that page. */
export function linkHref(url: string): string {
  const r = linkRoute(url);
  return r.kind === "session" ? `/s/${encodeURIComponent(r.mid)}/${encodeURIComponent(r.sid)}` : url;
}

/** Opens a link where it belongs (see the top of this file). `title` names it in the panel until the page says. */
export function openLink(url: string, title?: string) {
  const r = linkRoute(url);
  switch (r.kind) {
    case "session":
      navigate({ name: "session", mid: r.mid, sid: r.sid });
      return;
    case "doc":
    case "moments":
      if (opensInApp(url)) {
        openPanel({ kind: "link", url, title: title || undefined });
        return;
      }
      window.open(url, "_blank", "noopener,noreferrer");
      return;
    case "web":
      window.open(url, "_blank", "noopener,noreferrer");
      return;
    case "system":
      location.href = url;
      return;
    case "none":
      return;
  }
}

/**
 * onClick for an <a href={linkHref(url)}>: a plain click on a link that opens in the page opens it there; ⌘ / Ctrl /
 * Shift / middle clicks and links that belong in a tab keep the browser's own behavior.
 */
export function clickLink(e: React.MouseEvent, url: string | undefined, title?: string) {
  if (!url || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  if (!opensInApp(url)) return;
  e.preventDefault();
  openLink(url, title);
}

// ── preloading (CLAUDE.md: opening speed first) ──────────

let prefetcher: ((url: string) => void) | null = null;

/** The reader registers how to warm a document (views/link/feishu-doc.ts). */
export function setLinkPrefetcher(fn: (url: string) => void) {
  prefetcher = fn;
}

/** A link the pointer rests on: start reading it so the panel opens on a finished page. */
export function prefetchLink(url: string | undefined) {
  if (url && opensInApp(url) && linkRoute(url).kind === "doc") prefetcher?.(url);
}

// ── <jarvis-context> ─────────────────────────────────────

/** A piece of a page the user picked (LinkSheet.swift ContextRef). `text` is "（整篇）" for a whole document. */
export interface ContextRef {
  id: string;
  kind: "context" | "edit";
  title: string;
  url: string;
  text: string;
}

// i18n-ignore: a marker inside <jarvis-context>, the app (QuoteViews.swift) matches it
export const WHOLE = "（整篇）";

function escAttr(s: string) {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** `<jarvis-context kind="…" title="…" url="…">\ntext\n</jarvis-context>`, as the phone writes it. */
export function contextXML(r: Pick<ContextRef, "kind" | "title" | "url" | "text">): string {
  const body = r.text.replace(/<\/jarvis-context\s*>/gi, "");
  return `<jarvis-context kind="${escAttr(r.kind)}" title="${escAttr(r.title)}" url="${escAttr(r.url)}">\n${body}\n</jarvis-context>`;
}

/** A message with its picked contexts in front (ChatView.swift sendText). */
export function withContexts(refs: Pick<ContextRef, "kind" | "title" | "url" | "text">[], text: string, hasImages = false): string {
  const typed = text.trim();
  if (!refs.length) return typed;
  const body = typed || (hasImages ? t("lib-links.see_images") : t("lib-links.see_context"));
  return `${refs.map(contextXML).join("\n")}\n\n${body}`;
}

/** 「AI 修改」: the picked text and what to do with it, sent at once (LinkSheet.swift sendEdit). */
export function editMessage(r: Pick<ContextRef, "title" | "url" | "text">, ask: string): string {
  const a = ask.trim();
  return `${contextXML({ ...r, kind: "edit" })}\n\n${a ? t("lib-links.edit_with_ask", { ask: a }) : t("lib-links.edit_plain")}`;
}

export function newContext(kind: ContextRef["kind"], title: string, url: string, text: string): ContextRef {
  return { id: uuid(), kind, title, url, text: text.trim() || WHOLE };
}

// ── contexts waiting in a composer (memory only, per draft key) ──

const pending = new Map<string, ContextRef[]>();
const ctxListeners = new Set<() => void>();
const NONE: ContextRef[] = [];

function emitContexts() {
  ctxListeners.forEach((l) => l());
}

export function getContexts(key: string): ContextRef[] {
  return pending.get(key) ?? NONE;
}

export function addContext(key: string, ref: ContextRef) {
  const cur = getContexts(key);
  // the same passage of the same page once
  if (cur.some((c) => c.url === ref.url && c.text === ref.text && c.kind === ref.kind)) return;
  pending.set(key, [...cur, ref]);
  emitContexts();
}

export function removeContext(key: string, id: string) {
  const next = getContexts(key).filter((c) => c.id !== id);
  if (next.length) pending.set(key, next);
  else pending.delete(key);
  emitContexts();
}

export function clearContexts(key: string) {
  if (!pending.delete(key)) return;
  emitContexts();
}

export function useContexts(key: string): ContextRef[] {
  return useSyncExternalStore(
    (l) => {
      ctxListeners.add(l);
      return () => ctxListeners.delete(l);
    },
    () => getContexts(key),
  );
}

// ── the composer being written in ────────────────────────

/** What a mounted composer lets the panel do: its draft key, and sending a message now (「AI 修改」). */
export interface ComposerHandle {
  key: string;
  send: (text: string) => Promise<boolean>;
  focus: () => void;
  /** False while it can't send (a read-only source): picked text then goes elsewhere. */
  ready?: () => boolean;
}

let composers: ComposerHandle[] = []; // most recently mounted / focused last
const waiting: { key?: string; run: (c: ComposerHandle) => void }[] = [];

/** A composer mounts: it becomes the active one. Returns the unregister for its cleanup. */
export function registerComposer(h: ComposerHandle): () => void {
  composers = [...composers.filter((c) => c !== h), h];
  for (let i = 0; i < waiting.length; i++) {
    const w = waiting[i];
    if (w.key !== undefined && w.key !== h.key) continue;
    waiting.splice(i--, 1);
    w.run(h);
  }
  return () => {
    composers = composers.filter((c) => c !== h);
  };
}

/** The user went back to a composer (focus): it is the active one again. */
export function activateComposer(h: ComposerHandle) {
  if (composers.includes(h)) composers = [...composers.filter((c) => c !== h), h];
}

export function activeComposer(): ComposerHandle | undefined {
  return composers[composers.length - 1];
}

/** The active composer if it can send now. */
export function usableComposer(): ComposerHandle | undefined {
  const c = activeComposer();
  return c && (c.ready?.() ?? true) ? c : undefined;
}

/** Runs `run` with the composer for `key` once it mounts (now, if it is mounted); after `ms` gives up (`expired`). */
export function whenComposer(key: string | undefined, run: (c: ComposerHandle) => void, expired?: () => void, ms = 8000) {
  const now = key === undefined ? activeComposer() : composers.find((c) => c.key === key);
  if (now) {
    run(now);
    return;
  }
  const w = { key, run };
  waiting.push(w);
  setTimeout(() => {
    const i = waiting.indexOf(w);
    if (i < 0) return;
    waiting.splice(i, 1);
    expired?.();
  }, ms);
}
