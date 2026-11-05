// One conversation: an agent in herdr on one of your computers, or a Persona assistant conversation.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, attachSession, errorText, sessionPath } from "../../lib/api";
import { ago, attentionReason, externalLink, resumeCommand, sessionCan, sessionDeletable, sessionMenuActions, sourceSubtitle, sessionTitle, shortPath, stateOf, tabLabel, uuid } from "../../lib/format";
import { navigate, type SessionView } from "../../lib/router";
import { canShowTerminal, otherView, useSessionView } from "../../lib/session-view";
import { openLink } from "../../lib/links";
import { togglePanel, usePanel } from "../../lib/panel";
import { machineWithHub } from "../../lib/mesh";
import { hasEffortOnly, hasModes, isMuted, toggleMute } from "../../lib/session-actions";
import { flipPin } from "../../lib/agent-actions";
import { isPinned } from "../../lib/pins";
import { acknowledgeLaunch, launchMessageId } from "../../lib/new-agent";
import { getSession, getState, listenSession, markSeenLocally, refresh, refreshTodos, setMutes, useStore } from "../../lib/store";
import { buildTaskIndex, STOP_LOOP_MESSAGE, subagentParent } from "../../lib/tasks";
import { planProgress } from "../../lib/work-groups";
import { echoed, taggedSeen } from "../../lib/echo";
import { outboxKey, settleOutbox } from "../../lib/outbox";
import { apiOp } from "../../lib/ops";
import { conversationKey, pickedLauncher, pickedModel } from "../../lib/assistant-chat";
import { unlinkAgentFromTodo } from "../../lib/todos";
import { LinkTodoDialog } from "../LinkTodoDialog";
import { chatLines, freshWords, openRecap, parseUserText, segments, type UserMessage } from "../../lib/turns";
import type { Item, Session } from "../../lib/types";
import { anchoredScroll, followsAgain, isScrollbarPress, jumpLabel, leavesOnKey, leavesOnScrollbarDrag, leavesOnTouch, leavesOnWheel, followsAfterUnfold, UNFOLD_PAUSE, type ScrollAnchor } from "../../lib/disclosure";
import { Button, copyText, cx, Dialog, IconButton, Loading, Menu, StatePill, StatusDot, toast, useMenu, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { AGENT_COMMANDS, Composer, type Outgoing } from "./Composer";
import { hasLoop, LOOP_TEXT } from "../../lib/loop-command";
import { PendingCards } from "./Interactions";
import { DOCK_MAX, turnStart } from "./dock";
import { HEADER_SLOT, HEADER_SLOT_END, HEADER_SLOT_START } from "./header";
import { NARROW, SHORT, useFinePointer, useMedia } from "../../lib/viewport";
import { UserBubble } from "./SegmentView";
import { ActivityChips } from "./Activity";
import { paneRoute, tabSessions } from "../../lib/herdr-tree";
import { useSessionLive } from "../../lib/live";
import { PaneTabs } from "./PaneTabs";
import { RunningStatusBar } from "./RunningStatusBar";
import { latestPlan, PlanPill, usePlanOutOfView } from "./PlanCard";
import { ChatContext, type ChatContextValue } from "./chat-context";
import { ChatLineView, closedRecap, closeRecap, OpenRecapLine } from "./Lines";
import { useTranscript } from "./useTranscript";
import { DeleteSessionDialog, ModeChips, RenameDialog, RenameSpaceDialog, SessionInfoDialog } from "./SessionDialogs";
import { ChatActionsContext, type ChatActions } from "./chat-actions";
import { QueueStrip } from "./QueueStrip";
import { chatFiles } from "../../lib/mentions";
import { sameMessage } from "../../lib/queue";
import { OutboxBubbles } from "./OutboxBubbles";
import { AssistantFace, AssistantLauncherChip, CompactionStrip, useAssistantMenu } from "../assistant/AssistantChat";
import { Segmented } from "../assistant/parts";
import { TerminalPanel } from "../session/TerminalPanel";
import { ViewLayer } from "../session/ViewLayer";
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

/** 分支到新聊天 (#2137): the node copies the conversation up to this reply's turn into a new one, which opens here. */
async function forkAt(mid: string, sid: string, itemId: string, assistant: boolean) {
  try {
    const r = await api<{ session_id: string }>(`${sessionPath(mid, sid)}/fork`, { method: "POST", json: { at_item_id: itemId } });
    await refresh();
    navigate(assistant ? { name: "jarvis", sid: r.session_id } : { name: "session", mid, sid: r.session_id });
    toast("已分支到新聊天", "ok");
  } catch (e) {
    toast(`分支失败：${errorText(e)}`, "error");
  }
}

/** `readOnly`: a subagent's conversation (`<parent>@<agent>`) in the side panel (SubagentPanel): the transcript only,
 *  no header, no composer. */
export function ChatView({ mid, sid, assistant, starting: launching, onToggleSidebar, readOnly, topCenter, inPage }: {
  mid: string;
  sid: string;
  assistant?: boolean;
  starting?: boolean;
  onToggleSidebar?: () => void;
  readOnly?: boolean;
  /** On a narrow screen, in the header's middle in place of the title (the shell's 助手 | Agents switch). */
  topCenter?: React.ReactNode;
  /** The agent's own page (`/s/…`, `/t/…`): its terminal is a view of the middle, switched in place with the header's
   *  终端 | 对话 and ⌘J (docs/web.md「终端 ⇄ 对话」). Elsewhere (a task's panel) the chat is all there is. */
  inPage?: boolean;
}) {
  const session = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === sid) ?? s.assistant?.conversations.find((c) => c.session_id === sid));
  // a subagent's own launches are listed on the session that launched it
  const parentSid = subagentParent(sid);
  const subagents = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === parentSid)?.subagents);
  const machineName = useStore((s) => machineWithHub(mid, s.machines));
  const allSessions = useStore((s) => s.sessions[mid]);
  const panes = useMemo(() => (session && !assistant && !readOnly ? tabSessions(allSessions ?? [], session) : []), [allSessions, session, assistant, readOnly]);
  const siblings = useStore((s) => (session?.space_id ? (s.sessions[mid] ?? []).filter((x) => x.space_id === session.space_id && stateOf(x) !== "offline") : []));
  const interactions = useStore((s) => s.interactions.filter((i) => i.machine_id === mid && i.session_id === sid));
  const answered = useStore((s) => Object.values(s.answered).filter((a) => a.ix.machine_id === mid && a.ix.session_id === sid));
  const viewed = readOnly ? (subagents?.find((a) => a.session_id === sid) ?? null) : null;
  const tr = useTranscript(mid, sid, { starting: Boolean(launching) && !readOnly });
  const status = useSessionLive(mid, sid);
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

  // ── the middle of an agent's page: its conversation, or its terminal in place (lib/session-view.ts) ──
  const switchable = Boolean(inPage && !assistant && !readOnly && canShowTerminal(session));
  const openFiles = () => session && togglePanel({ kind: "files", mid, sid, cwd: session.cwd });
  const [view, setView] = useSessionView(mid, sid, switchable);
  // The terminal connects when first shown and stays connected while the page is on screen (the chat is hidden under it,
  // not gone: coming back is instant). In the background, or when the agent stops being reachable, it lets the pane go.
  const [termKept, setTermKept] = useState(false);
  if (switchable && view === "term" && !termKept) setTermKept(true);
  if (!switchable && termKept) setTermKept(false);
  const viewNow = useRef(view);
  viewNow.current = view;
  useEffect(() => {
    const hide = () => document.visibilityState === "hidden" && viewNow.current !== "term" && setTermKept(false);
    document.addEventListener("visibilitychange", hide);
    return () => document.removeEventListener("visibilitychange", hide);
  }, []);

  const segs = useMemo(() => segments(tr.items), [tr.items]);
  // finished exchanges of several messages the user split open (iOS openWork on a merged exchange)
  const [split, setSplit] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => setSplit((x) => (x.size ? new Set() : x)), [mid, sid]);

  // what the messages' menus, the image viewer and the @ menu read from this chat (chat-actions.ts)
  const items = useRef(tr.items);
  items.current = tr.items;
  const draftKey = `${mid}/${sid}`;
  const canCompose = sessionCan(session, "send");
  const canFork = Boolean(session?.can_fork) && !readOnly;
  const actions = useMemo<ChatActions>(
    () => ({ draftKey, canCompose, speaker: assistant ? persona.name : "Agent", images: () => items.current.filter((i) => i.kind === "image"), fork: canFork ? (itemId: string) => void forkAt(mid, sid, itemId, Boolean(assistant)) : undefined }),
    [draftKey, canCompose, canFork, mid, sid, assistant, persona.name],
  );
  const mention = useMemo(() => ({ cwd: session?.cwd, self: draftKey, files: () => chatFiles(items.current) }), [session?.cwd, draftKey]);

  // a sent message stays as a "sending" bubble until the transcript has it (lib/echo.ts): an item the hub tied to it
  // (any kind, a notice too), or else a latest user message with its words; one that answered one answers no other
  const claimed = useRef(new Set<string>());
  useEffect(() => {
    const users = [...segs.slice(-4).flatMap((seg) => (seg.user && !seg.user.injected ? [seg.user] : [])), ...taggedSeen(tr.items)];
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

  // what came in over the WebSocket after the chat subscribed: the agent's words among it fade in (contract §3); the
  // first page, older pages and unfolded ranges come over HTTP and do not
  const [fresh, setFresh] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    setFresh(new Set());
    return listenSession(mid, sid, {
      items: (e) => {
        const add = freshWords(items.current, e.items ?? []);
        if (add.length) setFresh((f) => new Set([...f, ...add]));
      },
    });
  }, [mid, sid]);

  // ── scrolling (lib/disclosure.ts「following the end」): the end stays in view until the reader scrolls away from it;
  // older pages landing above keep the view still ──
  const scroller = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);
  const [away, setAway] = useState(false);
  // something came in since the reader left the end: the pill says 有新消息
  const [newSince, setNewSince] = useState(false);
  const follow = useCallback((on: boolean) => {
    atEnd.current = on;
    setAway(!on);
    if (on) setNewSince(false);
  }, []);
  const lastTop = useRef(0);
  const dragging = useRef(false);
  const before = useRef<{ h: number; top: number } | null>(null);
  const first = useRef(true);

  useEffect(() => {
    first.current = true;
    follow(true);
    lastTop.current = 0;
  }, [mid, sid, follow]);

  const lastId = tr.items[tr.items.length - 1]?.id;
  useEffect(() => {
    if (lastId && !atEnd.current) setNewSince(true);
  }, [lastId]);

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

  // A card that unfolds (#1124): following holds off for UNFOLD_PAUSE while the clicked row stays where it was on
  // screen (anchoredScroll), then picks up again if the end is still near; a long card opened at the end stays put to
  // be read, and the pill brings the end back. Held until the sizes stop changing for a moment, or the reader scrolls.
  const anchor = useRef<(ScrollAnchor & { el: Element; quiet: number }) | null>(null);
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    let resume = 0;
    // the reader scrolled (wheel, finger, keys) while an unfold held following off: the pause does not pull them back
    let moved = false;
    const drop = () => {
      if (anchor.current) clearTimeout(anchor.current.quiet);
      anchor.current = null;
    };
    const readerMoves = () => {
      moved = true;
      drop();
    };
    const onClick = (e: MouseEvent) => {
      const el = e.target instanceof Element ? e.target.closest("button[aria-expanded]") : null;
      if (!el || !sc.contains(el)) return;
      drop();
      clearTimeout(resume);
      const was = atEnd.current;
      atEnd.current = false;
      moved = false;
      anchor.current = { el, edge: "top", at: el.getBoundingClientRect().top, follow: false, quiet: window.setTimeout(drop, 1000) };
      resume = window.setTimeout(() => {
        if (!was) return;
        if (!followsAfterUnfold({ wasAtEnd: was, readerMoved: moved, gap: sc.scrollHeight - sc.scrollTop - sc.clientHeight })) return follow(false);
        drop();
        follow(true);
        sc.scrollTop = sc.scrollHeight;
      }, UNFOLD_PAUSE);
    };
    sc.addEventListener("click", onClick, true);
    for (const ev of ["wheel", "touchstart", "keydown", "pointerdown"]) sc.addEventListener(ev, readerMoves, { passive: true });
    return () => {
      drop();
      clearTimeout(resume);
      sc.removeEventListener("click", onClick, true);
      for (const ev of ["wheel", "touchstart", "keydown", "pointerdown"]) sc.removeEventListener(ev, readerMoves);
    };
  }, [follow]);

  // content that grows by itself (images loading, a live group's steps) keeps the end in view; a held row stays put.
  // A ResizeObserver runs after layout and before paint, so the offset moves in the same frame as the height. The view
  // itself is watched too: what sits under it (the composer growing, the task dock, a question card) makes it shorter
  // without the content changing, and at the end the newest line stays in view.
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
          viewTop: sc.getBoundingClientRect().top, edgeAt: r.top, elTop: r.top,
        });
        if (Math.abs(next.scrollTop - sc.scrollTop) >= 0.5) sc.scrollTop = next.scrollTop;
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

  // the reader leaving the end: the wheel or a trackpad going up, a finger pulling the list down, PageUp / ↑ / Home,
  // the scroll bar dragged up (onScroll)
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    const size = () => ({ scrollHeight: sc.scrollHeight, clientHeight: sc.clientHeight });
    const leave = () => follow(false);
    // (checked before sizes are read: no layout read per wheel or touch event while away from the end)
    const wheel = (e: WheelEvent) => atEnd.current && leavesOnWheel({ deltaX: e.deltaX, deltaY: e.deltaY, ...size() }) && leave();
    let startY: number | null = null;
    const touchStart = (e: TouchEvent) => (startY = e.touches[0]?.clientY ?? null);
    const touchMove = (e: TouchEvent) => {
      const y = e.touches[0]?.clientY;
      if (atEnd.current && startY !== null && y !== undefined && leavesOnTouch({ startY, y, ...size() })) leave();
    };
    const key = (e: KeyboardEvent) => {
      const t = e.target instanceof Element ? e.target : null;
      if (t?.closest("input, textarea, [contenteditable=true]")) return;
      if (atEnd.current && leavesOnKey({ key: e.key, ...size() })) leave();
    };
    const down = (e: PointerEvent) => (dragging.current = isScrollbarPress({ targetIsView: e.target === sc }));
    const up = () => (dragging.current = false);
    sc.addEventListener("wheel", wheel, { passive: true });
    sc.addEventListener("touchstart", touchStart, { passive: true });
    sc.addEventListener("touchmove", touchMove, { passive: true });
    sc.addEventListener("pointerdown", down);
    window.addEventListener("pointerup", up);
    window.addEventListener("keydown", key);
    return () => {
      sc.removeEventListener("wheel", wheel);
      sc.removeEventListener("touchstart", touchStart);
      sc.removeEventListener("touchmove", touchMove);
      sc.removeEventListener("pointerdown", down);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("keydown", key);
    };
  }, [follow]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    const delta = el.scrollTop - lastTop.current;
    lastTop.current = el.scrollTop;
    if (atEnd.current) {
      if (leavesOnScrollbarDrag({ dragging: dragging.current, delta })) follow(false);
    } else if (!anchor.current && followsAgain({ gap, delta })) follow(true);
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
    follow(true);
    el.scrollTo({ top: el.scrollHeight, behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
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
      follow(true); // your own message: to the end, whatever you were reading
      try {
        // the bubble stays as sending while a gap (a hub restart) is waited out; the id is the op id too
        await apiOp(`${sessionPath(mid, sid)}/messages`, {
          method: "POST",
          opId: id,
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
    [mid, sid, session, assistant, follow],
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
      // in the terminal view Esc belongs to the program in the terminal
      if (e.key === "Escape" && state === "working" && view !== "term" && !document.querySelector("[role=dialog],[role=menu]")) {
        e.preventDefault();
        void interrupt();
      }
    };
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, session, view]);

  const lastLive = (readOnly ? Boolean(viewed?.running) : working) && !tr.loading;
  // The main conversation is always Persona (#1313): not its first message, the node's title for it.
  const isMain = Boolean(assistant && sid === mainConversation);
  const title = isMain ? MAIN_TITLE : session ? sessionTitle(session) : assistant ? "新对话" : "会话";
  const lines = useMemo(() => chatLines(tr.items, { live: lastLive && (readOnly || !outgoing), split }), [tr.items, lastLive, readOnly, outgoing, split]);
  const ctx = useMemo<ChatContextValue>(
    () => ({ tasks: buildTaskIndex(tr.items), subagents: assistant ? [] : (subagents ?? []), fresh, scope: `${mid}/${sid}:` }),
    [tr.items, subagents, assistant, fresh, mid, sid],
  );
  // the newest plan of the chat (lib/work-groups.ts planSteps): while its card is out of view, a pill above the composer
  const plan = useMemo(() => latestPlan(segs), [segs]);
  const planOut = usePlanOutOfView(scroller, plan?.id);
  const onSplit = useCallback((g: string) => setSplit((x) => new Set([...x, g])), []);
  const onUnsplit = useCallback((g: string) => setSplit((x) => new Set([...x].filter((y) => y !== g))), []);
  const lastLine = lines[lines.length - 1];
  // 重试 on an API error card: only when it is the last thing, the agent is not working (someone may have retried already)
  const retry = useCallback(() => void deliver({ text: "继续", images: [] }, uuid()), [deliver]);
  const canRetry = !readOnly && lastLine?.kind === "apiError" && state !== "working" && !outgoing && sessionCan(session, "send");
  // Claude Code's "while you were away" recap, open in full while it is the newest thing and not closed: the chat's last
  // line, in the flow with the agent's words (#1360: above the composer it covered the end of the reply)
  const recapKey = `${mid}/${sid}`;
  const [recapClosed, setRecapClosed] = useState(() => closedRecap(recapKey));
  useEffect(() => setRecapClosed(closedRecap(recapKey)), [recapKey]);
  const newestRecap = !assistant && !readOnly && lastLine?.kind === "recaps" ? lastLine.items[lastLine.items.length - 1] : undefined;
  const pendingRecap = newestRecap && recapClosed !== newestRecap.id ? newestRecap : undefined;
  const open = useMemo(() => openRecap(lines, pendingRecap), [lines, pendingRecap]);
  const dismissRecap = () => {
    if (!pendingRecap) return;
    closeRecap(recapKey, pendingRecap.id);
    setRecapClosed(pendingRecap.id);
  };
  // On a phone (#1996): what is under the chat stays within about half of the visible height (dock.ts); the keyboard is
  // up while the composer has the focus on a touch screen alone
  const narrow = useMedia(NARROW);
  const short = useMedia(SHORT);
  const [typing, setTyping] = useState(false);
  const fine = useFinePointer();
  const keyboard = typing && !fine;
  const composerBox = useRef<HTMLDivElement>(null);
  // after a switch the keyboard goes to what is now shown (not on a touch screen: that would raise the keyboard)
  const termLayer = useRef<HTMLDivElement>(null);
  const shown = useRef(view);
  useEffect(() => {
    if (shown.current === view) return;
    shown.current = view;
    if (!fine) return;
    const frame = requestAnimationFrame(() => {
      const box = view === "term" ? termLayer.current : composerBox.current;
      box?.querySelector("textarea")?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [view, fine]);
  // the run's time counts from the turn's start, as its 正在工作… does (not from the session's last state change)
  const stateSince = session?.state_since;
  const runStart = useMemo(() => {
    const live = lines.findLast((l) => l.kind === "segment" && l.live);
    return (live?.kind === "segment" ? turnStart(live.seg) : undefined) ?? stateSince;
  }, [lines, stateSince]);
  const planPill = plan && planOut && (lastLive || !planProgress(plan.todos).allDone) ? (
    <PlanPill todos={plan.todos} onClick={() => scroller.current?.querySelector(`[data-plan-id="${CSS.escape(plan.id)}"]`)?.scrollIntoView({ block: "center", behavior: "smooth" })} />
  ) : null;
  const running = session && !assistant ? <RunningStatusBar mid={mid} sid={sid} working={state === "working"} startedAt={runStart} /> : null;
  const queue = session && queued.length > 0 ? <QueueStrip session={session} draftKey={draftKey} canEdit={canCompose && !assistant} compact={short} /> : null;
  // with the plan's pill showing, 跳到底部 goes beside it (over the chat, they hid each other)
  const jumpInDock = Boolean(planPill) && !readOnly;
  const pills = jumpInDock && away ? (
    <div className="flex shrink-0 items-center justify-center gap-2">
      {planPill}
      <JumpPill label={jumpLabel(newSince)} onClick={toEnd} inline />
    </div>
  ) : planPill;

  return (
    <div className="flex h-full min-w-0 flex-col">
      {!readOnly && (
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
          topCenter={topCenter}
          view={switchable ? view : undefined}
          onView={switchable ? setView : undefined}
          viewDot={view === "term" && (interactions.length > 0 || state === "needs_you")}
        />
      )}
      {session && panes.length > 1 && <PaneTabs sessions={panes} current={sid} onSelect={(s) => navigate(paneRoute(s), true)} />}

      {/* the page's middle: the chat and (once shown) the terminal stacked in one box; without a terminal to show it adds no box */}
      <div className={switchable ? "relative min-h-0 flex-1" : "contents"}>
      <ViewLayer bare={!switchable} on={view === "chat"} from="start">
      <div className="relative min-h-0 flex-1">
        <div ref={scroller} onScroll={onScroll} data-chat-scroll className="h-full overflow-y-auto [overflow-anchor:none]">
          <ChatActionsContext.Provider value={actions}>
          <div className="mx-auto flex w-full max-w-[880px] flex-col gap-7 px-5 pt-6 pb-10 sm:px-8">
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
                        mid={mid}
                        sid={sid}
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
                  // only a live line streams and shows the status line: the others keep their memo
                  status={line.kind === "segment" && line.live ? status : undefined}
                  mid={mid}
                  sid={sid}
                  streaming={line.kind === "segment" && line.live ? tr.streaming || undefined : undefined}
                  expandFold={tr.expandFold}
                  onSplit={onSplit}
                  onUnsplit={onUnsplit}
                  onRetry={canRetry && i === lines.length - 1 ? retry : undefined}
                />
              ))}
            </ChatContext.Provider>
            {!readOnly && shownPending.map((p) => (
              <div key={p.id} className="flex flex-col gap-3">
                <UserBubble msg={p.msg} mid={mid} sid={sid} pending />
              </div>
            ))}
            {!readOnly && <OutboxBubbles mid={mid} sid={sid} entries={outbox} />}
            {!readOnly && (shownPending.length > 0 || outbox.some((e) => e.state === "sending") || (starting && outgoing > 0)) && (
              <div className="flex items-center gap-2.5">
                <Loading label={starting ? "正在准备" : "正在送达"} />
              </div>
            )}
          </div>
          </ChatActionsContext.Provider>
        </div>
        {away && !jumpInDock && <JumpPill label={jumpLabel(newSince)} onClick={toEnd} />}
      </div>

      {!readOnly && (
      <div
        data-chat-dock
        // narrow: at most DOCK_MAX; the cards' box gives up its height first, and should the composer alone need more,
        // it grows up over the chat (justify-end) instead of down off the screen
        className="mx-auto flex w-full max-w-[880px] flex-col gap-2 px-4 pb-4 sm:px-7 max-lg:max-h-(--dock-max) max-lg:justify-end [@media(max-height:500px)]:pb-2"
        style={{ "--dock-max": DOCK_MAX } as React.CSSProperties}
        onInput={pendingRecap ? dismissRecap : undefined}
      >
        {short ? (
          // a phone on its side: the plan's pill, the run's status and the queue share one line
          (pills || running || queue) && (
            <div className="flex shrink-0 items-center gap-2">
              {pills && <div className="shrink-0">{pills}</div>}
              {running && <div className="shrink-0">{running}</div>}
              {queue && <div className="min-w-0 flex-1">{queue}</div>}
            </div>
          )
        ) : (
          <>
            {pills}
            {running}
          </>
        )}
        {assistant && (
          <>
            <TaskQuestions conversation={sid} />
            <TaskDock conversation={sid} />
            <RouteChip conversation={sid} />
          </>
        )}
        <PendingCards
          interactions={interactions}
          answered={answered}
          narrow={narrow}
          short={short}
          keyboard={keyboard}
          onDismissKeyboard={() => composerBox.current?.querySelector("textarea")?.blur()}
        />
        {assistant && session && <CompactionStrip session={session} />}
        {session && !assistant && <AttentionNote session={session} />}
        {!short && queue}
        <div ref={composerBox} className="shrink-0">
        <Composer
          draftKey={draftKey}
          placeholder={!sessionCan(session, "send") && session?.source_kind ? "此会话仅可查看" : assistant ? `问 ${persona.name}…` : state === "offline" ? "发消息会在电脑上恢复这个会话…" : "给 Agent 发消息…"}
          machineId={mid}
          working={state === "working"}
          onSend={onSend}
          disabled={!sessionCan(session, "send")}
          // Persona's conversations too, the main one included (#1467): the node stops their turn like an agent's
          onStop={sessionCan(session, "interrupt") ? interrupt : undefined}
          commands={assistant ? [] : hasLoop(session) ? undefined : AGENT_COMMANDS.filter((c) => c.text !== LOOP_TEXT)}
          suggestion={session?.suggestion}
          mention={mention}
          autoFocus
          onFocusChange={setTyping}
          footer={session ? <ComposerFooter session={session} assistant={assistant} /> : null}
        />
        </div>
      </div>
      )}
      </ViewLayer>
      {switchable && termKept && (
        <ViewLayer on={view === "term"} from="end" enter layerRef={termLayer}>
          {/* Files opens beside it (the Files panel, as on the 终端 page); × goes back to the conversation */}
          <TerminalPanel mid={mid} sid={sid} onFiles={session?.cwd ? openFiles : undefined} onClose={() => setView("chat")} />
        </ViewLayer>
      )}
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
                  void deliver(r.out, uuid(), r.clearable); // another operation: the refused one keeps its answer
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

/** Away from the end: 有新消息 / 跳到底部 over the chat's bottom edge (cindy's JumpToBottomChip). */
function JumpPill({ label, onClick, inline }: { label: string; onClick: () => void; /** In a row (beside the plan's pill), not over the chat. */ inline?: boolean }) {
  return (
    <div aria-live="polite" className={inline ? "shrink-0" : "absolute bottom-3 left-1/2 z-10 -translate-x-1/2"} style={{ animation: "fade-up 150ms ease-out both" }}>
      <button
        type="button"
        onClick={onClick}
        aria-label={label}
        data-jump-pill
        className="flex h-8 items-center gap-1.5 rounded-full border border-line bg-surface px-3 py-1.5 text-[12px] leading-none font-medium whitespace-nowrap text-ink-2 shadow-raised transition-colors duration-150 hover:bg-hover active:scale-[0.98]"
      >
        <Icon name="arrowDown" size={14} className="shrink-0" />
        <span className="translate-y-[0.5px]">{label}</span>
      </button>
    </div>
  );
}

function ComposerFooter({ session, assistant }: { session: Session; assistant?: boolean }) {
  // a Persona conversation's launcher and model are its chip (they switch there)
  const parts = [sourceSubtitle(session), ...(assistant ? [] : [session.launcher, session.model?.split("/").pop(), shortPath(session.cwd)])].filter(Boolean);
  // a Persona conversation (the main one too) switches its modes whatever its state: the hub applies them (#2416)
  const modes = assistant || (hasModes(session) && stateOf(session) !== "offline");
  const effortOnly = !assistant && hasEffortOnly(session) && stateOf(session) !== "offline"; // a Codex thread on its app-server
  return (
    <span className="flex min-w-0 items-center gap-2">
      {(modes || effortOnly) && <ModeChips session={session} effortOnly={effortOnly} assistant={assistant} />}
      {assistant && <AssistantLauncherChip session={session} />}
      <span className="min-w-0 truncate" title={[sourceSubtitle(session), session.cwd].filter(Boolean).join(" · ")}>{parts.join(" · ")}</span>
    </span>
  );
}

/** Above the composer while a session needs you and its host says why (a botmux attention, iOS AttentionNote):
 *  the reason and 在飞书打开 its topic. */
function AttentionNote({ session }: { session: Session }) {
  const reason = attentionReason(session);
  if (!reason) return null;
  const link = externalLink(session);
  return (
    <div role="status" data-testid="chat-attention" className="flex items-start gap-2 rounded-[12px] bg-red-tint px-3 py-2 text-[12.5px]" style={{ animation: "fade-up 200ms ease-out both" }}>
      <span className="flex h-[18px] shrink-0 items-center"><StatusDot state="needs_you" size={7} /></span>
      <span className="line-clamp-3 min-w-0 flex-1 text-ink-2">{reason}</span>
      {link && (
        <a href={link} target="_blank" rel="noopener noreferrer" className="flex shrink-0 items-center gap-1 whitespace-nowrap font-medium text-accent hover:underline">
          <Icon name="external" size={12} />
          在飞书打开
        </a>
      )}
    </div>
  );
}

export function Header({
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
  topCenter,
  view,
  onView,
  viewDot,
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
  topCenter?: React.ReactNode;
  /** On an agent's page with a terminal to show: the view in the middle, and how to change it (the 终端 | 对话 switch). */
  view?: SessionView;
  onView?: (view: SessionView) => void;
  /** The conversation has something that wants an answer while the terminal is shown. */
  viewDot?: boolean;
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
  // the session's files and Git open in the side panel (docs/web.md「侧边面板」); the assistant has none. Its terminal is
  // the page's other view, switched in place (view / onView)
  const here = (p: typeof panel, kind: string) => Boolean(p && p.kind === kind && "sid" in p && session && p.mid === session.machine_id && p.sid === session.session_id);
  const showTerminal = Boolean(view && onView);
  const showFiles = Boolean(session && !assistant && session.cwd);
  const flipView = () => view && onView?.(otherView(view));
  const openFiles = () => session && togglePanel({ kind: "files", mid: session.machine_id, sid: session.session_id, cwd: session.cwd });
  const openGit = () => session && togglePanel({ kind: "git", mid: session.machine_id, sid: session.session_id, cwd: session.cwd });
  const othersNeed = others.some((s) => stateOf(s) === "needs_you");
  const { items: assistantItems, dialogs: assistantDialogs } = useAssistantMenu(assistant ? session : undefined);
  const feishu = externalLink(session);

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
                label: "改挂到其他待办…",
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
        ...(showTerminal ? [{ key: "terminal", label: "终端", icon: "terminal" as const, checked: view === "term", onSelect: flipView }] : []),
        ...(showFiles ? [{ key: "files", label: "文件", icon: "folder" as const, checked: here(panel, "files"), onSelect: openFiles }, { key: "git", label: "Git", icon: "git" as const, checked: here(panel, "git"), onSelect: openGit }] : []),
        ...(allowed.includes("stop") ? [{ key: "stop", label: "中断", icon: "stop" as const, hint: "Esc", onSelect: onInterrupt }] : []),
        ...(feishu ? [{ key: "feishu", label: "在飞书打开", icon: "external" as const, onSelect: () => openLink(feishu) }] : []),
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
      <div className={headerSlot(Boolean(topCenter), "start")}>
        {onToggleSidebar && <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />}
      </div>
      <div className={cx("min-w-0 flex-1", Boolean(topCenter) && "max-lg:hidden")}>
        <div className="flex min-w-0 items-center gap-2">
          {session && !assistant && <StatusDot state={state} />}
          {assistant && <AssistantFace session={session} last={last} />}
          {/* narrow: the dot before the title says the state; the pill would leave the title a word or two */}
          <h1 className="truncate text-[14px] font-semibold text-ink">{title}</h1>
          {session && !assistant && state !== "idle" && <span className="hidden sm:contents"><StatePill state={state} /></span>}
        </div>
        {sub && !(session?.todo && !assistant) && <div title={sub} className="truncate text-[11.5px] text-ink-3">{sub}</div>}
        {/* the todo it works for, up top (#1836): opens the todo, 改挂 picks another */}
        {session?.todo && !assistant && (
          <div className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-ink-3">
            <button type="button" onClick={() => navigate(`/todos?id=${encodeURIComponent(session.todo!.id)}`)} title={`待办：${session.todo.title}`}
              className="flex min-w-0 items-center gap-1 whitespace-nowrap rounded px-1 text-ink-2 hover:bg-hover hover:text-ink">
              <Icon name="todo" size={11} stroke={2} className="shrink-0" />
              <span className="truncate">{session.todo.title}</span>
            </button>
            <button type="button" onClick={() => setLinkTodoOpen(true)} className="shrink-0 whitespace-nowrap rounded px-1 hover:bg-hover hover:text-ink">改挂</button>
            {sub && <span title={sub} className="min-w-0 truncate">· {sub}</span>}
          </div>
        )}
      </div>
      {topCenter && <div className="flex min-w-0 flex-1 justify-center lg:hidden">{topCenter}</div>}
      <div className={headerSlot(Boolean(topCenter), "end")}>
      {session && !assistant && <ActivityChips session={session} onStopLoop={onStopLoop} />}
      {others.length > 0 && (
        <button
          type="button"
          onClick={tabs.open}
          aria-label={`同空间 ${others.length}`}
          title={`同空间 ${others.length}`}
          className="touch-hit flex h-7 shrink-0 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 shadow-btn transition-colors hover:bg-hover hover:text-ink max-sm:gap-1 max-sm:px-1.5"
        >
          <Icon name="layers" size={13} />
          <span className="max-sm:hidden">同空间</span> {others.length}
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
      {view && onView && (
        <Segmented
          label="终端或对话"
          value={view}
          onChange={onView}
          compact
          options={[
            { v: "term", label: "终端" },
            { v: "chat", label: "对话", dot: viewDot },
          ]}
        />
      )}
      {items.length > 0 && <IconButton icon="more" label="更多" onClick={menu.open} active={Boolean(menu.anchor)} />}
      </div>
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} align="right" items={items} />}
      {session && <CloseAgent open={closing} onClose={() => setClosing(false)} session={session} machineName={machineName} />}
      {session && <SessionInfoDialog open={dialog === "info"} onClose={() => setDialog(null)} session={session} machineName={machineName} />}
      {session && <RenameDialog open={dialog === "rename"} onClose={() => setDialog(null)} session={session} />}
      {session && <RenameSpaceDialog open={dialog === "space"} onClose={() => setDialog(null)} session={session} />}
      {session && <DeleteSessionDialog open={dialog === "delete"} onClose={() => setDialog(null)} session={session} machineName={machineName} />}
      <LinkTodoDialog open={linkTodoOpen} session={session} onClose={() => setLinkTodoOpen(false)} />
      {assistantDialogs}
    </header>
  );
}

/** The header's side slots around a switch in its middle (narrow screens, header.ts); wide, or without one, no boxes
 *  at all. */
function headerSlot(centered: boolean, side: "start" | "end"): string {
  if (!centered) return "contents";
  return cx(HEADER_SLOT, side === "start" ? HEADER_SLOT_START : HEADER_SLOT_END, "lg:contents");
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
