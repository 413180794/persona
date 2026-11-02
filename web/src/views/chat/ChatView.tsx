// One conversation: an agent in herdr on one of your computers, or a Jarvis assistant conversation.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, attachSession, errorText, sessionPath } from "../../lib/api";
import { ago, resumeCommand, sessionCan, sessionMenuActions, sourceSubtitle, sessionTitle, shortPath, stateOf, tabLabel, uuid } from "../../lib/format";
import { navigate } from "../../lib/router";
import { acknowledgeLaunch } from "../../lib/new-agent";
import { getSession, getState, markSeenLocally, refresh, useStore } from "../../lib/store";
import { parseUserText, segments, type UserMessage } from "../../lib/turns";
import type { Session } from "../../lib/types";
import { Button, copyText, Dialog, IconButton, Loading, Menu, StatePill, StatusDot, toast, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Composer, type Outgoing } from "./Composer";
import { InteractionCard } from "./Interactions";
import { SegmentView, UserBubble } from "./SegmentView";
import { useTranscript } from "./useTranscript";

interface Pending {
  id: string;
  msg: UserMessage;
  at: number;
}

export function ChatView({ mid, sid, assistant, starting, onToggleSidebar }: { mid: string; sid: string; assistant?: boolean; starting?: boolean; onToggleSidebar?: () => void }) {
  const session = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === sid) ?? s.assistant?.conversations.find((c) => c.session_id === sid));
  const machine = useStore((s) => s.machines.find((m) => m.id === mid));
  const siblings = useStore((s) => (session?.space_id ? (s.sessions[mid] ?? []).filter((x) => x.space_id === session.space_id && stateOf(x) !== "offline") : []));
  const interactions = useStore((s) => s.interactions.filter((i) => i.machine_id === mid && i.session_id === sid));
  const tr = useTranscript(mid, sid, { starting });
  const [pending, setPending] = useState<Pending[]>(() => {
    const launch = Object.values(getState().launches).find((l) => l.phase === "ready" && l.machineId === mid && l.session?.session_id === sid);
    const out = launch?.outgoing;
    if (!launch || !out || (!out.text && !out.images.length)) return [];
    return [{ id: launch.id, at: Date.now(), msg: { id: `pending:${launch.id}`, text: out.text, images: out.images.map((image, i) => ({ id: `pending:${launch.id}#${i}`, seq: 0, role: "user", kind: "image", image })), contexts: [] } }];
  });
  const [draftRefusal, setDraftRefusal] = useState<{ out: Outgoing; id: string; draft: string; clearable?: string } | null>(null);
  const state = session ? stateOf(session) : "unknown";
  const working = state === "working" || Boolean(tr.streaming) || pending.length > 0;
  const now = useNow(1000, working);

  const segs = useMemo(() => segments(tr.items), [tr.items]);

  // a sent message stays as a "sending" bubble until the transcript has it
  useEffect(() => {
    const users = segs.slice(-4).flatMap((seg) => seg.user ? [seg.user] : []);
    users.forEach((user) => acknowledgeLaunch(mid, sid, user));
    if (!pending.length) return;
    setPending((ps) => ps.filter((p) => !users.some((user) => user.text === parseUserText(p.msg.text).text && user.images.length >= p.msg.images.length) && Date.now() - p.at < 30_000));
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
  const [showDown, setShowDown] = useState(false);
  const before = useRef<{ h: number; top: number } | null>(null);
  const first = useRef(true);

  useEffect(() => {
    first.current = true;
    atEnd.current = true;
  }, [mid, sid]);

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
  }, [tr.items, pending, tr.streaming, interactions.length]);

  // content that grows by itself (images loading, a row expanding at the end) keeps the end in view
  useEffect(() => {
    const el = scroller.current?.firstElementChild;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (atEnd.current && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight;
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    atEnd.current = gap < 40;
    setShowDown(gap > 400);
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
      const p: Pending = { id, at: Date.now(), msg: { id: `pending:${id}`, text: out.text, images: [], contexts: [] } };
      setPending((ps) => [...ps.filter((x) => x.id !== id), p]);
      atEnd.current = true;
      try {
        await api(`${sessionPath(mid, sid)}/messages`, {
          method: "POST",
          json: { text: out.text, images: out.images.length ? out.images : undefined, client_msg_id: id, clear_draft: clearDraft },
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
    [mid, sid, session],
  );

  const onSend = (out: Outgoing) => deliver(out, uuid());

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

  const lastLive = working && !tr.loading;
  const title = session ? sessionTitle(session) : assistant ? "新对话" : "会话";

  return (
    <div className="flex h-full min-w-0 flex-col">
      <Header session={session} title={title} machineName={machine?.name ?? mid} assistant={assistant} siblings={siblings} onToggleSidebar={onToggleSidebar} onInterrupt={interrupt} />

      <div className="relative min-h-0 flex-1">
        <div ref={scroller} onScroll={onScroll} className="h-full overflow-y-auto [overflow-anchor:none]">
          <div className="mx-auto flex w-full max-w-[760px] flex-col gap-7 px-5 pt-6 pb-10 sm:px-8">
            {session?.start_error && <div role="alert" className="rounded-control bg-inset px-3 py-2 text-[13px] text-red">启动失败：{session.start_error}</div>}
            {starting && !tr.loading && !pending.length && <Loading label="正在启动 Agent" />}
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
            {segs.map((seg, i) => {
              const live = lastLive && i === segs.length - 1 && !pending.length;
              return <SegmentView key={seg.id} seg={seg} live={live} now={now} mid={mid} sid={sid} streaming={live && tr.streaming ? tr.streaming : undefined} expandFold={tr.expandFold} />;
            })}
            {pending.map((p) => (
              <div key={p.id} className="flex flex-col gap-3">
                <UserBubble msg={p.msg} mid={mid} sid={sid} pending />
              </div>
            ))}
            {pending.length > 0 && (
              <div className="flex items-center gap-2.5">
                <Loading label={starting ? "正在准备" : "正在送达"} />
              </div>
            )}
          </div>
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

      <div className="mx-auto flex w-full max-w-[760px] flex-col gap-2 px-4 pb-4 sm:px-7">
        {interactions.map((ix) => (
          <InteractionCard key={ix.id} ix={ix} />
        ))}
        {(session?.queued?.length ?? 0) > 0 && (
          <div className="flex flex-col gap-1 rounded-[10px] bg-inset px-3 py-2 shadow-hairline">
            <span className="text-[11px] font-medium text-ink-3">排队中 · 它做完这一步就会看到</span>
            {session!.queued!.map((q, i) => (
              <span key={i} className="truncate text-[12.5px] text-ink-2">
                {q}
              </span>
            ))}
          </div>
        )}
        <Composer
          draftKey={`${mid}/${sid}`}
          placeholder={!sessionCan(session, "send") && session?.source_kind ? "此会话仅可查看" : assistant ? "问 Jarvis…" : state === "offline" ? "发消息会在电脑上恢复这个会话…" : "给 Agent 发消息…"}
          machineId={mid}
          working={state === "working"}
          onSend={onSend}
          disabled={!sessionCan(session, "send")}
          onStop={assistant || !sessionCan(session, "interrupt") ? undefined : interrupt}
          commands={assistant ? [] : undefined}
          suggestion={session?.suggestion}
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
  const parts = [sourceSubtitle(session), session.launcher, session.model?.split("/").pop(), assistant ? "" : shortPath(session.cwd)].filter(Boolean);
  return <span title={[sourceSubtitle(session), session.cwd].filter(Boolean).join(" · ")}>{parts.join(" · ")}</span>;
}

function Header({
  session,
  title,
  machineName,
  assistant,
  siblings,
  onToggleSidebar,
  onInterrupt,
}: {
  session?: Session;
  title: string;
  machineName: string;
  assistant?: boolean;
  siblings: Session[];
  onToggleSidebar?: () => void;
  onInterrupt: () => void;
}) {
  const menu = useMenu();
  const tabs = useMenu();
  const [closing, setClosing] = useState(false);
  const state = session ? stateOf(session) : "unknown";
  const sub = assistant
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
  const othersNeed = others.some((s) => stateOf(s) === "needs_you");

  const items: MenuItem[] = session
    ? [
        ...(allowed.includes("attach") ? [{ key: "attach", label: "实时接入", icon: "link" as const, onSelect: () => void attach() }] : []),
        ...(allowed.includes("stop") ? [{ key: "stop", label: "中断", icon: "stop" as const, hint: "Esc", onSelect: onInterrupt }] : []),
        { key: "sid", label: "拷贝会话 ID", icon: "copy", onSelect: () => copyText(session.session_id) },
        ...(session.cwd ? [{ key: "cwd", label: "拷贝目录", icon: "folder" as const, onSelect: () => copyText(session.cwd) }] : []),
        ...(allowed.includes("resume") ? [{ key: "resume", label: "拷贝恢复命令", icon: "terminal" as const, onSelect: () => copyText(resumeCommand(session)) }] : []),
        ...(!assistant && allowed.includes("close") ? [{ key: "close", label: "关闭 Agent…", icon: "x" as const, danger: true, onSelect: () => setClosing(true) }] : []),
      ]
    : [];

  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line bg-page/85 px-3 backdrop-blur-md sm:px-4">
      {onToggleSidebar && <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />}
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          {session && !assistant && <StatusDot state={state} />}
          <h1 className="truncate text-[14px] font-semibold text-ink">{title}</h1>
          {session && !assistant && state !== "idle" && <StatePill state={state} />}
        </div>
        <div title={sub} className="truncate text-[11.5px] text-ink-3">{sub}</div>
      </div>
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
      {items.length > 0 && <IconButton icon="more" label="更多" onClick={menu.open} active={Boolean(menu.anchor)} />}
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} align="right" items={items} />}
      {session && <CloseAgent open={closing} onClose={() => setClosing(false)} session={session} machineName={machineName} />}
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
