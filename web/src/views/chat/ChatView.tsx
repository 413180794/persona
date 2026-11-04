// One conversation: an agent in herdr on one of your computers, or a Persona assistant conversation.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, attachSession, errorText, sessionPath } from "../../lib/api";
import { ago, resumeCommand, sessionCan, sessionDeletable, sessionMenuActions, sourceSubtitle, sessionTitle, shortPath, stateOf, tabLabel, uuid } from "../../lib/format";
import { navigate } from "../../lib/router";
import { togglePanel, usePanel } from "../../lib/panel";
import { machineWithHub } from "../../lib/mesh";
import { hasModes, isMuted, toggleMute } from "../../lib/session-actions";
import { flipPin } from "../../lib/agent-actions";
import { isPinned } from "../../lib/pins";
import { acknowledgeLaunch, launchMessageId } from "../../lib/new-agent";
import { getSession, getState, markSeenLocally, refresh, refreshTodos, setMutes, useStore } from "../../lib/store";
import { buildTaskIndex, STOP_LOOP_MESSAGE, visibleSubagents } from "../../lib/tasks";
import { echoed, taggedSeen } from "../../lib/echo";
import { outboxKey, settleOutbox } from "../../lib/outbox";
import { conversationKey, pickedLauncher, pickedModel } from "../../lib/assistant-chat";
import { unlinkAgentFromTodo } from "../../lib/todos";
import { LinkTodoDialog } from "../LinkTodoDialog";
import { chatLines, openRecap, parseUserText, segments, type UserMessage } from "../../lib/turns";
import type { Item, Session, Subagent } from "../../lib/types";
import { anchoredScroll, followsAfterScroll, READER_WINDOW, type ScrollAnchor } from "../../lib/disclosure";
import { Button, copyText, cx, Dialog, IconButton, Loading, Menu, StatePill, StatusDot, toast, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Composer, type Outgoing } from "./Composer";
import { InteractionCard } from "./Interactions";
import { UserBubble } from "./SegmentView";
import { ActivityChips, AgentStrip, SubagentFooter } from "./Activity";
import { ChatContext, type ChatContextValue } from "./chat-context";
import { ChatLineView, closedRecap, closeRecap, OpenRecapLine } from "./Lines";
import { useTranscript } from "./useTranscript";
import { DeleteSessionDialog, ModeChips, RenameDialog, RenameSpaceDialog, SessionInfoDialog } from "./SessionDialogs";
import { ChatActionsContext, type ChatActions } from "./chat-actions";
import { QueueStrip } from "./QueueStrip";
import { chatFiles } from "../../lib/mentions";
import { sameMessage } from "../../lib/queue";
import { OutboxBubbles } from "./OutboxBubbles";
import { AssistantFace, AssistantLauncherChip, useAssistantMenu } from "../assistant/AssistantChat";
import { MAIN_TITLE } from "../assistant/conversations";
import { usePersona } from "../assistant/Persona";
import { displayLines, loadFeed, loadToday, sendRouted, useFeed, type FeedItem } from "../../lib/feed";
import { DayLine, FeedLineView, QuickResultCard, RouteChip, TaskDock, TaskQuestions } from "../assistant/Tasks";

const NO_FEED: FeedItem[] = [];

interface Pending {
  /** The client_msg_id it went with. */
  id: string;
  msg: UserMessage;
  at: number;
}

export function ChatView({ mid, sid, assistant, starting: launching, onToggleSidebar }: { mid: string; sid: string; assistant?: boolean; starting?: boolean; onToggleSidebar?: () => void }) {
  const session = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === sid) ?? s.assistant?.conversations.find((c) => c.session_id === sid));
  const machineName = useStore((s) => machineWithHub(mid, s.machines));
  const siblings = useStore((s) => (session?.space_id ? (s.sessions[mid] ?? []).filter((x) => x.space_id === session.space_id && stateOf(x) !== "offline") : []));
  const interactions = useStore((s) => s.interactions.filter((i) => i.machine_id === mid && i.session_id === sid));
  // a subagent's conversation is read in the same chat, read-only (its session id is "<parent>@<agent>")
  const [viewing, setViewing] = useState<Subagent | null>(null);
  useEffect(() => setViewing(null), [mid, sid]);
  const viewed = viewing ? (session?.subagents?.find((a) => a.id === viewing.id) ?? viewing) : null;
  const shownSid = viewed?.session_id ?? sid;
  const tr = useTranscript(mid, shownSid, { starting: Boolean(launching) && !viewed });
  const [pending, setPending] = useState<Pending[]>(() => {
    const launch = Object.values(getState().launches).find((l) => l.phase === "ready" && l.machineId === mid && l.session?.session_id === sid);
    const out = launch?.outgoing;
    if (!launch || !out || (!out.text && !out.images.length)) return [];
    const id = launchMessageId(launch);
    return [{ id, at: Date.now(), msg: { id: `pending:${id}`, ...parseUserText(out.text), images: out.images.map((image, i) => ({ id: `pending:${id}#${i}`, seq: 0, role: "user", kind: "image", image })) } }];
  });
  const [draftRefusal, setDraftRefusal] = useState<{ out: Outgoing; id: string; draft: string; clearable?: string } | null>(null);
  // what other pages sent here (问 Persona, 交给 AI…): shown like this page's own sending bubbles (lib/outbox.ts)
  const outbox = useStore((s) => s.outbox[outboxKey(mid, sid)] ?? []);
  // a message Claude queued shows in the queue strip, not as a sending bubble too (ChatView.swift chatScroll)
  const queued = session?.queued ?? [];
  const shownPending = queued.length ? pending.filter((p) => !queued.some((q) => sameMessage(q, p.msg.text))) : pending;
  const outgoing = shownPending.length + outbox.filter((e) => e.state !== "failed").length;
  const persona = usePersona();
  const state = session ? stateOf(session) : "unknown";
  const working = state === "working" || Boolean(tr.streaming) || outgoing > 0;
  // still starting only until the conversation has anything in it: the flag the caller passes may outlive the start
  const starting = Boolean(launching) && !tr.items.length && !tr.streaming && state !== "working";
  const now = useNow(1000, working);

  const segs = useMemo(() => segments(tr.items), [tr.items]);
  // finished exchanges of several messages the user split open (iOS openWork on a merged exchange)
  const [split, setSplit] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => setSplit((x) => (x.size ? new Set() : x)), [mid, shownSid]);

  // what the messages' menus, the image viewer and the @ menu read from this chat (chat-actions.ts)
  const items = useRef(tr.items);
  items.current = tr.items;
  const draftKey = `${mid}/${sid}`;
  const canCompose = sessionCan(session, "send");
  const actions = useMemo<ChatActions>(() => ({ draftKey, canCompose, images: () => items.current.filter((i) => i.kind === "image") }), [draftKey, canCompose]);
  const mention = useMemo(() => ({ cwd: session?.cwd, self: draftKey, files: () => chatFiles(items.current) }), [session?.cwd, draftKey]);

  // a sent message stays as a "sending" bubble until the transcript has it (lib/echo.ts): an item the hub tied to it
  // (any kind, a notice too), or else a latest user message with its words; one that answered one answers no other
  const claimed = useRef(new Set<string>());
  useEffect(() => {
    const users = [...segs.slice(-4).flatMap((seg) => seg.user ? [seg.user] : []), ...taggedSeen(tr.items)];
    users.forEach((user) => acknowledgeLaunch(mid, sid, user));
    settleOutbox(mid, sid, users);
    if (!pending.length) return;
    setPending((ps) => {
      const shown = echoed(ps, users, (p) => ({ id: p.id, text: p.msg.text, images: p.msg.images, at: p.at }), claimed.current);
      return ps.filter((p) => !shown.has(p) && Date.now() - p.at < 30_000);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [segs]);

  // seen: on open, and whenever a turn ends while we look
  useEffect(() => {
    if (document.visibilityState === "visible" && (state === "unread" || state === "idle")) markSeenLocally(mid, sid);
  }, [mid, sid, state]);
  useEffect(() => {
    const v = () => document.visibilityState === "visible" && markSeenLocally(mid, sid);
    document.addEventListener("visibilitychange", v);
    return () => document.removeEventListener("visibilitychange", v);
  }, [mid, sid]);

  // ── scrolling: stick to the end while at the end; keep the view still when older pages load ──
  const scroller = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);
  // the offset at the last scroll event, and when the reader last scrolled (followsAfterScroll: only the reader leaves the end)
  const lastTop = useRef(0);
  const readerAt = useRef(-Infinity);
  const dragging = useRef(false);
  const [showDown, setShowDown] = useState(false);
  const before = useRef<{ h: number; top: number } | null>(null);
  const first = useRef(true);

  useEffect(() => {
    first.current = true;
    atEnd.current = true;
    lastTop.current = 0;
  }, [mid, shownSid]);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (before.current) {
      el.scrollTop = before.current.top + (el.scrollHeight - before.current.h);
      before.current = null;
      return;
    }
    if (atEnd.current || first.current) {
      el.scrollTop = el.scrollHeight;
      if (tr.items.length) first.current = false;
    }
  }, [tr.items, pending, outbox, tr.streaming, interactions.length]);

  // A row that unfolds or folds stays where it was on screen while its height runs (#1124, lib/disclosure.ts): the
  // clicked header's top, or a work row's end when it folds from its 收起 button. At the end it keeps following.
  // Held until the sizes stop changing for a moment, or the reader scrolls.
  const anchor = useRef<(ScrollAnchor & { el: Element; quiet: number }) | null>(null);
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    const drop = () => {
      if (anchor.current) clearTimeout(anchor.current.quiet);
      anchor.current = null;
    };
    const onClick = (e: MouseEvent) => {
      const t = e.target instanceof Element ? e.target : null;
      const fromEnd = t?.closest("[data-fold-from-end]")?.closest("[data-work-row]");
      const el = fromEnd ?? t?.closest("button[aria-expanded]");
      if (!el || !sc.contains(el)) return;
      drop();
      const r = el.getBoundingClientRect();
      anchor.current = { el, edge: fromEnd ? "bottom" : "top", at: fromEnd ? r.bottom : r.top, follow: atEnd.current, quiet: window.setTimeout(drop, 1000) };
    };
    sc.addEventListener("click", onClick, true);
    for (const ev of ["wheel", "touchstart", "keydown"]) sc.addEventListener(ev, drop, { passive: true });
    return () => {
      drop();
      sc.removeEventListener("click", onClick, true);
      for (const ev of ["wheel", "touchstart", "keydown"]) sc.removeEventListener(ev, drop);
    };
  }, []);

  // content that grows by itself (images loading, a row expanding at the end) keeps the end in view; a held row stays
  // put. A ResizeObserver runs after layout and before paint, so the offset moves in the same frame as the height.
  // The view itself is watched too: what sits under it (the composer growing, the task dock, a question card) makes it
  // shorter without the content changing, and at the end the newest line stays in view.
  useEffect(() => {
    const view = scroller.current;
    const el = view?.firstElementChild;
    if (!view || !el) return;
    const ro = new ResizeObserver(() => {
      const sc = scroller.current;
      if (!sc) return;
      const a = anchor.current;
      if (a && a.el.isConnected) {
        const r = a.el.getBoundingClientRect();
        const next = anchoredScroll(a, {
          scrollTop: sc.scrollTop, scrollHeight: sc.scrollHeight, clientHeight: sc.clientHeight,
          viewTop: sc.getBoundingClientRect().top, edgeAt: a.edge === "top" ? r.top : r.bottom, elTop: r.top,
        });
        if (Math.abs(next.scrollTop - sc.scrollTop) >= 0.5) sc.scrollTop = next.scrollTop;
        if (a.follow && !next.follow) {
          // following would push the row off the top: hold it from here
          a.follow = false;
          a.edge = "top";
          a.at = a.el.getBoundingClientRect().top;
          atEnd.current = false;
        }
        clearTimeout(a.quiet);
        a.quiet = window.setTimeout(() => (anchor.current = null), 600);
        return;
      }
      if (atEnd.current) sc.scrollTop = sc.scrollHeight;
    });
    ro.observe(el);
    ro.observe(view);
    return () => ro.disconnect();
  }, []);

  // what the reader does to scroll: the wheel or a trackpad going up, a finger, the keys that scroll, the scroll bar
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    const mark = () => (readerAt.current = performance.now());
    const wheel = (e: WheelEvent) => e.deltaY < 0 && mark();
    const key = (e: KeyboardEvent) => {
      const t = e.target instanceof Element ? e.target : null;
      if (t?.closest("input, textarea, [contenteditable=true]")) return;
      if (["ArrowUp", "PageUp", "Home", " "].includes(e.key)) mark();
    };
    // a press on the view itself (not on what is in it) is on its scroll bar
    const down = (e: PointerEvent) => {
      if (e.target !== sc) return;
      dragging.current = true;
      mark();
    };
    const up = () => (dragging.current = false);
    sc.addEventListener("wheel", wheel, { passive: true });
    sc.addEventListener("touchmove", mark, { passive: true });
    sc.addEventListener("pointerdown", down);
    window.addEventListener("pointerup", up);
    window.addEventListener("keydown", key);
    return () => {
      sc.removeEventListener("wheel", wheel);
      sc.removeEventListener("touchmove", mark);
      sc.removeEventListener("pointerdown", down);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("keydown", key);
    };
  }, []);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    // a held row decides; otherwise only the reader scrolling up leaves the end, and following picks up again once the
    // reader is back at the end (#1340: the pin's own scroll event, late behind more content, does not count)
    if (!anchor.current || anchor.current.follow) atEnd.current = followsAfterScroll(atEnd.current, { scrollTop: el.scrollTop, lastTop: lastTop.current, gap, reader: dragging.current || performance.now() - readerAt.current < READER_WINDOW });
    lastTop.current = el.scrollTop;
    setShowDown(!atEnd.current && gap > 400);
    if (el.scrollTop < 300 && tr.hasMore && !tr.loadingEarlier && !first.current) {
      before.current = { h: el.scrollHeight, top: el.scrollTop };
      void tr.loadEarlier().finally(() => {
        // nothing came: forget the anchor
        requestAnimationFrame(() => (before.current = null));
      });
    }
  };

  const toEnd = () => {
    const el = scroller.current;
    if (!el) return;
    atEnd.current = true;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  };

  // ── sending ──
  const deliver = useCallback(
    async (out: Outgoing, id: string, clearDraft?: string): Promise<boolean> => {
      if (!sessionCan(getSession(mid, sid), "send")) {
        toast(sourceSubtitle(session) || "正在读取会话信息，稍后再试", "error");
        return false;
      }
      // a sending bubble shows picked contexts as quote cards, like the transcript will (not the raw tags)
      const p: Pending = { id, at: Date.now(), msg: { id: `pending:${id}`, ...parseUserText(out.text), images: out.images.map((image, i) => ({ id: `pending:${id}#${i}`, seq: 0, role: "user", kind: "image", image })) } };
      setPending((ps) => [...ps.filter((x) => x.id !== id), p]);
      atEnd.current = true;
      try {
        await api(`${sessionPath(mid, sid)}/messages`, {
          method: "POST",
          json: { text: out.text, images: out.images.length ? out.images : undefined, client_msg_id: id, clear_draft: clearDraft, launcher: assistant ? pickedLauncher(conversationKey(mid, sid)) : undefined, model: assistant ? pickedModel(conversationKey(mid, sid)) : undefined },
        });
        return true;
      } catch (e) {
        setPending((ps) => ps.filter((x) => x.id !== id));
        if (e instanceof ApiError && e.code === "input_draft") {
          setDraftRefusal({ out, id, draft: String(e.body.draft ?? ""), clearable: typeof e.body.clearable === "string" ? e.body.clearable : undefined });
        } else if (e instanceof ApiError && e.code === "dialog_open") {
          toast("电脑上开着一个对话框，先处理它再发", "error");
        } else if (e instanceof ApiError && e.body.delivered === "unknown") {
          toast("不确定送到没有：先看一眼对话再决定要不要重发", "error");
        } else {
          toast(`没发出去：${errorText(e)}`, "error");
        }
        return false;
      }
    },
    [mid, sid, session, assistant],
  );

  const onSend = async (out: Outgoing): Promise<boolean> => {
    // Routed to a task (「对「xx」说」): straight to its agent, not to Persona (docs/unified-entry.md「指向谁」).
    if (assistant && !out.images.length && out.text.trim()) {
      try {
        if (await sendRouted(sid, out.text)) return true;
      } catch (e) {
        toast(`没送到：${errorText(e)}`, "error");
        return false;
      }
    }
    return deliver(out, uuid());
  };
  // The assistant's tasks (docs/unified-entry.md): the conversation's feed, and the main one's 今天 card.
  const mainConversation = useStore((s) => s.assistant?.main);
  const feed = useFeed((s) => (assistant ? (s.feeds[sid] ?? NO_FEED) : NO_FEED));
  useEffect(() => {
    if (!assistant) return;
    void loadFeed(sid);
    if (sid === mainConversation) void loadToday(sid);
  }, [assistant, sid, mainConversation]);
  // The 今天 card's actions say something to Persona here.
  useEffect(() => {
    if (!assistant) return;
    const say = (e: Event) => {
      const d = (e as CustomEvent<{ mid: string; sid: string; text: string }>).detail;
      if (d.mid === mid && d.sid === sid) void deliver({ text: d.text, images: [] }, uuid());
    };
    window.addEventListener("persona:say", say);
    return () => window.removeEventListener("persona:say", say);
  }, [assistant, mid, sid, deliver]);

  const interrupt = async () => {
    if (!sessionCan(getSession(mid, sid), "interrupt")) return;
    try {
      await api(`${sessionPath(mid, sid)}/interrupt`, { method: "POST" });
      toast("已中断");
    } catch (e) {
      toast(`中断失败：${errorText(e)}`, "error");
    }
  };

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape" && state === "working" && !document.querySelector("[role=dialog],[role=menu]")) {
        e.preventDefault();
        void interrupt();
      }
    };
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, session]);

  const lastLive = viewed ? Boolean(viewed.running) && !tr.loading : working && !tr.loading;
  // The main conversation is always Persona (#1313): not its first message, the node's title for it.
  const isMain = Boolean(assistant && sid === mainConversation);
  const title = isMain ? MAIN_TITLE : session ? sessionTitle(session) : assistant ? "新对话" : "会话";
  const lines = useMemo(() => chatLines(tr.items, { live: lastLive && (viewed ? true : !outgoing), split }), [tr.items, lastLive, viewed, outgoing, split]);
  const ctx = useMemo<ChatContextValue>(
    () => ({ tasks: buildTaskIndex(tr.items), subagents: session?.subagents ?? [], openSubagent: assistant ? undefined : (a) => setViewing(a) }),
    [tr.items, session?.subagents, assistant],
  );
  const agents = assistant ? [] : visibleSubagents(session?.subagents, viewed?.id);
  const onSplit = useCallback((g: string) => setSplit((x) => new Set([...x, g])), []);
  const onUnsplit = useCallback((g: string) => setSplit((x) => new Set([...x].filter((y) => y !== g))), []);
  const lastLine = lines[lines.length - 1];
  // 重试 on an API error card: only when it is the last thing, the agent is not working (someone may have retried already)
  const retry = useCallback(() => void deliver({ text: "继续", images: [] }, uuid()), [deliver]);
  const canRetry = !viewed && lastLine?.kind === "apiError" && state !== "working" && !outgoing && sessionCan(session, "send");
  // Claude Code's "while you were away" recap, open in full while it is the newest thing and not closed: the chat's last
  // line, in the flow with the agent's words (#1360: above the composer it covered the end of the reply)
  const recapKey = `${mid}/${sid}`;
  const [recapClosed, setRecapClosed] = useState(() => closedRecap(recapKey));
  useEffect(() => setRecapClosed(closedRecap(recapKey)), [recapKey]);
  const newestRecap = !assistant && !viewed && lastLine?.kind === "recaps" ? lastLine.items[lastLine.items.length - 1] : undefined;
  const pendingRecap = newestRecap && recapClosed !== newestRecap.id ? newestRecap : undefined;
  const open = useMemo(() => openRecap(lines, pendingRecap), [lines, pendingRecap]);
  const dismissRecap = () => {
    if (!pendingRecap) return;
    closeRecap(recapKey, pendingRecap.id);
    setRecapClosed(pendingRecap.id);
  };

  return (
    <div className="flex h-full min-w-0 flex-col">
      <Header
        session={session}
        title={title}
        machineName={machineName}
        assistant={assistant}
        main={isMain}
        siblings={siblings}
        onToggleSidebar={onToggleSidebar}
        onInterrupt={interrupt}
        onStopLoop={() => void deliver({ text: STOP_LOOP_MESSAGE, images: [] }, uuid())}
        last={assistant ? tr.items[tr.items.length - 1] : undefined}
      />
      {agents.length > 0 && <AgentStrip agents={agents} viewing={viewed?.id} onSelect={setViewing} />}

      <div className="relative min-h-0 flex-1">
        <div ref={scroller} onScroll={onScroll} data-chat-scroll className="h-full overflow-y-auto [overflow-anchor:none]">
          <ChatActionsContext.Provider value={actions}>
          <div className="mx-auto flex w-full max-w-[760px] flex-col gap-7 px-5 pt-6 pb-10 sm:px-8">
            {session?.start_error && <div role="alert" className="rounded-control bg-inset px-3 py-2 text-[13px] text-red">启动失败：{session.start_error}</div>}
            {starting && !tr.loading && !outgoing && <Loading label="正在启动 Agent" />}
            {tr.loadingEarlier && (
              <div className="flex justify-center py-2">
                <Loading label="加载更早的消息" />
              </div>
            )}
            {!tr.hasMore && !tr.loading && tr.items.length > 0 && <div className="text-center text-[11.5px] text-ink-3">{session?.cwd && !assistant ? `在 ${shortPath(session.cwd)} 开始` : "对话开始"}</div>}
            {tr.loading && !tr.items.length && (
              <div className="flex justify-center py-16">
                <Loading label={starting ? "正在准备" : "正在读取对话"} />
              </div>
            )}
            {tr.error && !tr.items.length && (
              <div className="flex flex-col items-center gap-3 py-16 text-center">
                <p className="text-[13px] text-ink-2">{tr.error}</p>
                <Button onClick={tr.reload} icon="retry">
                  重试
                </Button>
              </div>
            )}
            <ChatContext.Provider value={ctx}>
              {assistant
                ? displayLines(lines, feed).map((d) =>
                    d.kind === "feed" ? (
                      <FeedLineView key={d.id} item={d.item} />
                    ) : d.kind === "quick" ? (
                      <QuickResultCard key={d.id} result={d.result} />
                    ) : d.kind === "day" ? (
                      <DayLine key={d.id} date={d.date} />
                    ) : (
                      <ChatLineView
                        key={d.id}
                        line={d.line}
                        now={(d.line.kind === "segment" || d.line.kind === "wakes") && d.line.live ? now : 0}
                        mid={mid}
                        sid={shownSid}
                        streaming={d.line.kind === "segment" && d.line.live ? tr.streaming || undefined : undefined}
                        expandFold={tr.expandFold}
                        onSplit={onSplit}
                        onUnsplit={onUnsplit}
                        onRetry={canRetry && d.line.id === lines.at(-1)?.id ? retry : undefined}
                      />
                    ),
                  )
                : lines.map((line, i) => open?.line === line.id ? (
                <OpenRecapLine key={line.id} open={open} onClose={dismissRecap} />
              ) : (
                <ChatLineView
                  key={line.id}
                  line={line}
                  // only a live line ticks or streams: the others keep their memo
                  now={(line.kind === "segment" || line.kind === "wakes") && line.live ? now : 0}
                  mid={mid}
                  sid={shownSid}
                  streaming={line.kind === "segment" && line.live ? tr.streaming || undefined : undefined}
                  expandFold={tr.expandFold}
                  onSplit={onSplit}
                  onUnsplit={onUnsplit}
                  onRetry={canRetry && i === lines.length - 1 ? retry : undefined}
                />
              ))}
            </ChatContext.Provider>
            {!viewed && shownPending.map((p) => (
              <div key={p.id} className="flex flex-col gap-3">
                <UserBubble msg={p.msg} mid={mid} sid={sid} pending />
              </div>
            ))}
            {!viewed && <OutboxBubbles mid={mid} sid={sid} entries={outbox} />}
            {!viewed && (shownPending.length > 0 || outbox.some((e) => e.state === "sending") || (starting && outgoing > 0)) && (
              <div className="flex items-center gap-2.5">
                <Loading label={starting ? "正在准备" : "正在送达"} />
              </div>
            )}
          </div>
          </ChatActionsContext.Provider>
        </div>
        {showDown && (
          <button
            type="button"
            aria-label="回到最新"
            onClick={toEnd}
            className="absolute bottom-3 left-1/2 flex size-8 -translate-x-1/2 items-center justify-center rounded-full bg-surface text-ink-2 shadow-raised transition-transform hover:text-ink active:scale-95"
            style={{ animation: "pop-in 200ms ease-out both" }}
          >
            <Icon name="arrowDown" size={15} stroke={2.2} />
          </button>
        )}
      </div>

      {viewed && (
        <div className="mx-auto flex w-full max-w-[760px] flex-col gap-2 px-4 pb-4 sm:px-7">
          <SubagentFooter agent={viewed} onBack={() => setViewing(null)} />
        </div>
      )}
      <div className={cx("mx-auto flex w-full max-w-[760px] flex-col gap-2 px-4 pb-4 sm:px-7", viewed && "hidden")} onInput={pendingRecap ? dismissRecap : undefined}>
        {assistant && (
          <>
            <TaskQuestions conversation={sid} />
            <TaskDock conversation={sid} />
            <RouteChip conversation={sid} />
          </>
        )}
        {interactions.map((ix) => (
          <InteractionCard key={ix.id} ix={ix} />
        ))}
        {session && queued.length > 0 && <QueueStrip session={session} draftKey={draftKey} canEdit={canCompose && !assistant} />}
        <Composer
          draftKey={draftKey}
          placeholder={!sessionCan(session, "send") && session?.source_kind ? "此会话仅可查看" : assistant ? `问 ${persona.name}…` : state === "offline" ? "发消息会在电脑上恢复这个会话…" : "给 Agent 发消息…"}
          machineId={mid}
          working={state === "working"}
          onSend={onSend}
          disabled={!sessionCan(session, "send")}
          onStop={assistant || !sessionCan(session, "interrupt") ? undefined : interrupt}
          commands={assistant ? [] : undefined}
          suggestion={session?.suggestion}
          mention={mention}
          autoFocus
          footer={session ? <ComposerFooter session={session} assistant={assistant} /> : null}
        />
      </div>

      <Dialog
        open={Boolean(draftRefusal)}
        onClose={() => setDraftRefusal(null)}
        title="电脑上的输入框里有没发出去的字"
        footer={
          <>
            <Button tone="quiet" onClick={() => setDraftRefusal(null)}>
              先不发
            </Button>
            {draftRefusal?.clearable !== undefined && (
              <Button
                tone="primary"
                onClick={() => {
                  const r = draftRefusal!;
                  setDraftRefusal(null);
                  void deliver(r.out, r.id, r.clearable);
                }}
              >
                清空并发送
              </Button>
            )}
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">为了不把你的消息和它拼在一起，什么都没打。{draftRefusal?.clearable === undefined && "这段字有好几行，要在电脑上处理。"}</p>
        {draftRefusal?.draft && <pre className="mt-2.5 max-h-40 overflow-auto rounded-[8px] bg-inset px-2.5 py-2 font-mono text-[12px] whitespace-pre-wrap text-ink shadow-hairline">{draftRefusal.draft}</pre>}
      </Dialog>
    </div>
  );
}

function ComposerFooter({ session, assistant }: { session: Session; assistant?: boolean }) {
  // a Persona conversation's launcher and model are its chip (they switch there)
  const parts = [sourceSubtitle(session), ...(assistant ? [] : [session.launcher, session.model?.split("/").pop(), shortPath(session.cwd)])].filter(Boolean);
  const modes = !assistant && hasModes(session) && stateOf(session) !== "offline";
  return (
    <span className="flex min-w-0 items-center gap-2">
      {modes && <ModeChips session={session} />}
      {assistant && <AssistantLauncherChip session={session} />}
      <span className="min-w-0 truncate" title={[sourceSubtitle(session), session.cwd].filter(Boolean).join(" · ")}>{parts.join(" · ")}</span>
    </span>
  );
}

function Header({
  session,
  title,
  machineName,
  assistant,
  main,
  siblings,
  onToggleSidebar,
  onInterrupt,
  onStopLoop,
  last,
}: {
  session?: Session;
  title: string;
  machineName: string;
  assistant?: boolean;
  /** The assistant's main conversation: no subtitle (iOS's AssistantTopBar has none) and no 重命名. */
  main?: boolean;
  siblings: Session[];
  onToggleSidebar?: () => void;
  onInterrupt: () => void;
  onStopLoop: () => void;
  last?: Item;
}) {
  const menu = useMenu();
  const tabs = useMenu();
  const panel = usePanel();
  const mutes = useStore((s) => s.mutes);
  const [closing, setClosing] = useState(false);
  const [dialog, setDialog] = useState<"info" | "rename" | "space" | "delete" | null>(null);
  const muted = Boolean(session && !assistant && isMuted(session, mutes));
  const pinned = useStore((s) => Boolean(session && isPinned(session, s.pins)));
  // 屏蔽通知: the hub stops pushing this agent's 需要你 / 做完了 (an agent's switch sits on top of its space's)
  const flipMute = async () => {
    if (!session) return;
    const before = getState().mutes;
    const next = toggleMute(session, before);
    setMutes(next);
    try {
      await api("/mutes", { method: "PUT", json: next });
      toast(isMuted(session, next) ? "已屏蔽通知" : "已恢复通知", "ok");
    } catch (e) {
      setMutes(before);
      toast(`没改成：${errorText(e)}`, "error");
    }
  };
  const [linkTodoOpen, setLinkTodoOpen] = useState(false);
  const state = session ? stateOf(session) : "unknown";
  const sub = main
    ? ""
    : assistant
    ? [sourceSubtitle(session), session?.launcher, session?.updated_at ? ago(session.updated_at) : ""].filter(Boolean).join(" · ")
    : [sourceSubtitle(session), machineName, session?.space_name, session && tabLabel(session)].filter(Boolean).join(" › ");
  const allowed = session ? sessionMenuActions(session) : [];
  const attach = async () => {
    if (!session || !sessionCan(getSession(session.machine_id, session.session_id), "attach")) return;
    try {
      await attachSession(session.machine_id, session.session_id);
      await refresh();
      toast("已实时接入", "ok");
    } catch (e) { toast(`接入失败：${errorText(e)}`, "error"); }
  };
  const others = siblings.filter((s) => s.session_id !== session?.session_id);
  // the session's terminal, files and Git open in the side panel (docs/web.md「侧边面板」); the assistant has none
  const here = (p: typeof panel, kind: string) => Boolean(p && p.kind === kind && "sid" in p && session && p.mid === session.machine_id && p.sid === session.session_id);
  const showTerminal = Boolean(session && !assistant && sessionCan(session, "terminal") && stateOf(session) !== "offline");
  const showFiles = Boolean(session && !assistant && session.cwd);
  const openTerminal = () => session && togglePanel({ kind: "terminal", mid: session.machine_id, sid: session.session_id });
  const openFiles = () => session && togglePanel({ kind: "files", mid: session.machine_id, sid: session.session_id, cwd: session.cwd });
  const openGit = () => session && togglePanel({ kind: "git", mid: session.machine_id, sid: session.session_id, cwd: session.cwd });
  const othersNeed = others.some((s) => stateOf(s) === "needs_you");
  const assistantItems = useAssistantMenu(assistant ? session : undefined);

  const items: MenuItem[] = session
    ? [
        ...assistantItems,
        ...(session.todo
          ? [
              {
                key: "todo-detail",
                label: `待办：${session.todo.title}`,
                icon: "todo" as const,
                onSelect: () => navigate(`/todos?id=${encodeURIComponent(session.todo!.id)}`),
              },
              {
                key: "unlink-todo",
                label: "解除关联",
                icon: "x" as const,
                onSelect: async () => {
                  try {
                    await unlinkAgentFromTodo(session.todo!.id, session.machine_id, session.session_id);
                    await refreshTodos();
                    await refresh();
                    toast("已解除待办关联", "ok");
                  } catch (e) {
                    toast(`解除关联失败：${errorText(e)}`, "error");
                  }
                },
              },
              {
                key: "relink-todo",
                label: "关联到待办…",
                icon: "todo" as const,
                onSelect: () => setLinkTodoOpen(true),
              },
            ]
          : !assistant
            ? [
                {
                  key: "link-todo",
                  label: "关联到待办…",
                  icon: "todo" as const,
                  onSelect: () => setLinkTodoOpen(true),
                },
              ]
            : []),
        ...(allowed.includes("attach") ? [{ key: "attach", label: "实时接入", icon: "link" as const, onSelect: () => void attach() }] : []),
        ...(showTerminal ? [{ key: "terminal", label: "终端", icon: "terminal" as const, checked: here(panel, "terminal"), onSelect: openTerminal }] : []),
        ...(showFiles ? [{ key: "files", label: "文件", icon: "folder" as const, checked: here(panel, "files"), onSelect: openFiles }, { key: "git", label: "Git", icon: "git" as const, checked: here(panel, "git"), onSelect: openGit }] : []),
        ...(allowed.includes("stop") ? [{ key: "stop", label: "中断", icon: "stop" as const, hint: "Esc", onSelect: onInterrupt }] : []),
        { key: "info", label: "会话信息", icon: "info", onSelect: () => setDialog("info") },
        ...(!main && sessionCan(session, "send") && (session.assistant || !session.source_kind) ? [{ key: "rename", label: "重命名…", icon: "pencil" as const, onSelect: () => setDialog("rename") }] : []),
        ...(!assistant && session.space_id && !session.source_kind ? [{ key: "space", label: "重命名空间…", icon: "folder" as const, onSelect: () => setDialog("space") }] : []),
        ...(!assistant ? [{ key: "mute", label: muted ? "恢复通知" : "屏蔽通知", icon: muted ? ("bell" as const) : ("bellOff" as const), onSelect: () => void flipMute() }] : []),
        ...(!assistant ? [{ key: "pin", label: pinned ? "取消置顶" : "置顶", icon: "pin" as const, onSelect: () => void flipPin(session) }] : []),
        { key: "sid", label: "拷贝会话 ID", icon: "copy", onSelect: () => copyText(session.session_id) },
        ...(session.cwd ? [{ key: "cwd", label: "拷贝目录", icon: "folder" as const, onSelect: () => copyText(session.cwd) }] : []),
        ...(allowed.includes("resume") ? [{ key: "resume", label: "拷贝恢复命令", icon: "terminal" as const, onSelect: () => copyText(resumeCommand(session)) }] : []),
        ...(!assistant && allowed.includes("close") ? [{ key: "close", label: "关闭 Agent…", icon: "x" as const, danger: true, onSelect: () => setClosing(true) }] : []),
        ...(sessionDeletable(session) ? [{ key: "delete", label: "删除对话…", icon: "trash" as const, danger: true, onSelect: () => setDialog("delete") }] : []),
      ]
    : [];

  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line bg-page/85 px-3 backdrop-blur-md sm:px-4">
      {onToggleSidebar && <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />}
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          {session && !assistant && <StatusDot state={state} />}
          {assistant && <AssistantFace session={session} last={last} />}
          <h1 className="truncate text-[14px] font-semibold text-ink">{title}</h1>
          {session && !assistant && state !== "idle" && <StatePill state={state} />}
        </div>
        {sub && <div title={sub} className="truncate text-[11.5px] text-ink-3">{sub}</div>}
      </div>
      {session && !assistant && <ActivityChips session={session} onStopLoop={onStopLoop} />}
      {others.length > 0 && (
        <button type="button" onClick={tabs.open} className="relative flex h-7 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 shadow-btn transition-colors hover:bg-hover hover:text-ink">
          <Icon name="layers" size={13} />
          同空间 {others.length}
          {othersNeed && <span className="absolute -top-0.5 -right-0.5 size-2 rounded-full bg-red shadow-[0_0_0_2px_var(--page)]" />}
        </button>
      )}
      {tabs.anchor && (
        <Menu
          anchor={tabs.anchor}
          onClose={tabs.close}
          align="right"
          width={360}
          header={session?.space_name ? `空间 ${session.space_name}` : undefined}
          items={siblings
            .slice()
            .sort((a, b) => (a.slot_index ?? 0) - (b.slot_index ?? 0))
            .map((s) => ({
              key: s.session_id,
              label: `${tabLabel(s) || sessionTitle(s)} · ${sessionTitle(s)}`,
              state: stateOf(s),
              checked: s.session_id === session?.session_id,
              onSelect: () => navigate({ name: "session", mid: s.machine_id, sid: s.session_id }),
            }))}
        />
      )}
      {showFiles && <IconButton icon="folder" label="文件" onClick={openFiles} active={here(panel, "files") || here(panel, "git")} className="hidden sm:flex" />}
      {showTerminal && <IconButton icon="terminal" label="终端" onClick={openTerminal} active={here(panel, "terminal")} className="hidden sm:flex" />}
      {items.length > 0 && <IconButton icon="more" label="更多" onClick={menu.open} active={Boolean(menu.anchor)} />}
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} align="right" items={items} />}
      {session && <CloseAgent open={closing} onClose={() => setClosing(false)} session={session} machineName={machineName} />}
      {session && <SessionInfoDialog open={dialog === "info"} onClose={() => setDialog(null)} session={session} machineName={machineName} />}
      {session && <RenameDialog open={dialog === "rename"} onClose={() => setDialog(null)} session={session} />}
      {session && <RenameSpaceDialog open={dialog === "space"} onClose={() => setDialog(null)} session={session} />}
      {session && <DeleteSessionDialog open={dialog === "delete"} onClose={() => setDialog(null)} session={session} machineName={machineName} />}
      <LinkTodoDialog open={linkTodoOpen} session={session} onClose={() => setLinkTodoOpen(false)} />
    </header>
  );
}

function CloseAgent({ open, onClose, session, machineName }: { open: boolean; onClose: () => void; session: Session; machineName: string }) {
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (!sessionCan(getSession(session.machine_id, session.session_id), "terminal")) return;
    setBusy(true);
    try {
      await api(`${sessionPath(session.machine_id, session.session_id)}/close`, { method: "POST" });
      toast("已关闭", "ok");
      onClose();
      navigate({ name: "home" });
    } catch (e) {
      toast(`关闭失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="关闭这个 Agent？"
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="danger" busy={busy} onClick={go}>
            关闭 Agent
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-ink-2">
        会退出 {machineName} 上「{[session.space_name, tabLabel(session)].filter(Boolean).join(" › ") || sessionTitle(session)}」里的 Claude 并关掉这个标签页。对话记录还在，之后可以从「最近」里恢复。
      </p>
    </Dialog>
  );
}
