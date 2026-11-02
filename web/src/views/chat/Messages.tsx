// Messages that are not plain bubbles (iOS MergedUserBubble / PeerMessage / LoopTickRow): a finished exchange's several
// messages in one bubble, a message another Claude session sent, and a /loop waking the agent again.
import { clock } from "../../lib/format";
import type { UserMessage } from "../../lib/turns";
import { Icon } from "../../ui/icons";
import { MessageActions } from "./MessageActions";
import { Thumb } from "./Steps";

export function MergedBubble({ msg, mid, sid }: { msg: UserMessage; mid: string; sid: string }) {
  const parts = msg.parts ?? [msg.text];
  return (
    <div className="flex flex-col items-end gap-1.5 pl-[12%]">
      {msg.images.length > 0 && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {msg.images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} size={96} />
          ))}
        </div>
      )}
      <MessageActions text={parts.join("\n\n")} isUser ts={msg.ts} side className="max-w-full">
        <div className="flex max-w-full flex-col gap-2 rounded-[14px] bg-user-bubble px-3.5 py-2 text-[14px] leading-[1.55] text-ink" title={clock(msg.ts)} aria-label={`${parts.length} 条消息`}>
          {parts.map((t, i) => (
            <div key={i} className="flex flex-col gap-2">
              {i > 0 && <span aria-hidden className="mx-auto h-[2px] w-6 rounded-full bg-ink/20" />}
              <span className="whitespace-pre-wrap [overflow-wrap:anywhere]">{t}</span>
            </div>
          ))}
        </div>
      </MessageActions>
    </div>
  );
}

export function PeerMessage({ msg }: { msg: UserMessage }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-card bg-inset px-3.5 py-2.5 shadow-hairline sm:mr-[12%]" title={clock(msg.ts)}>
      <span className="flex items-center gap-1.5 text-[12px] font-medium whitespace-nowrap text-ink-3">
        <Icon name="loop" size={12} />
        来自 {msg.from}
      </span>
      <span className="text-[14px] leading-[1.55] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">{msg.text}</span>
    </div>
  );
}

export function LoopTick({ msg }: { msg: UserMessage }) {
  const rest = msg.text.slice(5).trim();
  return (
    <div className="flex justify-end">
      <span className="flex max-w-full min-w-0 items-center gap-1.5 rounded-full bg-inset px-3 py-1 text-[12px] text-ink-2 shadow-hairline">
        <Icon name="loop" size={12} className="shrink-0" />
        <span className="shrink-0 font-medium whitespace-nowrap">循环唤醒</span>
        {msg.ts && <span className="shrink-0 tabular-nums">{clock(msg.ts)}</span>}
        {rest && <span className="min-w-0 truncate text-ink-3">{rest}</span>}
      </span>
    </div>
  );
}
