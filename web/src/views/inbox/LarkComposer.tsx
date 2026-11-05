// The Feishu chat's composer: text, the quoted message, and pictures / files picked, pasted or dropped
// (docs/lark-attachments.md, iOS LarkComposer). Text and each attachment go as messages of their own.
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { fitPick, releaseLarkAttachments, stageLarkAttachment, type LarkAttachment } from "../../lib/lark-attachments";
import { t } from "../../i18n";
import { AttachmentTray } from "../../ui/AttachmentTray";
import { Button, cx, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export interface LarkReplyTarget {
  id: string;
  sender_name?: string;
  text?: string;
}

export interface LarkComposerHandle {
  addFiles: (files: FileList | File[]) => void;
  focus: () => void;
}

export interface LarkComposerProps {
  replyTo?: LarkReplyTarget | null;
  onClearReply?: () => void;
  /** Sends what is in the box; resolves false to keep it all there (nothing was sent). */
  onSend: (text: string, attachments: LarkAttachment[], replyTo?: LarkReplyTarget) => Promise<boolean | void>;
  /** Pictures and files can be added (the hub's extension sends them, and can route them where this box sends). */
  attachments?: boolean;
  placeholder?: string;
  /** Smaller, under a thread. */
  compact?: boolean;
}

export const LarkComposer = forwardRef<LarkComposerHandle, LarkComposerProps>(function LarkComposer(
  { replyTo = null, onClearReply, onSend, attachments: canAttach = false, placeholder, compact = false }, ref,
) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState<LarkAttachment[]>([]);
  const [staging, setStaging] = useState(0);
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const filesRef = useRef(files);
  filesRef.current = files;
  const stagingRef = useRef(0);

  // previews left in the box go with it
  useEffect(() => () => releaseLarkAttachments(filesRef.current), []);

  const addFiles = (list: FileList | File[]) => {
    if (!canAttach) return;
    const { take, dropped } = fitPick(filesRef.current.length + stagingRef.current, Array.from(list));
    if (dropped) toast(t("inbox-larkcomposer.too_many", { count: dropped }), "error");
    if (!take.length) return;
    stagingRef.current += take.length;
    setStaging(stagingRef.current);
    for (const f of take) {
      stageLarkAttachment(f)
        .then((a) => setFiles((cur) => [...cur, a]))
        .catch((e: unknown) => toast(e instanceof Error ? e.message : String(e), "error"))
        .finally(() => {
          stagingRef.current -= 1;
          setStaging(stagingRef.current);
        });
    }
  };

  useImperativeHandle(ref, () => ({ addFiles, focus: () => input.current?.focus() }));

  const remove = (id: string) => {
    setFiles((cur) => {
      releaseLarkAttachments(cur.filter((a) => a.id === id));
      return cur.filter((a) => a.id !== id);
    });
  };

  const ready = (text.trim() !== "" || files.length > 0) && staging === 0;
  const submit = async () => {
    if (!ready || sending) return;
    const trimmed = text.trim();
    const picked = files;
    setSending(true);
    try {
      const sent = await onSend(trimmed, picked, replyTo ?? undefined);
      if (sent === false) return;
      // the attachments' previews now belong to the messages drawn for them
      setText("");
      setFiles([]);
      if (input.current) input.current.style.height = "auto";
    } finally {
      setSending(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      if (e.nativeEvent.isComposing) return;
      e.preventDefault();
      void submit();
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    const el = e.target;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
  };

  return (
    <div
      className={cx("relative", compact ? "pt-1.5" : "shrink-0 border-t border-line bg-surface px-3 py-2 sm:px-4")}
      onDragOver={(e) => {
        if (canAttach && e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
      }}
      onDrop={(e) => {
        if (!canAttach || !e.dataTransfer.files.length) return;
        e.preventDefault();
        e.stopPropagation();
        setDragging(false);
        addFiles(e.dataTransfer.files);
      }}
    >
      {dragging && (
        <div className="pointer-events-none absolute inset-1 z-10 flex items-center justify-center rounded-[10px] border-2 border-dashed border-accent bg-accent-tint/70 text-[12.5px] font-medium text-accent-ink">
          {t("inbox-larkcomposer.drop")}
        </div>
      )}
      <div className={cx(!compact && "mx-auto max-w-[760px]")}>
        {replyTo && (
          <div className="mb-2 flex items-center justify-between rounded-[8px] bg-field/60 px-3 py-1.5 text-[12px] text-ink-2">
            <div className="flex min-w-0 items-center gap-1.5">
              <Icon name="quote" size={12} className="shrink-0 text-accent-ink" />
              <span className="shrink-0 font-medium text-ink">{t("inbox-larkthread.quoted_sender", { name: replyTo.sender_name ?? "" })}</span>
              <span className="truncate text-ink-3">{replyTo.text || t("inbox-larkthread.quoted_message")}</span>
            </div>
            <button
              type="button"
              onClick={onClearReply}
              className="ml-2 shrink-0 rounded p-0.5 text-ink-3 hover:bg-surface hover:text-ink"
              title={t("inbox-larkthread.cancel_quote")}
              aria-label={t("inbox-larkthread.cancel_quote")}
            >
              <Icon name="x" size={12} />
            </button>
          </div>
        )}
        {(files.length > 0 || staging > 0) && (
          <div className="mb-2 flex items-center gap-2">
            <AttachmentTray attachments={files} onRemove={sending ? undefined : remove} />
            {staging > 0 && <Spinner size={14} />}
          </div>
        )}
        <div className="flex items-end gap-2">
          {canAttach && (
            <>
              <input
                ref={picker}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files) addFiles(e.target.files);
                  e.target.value = "";
                }}
              />
              <button
                type="button"
                onClick={() => picker.current?.click()}
                disabled={sending}
                aria-label={t("inbox-larkcomposer.add")}
                title={t("inbox-larkcomposer.add_hint")}
                className={cx("touch-hit flex shrink-0 items-center justify-center rounded-[10px] text-ink-3 transition-colors hover:bg-hover hover:text-ink disabled:opacity-50", compact ? "size-8" : "size-9")}
              >
                <Icon name="clip" size={16} />
              </button>
            </>
          )}
          <textarea
            ref={input}
            value={text}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            onPaste={(e) => {
              const pasted = Array.from(e.clipboardData.files);
              if (canAttach && pasted.length) {
                e.preventDefault();
                addFiles(pasted);
              }
            }}
            placeholder={placeholder ?? (replyTo ? t("inbox-larkthread.reply_to_placeholder", { name: replyTo.sender_name ?? "" }) : t("inbox-larkthread.reply_placeholder"))}
            aria-label={t("inbox-larkcomposer.message")}
            rows={1}
            className={cx(
              "max-h-[120px] flex-1 resize-none rounded-[10px] border border-line bg-field px-3 py-1.5 text-[13px] leading-relaxed text-ink placeholder:text-ink-3 focus:border-accent focus:outline-none",
              compact ? "min-h-[32px]" : "min-h-[36px]",
            )}
          />
          <Button
            tone="accent"
            busy={sending}
            disabled={!ready || sending}
            onClick={() => void submit()}
            className={cx("shrink-0 whitespace-nowrap", compact ? "h-[32px]" : "h-[36px]")}
          >
            {t("inbox-larkthread.send")}
          </Button>
        </div>
      </div>
    </div>
  );
});
