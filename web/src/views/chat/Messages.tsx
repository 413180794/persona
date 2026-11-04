// Messages that are not plain bubbles (iOS MergedUserBubble / PeerMessage / LoopTickRow): a finished exchange's several
// messages in one bubble, a message another Claude session sent, and a /loop waking the agent again.
import { clock } from "../../lib/format";
import { getSession } from "../../lib/store";
import type { UserMessage } from "../../lib/turns";
import { Icon } from "../../ui/icons";
import { FileCards } from "./FileCards";
import { MessageActions } from "./MessageActions";
import { Markdown } from "../../ui/Markdown";
import { PathText } from "../../ui/PathLinks";
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
      {msg.files && <FileCards files={msg.files} mid={mid} />}
      <MessageActions text={parts.join("\n\n")} isUser ts={msg.ts} side className="max-w-full">
        <div className="flex max-w-full flex-col gap-2 rounded-[14px] bg-user-bubble px-3.5 py-2 text-[length:var(--chat-secondary)] leading-[1.55] text-ink" title={clock(msg.ts)} aria-label={`${parts.length} 条消息`}>
          {parts.map((t, i) => (
            <div key={i} className="flex flex-col gap-2">
              {i > 0 && <span aria-hidden className="mx-auto h-[2px] w-6 rounded-full bg-ink/20" />}
              <span className="whitespace-pre-wrap [overflow-wrap:anywhere]">
                <PathText text={t} mid={mid} cwd={getSession(mid, sid)?.cwd} />
              </span>
            </div>
          ))}
        </div>
      </MessageActions>
    </div>
  );
}

// Its words are Markdown like an answer: agents write Markdown, and the hub turns a teammate's JSON status notes into it.
// A single line break stays one, as in the message it was (iOS: markdownSoftBreakMode(.lineBreak)).
export function PeerMessage({ msg }: { msg: UserMessage }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-card bg-inset px-3.5 py-2.5 shadow-hairline sm:mr-[12%]" title={clock(msg.ts)}>
      <span className="flex items-center gap-1.5 text-[12px] font-medium whitespace-nowrap text-ink-3">
        <Icon name="loop" size={12} />
        来自 {msg.from}
      </span>
      <Markdown text={keepLineBreaks(msg.text)} small />
    </div>
  );
}

// Markdown joins the lines of a paragraph; two spaces before the newline keep it a line break.
export function keepLineBreaks(text: string): string {
  return text.replace(/([^\n])\n(?=[^\n])/g, "$1  \n");
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
