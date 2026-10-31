import { memo } from "react";
import { clock } from "../../lib/format";
import type { Segment, UserMessage } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { copyText, EASE } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
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
      {msg.contexts.map((c, i) => (
        <a
          key={i}
          href={c.url || undefined}
          target="_blank"
          rel="noreferrer"
          className="flex max-w-full flex-col gap-0.5 rounded-[10px] border-l-2 border-accent bg-inset px-2.5 py-1.5 text-left shadow-hairline"
        >
          <span className="truncate text-[11.5px] font-medium text-ink-2">{c.kind === "edit" ? "AI 修改 · " : "上下文 · "}{c.title || c.url || "引用"}</span>
          <span className="line-clamp-2 text-[12px] text-ink-3">{c.text}</span>
        </a>
      ))}
      {msg.text && (
        <div
          className="max-w-full rounded-[14px] bg-user-bubble px-3.5 py-2 text-[14px] leading-[1.55] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]"
          style={{ opacity: pending ? 0.6 : 1 }}
          title={clock(msg.ts)}
        >
          {msg.text}
        </div>
      )}
      {(pending || msg.queued) && <span className="text-[11px] text-ink-3">{pending ? "发送中…" : "工作中插入的消息"}</span>}
    </div>
  );
}

function Answer({ item, streaming }: { item?: Item; streaming?: string }) {
  const text = streaming ?? item?.text ?? "";
  if (!text) return null;
  return (
    <div className="group/answer">
      <Markdown text={text} />
      {streaming !== undefined && <span className="stream-caret" aria-hidden />}
      {streaming === undefined && (
        <div className="mt-1.5 flex h-6 items-center gap-0.5 opacity-0 transition-opacity duration-200 group-hover/answer:opacity-100 focus-within:opacity-100">
          <button type="button" aria-label="拷贝回复" title="拷贝" onClick={() => copyText(text)} className="flex size-6 items-center justify-center rounded-[6px] text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2">
            <Icon name="copy" size={14} />
          </button>
          {item?.ts && <span className="ml-1 text-[11px] text-ink-3">{clock(item.ts)}</span>}
        </div>
      )}
    </div>
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
}: {
  seg: Segment;
  live: boolean;
  now: number;
  mid: string;
  sid: string;
  streaming?: string;
  expandFold: (f: Item) => Promise<void>;
}) {
  // while the agent works, its latest words are still part of the work, unless the headless assistant streams
  const showAnswer = !live || streaming !== undefined || Boolean(seg.answer && !seg.steps.length);
  return (
    <section className="flex flex-col gap-3">
      {seg.user && <UserBubble msg={seg.user} mid={mid} sid={sid} />}
      <WorkRow seg={live && seg.answer && !showAnswer ? { ...seg, steps: [...seg.steps, { kind: "text", id: seg.answer.id, item: seg.answer }] } : seg} live={live} now={now} mid={mid} sid={sid} expandFold={expandFold} />
      {showAnswer && <Answer item={seg.answer} streaming={streaming} />}
    </section>
  );
});
