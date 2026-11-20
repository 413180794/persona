import { t } from "../../i18n";
import { mediaURL } from "../../lib/connectors";
import type { FeedItem } from "../../lib/feed";
import { clock } from "../../lib/format";
import { conversationManager } from "../../lib/manager-choice";
import { Monogram } from "../inbox/InboxParts";

export function LarkMessage({ item }: { item: FeedItem }) {
  const body = item.body;
  const author = body?.author as { name?: unknown; avatar?: unknown; owner?: unknown; on_behalf?: unknown; source?: { title?: unknown } } | undefined;
  if (typeof author?.name !== "string" || typeof body?.text !== "string") return null;
  const { name, avatar } = author;
  const owner = author.owner === true;
  const behalf = owner && author.on_behalf === true;
  const ref = typeof avatar === "string" && /^avatar:[0-9]{1,64}:[0-9a-f]{64}$/.test(avatar) ? avatar : undefined;
  const chat = typeof author.source?.title === "string" && author.source.title;
  const label = behalf ? t("assistant-tasks.lark_author_on_behalf") : owner ? t("assistant-tasks.lark_author_owner") : "";
  const at = typeof body.at === "string" ? body.at : item.at;
  return (
    <div data-lark-message data-author-owner={owner} data-on-behalf={behalf}
      className={`flex items-start gap-2 ${owner ? "flex-row-reverse pl-[12%]" : "pr-[12%]"}`}>
      <Monogram key={ref ?? name} name={name} size={32} manager={conversationManager(item.conversation)} src={ref ? mediaURL("larkcontext", ref) : undefined} />
      <div className={`flex min-w-0 max-w-[488px] flex-col gap-1 ${owner ? "items-end" : "items-start"}`}>
        <div className="flex flex-wrap items-baseline gap-x-2 px-1 text-[12px] text-ink-2">
          <span className="font-medium">{name}</span>
          {label && <span>{label}</span>}
          <span className="text-ink-3" title={chat || undefined}>{chat || t("assistant-tasks.lark_source")}</span>
          <time dateTime={at} className="tabular-nums text-ink-3">{clock(at)}</time>
        </div>
        <div className={`rounded-[18px] px-3.5 py-2 text-[length:var(--chat-body)] leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere] ${owner ? "bg-user-bubble" : "bg-neutral-bubble"}`}>
          {body.text}
        </div>
      </div>
    </div>
  );
}
