// The Jarvis tab: the headless assistant on the hub's machine (docs/assistant.md). /j is a new conversation,
// /j/<id> one of them.
import { useState } from "react";
import { api, errorText } from "../lib/api";
import { navigate } from "../lib/router";
import { refreshAssistant, useStore } from "../lib/store";
import type { Session } from "../lib/types";
import { EASE, IconButton, Loading, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { ChatView } from "./chat/ChatView";
import { Composer, type Outgoing } from "./chat/Composer";

const SUGGESTIONS = ["今天我有哪些会？帮我准备一下", "看看各台电脑上的 Agent 都在干什么", "总结一下飞书里需要我回复的消息", "我最近在做哪些项目？进展如何"];

const starting = new Set<string>();

export function Jarvis({ sid, onToggleSidebar }: { sid?: string; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  if (sid) {
    if (!assistant) return <Centered><Loading /></Centered>;
    return <ChatView key={sid} mid={assistant.machine_id} sid={sid} assistant starting={starting.has(sid)} onToggleSidebar={onToggleSidebar} />;
  }
  return <Starter onToggleSidebar={onToggleSidebar} />;
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full items-center justify-center">{children}</div>;
}

function Starter({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const [seed, setSeed] = useState(0);

  const start = async (out: Outgoing): Promise<boolean> => {
    try {
      const r = await api<{ session: Session }>("/assistant/conversations", { method: "POST", json: { prompt: out.text, images: out.images.length ? out.images : undefined } });
      starting.add(r.session.session_id);
      void refreshAssistant().catch(() => {});
      navigate({ name: "jarvis", sid: r.session.session_id });
      return true;
    } catch (e) {
      toast(`没开起来：${errorText(e)}`, "error");
      return false;
    }
  };

  const fill = (text: string) => {
    localStorage.setItem("draft:jarvis-new", text);
    setSeed((x) => x + 1);
  };

  const online = machines.filter((m) => m.online);
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex-1" />
        {assistant?.main && (
          <button type="button" onClick={() => navigate({ name: "jarvis", sid: assistant.main })} className="flex h-7 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium text-ink-2 shadow-btn hover:bg-hover hover:text-ink">
            <Icon name="chat" size={13} /> 主对话
          </button>
        )}
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-5 pb-[10vh]">
        <div className="w-full max-w-[640px]" style={{ animation: `fade-up 450ms ${EASE} both` }}>
          <div className="mb-6 flex flex-col items-center text-center">
            <span className="mb-4 flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
              <Icon name="spark" size={20} />
            </span>
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">有什么要我做的？</h1>
            <p className="mt-1.5 text-[13px] text-ink-2">能看你的日程、飞书、邮件、知识库，也能到{online.length ? ` ${online.map((m) => m.name).join("、")} ` : "各台电脑"}上动手。</p>
          </div>
          <Composer key={seed} draftKey="jarvis-new" placeholder="问 Jarvis…" onSend={start} autoFocus commands={[]} footer={assistant?.launcher ? `用 ${assistant.launcher}` : undefined} />
          <div className="mt-4 grid grid-cols-1 gap-1.5 sm:grid-cols-2">
            {SUGGESTIONS.map((s, i) => (
              <button
                key={s}
                type="button"
                onClick={() => fill(s)}
                className="flex items-center gap-2 rounded-[10px] px-3 py-2 text-left text-[12.5px] text-ink-2 shadow-hairline transition-colors hover:bg-hover hover:text-ink"
                style={{ animation: `fade-up 380ms ${EASE} ${120 + i * 60}ms both` }}
              >
                <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3" />
                <span className="truncate">{s}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
