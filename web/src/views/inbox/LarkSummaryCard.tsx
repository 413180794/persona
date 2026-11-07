// Cards the Lark gateway summarises itself, drawn by their `metadata.variant` (#3032, iOS LarkSummaryCards.swift): a
// merge-forward opens every forwarded message in a dialog; group / contact cards show their picture, name and facts;
// meetings, events, votes, tasks and the rest an accent bar, a glyph, eyebrow, title, status, facts and footer.
import { useState } from "react";
import { clock } from "../../lib/format";
import type { LarkPart } from "../../lib/inbox";
import {
  fieldValue, forwardCount, forwardedMessages, forwardedText, forwardPreviews, hubMediaURL, meetingNumber, stripHTML, summaryFields,
  summaryTone, summaryVariant, voteOptions, type ForwardedMessage, type SummaryTone,
} from "../../lib/lark-summary";
import { useItemManager, withManager } from "../../lib/manager-choice";
import { currentLanguage, t } from "../../i18n";
import { copyText, cx, Dialog } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Monogram } from "./InboxParts";

type Meta = Record<string, unknown>;

const str = (v: unknown): string => (typeof v === "string" ? v : "");

/** Whether a card part is one of these (else the element tree is drawn). */
export function isSummaryCard(meta: Meta | undefined): boolean {
  return summaryVariant(meta) !== "";
}

const FRAME = "w-full max-w-[460px] overflow-hidden rounded-[14px] bg-surface text-left shadow-card";

export function LarkSummaryCard({
  meta,
  fallbackTitle,
  renderParts,
}: {
  meta: Meta;
  fallbackTitle: string;
  /** Draws a forwarded message's parts the way the chat draws a message's. */
  renderParts: (parts: LarkPart[]) => React.ReactNode;
}) {
  const variant = summaryVariant(meta);
  const title = str(meta.title) || fallbackTitle;
  switch (variant) {
    case "merge-forward":
      return <MergeForwardCard meta={meta} title={title} renderParts={renderParts} />;
    case "share-group":
    case "share-user":
      return <ShareCard meta={meta} title={title} group={variant === "share-group"} />;
    default:
      return <GenericSummaryCard meta={meta} title={title} variant={variant} />;
  }
}

/** "Chat history": a few previews; a click opens every forwarded message. */
function MergeForwardCard({ meta, title, renderParts }: { meta: Meta; title: string; renderParts: (parts: LarkPart[]) => React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const name = title || t("inbox-larksummarycard.chat_history");
  const count = forwardCount(meta);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} data-testid="lark-forward" className={cx(FRAME, "block px-3 py-2.5 hover:bg-surface-hover")}>
        <p className="line-clamp-2 text-[13px] font-semibold text-ink">{name}</p>
        <div className="mt-1 flex flex-col gap-0.5">
          {forwardPreviews(meta).map((p, i) => (
            <p key={i} className="truncate text-[12px] text-ink-3">
              {t("inbox-larksummarycard.line", { name: str(p.senderName), text: previewText(p) })}
            </p>
          ))}
        </div>
        <p className="mt-2 flex items-center gap-1.5 border-t border-line pt-1.5 text-[11.5px] text-ink-3">
          <Icon name="chat" size={12} />
          <span className="whitespace-nowrap">{t("inbox-larksummarycard.chat_history")}</span>
          <span className="ml-auto whitespace-nowrap tabular-nums">{t("inbox-larksummarycard.count", { count })}</span>
        </p>
      </button>
      {open && <ForwardedDialog title={name} meta={meta} renderParts={renderParts} onClose={() => setOpen(false)} />}
    </>
  );
}

/** A forwarded message's line: recalled and encrypted ones are said, not shown. */
function previewText(p: Meta): string {
  if (p.isRecalled === true && p.hasRetainedRecallContent !== true) return t("inbox-larksummarycard.recalled");
  if (p.olk) return t("inbox-larksummarycard.encrypted");
  const text = forwardedText(p) || partsLabel(p.parts) || t("inbox-larksummarycard.message");
  return p.isRecalled === true ? t("inbox-larksummarycard.recalled_kept", { text }) : text;
}

/** A forwarded message without text said by what it holds: [Image], [File] …. */
function partsLabel(parts: unknown): string {
  if (!Array.isArray(parts)) return "";
  const labels: Record<string, string> = {
    image: t("inbox-larksummarycard.image"), file: t("inbox-larksummarycard.file"), media: t("inbox-larksummarycard.file"),
    card: t("inbox-larksummarycard.card"), emotion: t("inbox-larksummarycard.sticker"),
  };
  return parts.map((x) => labels[str((x as Meta | null)?.kind)] ?? "").join("");
}

/** Every message of a merge-forward, drawn like the chat. */
function ForwardedDialog({ title, meta, renderParts, onClose }: { title: string; meta: Meta; renderParts: (parts: LarkPart[]) => React.ReactNode; onClose: () => void }) {
  const messages = forwardedMessages(meta);
  return (
    <Dialog open onClose={onClose} title={title} width={620}>
      <div className="-mx-2 flex max-h-[60vh] flex-col gap-3 overflow-y-auto px-2" data-testid="lark-forwarded">
        {!messages.length && <p className="py-4 text-center text-[12.5px] text-ink-3">{t("inbox-larksummarycard.empty")}</p>}
        {messages.map((m) => <ForwardedRow key={m.id} m={m} renderParts={renderParts} />)}
      </div>
    </Dialog>
  );
}

function ForwardedRow({ m, renderParts }: { m: ForwardedMessage; renderParts: (parts: LarkPart[]) => React.ReactNode }) {
  const name = m.senderName || t("inbox-larksummarycard.unknown");
  const hidden = m.isRecalled === true && m.hasRetainedRecallContent !== true;
  return (
    <div className="flex gap-2.5">
      <Monogram name={name} size={32} src={hubMediaURL(m.senderAvatarUrl)} />
      <div className="min-w-0 flex-1">
        <p className="flex items-baseline gap-2">
          <span className="truncate text-[12.5px] font-medium text-ink-2">{name}</span>
          {m.createdAt && <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">{clock(m.createdAt)}</span>}
        </p>
        {hidden ? (
          <p className="text-[12.5px] text-ink-3 italic">{t("inbox-larksummarycard.recalled")}</p>
        ) : m.olk ? (
          <p className="text-[12.5px] text-ink-3">{t("inbox-larksummarycard.encrypted")}</p>
        ) : m.parts?.length ? (
          renderParts(m.parts)
        ) : (
          <p className="text-[13.5px] leading-relaxed break-words whitespace-pre-wrap text-ink">{m.textPreview || m.text}</p>
        )}
        {m.isRecalled === true && !hidden && <p className="text-[11px] text-ink-3">{t("inbox-larksummarycard.recalled_mark")}</p>}
      </div>
    </div>
  );
}

/** Group / contact card: picture, name, facts, then what it is. */
function ShareCard({ meta, title, group }: { meta: Meta; title: string; group: boolean }) {
  const facts = summaryFields(meta, currentLanguage()).map((f) => `${f.label} ${f.value}`);
  const name = title || (group ? t("inbox-larksummarycard.group_card") : t("inbox-larksummarycard.user_card"));
  return (
    <div className={cx(FRAME, "px-3 py-2.5")} data-testid="lark-share">
      <div className="flex items-center gap-3">
        <Monogram name={name} size={44} square={group} src={str(meta.imageUrl) || undefined} />
        <div className="min-w-0">
          <p className="line-clamp-2 text-[13px] font-semibold text-ink">{name}</p>
          {facts.length > 0 && <p className="truncate text-[11.5px] text-ink-3">{facts.join(" · ")}</p>}
        </div>
      </div>
      <p className="mt-2.5 flex items-center border-t border-line pt-1.5 text-[11.5px] text-ink-3">
        <span className="whitespace-nowrap">{str(meta.eyebrow) || (group ? t("inbox-larksummarycard.group_card") : t("inbox-larksummarycard.user_card"))}</span>
        {meta.isDissolved === true && <span className="ml-auto whitespace-nowrap">{t("inbox-larksummarycard.dissolved")}</span>}
      </p>
    </div>
  );
}

const TONE: Record<SummaryTone, { bar: string; glyph: string }> = {
  accent: { bar: "bg-accent", glyph: "bg-accent-tint text-accent-ink" },
  red: { bar: "bg-accent", glyph: "bg-accent-tint text-accent-ink" },
  orange: { bar: "bg-orange", glyph: "bg-orange-tint text-orange" },
  green: { bar: "bg-green", glyph: "bg-green-tint text-green" },
};

function glyph(variant: string): IconName {
  switch (variant) {
    case "video-chat":
    case "meeting":
      return "play";
    case "calendar":
      return "calendar";
    case "vote":
      return "chart";
    case "hongbao":
      return "money";
    case "todo":
    case "task":
      return "todo";
    case "share-user":
      return "person";
    case "location":
      return "target";
    case "mail":
      return "mail";
    default:
      return "read";
  }
}

/** Meetings, events, votes, tasks and anything else. */
function GenericSummaryCard({ meta, title, variant }: { meta: Meta; title: string; variant: string }) {
  const manager = useItemManager();
  const tone = TONE[summaryTone(variant)];
  const fields = summaryFields(meta, currentLanguage());
  const description = str(meta.description) ? stripHTML(str(meta.description)) : "";
  const vote = voteOptions(meta);
  const number = meetingNumber(meta);
  const image = str(meta.imageUrl);
  const status = str(meta.status);
  const eyebrow = str(meta.eyebrow);
  const footer = str(meta.footer);
  return (
    <div className={cx(FRAME, "flex")} data-testid="lark-summary" data-variant={variant}>
      <span aria-hidden className={cx("w-1 shrink-0", tone.bar)} />
      <div className="min-w-0 flex-1 px-3 py-2.5">
        <div className="flex items-start gap-3">
          {image ? (
            <img src={withManager(hubMediaURL(image) ?? "", manager)} alt="" loading="lazy" className="size-10 shrink-0 rounded-[10px] object-cover shadow-hairline" />
          ) : (
            <span aria-hidden className={cx("flex size-10 shrink-0 items-center justify-center rounded-[10px]", tone.glyph)}>
              <Icon name={glyph(variant)} size={18} stroke={2.2} />
            </span>
          )}
          <div className="min-w-0 flex-1">
            {eyebrow && <p className="text-[11.5px] text-ink-3">{eyebrow}</p>}
            <p className="line-clamp-3 text-[13px] font-semibold text-ink">{title}</p>
          </div>
          {status && <span className="shrink-0 rounded-full bg-field px-2 py-0.5 text-[11px] font-medium whitespace-nowrap text-ink-2">{status}</span>}
        </div>
        {description && <p className="mt-2 line-clamp-4 text-[12.5px] whitespace-pre-wrap text-ink-2">{description}</p>}
        {vote.options.length > 0 && (
          <ul className="mt-2 flex flex-col gap-1">
            {vote.options.map((o, i) => (
              <li key={i} className="flex items-center gap-2 text-[12.5px] text-ink">
                <span aria-hidden className={cx("size-3 shrink-0 border border-line-strong", vote.multiple ? "rounded-[3px]" : "rounded-full")} />
                <span className="min-w-0 break-words">{o}</span>
              </li>
            ))}
          </ul>
        )}
        {fields.length > 0 && (
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-2.5 gap-y-1 text-[12px]">
            {fields.map((f, i) => (
              <div key={i} className="contents">
                <dt className="whitespace-nowrap text-ink-3">{f.label}</dt>
                <dd className="line-clamp-3 min-w-0 text-ink">{f.value}</dd>
              </div>
            ))}
          </dl>
        )}
        {footer && <p className="mt-2 text-[11px] text-ink-3">{fieldValue(footer, currentLanguage())}</p>}
        {number && (
          <button
            type="button"
            onClick={() => void copyText(number)}
            className="mt-2 inline-flex items-center gap-1.5 rounded-[8px] px-2.5 py-1 text-[11.5px] font-medium whitespace-nowrap text-ink-2 shadow-btn hover:text-ink"
          >
            <Icon name="copy" size={12} />
            {t("inbox-larksummarycard.copy_number")}
          </button>
        )}
      </div>
    </div>
  );
}
