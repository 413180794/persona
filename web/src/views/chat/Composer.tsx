// Beautiful UI's Prompt Bar, wired to a session: multi-line input, / commands, @ files and sessions, image attachments
// (paste, drop, pick; click one to mark it up), files uploaded to the session's machine, send — and stop while the
// agent works and nothing is typed. Quotes, edits and edited images from the chat arrive as draft events (drafts.ts).
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ApiError, api, enc, errorText } from "../../lib/api";
import { uuid } from "../../lib/format";
import { imagePayload, MAX_IMAGE, quotePath } from "../../lib/attachments";
import { activateComposer, addContext, getContexts, newContext, registerComposer, removeContext, useContexts, withContexts, type ComposerHandle } from "../../lib/links";
import { applyDraft, onDraftEvent, replaceNeedsConfirm, type DraftImage } from "../../lib/drafts";
import { browseTarget, mentionAt, replaceToken, sessionQuote } from "../../lib/mentions";
import { AttachmentTray } from "../../ui/AttachmentTray";
import { cx, EASE, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ContextChips } from "../link/ContextChips";
import { ImageEditor } from "./ImageEditor";
import { MentionMenu, useMentionRows, type MentionRow, type MentionSource } from "./MentionMenu";

export interface Attachment {
  id: string;
  name: string;
  mime: string;
  data: string; // base64
  url: string; // object URL for the preview
}

export interface Outgoing {
  text: string;
  images: { mime: string; data: string }[];
}

interface Command {
  word: string;
  desc: string;
  text: string;
  confirm?: string;
}

export const AGENT_COMMANDS: Command[] = [
  { word: "/继续", desc: "让它接着做", text: "继续" },
  { word: "/提交", desc: "按规范提交并推送", text: "把当前改动按规范提交并推送" },
  { word: "/汇报", desc: "汇报进展、还剩什么", text: "简要汇报一下目前的进展、还剩什么" },
  { word: "/compact", desc: "压缩上下文", text: "/compact" },
  { word: "/context", desc: "查看上下文", text: "/context" },
  { word: "/model", desc: "切换模型", text: "/model " },
  { word: "/clear", desc: "清空对话，从头开始", text: "/clear", confirm: "清空这个 Agent 的对话？它会忘掉之前的上下文。" },
];

function parseSlash(draft: string) {
  const m = /^\/(\S*)$/.exec(draft);
  return m ? m[1].toLowerCase() : null;
}

async function readImage(file: File): Promise<Attachment> {
  return { ...await imagePayload(file), id: uuid(), name: file.name || "image.png", url: URL.createObjectURL(file) };
}

function fromDraftImage(img: DraftImage, id = uuid()): Attachment {
  const bin = atob(img.data);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return { ...img, id, url: URL.createObjectURL(new Blob([bytes], { type: img.mime })) };
}

export function Composer({
  draftKey,
  placeholder,
  machineId,
  working,
  disabled,
  commands = AGENT_COMMANDS,
  onSend,
  onStop,
  autoFocus,
  footer,
  suggestion,
  mention,
}: {
  draftKey: string;
  placeholder: string;
  machineId?: string;
  working?: boolean;
  disabled?: boolean;
  commands?: Command[];
  onSend: (m: Outgoing) => Promise<boolean>;
  onStop?: () => void;
  autoFocus?: boolean;
  footer?: React.ReactNode;
  suggestion?: string;
  /** Turns on the @ menu: the chat's files and folder, and other sessions. */
  mention?: MentionSource;
}) {
  const [draft, setDraft] = useState(() => localStorage.getItem(`draft:${draftKey}`) ?? "");
  const [atts, setAtts] = useState<Attachment[]>([]);
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [active, setActive] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [rowBox, setRowBox] = useState<{ top: number; height: number } | null>(null);
  const [caret, setCaret] = useState(0);
  const [focused, setFocused] = useState(false);
  const [mActive, setMActive] = useState(0);
  const [editing, setEditing] = useState<Attachment | null>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const rowRefs = useRef<(HTMLButtonElement | null)[]>([]);
  // text picked in the link panel waits here (lib/links.ts), and 「AI 修改」 sends through this composer
  const contexts = useContexts(draftKey);
  const sendNow = useRef<(text: string) => Promise<boolean>>(async () => false);
  sendNow.current = async (text) => (disabled ? false : onSend({ text, images: [] }));
  const canTake = useRef(!disabled);
  canTake.current = !disabled;
  const handle = useMemo<ComposerHandle>(() => ({ key: draftKey, send: (text) => sendNow.current(text), focus: () => input.current?.focus(), ready: () => canTake.current }), [draftKey]);
  useEffect(() => registerComposer(handle), [handle]);

  // a different conversation: its own draft
  useEffect(() => {
    setDraft(localStorage.getItem(`draft:${draftKey}`) ?? "");
    setAtts([]);
    if (autoFocus) setTimeout(() => input.current?.focus(), 0);
  }, [draftKey, autoFocus]);

  // 引用 / 编辑 / a queued message pulled back / an edited picture, from the chat (lib/drafts.ts)
  const draftNow = useRef(draft);
  draftNow.current = draft;
  useEffect(
    () =>
      onDraftEvent(draftKey, (e) => {
        if (!canTake.current) return;
        if (e.type === "image") {
          setAtts((cur) => [...cur, fromDraftImage(e.image)]);
          input.current?.focus();
          return;
        }
        if (e.type === "replace" && replaceNeedsConfirm(draftNow.current, e.text) && !confirm("输入框里有没发出去的字，换成这条消息？")) return;
        const next = applyDraft(draftNow.current, e);
        setDraft(next);
        setDismissed(false);
        requestAnimationFrame(() => {
          const el = input.current;
          if (!el) return;
          el.focus();
          el.setSelectionRange(next.length, next.length);
          setCaret(next.length);
        });
      }),
    [draftKey],
  );

  useEffect(() => {
    const t = setTimeout(() => {
      if (draft) localStorage.setItem(`draft:${draftKey}`, draft);
      else localStorage.removeItem(`draft:${draftKey}`);
    }, 250);
    return () => clearTimeout(t);
  }, [draft, draftKey]);

  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "0px";
    const h = el.scrollHeight;
    el.style.height = `${Math.min(Math.max(h, 24), 240)}px`;
    el.style.overflowY = h > 240 ? "auto" : "hidden";
  }, [draft]);

  const query = dismissed ? null : parseSlash(draft);
  const rows = useMemo(() => (query === null ? [] : commands.filter((c) => c.word.slice(1).toLowerCase().startsWith(query) || c.desc.includes(query))), [query, commands]);
  useEffect(() => setActive(0), [query]);
  useLayoutEffect(() => {
    const el = rowRefs.current[active];
    setRowBox(el ? { top: el.offsetTop, height: el.offsetHeight } : null);
  }, [active, rows.length]);

  // @: the token just before the caret (lib/mentions.ts)
  const token = mention && focused && !dismissed && query === null && caret <= draft.length ? mentionAt(draft, caret) : null;
  const { rows: mRows, loading: mLoading } = useMentionRows(token, mention, machineId);
  useEffect(() => setMActive(0), [token?.query]);
  const mOpen = Boolean(token);

  const trackCaret = () => {
    const el = input.current;
    if (el) setCaret(el.selectionStart);
  };

  const pickMention = (r: MentionRow) => {
    if (!token) return;
    if (r.kind === "session") {
      // a session goes with the message as a quote, not as text (PromptItems.quote)
      const q = sessionQuote(r.session);
      addContext(draftKey, newContext("context", q.title, q.url, q.text));
      const out = replaceToken(draft, token, "");
      setDraft(out.text);
      setCaret(out.caret);
      requestAnimationFrame(() => input.current?.setSelectionRange(out.caret, out.caret));
      return;
    }
    const out = replaceToken(draft, token, r.insert);
    setDraft(out.text);
    setCaret(out.caret);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(out.caret, out.caret);
    });
  };

  const typeAt = () => {
    const el = input.current;
    const at = el ? Math.min(el.selectionStart, draft.length) : draft.length;
    const before = draft.slice(0, at);
    const ins = before && !/\s$/.test(before) ? " @" : "@";
    const next = before + ins + draft.slice(at);
    setDraft(next);
    setDismissed(false);
    setCaret(at + ins.length);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(at + ins.length, at + ins.length);
    });
  };

  const addFiles = async (files: FileList | File[]) => {
    if (disabled) return;
    for (const f of Array.from(files)) {
      if (f.type.startsWith("image/")) {
        if (f.size > MAX_IMAGE) {
          toast(`${f.name} 太大了（图片最多 8 MB）`, "error");
          continue;
        }
        const a = await readImage(f);
        setAtts((cur) => [...cur, a]);
      } else if (machineId) {
        void upload(f);
      } else {
        toast("这里只能发图片", "error");
      }
    }
  };

  // a file goes to the session's machine; its path lands in the input (docs/protocol.md「发送文件」)
  const upload = async (f: File) => {
    setUploading((n) => n + 1);
    try {
      const r = await api<{ path: string }>(`/machines/${enc(machineId!)}/files?name=${enc(f.name)}`, { method: "POST", body: f, headers: { "Content-Type": "application/octet-stream" } });
      const el = input.current;
      const p = quotePath(r.path);
      setDraft((d) => {
        const at = el && document.activeElement === el ? Math.min(el.selectionStart, d.length) : d.length;
        const before = d.slice(0, at),
          after = d.slice(at);
        return `${before}${before && !before.endsWith(" ") ? " " : ""}${p}${after.startsWith(" ") || !after ? "" : " "}${after}`;
      });
      toast(`已传到电脑：${r.path}`, "ok");
    } catch (e) {
      toast(e instanceof ApiError && e.code === "too_large" ? "文件太大（最多 200 MB）" : `上传失败：${errorText(e)}`, "error");
    } finally {
      setUploading((n) => n - 1);
    }
  };

  const canSend = (draft.trim().length > 0 || atts.length > 0 || contexts.length > 0) && !sending && !disabled;
  const showStop = working && !draft.trim() && !atts.length && !contexts.length && onStop;

  const submit = async (text = draft) => {
    const refs = getContexts(draftKey);
    if (sending || disabled || (!text.trim() && !atts.length && !refs.length)) return;
    setSending(true);
    // picked contexts ride in front as <jarvis-context> tags (docs/protocol.md「消息里的标签」)
    const ok = await onSend({ text: withContexts(refs, text, atts.length > 0), images: atts.map((a) => ({ mime: a.mime, data: a.data })) });
    setSending(false);
    if (ok) {
      setDraft("");
      atts.forEach((a) => URL.revokeObjectURL(a.url));
      setAtts([]);
      localStorage.removeItem(`draft:${draftKey}`);
      refs.forEach((r) => removeContext(draftKey, r.id));
    }
    input.current?.focus();
  };

  const pick = (c: Command) => {
    if (c.confirm && !confirm(c.confirm)) return;
    setDraft(c.text);
    setDismissed(true);
    input.current?.focus();
  };

  return (
    <div
      className="relative"
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        void addFiles(e.dataTransfer.files);
      }}
    >
      {rows.length > 0 && (
        <div className="absolute inset-x-0 bottom-full z-10 mb-2 rounded-[10px] bg-surface p-1 shadow-raised" style={{ animation: `pop-in 180ms ${EASE} both`, transformOrigin: "bottom center" }}>
          <div className="relative">
            <span aria-hidden className="pointer-events-none absolute inset-x-0 rounded-[6px] bg-hover" style={{ top: rowBox?.top ?? 0, height: rowBox?.height ?? 0, opacity: rowBox ? 1 : 0, transition: `top 200ms ${EASE}, height 200ms ${EASE}` }} />
            {rows.map((c, i) => (
              <button
                key={c.word}
                ref={(el) => {
                  rowRefs.current[i] = el;
                }}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setActive(i)}
                onClick={() => pick(c)}
                className="relative z-10 flex h-9 w-full items-center gap-2.5 rounded-[6px] px-2 text-left"
              >
                <span className="shrink-0 font-mono text-[12.5px] font-medium text-ink">{c.word}</span>
                <span className="min-w-0 flex-1 truncate text-[12px] text-ink-3">{c.desc}</span>
                {c.text.startsWith("/") ? null : <span className="max-w-[45%] shrink truncate text-[11px] text-ink-3">「{c.text}」</span>}
              </button>
            ))}
          </div>
          <div className="mt-1 border-t border-line px-2 pt-1.5 pb-1 text-[11px] text-ink-3">↑↓ 选择 · Enter 填入 · Esc 关闭</div>
        </div>
      )}

      {mOpen && (
        <MentionMenu rows={mRows} active={Math.min(mActive, Math.max(mRows.length - 1, 0))} loading={mLoading} browsing={Boolean(token && browseTarget(token.query, mention?.cwd))} onHover={setMActive} onPick={pickMention} />
      )}

      <div
        className={cx(
          "relative flex flex-col gap-1.5 rounded-[14px] border bg-surface p-1.5 shadow-card transition-[border-color,box-shadow] duration-150 focus-within:border-line-strong",
          dragging ? "border-accent shadow-[0_0_0_3px_var(--accent-tint)]" : "border-line",
        )}
      >
        <ContextChips draftKey={draftKey} />
        {atts.length > 0 && (
          <div className="px-0.5 pt-0.5">
            <AttachmentTray attachments={atts} onOpen={(id) => setEditing(atts.find((a) => a.id === id) ?? null)} onRemove={(id) => {
              const att = atts.find((a) => a.id === id);
              if (att) URL.revokeObjectURL(att.url);
              setAtts((cur) => cur.filter((a) => a.id !== id));
            }} />
          </div>
        )}
        <textarea
          ref={input}
          rows={1}
          value={draft}
          disabled={disabled}
          onFocus={() => {
            activateComposer(handle);
            setFocused(true);
          }}
          onBlur={() => setFocused(false)}
          onChange={(e) => {
            setDraft(e.target.value);
            setDismissed(false);
            setCaret(e.target.selectionStart);
          }}
          onSelect={trackCaret}
          onClick={trackCaret}
          onPaste={(e) => {
            const files = Array.from(e.clipboardData.files);
            if (files.length) {
              e.preventDefault();
              void addFiles(files);
            }
          }}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing || e.keyCode === 229) return;
            if (mOpen) {
              if ((e.key === "ArrowDown" || e.key === "ArrowUp") && mRows.length) {
                e.preventDefault();
                setMActive((a) => (a + (e.key === "ArrowDown" ? 1 : mRows.length - 1)) % mRows.length);
                return;
              }
              if ((e.key === "Enter" || e.key === "Tab") && mRows.length) {
                e.preventDefault();
                pickMention(mRows[Math.min(mActive, mRows.length - 1)]);
                return;
              }
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                setDismissed(true);
                return;
              }
            }
            if (rows.length) {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                setActive((a) => (a + (e.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length);
                return;
              }
              if (e.key === "Enter" || e.key === "Tab") {
                e.preventDefault();
                pick(rows[active]);
                return;
              }
              if (e.key === "Escape") {
                setDismissed(true);
                return;
              }
            }
            if (e.key === "Tab" && !draft && suggestion) {
              e.preventDefault();
              setDraft(suggestion);
              return;
            }
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void submit();
            }
          }}
          placeholder={suggestion && !draft ? `${suggestion}（Tab 填入）` : placeholder}
          aria-label="消息"
          className="min-h-6 w-full resize-none bg-transparent px-2 pt-1.5 text-[14px] leading-[21px] text-ink outline-none [overflow-wrap:anywhere] placeholder:text-ink-3 disabled:opacity-50"
        />
        <div className="flex items-center gap-1">
          <input ref={fileInput} type="file" multiple hidden onChange={(e) => e.target.files && void addFiles(e.target.files).finally(() => (e.target.value = ""))} />
          <button
            type="button"
            aria-label={machineId ? "添加图片或文件" : "添加图片"}
            title={machineId ? "图片作为附件；其他文件传到会话所在的电脑" : "添加图片"}
            onClick={() => fileInput.current?.click()}
            disabled={disabled}
            className="flex size-7 items-center justify-center rounded-[8px] text-ink-3 transition-[background-color,color,transform] duration-150 hover:bg-hover hover:text-ink active:scale-[0.94]"
          >
            {uploading ? <Spinner /> : <Icon name="plus" size={16} stroke={2} />}
          </button>
          {mention && (
            <button
              type="button"
              aria-label="引用文件或会话"
              title="引用文件或会话（@）"
              disabled={disabled}
              onClick={typeAt}
              className="flex size-7 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
            >
              <Icon name="at" size={15} />
            </button>
          )}
          {commands.length > 0 && (
          <button
            type="button"
            aria-label="命令"
            disabled={disabled}
            title="命令"
            onClick={() => {
              setDraft("/");
              setDismissed(false);
              input.current?.focus();
            }}
            className="flex h-7 items-center justify-center rounded-[8px] px-1.5 font-mono text-[13px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
          >
            /
          </button>
          )}
          <div className="min-w-0 flex-1 truncate px-1 text-[11.5px] text-ink-3">{footer}</div>
          {showStop ? (
            <button
              type="button"
              aria-label="中断"
              title="中断（Esc）"
              onClick={onStop}
              className="flex size-7 items-center justify-center rounded-[8px] bg-red text-white transition-transform duration-150 active:scale-[0.94]"
              style={{ animation: `pop-in 200ms ${EASE} both` }}
            >
              <Icon name="stop" size={12} />
            </button>
          ) : (
            <button
              type="button"
              aria-label="发送"
              title="发送（Enter）· 换行（Shift+Enter）"
              disabled={!canSend}
              onClick={() => void submit()}
              className="flex size-7 items-center justify-center rounded-[8px] transition-[background-color,color,transform] duration-200 enabled:active:scale-[0.94]"
              style={{ background: canSend ? "var(--ink)" : "var(--line-strong)", color: canSend ? "var(--surface)" : "var(--ink-2)" }}
            >
              {sending ? <Spinner /> : <Icon name="up" size={16} stroke={2.4} />}
            </button>
          )}
        </div>
      </div>
      {editing && (
        <ImageEditor
          src={editing.url}
          onCancel={() => setEditing(null)}
          onDone={(img) => {
            const old = editing;
            setEditing(null);
            setAtts((cur) => cur.map((a) => (a.id === old.id ? fromDraftImage(img, a.id) : a)));
            URL.revokeObjectURL(old.url);
          }}
        />
      )}
    </div>
  );
}
