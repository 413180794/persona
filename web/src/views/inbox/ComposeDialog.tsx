// 写邮件 (the app's ComposeView): from one of the hub's mail accounts, to / cc, subject and plain text; a reply comes
// with Re:, the thread ids and the start of the mail quoted (replyDraft). Sent by the hub (POST /mail/send).
// 取消 drops the mail; closing it any other way (Esc, a click outside) keeps it for the next 写邮件 in this page.
import { useEffect, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import type { MailAccount } from "../../lib/inbox";
import { badAddresses, parseAddresses, sendMail, type ComposeDraft } from "../../lib/mail";
import { Button, Dialog, toast } from "../../ui/atoms";

const kept = new Map<string, ComposeDraft>();

/** The mail left open under `key` ("new", or a reply's), else `fresh`. */
function keptDraft(key: string, fresh: ComposeDraft): ComposeDraft {
  return kept.get(key) ?? fresh;
}

const fieldClass = "h-8 min-w-0 flex-1 rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]";

export function ComposeDialog({ draftKey, initial, accounts, onClose }: { draftKey: string; initial: ComposeDraft; accounts: MailAccount[]; onClose: () => void }) {
  const [d, setD] = useState(() => keptDraft(draftKey, initial));
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const body = useRef<HTMLTextAreaElement>(null);
  const to = useRef<HTMLInputElement>(null);
  const dirty = d.to !== initial.to || d.cc !== initial.cc || d.subject !== initial.subject || d.body !== initial.body || d.accountID !== initial.accountID;

  // A reply starts in its body, above the quote; a new mail at its recipients.
  useEffect(() => {
    if (initial.to && body.current) {
      body.current.focus();
      body.current.setSelectionRange(0, 0);
    } else to.current?.focus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const set = (patch: Partial<ComposeDraft>) => {
    setD((x) => ({ ...x, ...patch }));
    setError("");
  };
  const keepOrDrop = () => {
    if (dirty) kept.set(draftKey, d);
    else kept.delete(draftKey);
    onClose();
  };
  const cancel = () => {
    kept.delete(draftKey);
    onClose();
  };
  const send = async () => {
    const bad = [...badAddresses(d.to), ...badAddresses(d.cc)];
    if (!parseAddresses(d.to).length) return setError("写上收件人");
    if (bad.length) return setError(`这些不像邮箱地址：${bad.join("、")}`);
    setSending(true);
    try {
      await sendMail(d);
      kept.delete(draftKey);
      toast("已发送", "ok");
      onClose();
    } catch (e) {
      setError(`没发出去：${errorText(e)}`);
    } finally {
      setSending(false);
    }
  };

  return (
    <Dialog
      open
      onClose={keepOrDrop}
      title={initial.inReplyTo || /^re:/i.test(initial.subject) ? "回复邮件" : "写邮件"}
      width={620}
      footer={
        <>
          <Button tone="quiet" onClick={cancel}>取消</Button>
          <Button tone="accent" icon="send" busy={sending} disabled={!d.to.trim() || !d.accountID} onClick={() => void send()}>发送</Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
        onKeyDown={(e) => {
          // ⌘↩ / Ctrl+↩ sends, as in a mail app.
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void send();
          }
        }}
      >
        <Field label="发件人">
          <select value={d.accountID} onChange={(e) => set({ accountID: e.target.value })} className={fieldClass} aria-label="发件人">
            {!accounts.some((a) => a.id === d.accountID) && <option value={d.accountID}>{d.accountID || "（没有邮箱）"}</option>}
            {accounts.map((a) => <option key={a.id} value={a.id}>{a.name && a.name !== a.email ? `${a.name} · ${a.email}` : a.email}</option>)}
          </select>
        </Field>
        <Field label="收件人">
          <input ref={to} value={d.to} onChange={(e) => set({ to: e.target.value })} placeholder="逗号分隔" aria-label="收件人" type="text" inputMode="email" autoComplete="off" spellCheck={false} className={fieldClass} />
        </Field>
        <Field label="抄送">
          <input value={d.cc} onChange={(e) => set({ cc: e.target.value })} aria-label="抄送" type="text" inputMode="email" autoComplete="off" spellCheck={false} className={fieldClass} />
        </Field>
        <Field label="主题">
          <input value={d.subject} onChange={(e) => set({ subject: e.target.value })} aria-label="主题" className={fieldClass} />
        </Field>
        <textarea
          ref={body}
          value={d.body}
          onChange={(e) => set({ body: e.target.value })}
          rows={12}
          aria-label="正文"
          placeholder="正文"
          className="mt-1 block w-full resize-y rounded-control bg-field px-3 py-2.5 text-[13.5px] leading-relaxed text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]"
        />
        {error && <p role="alert" className="text-[12px] text-red">{error}</p>}
      </form>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex items-center gap-3">
      <span className="w-12 shrink-0 text-[12px] whitespace-nowrap text-ink-3">{label}</span>
      {children}
    </label>
  );
}
