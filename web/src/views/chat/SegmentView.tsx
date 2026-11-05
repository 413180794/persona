import { memo, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clock } from "../../lib/format";
import { getSession, getState } from "../../lib/store";
import { agentStatus, subagentFor } from "../../lib/tasks";
import { turnImages } from "../../lib/turn-images";
import { TurnImages } from "./TurnImages";
import { exchangeSteps, isLoopTick, type Segment, type UserMessage } from "../../lib/turns";
import type { Item, SessionLive } from "../../lib/types";
import { planSteps, thinkingDurations, workBlocks, type ToolStep } from "../../lib/work-groups";
import { channelOf, useChannels } from "../../lib/channels";
import { splitQuote } from "../../lib/drafts";
import { EASE } from "../../ui/atoms";
import { ChannelMark } from "../../ui/ChannelMark";
import { Markdown } from "../../ui/Markdown";
import { InjectedHint, LoopTick, MergedBubble, PeerMessage } from "./Messages";
import { ContextGroup } from "./ContextGroup";
import { FileCards, ReplyFiles } from "./FileCards";
import { MessageActions } from "./MessageActions";
import { QuoteBlock } from "./Quote";
import { PathText } from "../../ui/PathLinks";
import { Thumb } from "./Steps";
import { useFoldScroll } from "./fold-scroll";
import { useChatContext } from "./chat-context";
import { WorkGroup, WorkNodes, type WorkProps } from "./WorkGroup";
import { t } from "../../i18n";

/** A message longer than this many lines folds to ten of them (FOLDED), with 展开 under it (cindy's UserMessage). */
export const BUBBLE_MAX_LINES = 14;
/** Before it is measured: about this many characters to a line of the 488 px bubble. */
const CHARS_PER_LINE = 30;
/** A folded message: ten lines high, scrolling inside (the chat scrolls on at its top and end). */
const FOLDED = "max-h-[10lh] overflow-y-auto";

/** How many lines a message takes in its bubble, by its text (the bubble measures itself once drawn). */
export function bubbleLines(text: string, perLine = CHARS_PER_LINE): number {
  return text.split("\n").reduce((n, l) => n + Math.max(1, Math.ceil([...l].length / perLine)), 0);
}

export function UserBubble({ msg, mid, sid, pending }: { msg: UserMessage; mid: string; sid: string; pending?: boolean }) {
  return (
    <div className="ml-auto flex max-w-[488px] min-w-0 flex-col items-end gap-1.5 pl-[12%]" style={pending ? { animation: `fade-up 300ms ${EASE} both` } : undefined} data-user-bubble>
      {msg.images.length > 0 && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {msg.images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} size={96} />
          ))}
        </div>
      )}
      {msg.contexts.length > 0 && <ContextGroup contexts={msg.contexts} />}
      {msg.files && <FileCards files={msg.files} mid={mid} />}
      {msg.text &&
        (pending ? (
          <BubbleText text={msg.text} mid={mid} sid={sid} pending />
        ) : (
          <MessageActions text={msg.text} isUser ts={msg.ts} side className="max-w-full">
            <BubbleText text={msg.text} ts={msg.ts} mid={mid} sid={sid} />
          </MessageActions>
        ))}
      {(pending || msg.queued) && <span className="text-[11px] text-ink-3">{pending ? t("chat-segmentview.sending") : t("chat-segmentview.queued")}</span>}
      {!pending && <ChannelSource clientMsgId={msg.client_msg_id} />}
    </div>
  );
}

/** The bubble's words, folded to ten lines past fourteen; the fold scrolls (the wheel, a trackpad, a finger), with a
 *  fade at its bottom while there is more (fold-scroll.ts). `pending` (still being sent): dimmed, the same fold. A
 *  leading `> ` block (引用) is a folded quote card at its top instead of `>` lines (Quote.tsx). */
function BubbleText({ text: whole, ts, mid, sid, pending }: { text: string; ts?: string; mid: string; sid: string; pending?: boolean }) {
  const split = useMemo(() => splitQuote(whole), [whole]);
  const text = split ? split.rest : whole;
  const ref = useRef<HTMLDivElement>(null);
  const [long, setLong] = useState(() => bubbleLines(text) > BUBBLE_MAX_LINES);
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || open) return;
    // its full height in lines (a fold's scrollHeight is all of it)
    const line = parseFloat(getComputedStyle(el).lineHeight) || 24;
    setLong(el.scrollHeight / line > BUBBLE_MAX_LINES + 0.5);
  }, [text, open]);
  const folded = long && !open;
  const fold = useFoldScroll(ref, folded, [text]);
  const words = text && (
    <div
      ref={ref}
      {...fold}
      className={`text-[length:var(--chat-body)] leading-[1.6] whitespace-pre-wrap [overflow-wrap:anywhere] ${folded ? FOLDED : ""}`}
      data-folded={folded || undefined}
    >
      <PathText text={text} mid={mid} cwd={getSession(mid, sid)?.cwd} />
    </div>
  );
  return (
    <div className="flex max-w-full flex-col items-end gap-1">
      {split ? (
        <div className={`flex max-w-full flex-col gap-1.5 rounded-[12px] bg-user-bubble px-4 py-2.5 text-ink ${pending ? "opacity-60" : ""}`} title={clock(ts)}>
          <QuoteBlock text={split.quote} />
          {words}
        </div>
      ) : (
        <div className={`max-w-full rounded-[12px] bg-user-bubble px-4 py-2.5 text-ink ${pending ? "opacity-60" : ""}`} title={clock(ts)}>
          {words}
        </div>
      )}
      {long && (
        <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="rounded-[6px] px-1.5 py-0.5 text-[12px] font-medium whitespace-nowrap text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2">
          {open ? t("chat-segmentview.collapse") : t("chat-segmentview.expand")}
        </button>
      )}
    </div>
  );
}

/** 「来自 微信」 under a message that came from a channel (its client_msg_id is `<channel>-…`, docs/channels.md). */
function ChannelSource({ clientMsgId }: { clientMsgId?: string }) {
  const via = channelOf(clientMsgId, useChannels());
  if (!via) return null;
  return (
    <span className="flex items-center gap-1.5 whitespace-nowrap text-[11px] text-ink-3">
      <ChannelMark c={via} size={14} />
      {t("chat-segmentview.from", { name: via.name })}
    </span>
  );
}

/** An answer's prose at the chat's body size, 1.6 lines. Important: styles.css's `.prose-j` (14 / 1.7) is outside
 *  Tailwind's layers, so it would win over a plain utility. */
const ANSWER = "[&_.prose-j]:text-[length:var(--chat-body)]! [&_.prose-j]:leading-[1.6]!";

/** What the agent said, without a bubble, the chat's body size (15, 16 on a phone) / 1.6. `fade`: it just came in, its
 *  words fade in once. Still streaming, it has the same menu (copy what is there so far, quote it). */
export function Answer({ item, streaming, mid, sid, fade }: { item?: Item; streaming?: string; mid?: string; sid?: string; fade?: boolean }) {
  const text = streaming ?? item?.text ?? "";
  if (!text) return null;
  const cwd = mid && sid ? getSession(mid, sid)?.cwd : undefined;
  if (streaming !== undefined) {
    return (
      <MessageActions text={text}>
        <div className={ANSWER} data-answer="streaming">
          <Markdown text={text} mid={mid} cwd={cwd} streaming />
        </div>
      </MessageActions>
    );
  }
  return (
    <MessageActions text={text} ts={item?.ts} itemId={item?.id}>
      <div className={ANSWER} data-answer>
        <Markdown text={text} mid={mid} cwd={cwd} fade={fade || undefined} />
      </div>
      {mid && sid && <ReplyFiles text={text} mid={mid} cwd={cwd} />}
    </MessageActions>
  );
}

export const SegmentView = memo(function SegmentView({
  seg,
  live,
  mid,
  sid,
  streaming,
  status,
  expandFold,
  onOpen,
}: {
  seg: Segment;
  live: boolean;
  mid: string;
  sid: string;
  streaming?: string;
  /** The screen's status line, for the live exchange only. */
  status?: SessionLive | null;
  expandFold: (f: Item) => Promise<void>;
  /** A merged exchange: opening its work splits it into its messages. */
  onOpen?: () => void;
}) {
  const { subagents, fresh, scope } = useChatContext();
  const steps = useMemo(() => planSteps(exchangeSteps(seg)), [seg]);
  const turnStart = seg.user?.ts ?? seg.start;
  const thinking = useMemo(() => thinkingDurations(steps, turnStart), [steps, turnStart]);
  const p = useMemo<WorkProps>(() => {
    const agent = (s: ToolStep) => agentStatus(s.result, subagentFor(subagents, s.use), live);
    return { mid, sid, scope, expandFold, agentStatus: agent, thinking, status, onOpen };
  }, [mid, sid, scope, expandFold, subagents, live, thinking, status, onOpen]);
  const nodes = useMemo(() => workBlocks(steps, { live, turnStart, agentStatus: p.agentStatus }), [steps, live, turnStart, p.agentStatus]);
  // the pictures the agent looked at this turn, between its work and its last words
  const images = useMemo(() => turnImages(seg, getState().machines.find((m) => m.id === mid)?.name), [seg, mid]);
  // a finished exchange's last words (the run of them at its end) come after those pictures
  let cut = nodes.length;
  for (let n = nodes[cut - 1]; !live && n?.kind === "step" && n.step.kind === "text"; n = nodes[cut - 1]) cut--;
  const working = live && streaming === undefined && !nodes.some((n) => n.kind === "group" && n.live);
  const imageStrip = images.length > 0 && <TurnImages images={images} mid={mid} sid={sid} cwd={getSession(mid, sid)?.cwd} />;
  return (
    <section className="flex flex-col gap-3" data-segment>
      {seg.user && (seg.user.injected ? <InjectedHint msg={seg.user} sid={sid} /> : seg.user.parts ? <MergedBubble msg={seg.user} mid={mid} sid={sid} /> : seg.user.from ? <PeerMessage msg={seg.user} /> : isLoopTick(seg.user) ? <LoopTick msg={seg.user} /> : <UserBubble msg={seg.user} mid={mid} sid={sid} />)}
      <WorkNodes nodes={nodes.slice(0, cut)} p={p} live={live} fresh={fresh} />
      {/* working with no run of steps going (its words just came, or nothing yet): the live group still shows */}
      {working && <WorkGroup g={{ kind: "group", id: `work-live-${seg.id}`, children: [], live: true, startedAt: Date.parse(turnStart ?? "") || undefined }} p={p} />}
      {imageStrip}
      <WorkNodes nodes={nodes.slice(cut)} p={p} fresh={fresh} />
      {streaming !== undefined && <Answer streaming={streaming} mid={mid} sid={sid} />}
    </section>
  );
});
