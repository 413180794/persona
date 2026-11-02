// A link opened in the side panel (CLAUDE.md「链接在 App 内打开」, docs/web.md「链接」): Feishu documents in a reader
// (the phone's FeishuDocReader), removed posts as the post; any other page belongs in a browser tab (lib/links.ts says
// why), so one that lands here says so and opens the tab. Text selected in the reader can go to the agent:
// 「作为上下文」 rides along with the next message, 「AI 修改」 asks for a change at once (<jarvis-context>).
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, enc, getToken } from "../../lib/api";
import { ago } from "../../lib/format";
import { DOC_LABEL, docKind, linkRoute, newContext, type ContextRef } from "../../lib/links";
import { Button, copyText, cx, Dialog, EASE, IconButton, Menu, useMenu } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { PanelHeader } from "../SidePanel";
import { DocMarkdown, DocTree } from "./DocContent";
import { loadDoc, mediaURL, prepareMarkdown, readInfo, type DocContent, type DocInfo } from "./feishu-doc";
import { giveContext, giveEdit, handoffLabel } from "./handoff";
import { QuoteIcon } from "./QuoteIcon";

export function LinkPanel({ url, title, onClose }: { url: string; title?: string; onClose: () => void }) {
  const r = linkRoute(url);
  if (r.kind === "doc") return <DocPanel url={url} title={title} onClose={onClose} />;
  if (r.kind === "moments") return <MomentsPanel id={r.id} url={url} onClose={onClose} />;
  return <WebPanel url={url} title={title} onClose={onClose} />;
}

function host(url: string) {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

function openTab(url: string) {
  window.open(url, "_blank", "noopener,noreferrer");
}

/** ⋯ in the header: copy the link, read again. */
function MoreMenu({ url, onReload }: { url: string; onReload?: () => void }) {
  const menu = useMenu();
  return (
    <>
      <IconButton icon="more" label="更多" onClick={menu.open} active={Boolean(menu.anchor)} />
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          align="right"
          width={180}
          items={[
            { key: "copy", label: "拷贝链接", icon: "link", onSelect: () => void copyText(url) },
            ...(onReload ? [{ key: "reload", label: "重新载入", icon: "retry" as const, onSelect: onReload }] : []),
          ]}
        />
      )}
    </>
  );
}

function ContextButton({ onClick, label = "作为上下文（整篇）" }: { onClick: () => void; label?: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-[background-color,color,transform] duration-150 hover:bg-hover hover:text-ink active:scale-[0.94]"
    >
      <QuoteIcon />
    </button>
  );
}

// ── Feishu documents ──

function DocPanel({ url, title, onClose }: { url: string; title?: string; onClose: () => void }) {
  const kind = docKind(url);
  const [content, setContent] = useState<DocContent | null>(null);
  const [error, setError] = useState("");
  const [info, setInfo] = useState<DocInfo | null>(null);
  const [round, setRound] = useState(0);
  const [editing, setEditing] = useState<ContextRef | null>(null);

  useEffect(() => {
    let live = true;
    setError("");
    loadDoc(url, title, round > 0)
      .then((c) => live && setContent(c))
      .catch((e: unknown) => {
        if (!live) return;
        setContent(null);
        setError(e instanceof Error ? e.message : String(e));
      });
    readInfo(url)
      .then((i) => live && setInfo(i))
      .catch(() => {});
    return () => {
      live = false;
    };
    // the title only names it while it loads
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, round]);

  const docTitle = content?.title || title || DOC_LABEL[kind];
  const pick = useCallback(
    (text: string, k: ContextRef["kind"]) => {
      const ref = newContext(k, docTitle, url, text);
      if (k === "edit") setEditing(ref);
      else giveContext(ref);
    },
    [docTitle, url],
  );

  return (
    <>
      <PanelHeader
        icon="read"
        title={docTitle}
        subtitle={`${DOC_LABEL[content?.kind === "unsupported" ? content.doc : kind]} · ${host(url)}`}
        onClose={onClose}
        actions={
          <>
            {/* the agent reads the document itself: the whole of it can go along even before (or without) it reading here */}
            {content?.kind !== "unsupported" && <ContextButton onClick={() => pick("", "context")} />}
            <IconButton icon="external" label="在新标签页打开" onClick={() => openTab(url)} />
            <MoreMenu url={url} onReload={() => setRound((n) => n + 1)} />
          </>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[760px] px-5 pt-5 pb-10 sm:px-6">
          {content?.kind === "unsupported" ? (
            <Unsupported content={content} url={url} />
          ) : content ? (
            <DocBody content={content} info={info} onPick={pick} url={url} />
          ) : error ? (
            <Failed message={error} onRetry={() => setRound((n) => n + 1)} url={url} />
          ) : (
            <Skeleton title={title} />
          )}
        </div>
      </div>
      <EditDialog editing={editing} onClose={() => setEditing(null)} />
    </>
  );
}

function metaLine(info: DocInfo | null) {
  if (!info) return "";
  const parts: string[] = [];
  if (info.owner) parts.push(info.owner);
  const t = ago(info.updatedAt);
  if (t) parts.push(`更新于 ${t}`);
  if (info.views) parts.push(`${info.views} 次阅读`);
  if (info.comments) parts.push(`${info.comments} 条评论`);
  return parts.join(" · ");
}

function DocBody({ content, info, onPick, url }: { content: Extract<DocContent, { kind: "markdown" | "tree" }>; info: DocInfo | null; onPick: (text: string, kind: ContextRef["kind"]) => void; url: string }) {
  const markdown = useMemo(() => (content.kind === "markdown" ? prepareMarkdown(content.markdown, content.title, getToken()) : ""), [content]);
  const meta = metaLine(info);
  return (
    <div style={{ animation: `fade-in 200ms ease-out both` }}>
      <Pickable enabled={content.copy} onPick={onPick}>
        <h1 className="text-[22px] leading-[1.3] font-semibold tracking-tight text-ink">{content.title}</h1>
        {meta && <div className="mt-2 text-[12px] text-ink-3">{meta}</div>}
        <div className="mt-5">{content.kind === "markdown" ? <DocMarkdown markdown={markdown} /> : <DocTree doc={content.doc} />}</div>
      </Pickable>
      {content.partial && <p className="mt-4 text-[12px] text-ink-3">文档较长，只显示了前面的部分。</p>}
      {!content.copy && <p className="mt-4 text-[12px] text-ink-3">作者禁止复制这篇文档，不能选中文字；可以用右上角的「作为上下文」带上整篇。</p>}
      <div className="mt-10 flex justify-center border-t border-line pt-5">
        <Button icon="external" onClick={() => openTab(url)}>
          在新标签页打开
        </Button>
      </div>
    </div>
  );
}

function Unsupported({ content, url }: { content: Extract<DocContent, { kind: "unsupported" }>; url: string }) {
  return (
    <div className="flex flex-col items-center gap-3 pt-12 text-center" style={{ animation: `fade-up 300ms ${EASE} both` }}>
      {content.image ? (
        <a href={mediaURL(content.image)} target="_blank" rel="noreferrer noopener" className="block w-full">
          <img src={mediaURL(content.image)} alt={content.title} className="w-full rounded-card bg-inset shadow-hairline" />
        </a>
      ) : (
        <span className="flex size-14 items-center justify-center rounded-[14px] bg-field text-ink-2">
          <Icon name={content.doc === "file" ? "clip" : "read"} size={24} />
        </span>
      )}
      <div>
        <div className="text-[15px] font-semibold text-ink">{content.title}</div>
        <div className="mt-1 text-[12.5px] text-ink-2">{content.message}</div>
      </div>
      <div className="mt-2 flex flex-wrap justify-center gap-2">
        {content.file && (
          <a
            href={mediaURL(`/api/v1/lark/raw/documents/drive/files/${enc(content.file)}/download`)}
            download
            className="inline-flex h-8 items-center gap-1.5 rounded-control bg-ink px-3 text-[12.5px] font-medium whitespace-nowrap text-surface"
          >
            <Icon name="arrowDown" size={14} stroke={2.1} /> 下载文件
          </a>
        )}
        <Button tone={content.file ? "secondary" : "primary"} icon="external" onClick={() => openTab(url)}>
          在新标签页打开
        </Button>
      </div>
    </div>
  );
}

function Failed({ message, onRetry, url }: { message: string; onRetry: () => void; url: string }) {
  return (
    <div role="alert" className="flex flex-col items-center gap-3 pt-14 text-center" style={{ animation: `fade-up 300ms ${EASE} both` }}>
      <span className="flex size-12 items-center justify-center rounded-[12px] bg-red-tint text-red">
        <Icon name="info" size={20} />
      </span>
      <div>
        <div className="text-[14.5px] font-semibold text-ink">打不开这篇文档</div>
        <div className="mt-1 max-w-[340px] text-[12.5px] text-ink-2">{message}</div>
      </div>
      <div className="mt-1 flex flex-wrap justify-center gap-2">
        <Button icon="retry" onClick={onRetry}>
          重试
        </Button>
        <Button tone="primary" icon="external" onClick={() => openTab(url)}>
          在新标签页打开
        </Button>
      </div>
    </div>
  );
}

const BARS = [92, 86, 58, 95, 80, 44, 90, 84, 52];

function Skeleton({ title }: { title?: string }) {
  return (
    <div aria-busy="true" aria-label="正在读取文档" className="flex flex-col gap-3">
      {title ? <h1 className="text-[22px] leading-[1.3] font-semibold tracking-tight text-ink">{title}</h1> : <div className="h-6 w-2/3 animate-pulse rounded-[6px] bg-field" />}
      <div className="h-3 w-1/3 animate-pulse rounded-[4px] bg-field" />
      <div className="mt-3 flex flex-col gap-2.5">
        {BARS.map((w, i) => (
          <div key={i} className="h-3.5 animate-pulse rounded-[4px] bg-field" style={{ width: `${w}%`, animationDelay: `${i * 60}ms` }} />
        ))}
      </div>
    </div>
  );
}

// ── removed posts ──

interface MomentsPost {
  userName?: string;
  createTime?: number | string;
  commentCount?: number;
  reactionCount?: number;
  categoryNames?: string[];
  content?: { text?: string };
}

interface MomentsComment {
  id: string;
  userName?: string;
  createTime?: number | string;
  replyUserName?: string;
  content?: { text?: string };
}

function when(t?: number | string) {
  const n = typeof t === "string" ? Number(t) : t;
  if (!n || !Number.isFinite(n)) return "";
  return ago(new Date(n > 1e11 ? n : n * 1000).toISOString());
}

function MomentsPanel({ id, url, onClose }: { id: string; url: string; onClose: () => void }) {
  const [post, setPost] = useState<MomentsPost | null>(null);
  const [comments, setComments] = useState<MomentsComment[]>([]);
  const [error, setError] = useState("");
  const [round, setRound] = useState(0);
  const [editing, setEditing] = useState<ContextRef | null>(null);

  useEffect(() => {
    let live = true;
    setError("");
    // the explicit token: a Feishu-side 401 must not sign this browser out of the hub
    api<MomentsPost>(`/lark/moments/posts/${enc(id)}`, { token: getToken() })
      .then((p) => live && setPost(p))
      .catch((e: unknown) => live && setError(e instanceof Error ? e.message : String(e)));
    api<{ comments?: MomentsComment[] }>(`/lark/moments/posts/${enc(id)}/comments`, { token: getToken() })
      .then((c) => live && setComments(c.comments ?? []))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [id, round]);

  const name = post?.userName ? `removed · ${post.userName}` : "removed";
  const pick = (text: string, k: ContextRef["kind"]) => {
    const ref = newContext(k, name, url, text);
    if (k === "edit") setEditing(ref);
    else giveContext(ref);
  };

  return (
    <>
      <PanelHeader
        icon="chat"
        title={name}
        subtitle={host(url)}
        onClose={onClose}
        actions={
          <>
            {post && <ContextButton label="这条帖子作为上下文" onClick={() => pick(post.content?.text ?? "", "context")} />}
            <IconButton icon="external" label="在新标签页打开" onClick={() => openTab(url)} />
            <MoreMenu url={url} onReload={() => setRound((n) => n + 1)} />
          </>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[680px] px-5 pt-5 pb-10 sm:px-6">
          {post ? (
            <Pickable enabled onPick={pick}>
              <div className="flex items-center gap-2.5">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-field text-[13px] font-semibold text-ink-2">{(post.userName ?? "?").slice(0, 1)}</span>
                <div className="min-w-0">
                  <div className="truncate text-[13.5px] font-semibold text-ink">{post.userName}</div>
                  <div className="text-[11.5px] text-ink-3">{when(post.createTime)}</div>
                </div>
              </div>
              <p className="mt-3 text-[14.5px] leading-[1.7] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{post.content?.text}</p>
              <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[12px] text-ink-3">
                {(post.categoryNames ?? []).map((c) => (
                  <span key={c} className="rounded-chip bg-field px-1.5 py-0.5 text-ink-2">
                    {c}
                  </span>
                ))}
                {Boolean(post.reactionCount) && <span>{post.reactionCount} 个赞</span>}
                {Boolean(post.commentCount) && <span>· {post.commentCount} 条评论</span>}
              </div>
              {comments.length > 0 && (
                <div className="mt-6 border-t border-line pt-4">
                  <div className="mb-2 text-[12px] font-medium text-ink-3">评论 {comments.length}</div>
                  <div className="flex flex-col gap-3">
                    {comments.map((c) => (
                      <div key={c.id} className="text-[13px] leading-[1.6]">
                        <div className="text-[12px] text-ink-3">
                          <span className="font-medium text-ink-2">{c.userName}</span>
                          {c.replyUserName && <> 回复 <span className="font-medium text-ink-2">{c.replyUserName}</span></>}
                          {when(c.createTime) && <> · {when(c.createTime)}</>}
                        </div>
                        <div className="whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{c.content?.text}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </Pickable>
          ) : error ? (
            <Failed message={error} onRetry={() => setRound((n) => n + 1)} url={url} />
          ) : (
            <Skeleton />
          )}
        </div>
      </div>
      <EditDialog editing={editing} onClose={() => setEditing(null)} />
    </>
  );
}

// ── any other page ──

function WebPanel({ url, title, onClose }: { url: string; title?: string; onClose: () => void }) {
  const web = linkRoute(url).kind === "web";
  return (
    <>
      <PanelHeader icon="web" title={title || host(url)} subtitle={host(url)} onClose={onClose} actions={<MoreMenu url={url} />} />
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 pb-[10vh] text-center">
        <span className="flex size-12 items-center justify-center rounded-[12px] bg-field text-ink-2">
          <Icon name="web" size={22} />
        </span>
        <div className="max-w-[360px]">
          <div className="text-[14.5px] font-semibold text-ink">{web ? "网页在浏览器的新标签页里打开" : "这个链接在网页里打不开"}</div>
          <div className="mt-1 text-[12.5px] text-ink-2">{web ? "多数网站不允许被嵌进别的页面，嵌进来只会是一片空白。" : "它要交给别的 App 处理，可以拷贝下来。"}</div>
          <div className="mt-2 text-[12px] break-all text-ink-3">{url}</div>
        </div>
        <div className="mt-1 flex flex-wrap justify-center gap-2">
          <Button icon="link" onClick={() => void copyText(url)}>
            拷贝链接
          </Button>
          {web && (
            <Button tone="primary" icon="external" onClick={() => openTab(url)}>
              在新标签页打开
            </Button>
          )}
        </div>
      </div>
    </>
  );
}

// ── picking text ──

interface Tip {
  x: number;
  y: number;
  below: boolean;
  text: string;
}

/**
 * Content whose selected text can be handed on: a small bar over the selection with 「作为上下文」 and 「AI 修改」
 * (mouse, keyboard and touch selections alike; it shows once the pointer is up, not while dragging).
 */
function Pickable({ enabled, onPick, children }: { enabled: boolean; onPick: (text: string, kind: ContextRef["kind"]) => void; children: React.ReactNode }) {
  const box = useRef<HTMLDivElement>(null);
  const [tip, setTip] = useState<Tip | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let down = false;
    let timer = 0;
    const read = () => {
      const el = box.current;
      const sel = getSelection();
      if (!el || !sel || sel.isCollapsed || !sel.rangeCount) return setTip(null);
      const range = sel.getRangeAt(0);
      if (!el.contains(range.commonAncestorContainer)) return setTip(null);
      const text = sel.toString().trim();
      if (!text) return setTip(null);
      const r = range.getBoundingClientRect();
      const b = el.getBoundingClientRect();
      const scroller = el.closest(".overflow-y-auto");
      const top = scroller ? scroller.getBoundingClientRect().top : 0;
      const below = r.top - Math.max(top, 0) < 52;
      const half = 92;
      const x = Math.min(Math.max(r.left + r.width / 2 - b.left, half), Math.max(half, b.width - half));
      setTip({ x, y: below ? r.bottom - b.top + 8 : r.top - b.top - 8, below, text });
    };
    const onDown = (e: PointerEvent) => {
      if ((e.target as HTMLElement).closest?.("[data-pickbar]")) return;
      down = true;
    };
    const onUp = () => {
      down = false;
      clearTimeout(timer);
      timer = window.setTimeout(read, 10);
    };
    const onChange = () => {
      clearTimeout(timer);
      const sel = getSelection();
      if (!sel || sel.isCollapsed) return setTip(null);
      if (!down) timer = window.setTimeout(read, 250); // keyboard / touch selections settle on their own
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("pointerup", onUp, true);
    document.addEventListener("selectionchange", onChange);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("pointerup", onUp, true);
      document.removeEventListener("selectionchange", onChange);
    };
  }, [enabled]);

  const choose = (kind: ContextRef["kind"]) => {
    if (!tip) return;
    onPick(tip.text, kind);
    getSelection()?.removeAllRanges();
    setTip(null);
  };

  return (
    <div ref={box} className={cx("relative", !enabled && "select-none")}>
      {children}
      {tip && (
        <div
          data-pickbar
          role="toolbar"
          aria-label="选中的文字"
          onMouseDown={(e) => e.preventDefault()}
          className="absolute z-20 flex items-center gap-0.5 rounded-[10px] border border-tooltip-border bg-tooltip-bg p-1 text-tooltip-fg shadow-overlay"
          style={{ left: tip.x, top: tip.y, translate: `-50% ${tip.below ? "0" : "-100%"}`, animation: `pop-in 140ms ${EASE} both`, transformOrigin: tip.below ? "top center" : "bottom center" }}
        >
          <PickButton onClick={() => choose("context")} label="作为上下文" title={`随下一条消息发给${handoffLabel()}`}>
            <QuoteIcon size={13} />
          </PickButton>
          <PickButton onClick={() => choose("edit")} label="AI 修改" title={`写下要怎么改，立即发给${handoffLabel()}`}>
            <Icon name="spark" size={12} />
          </PickButton>
        </div>
      )}
    </div>
  );
}

function PickButton({ onClick, label, title, children }: { onClick: () => void; label: string; title: string; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className="flex h-7 items-center gap-1.5 rounded-[7px] px-2 text-[12px] font-medium whitespace-nowrap transition-colors hover:bg-tooltip-border"
    >
      {children}
      {label}
    </button>
  );
}

// 「AI 修改」: what to change, then it goes at once.
function EditDialog({ editing, onClose }: { editing: ContextRef | null; onClose: () => void }) {
  const [ask, setAsk] = useState("");
  useEffect(() => setAsk(""), [editing]);
  const send = () => {
    if (!editing) return;
    giveEdit(editing, ask);
    onClose();
  };
  return (
    <Dialog
      open={Boolean(editing)}
      onClose={onClose}
      title="让 Agent 改这段"
      footer={
        <>
          <span className="mr-auto truncate text-[11.5px] text-ink-3">发给{handoffLabel()}</span>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" icon="send" onClick={send}>
            发送
          </Button>
        </>
      }
    >
      <blockquote className="line-clamp-4 border-l-2 border-line-strong pl-2.5 text-[12.5px] leading-[1.6] whitespace-pre-wrap text-ink-2">{editing?.text}</blockquote>
      <textarea
        autoFocus
        rows={3}
        value={ask}
        onChange={(e) => setAsk(e.target.value)}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing || e.keyCode === 229) return;
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
        placeholder="怎么改？（不填就按选中的内容改）"
        aria-label="怎么改"
        className="mt-3 w-full resize-none rounded-[10px] bg-field px-3 py-2 text-[13.5px] leading-[1.55] text-ink outline-none placeholder:text-ink-3 focus:shadow-hairline"
      />
    </Dialog>
  );
}
