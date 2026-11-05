// One item opened: what it is and where from, 已处理 on the right; the AI's summary, its 线索 (InboxLine), AI 草拟回复 / 交给 AI; then
// the thing itself — the Feishu chat at its first new message, the mail, the document's rows, the 公司圈 post — or,
// when the extension whose connector brought the item in draws its page (inbox_item), the extension's View.
import { useEffect, useState } from "react";
import { agentDraft, agentWorking, canDraft, isDone, kindLabel, labelIsUrgent, muteItem, muteShortName, muteTitle, openTitle, openURL, setDone, shortTime, sourceLine, type InboxItem } from "../../lib/inbox";
import { useInboxItemPlace } from "../../lib/extension-slots";
import { PHONE, useMedia } from "../../lib/viewport";
import { navigate } from "../../lib/router";
import { hubTranslation, useTranslatePrefs } from "../../lib/translate";
import { t } from "../../i18n";
import { Button, cx, IconButton, Loading, Menu, useMenu, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ConnectorBody, openSystem } from "./ConnectorThread";
import { DocBody, MailBody, MomentBody } from "./InboxBodies";
import { DraftDialog, HandOffDialog } from "./InboxDialogs";
import { markFromRow } from "./InboxList";
import { AgentWorking, ItemAvatar, Spark, Tag } from "./InboxParts";
import { InboxExtensionBody } from "./InboxExtensionBody";
import { InboxLine } from "./InboxLine";
import { chatOfItem, LarkThread } from "./LarkThread";

export function InboxDetail({ item, loaded }: { item?: InboxItem; loaded: boolean }) {
  const [dialog, setDialog] = useState<"" | "draft" | "handoff">("");
  const [pins, setPins] = useState(false);
  useEffect(() => setPins(false), [item?.id]);
  const prefs = useTranslatePrefs();
  const place = useInboxItemPlace(item?.source);
  const [ownShown, setOwnShown] = useState(false); // under an extension's page: it fell back to the item's own body
  const muteMenu = useMenu();
  const moreMenu = useMenu();
  // on a phone the title comes first: Pin and Mute fold into one ⋯ menu when both are there (#4144)
  const phone = useMedia(PHONE);
  if (!item) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        {loaded ? <p className="text-[13px] text-ink-3">{t("inbox-inboxdetail.gone")}</p> : <Loading />}
      </div>
    );
  }
  const done = isDone(item);
  const label = kindLabel(item);
  const draft = agentDraft(item);
  const translated = hubTranslation(item, prefs);
  const time = shortTime(item.updated_at);
  const canPin = Boolean(item.lark) && (!place || ownShown);
  const muteRows: MenuItem[] = (item.mutes ?? []).map((m) => ({ key: `${m.tag}:${m.value}`, label: muteTitle(m), hint: muteShortName(m), icon: "bellOff", onSelect: () => void muteItem(item, m) }));
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
              {/* the source line gives way first, so the time stays whole */}
              <p className="mt-0.5 flex min-w-0 text-[12px] text-ink-3">
                <span className="truncate">{sourceLine(item)}</span>
                {time && <span className="shrink-0 whitespace-pre">{sourceLine(item) ? ` · ${time}` : time}</span>}
              </p>
            </div>
            {phone && canPin && muteRows.length > 0 ? (
              <IconButton icon="more" label={t("inbox-inboxdetail.more")} onClick={moreMenu.open} active={Boolean(moreMenu.anchor)} size={32} />
            ) : (
              <>
                {canPin && <IconButton icon="pin" label={t("inbox-inboxdetail.pin_messages")} onClick={() => setPins(true)} size={32} />}
                {muteRows.length > 0 && <IconButton icon="bellOff" label={t("inbox-inboxdetail.mute")} onClick={muteMenu.open} active={Boolean(muteMenu.anchor)} size={32} />}
              </>
            )}
            {muteMenu.anchor && <Menu anchor={muteMenu.anchor} onClose={muteMenu.close} align="right" width={224} items={muteRows} />}
            {moreMenu.anchor && (
              <Menu anchor={moreMenu.anchor} onClose={moreMenu.close} align="right" width={224}
                items={[
                  ...(canPin ? [{ key: "pins", label: t("inbox-inboxdetail.pin_messages"), icon: "pin" as const, onSelect: () => setPins(true) }] : []),
                  ...muteRows.map((r, i) => (i === 0 && canPin ? { ...r, divider: true } : r)),
                ]} />
            )}
            {done ? (
              <Button tone="secondary" icon="inbox" onClick={() => void setDone(item, false)}>{t("inbox-inboxdetail.move_back")}</Button>
            ) : (
              <Button
                tone="secondary"
                icon="check"
                onClick={() => {
                  // the page leaves the item at once, so say so with 撤销 on any screen (#4162), as a row does on touch
                  void markFromRow(item, true, true);
                  navigate({ name: "inbox" });
                }}
              >
                {t("inbox-inboxdetail.done")}
              </Button>
            )}
          </div>
          {item.summary && (
            <p className="mt-3 text-[13px] leading-relaxed text-ink">
              <Spark />
              {item.summary}
            </p>
          )}
          <InboxLine item={item} />
          {!done && agentWorking(item) && <div className="mt-2"><AgentWorking it={item} /></div>}
          <div className="mt-3 flex flex-wrap gap-2">
            {canDraft(item) ? (
              <Button tone="accent" icon="write" onClick={() => setDialog("draft")}>{draft && !done ? t("inbox-inboxdetail.view_draft") : t("inbox-inboxdetail.ai_draft")}</Button>
            ) : (
              // Answered in the source's own app (a CI run has nothing to answer: only 交给 AI).
              openURL(item) && <Button tone="accent" icon="external" onClick={() => openSystem(openURL(item)!)}>{openTitle(item)}</Button>
            )}
            <Button tone="secondary" icon="spark" onClick={() => setDialog("handoff")}>{t("inbox-inboxdetail.hand_to_ai")}</Button>
          </div>
        </div>
      </div>
      {place ? <InboxExtensionBody key={`${item.id}/${place.extension}/${place.id}`} item={item} place={place} own={own} onOwn={setOwnShown} /> : own()}
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
      <p className="text-[13.5px] font-medium text-ink">{t("inbox-inboxdetail.placeholder_title")}</p>
      <p className="text-[12.5px] text-ink-3">{t("inbox-inboxdetail.placeholder_hint")}</p>
    </div>
  );
}
