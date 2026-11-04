// 收件箱 (docs/inbox.md): Feishu messages, mail, Feishu documents and 公司圈 in one list, sorted by the AI's triage,
// with the agents waiting on you at the head of 需要你. Wide: the list (380px) and the opened item side by side;
// narrow: the opened item takes the list's place, with a way back.
import { useState } from "react";
import { useInbox, refreshInbox } from "../../lib/inbox";
import { newDraft, useMailAccounts } from "../../lib/mail";
import { navigate } from "../../lib/router";
import { cx, IconButton, Spinner } from "../../ui/atoms";
import { ComposeDialog } from "./ComposeDialog";
import { DetailPlaceholder, InboxDetail } from "./InboxDetail";
import { InboxList } from "./InboxList";

export function Inbox({ id, onToggleSidebar }: { id?: string; onToggleSidebar: () => void }) {
  const item = useInbox((s) => (id ? s.items.find((x) => x.id === id) : undefined));
  const loaded = useInbox((s) => s.loaded);
  const loading = useInbox((s) => s.loading);
  const accounts = useMailAccounts();
  const [composing, setComposing] = useState(false);
  return (
    <div className="@container flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        {id && <IconButton icon="chevronLeft" label="返回收件箱" onClick={() => navigate({ name: "inbox" })} className="@4xl:hidden" />}
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink">收件箱</h1>
        {loading && loaded && <Spinner />}
        <IconButton icon="compose" label={accounts.length ? "写邮件" : "写邮件（先在设置 › 连接器里加一个邮箱）"} onClick={() => setComposing(true)} disabled={!accounts.length} />
        <IconButton icon="retry" label="刷新（立刻采集飞书和邮件）" onClick={() => void refreshInbox()} disabled={loading} />
      </header>
      <div className="flex min-h-0 flex-1">
        <section aria-label="收件箱列表" className={cx("min-h-0 w-full flex-col @4xl:flex @4xl:w-[380px] @4xl:shrink-0 @4xl:border-r @4xl:border-line", id ? "hidden" : "flex")}>
          <InboxList selected={id} />
        </section>
        <section aria-label="详情" className={cx("min-h-0 min-w-0 flex-1 flex-col @4xl:flex", id ? "flex" : "hidden")}>
          {id ? <InboxDetail item={item} loaded={loaded} /> : <DetailPlaceholder />}
        </section>
      </div>
      {composing && <ComposeDialog draftKey="new" initial={newDraft(accounts[0]?.id ?? "")} accounts={accounts} onClose={() => setComposing(false)} />}
    </div>
  );
}
