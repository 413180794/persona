// The file viewer (docs/ios.md「文件查看器」): one file or folder on a machine, read through the hub. Markdown
// rendered (or its source), code with line numbers and a little coloring, JSON indented, images, PDFs, folders.
// Text files come a page (2 MB) at a time; only the lines on screen are rendered, so big files stay smooth.
import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState } from "react";
import { api, enc, errorText, fileDownloadURL, fileThumbURL, isAbort } from "../../../lib/api";
import { delimitedSheet, type Sheet } from "../../../lib/sheets";
import type { SheetReply, SheetRequest } from "../../../lib/sheets.worker";
import {
  base64Bytes,
  columns,
  dirName,
  extendStates,
  extOf,
  fileName,
  isMachineRef,
  joinPath,
  languageOf,
  lexLine,
  markdownChunks,
  prettyJSON,
  resolveRef,
  simplifyHTML,
  splitLines,
  viewKind,
  type FileContent,
  type Lang,
  type LexState,
  type Token,
  type TokenKind,
} from "../../../lib/files";
import { bytes, shortPath } from "../../../lib/format";
import { Button, copyText, cx, IconButton, Loading, toast } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { Markdown } from "../../../ui/Markdown";
import { Lightbox } from "../../chat/Steps";
import { PanelHeader } from "../../SidePanel";
import { BackHeader, Empty, FileGlyph, Segmented, useWindow } from "./parts";
import { parseNotebook } from "../../../lib/notebook";
import { HtmlPreview } from "./HtmlPreview";
import { MediaPlayer } from "../../../ui/Media";
import { NotebookView } from "./NotebookView";
import { OfficeView } from "./OfficeView";
import { SheetView } from "./SheetView";

const LH = 20; // px per code line
const PAD = 8; // px above the first line
const MAX_LINE = 4000; // longer lines (minified JS / JSON) are cut for display
const MAX_JSON = 24 << 20; // JSON is read whole up to this, then indented
const COLOR_LINES = 100_000;
const COLOR_BYTES = 8 << 20;
const PRETTY_BYTES = 8 << 20; // bigger JSON shows as read, not re-indented
const MAX_TABLE_TEXT = 64 << 20; // a CSV is read whole up to this, then parsed
const MAX_NOTEBOOK = 64 << 20; // a notebook is read whole up to this (its outputs' pictures are inside)
const MAX_HTML = 16 << 20; // an HTML page is read whole up to this to preview
const MAX_TABLE_BYTES = 200 << 20; // a workbook bigger than this is not opened in the browser

// stream=1: a binary file comes described, its bytes from file/download by Range (docs/protocol.md「文件预览」); an
// older hub ignores it and sends base64 as before.
export function fileURL(mid: string, path: string, cwd?: string, offset = 0) {
  return `/machines/${enc(mid)}/file?path=${enc(path)}&cwd=${enc(cwd ?? "")}${offset > 0 ? `&offset=${offset}` : ""}&stream=1`;
}

/** A binary file's whole bytes: from file/download when the hub streams it, else the base64 an older hub sent. */
export async function fileBytes(mid: string, f: FileContent, cwd?: string, signal?: AbortSignal): Promise<ArrayBuffer> {
  if (!f.stream && typeof f.data === "string") {
    if (f.truncated) throw new Error("文件超过 2 MB，hub 版本太旧，更新后才能预览");
    return base64Bytes(f.data).buffer;
  }
  const res = await fetch(fileDownloadURL(mid, f.path, cwd), { signal });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as Record<string, unknown>);
    throw new Error(typeof body.error === "string" ? body.error : `HTTP ${res.status}`);
  }
  return res.arrayBuffer();
}

/** Where the browser can show a binary file itself (an image, a PDF): file/download inline, or the old base64. */
function inlineSrc(mid: string, f: FileContent, cwd?: string): string {
  if (typeof f.text === "string") return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(f.text)}`; // an SVG, read as text
  return f.stream ? fileDownloadURL(mid, f.path, cwd, { inline: true }) : `data:${f.mime};base64,${f.data ?? ""}`;
}

/** Parses a table file in a worker: a workbook's bytes, or delimited text (small ones right here). */
function parseSheets(req: SheetRequest, signal: AbortSignal): Promise<Sheet[]> {
  if ("text" in req && req.text.length < 256 << 10) return Promise.resolve([delimitedSheet(req.name, req.text)]);
  return new Promise((resolve, reject) => {
    const w = new Worker(new URL("../../../lib/sheets.worker.ts", import.meta.url), { type: "module" });
    const done = () => w.terminate();
    signal.addEventListener("abort", () => {
      done();
      reject(new DOMException("aborted", "AbortError"));
    });
    w.onmessage = (e: MessageEvent<SheetReply>) => {
      done();
      if ("error" in e.data) reject(new Error(e.data.error));
      else resolve(e.data.sheets);
    };
    w.onerror = (e) => {
      done();
      reject(new Error(e.message || "表格解析失败"));
    };
    w.postMessage(req, "bytes" in req ? [req.bytes] : []);
  });
}

// ── loading ───────────────────────────────────────────────

export interface FileDoc {
  file: FileContent | null;
  error: string;
  text: string;
  /** Where the next page starts; 0: all read. */
  next: number;
  loadingMore: boolean;
  more: () => Promise<boolean>;
  /** Reads the rest (up to `cap` bytes) and returns the whole text. */
  all: (cap?: number) => Promise<string>;
  reload: () => void;
}

export function useFileDoc(mid: string, path: string, cwd?: string): FileDoc {
  const d = useRef({ gen: 0, file: null as FileContent | null, pages: [] as string[], next: 0, busy: false });
  const [, bump] = useReducer((x: number) => x + 1, 0);
  const [error, setError] = useState("");
  const [reloads, setReloads] = useState(0);

  useEffect(() => {
    const s = d.current;
    const gen = ++s.gen;
    Object.assign(s, { file: null, pages: [], next: 0, busy: false });
    setError("");
    bump();
    const ac = new AbortController();
    api<FileContent>(fileURL(mid, path, cwd), { signal: ac.signal })
      .then((f) => {
        if (gen !== s.gen) return;
        Object.assign(s, { file: f, pages: typeof f.text === "string" ? [f.text] : [], next: f.next ?? 0 });
        bump();
      })
      .catch((e) => {
        if (!isAbort(e) && gen === s.gen) setError(errorText(e));
      });
    return () => ac.abort();
  }, [mid, path, cwd, reloads]);

  const more = useCallback(async () => {
    const s = d.current;
    if (!s.file || s.next <= 0 || s.busy) return false;
    const gen = s.gen;
    s.busy = true;
    bump();
    try {
      const f = await api<FileContent>(fileURL(mid, path, cwd, s.next)); // the same request as the first page, from `next`
      if (gen !== s.gen) return false;
      s.pages = [...s.pages, f.text ?? ""];
      s.next = (f.next ?? 0) > s.next ? (f.next ?? 0) : 0; // a page that does not move on ends it
      return true;
    } catch (e) {
      if (gen === s.gen) toast(`没读到后面的内容：${errorText(e)}`, "error");
      return false;
    } finally {
      if (gen === s.gen) {
        s.busy = false;
        bump();
      }
    }
  }, [mid, path, cwd]);

  const all = useCallback(
    async (cap = 64 << 20) => {
      const s = d.current;
      const gen = s.gen;
      while (s.gen === gen && s.next > 0 && s.next < cap) {
        if (s.busy) await new Promise((r) => setTimeout(r, 50)); // a page someone else asked for: wait for it
        else if (!(await more())) break;
      }
      return s.pages.join("");
    },
    [more],
  );

  const s = d.current;
  const text = useMemo(() => s.pages.join(""), [s.pages]);
  return { file: s.file, error, text, next: s.next, loadingMore: s.busy, more, all, reload: useCallback(() => setReloads((n) => n + 1), []) };
}

// ── the viewer ────────────────────────────────────────────

type Mode = "rendered" | "source";

/**
 * One file (or folder) with its header: back (in a stack) or the file's icon, the name and path, the Markdown / JSON
 * switch, copy path, download. `onOpenPath` opens another path it links to (a folder's entry, a Markdown link).
 */
export function FileView({
  mid,
  path,
  cwd,
  line,
  onBack,
  onClose,
  onOpenPath,
}: {
  mid: string;
  path: string;
  cwd?: string;
  line?: number;
  onBack?: () => void;
  onClose: () => void;
  onOpenPath: (path: string) => void;
}) {
  const doc = useFileDoc(mid, path, cwd);
  const f = doc.file;
  const kind = f ? viewKind(f) : null;
  const [mode, setMode] = useState<Mode>(line ? "source" : "rendered");
  const [saving, setSaving] = useState(false);

  // JSON is indented from the whole text: read the rest first (as the app does), up to a cap.
  const cap = kind === "notebook" ? MAX_NOTEBOOK : kind === "html" ? MAX_HTML : MAX_JSON;
  const wantAll = (kind === "json" || kind === "notebook" || kind === "html") && doc.next > 0 && (f?.size ?? 0) <= cap;
  const { all } = doc;
  useEffect(() => {
    if (wantAll) void all(cap);
  }, [wantAll, all, cap]);
  const notebook = useMemo(() => {
    if (kind !== "notebook" || doc.next > 0) return null;
    try {
      return { nb: parseNotebook(doc.text), error: "" };
    } catch (e) {
      return { nb: null, error: e instanceof Error ? e.message : String(e) };
    }
  }, [kind, doc.next, doc.text]);
  const json = useMemo(() => {
    if (kind !== "json" || doc.next > 0) return null;
    if (doc.text.length > PRETTY_BYTES) return { pretty: null, error: "", big: true };
    const pretty = prettyJSON(doc.text);
    if (pretty !== null) return { pretty, error: "" };
    try {
      JSON.parse(doc.text);
      return { pretty: null, error: "" };
    } catch (e) {
      return { pretty: null, error: e instanceof Error ? e.message : String(e) };
    }
  }, [kind, doc.next, doc.text]);

  const download = async () => {
    if (!f) return;
    if (f.stream) {
      // the hub sends it as an attachment, by Range: the browser saves it without holding it here
      const a = document.createElement("a");
      a.href = fileDownloadURL(mid, f.path, cwd);
      a.download = f.name || fileName(path);
      document.body.appendChild(a);
      a.click();
      a.remove();
      return;
    }
    setSaving(true);
    try {
      const blob =
        typeof f.data === "string"
          ? new Blob([base64Bytes(f.data)], { type: f.mime || "application/octet-stream" })
          : new Blob([doc.next > 0 ? await doc.all() : doc.text], { type: `${f.mime || "text/plain"};charset=utf-8` });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = f.name || fileName(path);
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 30_000);
    } finally {
      setSaving(false);
    }
  };

  const name = f?.name || fileName(path);
  const full = f?.path || path;
  const where = kind === "directory" ? shortPath(full) : shortPath(dirName(full)) || full; // the name is the title
  const actions = (
    <>
      {kind === "markdown" && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "预览"], ["source", "源码"]]} />}
      {kind === "json" && json?.pretty != null && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "格式化"], ["source", "原文"]]} />}
      {kind === "image" && typeof f?.text === "string" && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "图片"], ["source", "源码"]]} />}
      {kind === "html" && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "预览"], ["source", "源码"]]} />}
      {kind === "notebook" && notebook?.nb && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "笔记本"], ["source", "源文件"]]} />}
      {kind === "sheet" && typeof f?.text === "string" && <Segmented label="显示方式" value={mode} onChange={setMode} options={[["rendered", "表格"], ["source", "源文件"]]} />}
      <IconButton icon="copy" label="拷贝路径" onClick={() => copyText(f?.path || path)} />
      {kind !== "directory" && <IconButton icon="arrowDown" label={saving ? "正在下载" : "下载"} onClick={download} disabled={!f || saving} />}
    </>
  );

  let body: React.ReactNode;
  if (doc.error) {
    body = (
      <Empty
        icon="info"
        title="打不开这个文件"
        detail={doc.error}
        action={
          <Button icon="retry" onClick={doc.reload}>
            重试
          </Button>
        }
      />
    );
  } else if (!f || !kind) {
    body = (
      <div className="flex flex-1 items-center justify-center">
        <Loading label="正在读取" />
      </div>
    );
  } else if (kind === "directory") {
    body = <FolderList file={f} onOpen={onOpenPath} />;
  } else if (kind === "image" && !(typeof f.text === "string" && mode === "source")) {
    body = <ImageView file={f} src={inlineSrc(mid, f, cwd)} fallback={/^image\/hei[cf]/.test(f.mime ?? "") || /\.(heic|heif)$/i.test(f.name) ? fileThumbURL(mid, f.path, cwd, 1024) : undefined} />;
  } else if (kind === "video" || kind === "audio") {
    body = <MediaView mid={mid} cwd={cwd} file={f} kind={kind} />;
  } else if (kind === "pdf") {
    body = <PdfView file={f} src={f.stream ? inlineSrc(mid, f, cwd) : ""} />;
  } else if (kind === "html" && mode === "rendered" && !(doc.next > 0 && !wantAll)) {
    body =
      doc.next > 0 ? (
        <div className="flex flex-1 items-center justify-center">
          <Loading label={`正在读取 · ${bytes(f.size)}`} />
        </div>
      ) : (
        <HtmlPreview html={doc.text} title={f.name} mid={mid} path={f.path} />
      );
  } else if (kind === "notebook" && mode === "rendered" && !notebook?.error && !(doc.next > 0 && !wantAll)) {
    body = notebook?.nb ? (
      <NotebookView nb={notebook.nb} />
    ) : (
      <div className="flex flex-1 items-center justify-center">
        <Loading label={`正在读取笔记本 · ${bytes(f.size)}`} />
      </div>
    );
  } else if (kind === "office") {
    body = <OfficeView mid={mid} cwd={cwd} file={f} onDownload={download} />;
  } else if (kind === "sheet" && mode === "rendered") {
    body = <SheetLoader mid={mid} cwd={cwd} doc={doc} file={f} onDownload={download} />;
  } else if (kind === "binary") {
    body = (
      <Empty
        icon="read"
        title="无法预览"
        detail={`${f.mime || "二进制文件"} · ${bytes(f.size)}`}
        action={
          <Button icon="arrowDown" onClick={download} busy={saving}>
            下载
          </Button>
        }
      />
    );
  } else if (kind === "markdown" && mode === "rendered") {
    body = <MarkdownView doc={doc} file={f} mid={mid} onOpenPath={onOpenPath} />;
  } else {
    const shown = kind === "json" && mode === "rendered" && json?.pretty != null ? json.pretty : doc.text;
    const note =
      kind === "html" && doc.next > 0 && !wantAll
        ? "页面太大，按源码显示"
        : kind === "notebook" && notebook?.error
        ? `${notebook.error}，按文本显示`
        : kind === "notebook" && doc.next > 0 && !wantAll
          ? "笔记本太大，按文本显示"
          : kind === "json" && wantAll
        ? `已读 ${bytes(doc.next)} / ${bytes(f.size)}，正在读完整个文件…`
        : kind === "json" && doc.next > 0
          ? "文件太大，按文本显示"
          : json && "big" in json
            ? "文件太大，没有格式化"
            : json?.error
              ? `不是有效的 JSON，按文本显示：${json.error}`
              : "";
    body = <CodeView doc={doc} file={f} text={shown} line={line} note={note} />;
  }

  return (
    <>
      {onBack ? (
        <BackHeader title={name} subtitle={where} actions={actions} onBack={onBack} onClose={onClose} />
      ) : (
        <PanelHeader icon={kind === "directory" ? "folder" : kind === "image" ? "image" : "read"} title={name} subtitle={where} actions={actions} onClose={onClose} />
      )}
      <div className="flex min-h-0 flex-1 flex-col" key={path}>
        {body}
      </div>
    </>
  );
}

// ── code ──────────────────────────────────────────────────

export const TOKEN_CLASS: Record<TokenKind, string> = {
  comment: "text-ink-3",
  string: "text-green",
  number: "text-accent-ink",
  const: "text-accent-ink",
  key: "text-red",
  add: "text-green",
  del: "text-red",
  meta: "text-ink-3",
};

function CodeView({ doc, file, text, line, note }: { doc: FileDoc; file: FileContent; text: string; line?: number; note: string }) {
  const name = file.name;
  const lines = useMemo(() => splitLines(text), [text]);
  const lang = useMemo<Lang | null>(() => (text.length <= COLOR_BYTES && lines.length <= COLOR_LINES ? languageOf(name, lines[0]) : null), [name, text.length, lines]);
  const plainBecauseBig = !lang && (text.length > COLOR_BYTES || lines.length > COLOR_LINES) && languageOf(name, lines[0]) !== null;
  // The lexer's state at each line start, filled as far as the window has gone (a page appended keeps the lines before).
  const states = useMemo<LexState[]>(() => [], [lang, text]); // new text or language: start over
  const width = useMemo(() => lines.reduce((m, l) => Math.max(m, l.length > MAX_LINE ? MAX_LINE + 1 : columns(l)), 0), [lines]);
  const digits = String(Math.max(lines.length, 9)).length;
  const scroller = useRef<HTMLDivElement>(null);
  const [start, end] = useWindow(scroller, lines.length, LH, PAD);
  if (lang) extendStates(lines, lang, states, end);

  // Open at a line: read on until it is there, then put it a third of the way down and mark it.
  const placed = useRef(false);
  const { more, next, loadingMore } = doc;
  useEffect(() => {
    if (!line || placed.current) return;
    if (line > lines.length && next > 0 && !loadingMore) void more();
  }, [line, lines.length, next, loadingMore, more]);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!line || placed.current || !el || line > lines.length) return;
    placed.current = true;
    el.scrollTop = Math.max(0, PAD + (line - 1) * LH - el.clientHeight / 3);
  }, [line, lines.length]);

  const notes = [note, file.truncated && !file.next ? "文件太大，只显示了开头的部分" : "", plainBecauseBig ? "文件太大，没有上色" : ""].filter(Boolean);
  const rows: React.ReactNode[] = [];
  for (let i = start; i < end; i++) {
    const raw = lines[i];
    const cut = raw.length > MAX_LINE;
    const shown = cut ? raw.slice(0, MAX_LINE) : raw;
    const tokens = lang ? lexLine(shown, lang, states[i] ?? null).tokens : [];
    rows.push(<CodeRow key={i} n={i + 1} digits={digits} text={shown} tokens={tokens} cut={cut} mark={i + 1 === line} />);
  }

  return (
    <>
      <div className="flex min-h-8 shrink-0 flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-line px-3 py-1.5 text-[11.5px] text-ink-3">
        <span className="whitespace-nowrap tabular-nums">{lines.length.toLocaleString()} 行</span>
        <span>·</span>
        <span className="whitespace-nowrap">{bytes(file.size)}</span>
        {lang?.id && (
          <>
            <span>·</span>
            <span className="whitespace-nowrap">{lang.id}</span>
          </>
        )}
        {notes.map((n) => (
          <span key={n} className="min-w-0 text-orange">
            {n}
          </span>
        ))}
      </div>
      {lines.length === 0 ? (
        <Empty icon="read" title="空文件" />
      ) : (
        <div ref={scroller} className="min-h-0 flex-1 overflow-auto bg-inset font-mono text-[12px]" style={{ lineHeight: `${LH}px` }} data-testid="file-code">
          <div style={{ minWidth: `calc(${digits}ch + ${width}ch + 25px + 36px)`, paddingTop: PAD, paddingBottom: PAD }}>
            <div style={{ height: start * LH }} />
            {rows}
            <div style={{ height: (lines.length - end) * LH }} />
            {doc.next > 0 && <MoreButton doc={doc} file={file} />}
          </div>
        </div>
      )}
    </>
  );
}

function CodeRow({ n, digits, text, tokens, cut, mark }: { n: number; digits: number; text: string; tokens: Token[]; cut: boolean; mark: boolean }) {
  const parts: React.ReactNode[] = [];
  let at = 0;
  for (const t of tokens) {
    if (t.from > at) parts.push(text.slice(at, t.from));
    parts.push(
      <span key={t.from} className={TOKEN_CLASS[t.kind]}>
        {text.slice(t.from, t.to)}
      </span>,
    );
    at = t.to;
  }
  if (at < text.length) parts.push(text.slice(at));
  return (
    <div className={cx("flex", mark && "bg-accent-tint")} style={{ height: LH }} data-line={n}>
      <span
        className={cx("sticky left-0 shrink-0 border-r border-line pr-3 pl-3 text-right tabular-nums select-none", mark ? "font-semibold text-accent-ink" : "bg-inset text-ink-3")}
        style={{ width: `calc(${digits}ch + 25px)`, ...(mark ? { background: "linear-gradient(var(--accent-tint), var(--accent-tint)), var(--inset)" } : {}) }}
      >
        {n}
      </span>
      <span className="pr-6 pl-3 whitespace-pre text-ink">
        {parts}
        {cut && <span className="text-ink-3 select-none">… （这一行太长，只显示前 {MAX_LINE} 个字符）</span>}
      </span>
    </div>
  );
}

function MoreButton({ doc, file }: { doc: FileDoc; file: FileContent }) {
  return (
    <div className="sticky left-0 flex w-full max-w-[100vw] items-center gap-3 px-3 pt-3 pb-2 font-sans">
      <Button icon="arrowDown" onClick={() => void doc.more()} busy={doc.loadingMore}>
        继续加载
      </Button>
      <span className="text-[11.5px] whitespace-nowrap text-ink-3">
        已读 {bytes(doc.next)} / {bytes(file.size)}
      </span>
    </div>
  );
}

// ── Markdown ──────────────────────────────────────────────

const images = new Map<string, Promise<string | null>>();

/** An image a Markdown file refers to, read from the same machine (cached for the page's life). */
function machineImage(mid: string, path: string): Promise<string | null> {
  const key = `${mid}\u0000${path}`;
  let p = images.get(key);
  if (!p) {
    if (images.size > 64) images.clear();
    p = api<FileContent>(fileURL(mid, path))
      .then((f) => (typeof f.data === "string" && f.mime?.startsWith("image/") ? `data:${f.mime};base64,${f.data}` : null))
      .catch(() => null);
    images.set(key, p);
  }
  return p;
}

function MarkdownView({ doc, file, mid, onOpenPath }: { doc: FileDoc; file: FileContent; mid: string; onOpenPath: (p: string) => void }) {
  const chunks = useMemo(() => markdownChunks(simplifyHTML(doc.text)), [doc.text]);
  const box = useRef<HTMLDivElement>(null);
  const base = dirName(file.path);

  // Relative images point at the machine, not at this page: swap them for the bytes read from there before paint.
  useLayoutEffect(() => {
    for (const img of box.current?.querySelectorAll("img") ?? []) {
      const src = img.getAttribute("src");
      if (!src || img.dataset.machine || !isMachineRef(src)) continue;
      const p = resolveRef(base, src);
      img.dataset.machine = p;
      img.removeAttribute("src");
      img.title = p;
      void machineImage(mid, p).then((url) => {
        if (url && img.dataset.machine === p) img.src = url;
      });
    }
  }, [chunks, mid, base]);

  // Links to files next to it open in the viewer; in-page anchors stay put.
  const click = (e: React.MouseEvent) => {
    if (e.defaultPrevented) return; // the link already went where it belongs (lib/links.ts: a document, a conversation)
    const a = (e.target as HTMLElement).closest("a");
    const href = a?.getAttribute("href");
    if (!href) return;
    if (href.startsWith("#")) e.preventDefault();
    else if (isMachineRef(href)) {
      e.preventDefault();
      onOpenPath(resolveRef(base, href));
    }
  };

  return (
    <div className="min-h-0 flex-1 overflow-y-auto" onClick={click} data-testid="file-markdown">
      <div ref={box} className="mx-auto flex max-w-[760px] flex-col gap-4 px-5 py-5 sm:px-7">
        {chunks.map((c, i) => (
          <div key={i} style={{ contentVisibility: "auto", containIntrinsicSize: "auto 480px" }}>
            <Markdown text={c} />
          </div>
        ))}
        {doc.next > 0 && (
          <div className="flex items-center gap-3 pt-2">
            <Button icon="arrowDown" onClick={() => void doc.more()} busy={doc.loadingMore}>
              继续加载
            </Button>
            <span className="text-[11.5px] whitespace-nowrap text-ink-3">
              已读 {bytes(doc.next)} / {bytes(file.size)}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

// ── tables ────────────────────────────────────────────────

/** Reads a table file whole (a CSV's remaining pages, a workbook's bytes) and parses it off the main thread. */
function SheetLoader({ mid, cwd, doc, file, onDownload }: { mid: string; cwd?: string; doc: FileDoc; file: FileContent; onDownload: () => void }) {
  const [state, setState] = useState<{ sheets?: Sheet[]; error?: string; note?: string }>({});
  const { all } = doc;
  useEffect(() => {
    const ac = new AbortController();
    setState({});
    (async () => {
      let req: SheetRequest;
      let note = "";
      if (typeof file.text === "string") {
        const text = await all(MAX_TABLE_TEXT);
        if (ac.signal.aborted) return;
        if (text.length < file.size && file.size > MAX_TABLE_TEXT) note = `文件太大，只读了前 ${bytes(text.length)}`;
        req = { name: file.name, text };
      } else {
        if (file.size > MAX_TABLE_BYTES) throw new Error(`文件有 ${bytes(file.size)}，太大了，下载后用表格软件打开`);
        req = { name: file.name, bytes: await fileBytes(mid, file, cwd, ac.signal) };
      }
      const sheets = await parseSheets(req, ac.signal);
      if (!ac.signal.aborted) setState({ sheets, note });
    })().catch((e) => {
      if (!isAbort(e) && !ac.signal.aborted) setState({ error: errorText(e) });
    });
    return () => ac.abort();
  }, [mid, cwd, file, all]);
  // a Numbers file SheetJS cannot read still has the preview Numbers wrote into it
  if (state.error && extOf(file.name) === "numbers") return <OfficeView mid={mid} cwd={cwd} file={file} onDownload={onDownload} />;
  if (state.error) return <Empty icon="info" title="表格打不开" detail={state.error} />;
  if (!state.sheets)
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loading label={typeof file.text === "string" ? "正在读取表格" : `正在读取表格 · ${bytes(file.size)}`} />
      </div>
    );
  if (!state.sheets.length) return <Empty icon="info" title="表格里没有工作表" />;
  return <SheetView sheets={state.sheets} note={state.note} />;
}

// ── images, PDFs, folders ─────────────────────────────────

/**
 * An image; `fallback` is what to show when this browser cannot draw it (HEIC outside Safari): the picture the machine
 * draws for its thumbnail.
 */
function ImageView({ file, src: original, fallback }: { file: FileContent; src: string; fallback?: string }) {
  const [dims, setDims] = useState<[number, number] | null>(null);
  const [big, setBig] = useState(false);
  const [stage, setStage] = useState<"original" | "fallback" | "failed">("original");
  const src = stage === "fallback" && fallback ? fallback : original;
  const failed = () => setStage((s) => (s === "original" && fallback ? "fallback" : "failed"));
  if (stage === "failed")
    return <Empty icon="image" title="这个浏览器显示不了这张图" detail={`${file.mime || "图片"} · ${bytes(file.size)}。Safari 能直接显示 HEIC；也可以下载到本机看。`} />;
  // A tiny image (an icon) is drawn bigger, pixel for pixel, so it can be seen.
  const scale = dims ? Math.max(1, Math.min(8, Math.floor(160 / Math.max(dims[0], dims[1])))) : 1;
  return (
    <>
      <div className="flex min-h-8 shrink-0 items-center gap-2 border-b border-line px-3 py-1.5 text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums">
        {dims && (
          <>
            <span>
              {dims[0]} × {dims[1]}
            </span>
            <span>·</span>
          </>
        )}
        <span>{bytes(file.size)}</span>
        <span>·</span>
        <span>{file.mime}</span>
        {stage === "fallback" && <span className="truncate">· 浏览器不支持这种格式，显示的是那台电脑生成的预览（最长边 1024 像素）</span>}
        {scale > 1 && <span className="ml-auto">放大 {scale}×</span>}
      </div>
      <div
        className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-6"
        style={{ backgroundImage: "conic-gradient(var(--field) 25%, var(--inset) 0 50%, var(--field) 0 75%, var(--inset) 0)", backgroundSize: "20px 20px" }}
      >
        <button type="button" onClick={() => setBig(true)} aria-label="查看大图" className="max-h-full max-w-full cursor-zoom-in">
          <img
            src={src}
            alt={file.name}
            onLoad={(e) => setDims([e.currentTarget.naturalWidth, e.currentTarget.naturalHeight])}
            onError={failed}
            className="block max-h-full max-w-full shadow-hairline"
            style={dims && scale > 1 ? { width: dims[0] * scale, imageRendering: "pixelated" } : undefined}
          />
        </button>
      </div>
      {big && <Lightbox src={src} onClose={() => setBig(false)} />}
    </>
  );
}

/** A video or an audio file, playing from file/download by Range (or the old base64 when it is all there). */
function MediaView({ mid, cwd, file, kind }: { mid: string; cwd?: string; file: FileContent; kind: "video" | "audio" }) {
  if (!file.stream && file.truncated)
    return <Empty icon="info" title="hub 版本太旧，放不了大文件" detail={`${bytes(file.size)} · 更新 hub 后可以直接播放，或者先下载。`} />;
  return (
    <>
      <div className="flex min-h-8 shrink-0 items-center gap-2 border-b border-line px-3 py-1.5 text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums">
        <span>{bytes(file.size)}</span>
        <span>·</span>
        <span>{file.mime}</span>
      </div>
      <div className={cx("flex min-h-0 flex-1 items-center justify-center overflow-auto p-6", kind === "video" ? "bg-black/90" : "bg-inset")}>
        {kind === "video" ? <MediaPlayer mid={mid} path={file.path} cwd={cwd} kind="video" className="max-h-full max-w-full" /> : (
          <div className="flex w-full max-w-[560px] flex-col items-center gap-4 rounded-[16px] bg-surface p-6 shadow-hairline">
            <span className="flex size-16 items-center justify-center rounded-full bg-accent-tint text-accent-ink">
              <Icon name="play" size={26} />
            </span>
            <div className="max-w-full truncate text-[14px] font-medium text-ink">{file.name}</div>
            <MediaPlayer mid={mid} path={file.path} cwd={cwd} kind="audio" />
          </div>
        )}
      </div>
    </>
  );
}

/** A PDF in the browser's own viewer: straight from file/download (it reads by Range), or a blob of the old base64. */
function PdfView({ file, src }: { file: FileContent; src: string }) {
  const [url, setUrl] = useState(src);
  useEffect(() => {
    if (src) {
      setUrl(src);
      return;
    }
    const u = URL.createObjectURL(new Blob([base64Bytes(file.data ?? "")], { type: "application/pdf" }));
    setUrl(u);
    return () => URL.revokeObjectURL(u);
  }, [file, src]);
  return url ? <iframe src={url} title={file.name} className="min-h-0 w-full flex-1 border-0 bg-inset" /> : null;
}

function FolderList({ file, onOpen }: { file: FileContent; onOpen: (p: string) => void }) {
  const entries = file.entries ?? [];
  if (!entries.length) return <Empty icon="folder" title="空文件夹" />;
  return (
    <div className="min-h-0 flex-1 overflow-y-auto py-1">
      {entries.map((e) => {
        const dir = e.endsWith("/");
        const n = e.replace(/\/$/, "");
        return (
          <button
            key={e}
            type="button"
            onClick={() => onOpen(joinPath(file.path, n))}
            className="flex h-10 w-full items-center gap-3 px-4 text-left text-[13px] text-ink transition-colors hover:bg-hover"
          >
            <FileGlyph name={n} dir={dir} />
            <span className="min-w-0 flex-1 truncate">{n}</span>
          </button>
        );
      })}
      {entries.length >= 500 && <p className="px-4 py-2 text-[11.5px] text-ink-3">只列出了前 500 项</p>}
    </div>
  );
}
