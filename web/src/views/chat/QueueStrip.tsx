// Messages typed while the agent works and not yet taken (Session.queued), above the composer instead of in the
// chat (iOS QueueStrip). 撤回到输入框 pulls them back out of Claude (like ↑ in the terminal), puts that one in the
// composer and re-queues the others in order; 删除 drops it the same way.
import { useState } from "react";
import { api, errorText, sessionPath } from "../../lib/api";
import { sendDraft } from "../../lib/drafts";
import { agentLabel } from "../../lib/mentions";
import { keepAllBut } from "../../lib/queue";
import type { Session } from "../../lib/types";
import { Collapse, copyText, cx, Menu, Spinner, toast, useMenu, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

/** `compact` (a phone on its side): one line, 排队中 N 条, that opens the messages. */
export function QueueStrip({ session, draftKey, canEdit, compact }: { session: Session; draftKey: string; canEdit: boolean; compact?: boolean }) {
  const queued = session.queued ?? [];
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);

  const act = async (index: number, del: boolean) => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api<{ queue?: string[] }>(`${sessionPath(session.machine_id, session.session_id)}/queue`, {
        method: "POST",
        json: { keep: keepAllBut(queued.length, index) },
      });
      const pulled = r.queue ?? [];
      if (!del && pulled[index] !== undefined) sendDraft(draftKey, { type: "append", text: pulled[index] });
      toast(del ? t("chat-queuestrip.deleted") : t("chat-queuestrip.pulled_back"), "ok");
    } catch (e) {
      toast(t("chat-queuestrip.failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };

  if (!queued.length) return null;
  const rows = queued.map((q, i) => (
    <QueueRow key={`${i}:${q}`} n={i + 1} text={q} disabled={busy} canEdit={canEdit} onEdit={() => void act(i, false)} onDelete={() => void act(i, true)} />
  ));
  const head = (
    <>
      <Icon name="inbox" size={13} />
      <span className="min-w-0 flex-1 truncate">
        {t("chat-queuestrip.summary", { count: queued.length, agent: agentLabel(session) })}
      </span>
      {busy && <Spinner />}
    </>
  );
  if (compact)
    return (
      <div data-queue-strip className="flex flex-col rounded-[12px] bg-surface p-1 shadow-card" style={{ animation: "fade-up 200ms ease-out both" }}>
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex h-7 items-center gap-1.5 rounded-[8px] px-1.5 text-left text-[11.5px] font-medium text-ink-3 transition-colors hover:bg-hover">
          {head}
          <Icon name="chevron" size={12} stroke={2.2} className={cx("shrink-0 transition-transform duration-200", open && "rotate-180")} />
        </button>
        <Collapse open={open}>
          <div className="flex flex-col gap-1.5 px-1 pt-1 pb-0.5">{rows}</div>
        </Collapse>
      </div>
    );
  return (
    <div data-queue-strip className="flex flex-col gap-1.5 rounded-[12px] bg-surface p-2 shadow-card" style={{ animation: "fade-up 200ms ease-out both" }}>
      <div className="flex h-5 items-center gap-1.5 px-1 text-[11.5px] font-medium text-ink-3">{head}</div>
      {rows}
    </div>
  );
}

function QueueRow({ n, text, disabled, canEdit, onEdit, onDelete }: { n: number; text: string; disabled: boolean; canEdit: boolean; onEdit: () => void; onDelete: () => void }) {
  const menu = useMenu();
  const items: MenuItem[] = [
    ...(canEdit ? [{ key: "edit", label: t("chat-queuestrip.pull_back"), icon: "pencil" as const, onSelect: onEdit }] : []),
    { key: "copy", label: t("chat-queuestrip.copy"), icon: "copy", onSelect: () => void copyText(text) },
    ...(canEdit ? [{ key: "delete", label: t("chat-queuestrip.delete"), icon: "trash" as const, danger: true, onSelect: onDelete }] : []),
  ];
  return (
    <>
      <button
        type="button"
        onClick={menu.open}
        disabled={disabled}
        title={text}
        aria-label={t("chat-queuestrip.row_label", { n, text })}
        aria-haspopup="menu"
        className="flex w-full items-start gap-2 rounded-[8px] bg-inset px-2.5 py-1.5 text-left transition-colors hover:bg-hover disabled:opacity-60"
      >
        <span className="mt-px flex size-[18px] shrink-0 items-center justify-center rounded-full bg-accent-tint text-[10.5px] font-semibold text-accent-ink tabular-nums">{n}</span>
        <span className="line-clamp-2 min-w-0 flex-1 text-[12.5px] leading-[18px] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{text}</span>
        <Icon name="more" size={14} className="mt-0.5 shrink-0 text-ink-3" />
      </button>
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} align="right" items={items} width={160} />}
    </>
  );
}
