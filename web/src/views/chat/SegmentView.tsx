import { memo } from "react";
import { clock } from "../../lib/format";
import { isLoopTick, type Segment, type UserMessage } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { EASE } from "../../ui/atoms";
import { Markdown } from "../../ui/Markdown";
import { LoopTick, MergedBubble, PeerMessage } from "./Messages";
import { ContextGroup } from "./ContextGroup";
import { MessageActions } from "./MessageActions";
import { Thumb, WorkRow } from "./Steps";

export function UserBubble({ msg, mid, sid, pending }: { msg: UserMessage; mid: string; sid: string; pending?: boolean }) {
  return (
    <div className="flex flex-col items-end gap-1.5 pl-[12%]" style={pending ? { animation: `fade-up 300ms ${EASE} both` } : undefined}>
      {msg.images.length > 0 && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {msg.images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} size={96} />
          ))}
        </div>
      )}
      {msg.contexts.length > 0 && <ContextGroup contexts={msg.contexts} />}
      {msg.text && (pending ? (
        <div className="max-w-full rounded-[14px] bg-user-bubble px-3.5 py-2 text-[14px] leading-[1.55] whitespace-pre-wrap text-ink opacity-60 [overflow-wrap:anywhere]">{msg.text}</div>
      ) : (
        <MessageActions text={msg.text} isUser ts={msg.ts} side className="max-w-full">
          <div className="max-w-full rounded-[14px] bg-user-bubble px-3.5 py-2 text-[14px] leading-[1.55] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]" title={clock(msg.ts)}>
            {msg.text}
          </div>
        </MessageActions>
      ))}
      {(pending || msg.queued) && <span className="text-[11px] text-ink-3">{pending ? "发送中…" : "工作中插入的消息"}</span>}
    </div>
  );
}

export function Answer({ item, streaming }: { item?: Item; streaming?: string }) {
  const text = streaming ?? item?.text ?? "";
  if (!text) return null;
  if (streaming !== undefined) {
    return (
      <div>
        <Markdown text={text} />
        <span className="stream-caret" aria-hidden />
      </div>
    );
  }
  return (
    <MessageActions text={text} ts={item?.ts}>
      <Markdown text={text} />
    </MessageActions>
  );
}

export const SegmentView = memo(function SegmentView({
  seg,
  live,
  now,
  mid,
  sid,
  streaming,
  expandFold,
  onOpen,
}: {
  seg: Segment;
  live: boolean;
  now: number;
  mid: string;
  sid: string;
  streaming?: string;
  expandFold: (f: Item) => Promise<void>;
  /** A merged exchange: opening its work splits it into its messages. */
  onOpen?: () => void;
}) {
  // while the agent works, its latest words are still part of the work, unless the headless assistant streams
  const showAnswer = !live || streaming !== undefined || Boolean(seg.answer && !seg.steps.length);
  return (
    <section className="flex flex-col gap-3">
      {seg.user && (seg.user.parts ? <MergedBubble msg={seg.user} mid={mid} sid={sid} /> : seg.user.from ? <PeerMessage msg={seg.user} /> : isLoopTick(seg.user) ? <LoopTick msg={seg.user} /> : <UserBubble msg={seg.user} mid={mid} sid={sid} />)}
      <WorkRow seg={live && seg.answer && !showAnswer ? { ...seg, steps: [...seg.steps, { kind: "text", id: seg.answer.id, item: seg.answer }] } : seg} live={live} now={now} mid={mid} sid={sid} expandFold={expandFold} onOpen={onOpen} />
      {showAnswer && <Answer item={seg.answer} streaming={streaming} />}
    </section>
  );
});
