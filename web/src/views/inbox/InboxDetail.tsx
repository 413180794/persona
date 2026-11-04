// One item opened: what it is and where from, 已处理 on the right; the AI's summary with AI 草拟回复 / 交给 AI; then
// the thing itself — the Feishu chat at its first new message, the mail, the document's rows, the 公司圈 post — or,
// when the plugin whose connector brought the item in draws its page (inbox_item), the plugin's View.
import { useEffect, useState } from "react";
import { agentDraft, agentWorking, canDraft, isDone, kindLabel, labelIsUrgent, openTitle, openURL, setDone, shortTime, sourceLine, type InboxItem } from "../../lib/inbox";
import { useInboxItemPlace } from "../../lib/plugin-slots";
import { navigate } from "../../lib/router";
import { hubTranslation, useTranslatePrefs } from "../../lib/translate";
import { Button, cx, IconButton, Loading } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ConnectorBody, openSystem } from "./ConnectorThread";
import { DocBody, MailBody, MomentBody } from "./InboxBodies";
import { DraftDialog, HandOffDialog } from "./InboxDialogs";
import { AgentWorking, ItemAvatar, Spark, Tag } from "./InboxParts";
import { InboxPluginBody } from "./InboxPluginBody";
import { chatOfItem, LarkThread } from "./LarkThread";

export function InboxDetail({ item, loaded }: { item?: InboxItem; loaded: boolean }) {
  const [dialog, setDialog] = useState<"" | "draft" | "handoff">("");
  const [pins, setPins] = useState(false);
  useEffect(() => setPins(false), [item?.id]);
  const prefs = useTranslatePrefs();
  const place = useInboxItemPlace(item?.source);
  const [ownShown, setOwnShown] = useState(false); // under a plugin's page: it fell back to the item's own body
  if (!item) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        {loaded ? <p className="text-[13px] text-ink-3">这件事已经不在收件箱里了</p> : <Loading />}
      </div>
    );
  }
  const done = isDone(item);
  const label = kindLabel(item);
  const draft = agentDraft(item);
  const translated = hubTranslation(item, prefs);
  // the item's own body: the chat, the mail, the document's rows, the post, a connector's thread
  const own = () => (
    <div className={cx("min-h-0 flex-1", item.lark || item.connector ? "flex flex-col overflow-hidden" : "overflow-y-auto")}>
      {item.doc ? <DocBody item={item} /> : item.moment ? <MomentBody item={item} /> : item.lark ? <LarkThread chat={chatOfItem(item)} pinsOpen={pins} onPinsOpen={setPins} /> : item.connector ? <ConnectorBody item={item} /> : item.mail ? <MailBody item={item} /> : (
        <div className="mx-auto max-w-[760px] px-6 py-5 text-[13.5px] whitespace-pre-wrap text-ink">{item.preview}</div>
      )}
    </div>
  );
  return (
    <div className="flex h-full min-h-0 flex-col" key={item.id}>
      <div className="shrink-0 border-b border-line px-4 pt-4 pb-3 sm:px-6">
        <div className="mx-auto max-w-[760px]">
          <div className="flex items-start gap-3">
            <ItemAvatar it={item} size={36} />
            <div className="min-w-0 flex-1">
              <h2 className="flex min-w-0 items-center gap-2 text-[15px] font-semibold text-ink">
                <span className="truncate" title={translated?.subject ? item.title : undefined}>{translated?.subject || item.title}</span>
                {label && <Tag urgent={labelIsUrgent(item)}>{label}</Tag>}
              </h2>
              <p className="mt-0.5 truncate text-[12px] text-ink-3">{[sourceLine(item), shortTime(item.updated_at)].filter(Boolean).join(" · ")}</p>
            </div>
            {item.lark && (!place || ownShown) && <IconButton icon="pin" label="Pin 消息" onClick={() => setPins(true)} size={32} />}
            {done ? (
              <Button tone="secondary" icon="inbox" onClick={() => void setDone(item, false)}>移回</Button>
            ) : (
              <Button
                tone="secondary"
                icon="check"
                onClick={() => {
                  void setDone(item, true);
                  navigate({ name: "inbox" });
                }}
              >
                已处理
              </Button>
            )}
          </div>
          {item.summary && (
            <p className="mt-3 text-[13px] leading-relaxed text-ink">
              <Spark />
              {item.summary}
            </p>
          )}
          {!done && agentWorking(item) && <div className="mt-2"><AgentWorking it={item} /></div>}
          <div className="mt-3 flex flex-wrap gap-2">
            {canDraft(item) ? (
              <Button tone="accent" icon="write" onClick={() => setDialog("draft")}>{draft && !done ? "查看草稿" : "AI 草拟回复"}</Button>
            ) : (
              // Answered in the source's own app (a CI run has nothing to answer: only 交给 AI).
              openURL(item) && <Button tone="accent" icon="external" onClick={() => openSystem(openURL(item)!)}>{openTitle(item)}</Button>
            )}
            <Button tone="secondary" icon="spark" onClick={() => setDialog("handoff")}>交给 AI</Button>
          </div>
        </div>
      </div>
      {place ? <InboxPluginBody key={`${item.id}/${place.plugin}/${place.id}`} item={item} place={place} own={own} onOwn={setOwnShown} /> : own()}
      {dialog === "draft" && <DraftDialog item={item} onClose={() => setDialog("")} />}
      {dialog === "handoff" && <HandOffDialog item={item} onClose={() => setDialog("")} />}
    </div>
  );
}

export function DetailPlaceholder() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
      <span className="flex size-10 items-center justify-center rounded-full bg-field text-ink-3">
        <Icon name="inbox" size={18} />
      </span>
      <p className="text-[13.5px] font-medium text-ink">选一件事</p>
      <p className="text-[12.5px] text-ink-3">左边列表里点一件事，详情会在这里打开。</p>
    </div>
  );
}
