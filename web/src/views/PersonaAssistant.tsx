// The Persona tab: the headless assistant on the hub's machine (docs/assistant.md). `/` (and the old `/j`) is the main
// conversation, `/j/<id>` one of them, `/j/new` a new one-off conversation. With no main conversation yet, the first
// message on `/` starts it.
import { useState } from "react";
import { api, errorText } from "../lib/api";
import { navigate } from "../lib/router";
import { refreshAssistant, useStore } from "../lib/store";
import { NEW_CONVERSATION, pickedLauncher, pickLauncher } from "../lib/assistant-chat";
import { noteSent } from "../lib/outbox";
import type { Session } from "../lib/types";
import { cx, EASE, IconButton, Loading, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { ChatView } from "./chat/ChatView";
import { Composer, type Outgoing } from "./chat/Composer";
import { StarterLauncherChip } from "./assistant/AssistantChat";
import { PersonaAvatar, usePersona } from "./assistant/Persona";
import { ShellHeaderSlots, ShellSwitch } from "./ShellSwitch";

const SUGGESTIONS = ["今天我有哪些会？帮我准备一下", "看看各台电脑上的 Agent 都在干什么", "总结一下飞书里需要我回复的消息", "我最近在做哪些项目？进展如何"];

const starting = new Set<string>();

export function PersonaAssistant({ sid, fresh, onToggleSidebar }: { sid?: string; fresh?: boolean; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const loaded = useStore((s) => s.loaded);
  if (fresh) return <Starter onToggleSidebar={onToggleSidebar} />;
  const id = sid ?? assistant?.main;
  if (id) {
    if (!assistant) return <Centered><Loading /></Centered>;
    // the main conversation is the phone's 助手 page: 「助手 | Agents」 in its top bar (ChatView's `topCenter`, narrow only)
    const topCenter = id === assistant.main ? <ShellSwitch page="assistant" /> : undefined;
    return <ChatView key={id} mid={assistant.machine_id} sid={id} assistant starting={starting.has(id)} onToggleSidebar={onToggleSidebar} topCenter={topCenter} />;
  }
  if (!loaded) return <Centered><Loading /></Centered>;
  return <Starter main onToggleSidebar={onToggleSidebar} />;
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full items-center justify-center">{children}</div>;
}

function Starter({ main, onToggleSidebar }: { main?: boolean; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const persona = usePersona();
  const [seed, setSeed] = useState(0);

  const start = async (out: Outgoing): Promise<boolean> => {
    try {
      const launcher = pickedLauncher(NEW_CONVERSATION);
      const r = await api<{ session: Session }>("/assistant/conversations", { method: "POST", json: { prompt: out.text, images: out.images.length ? out.images : undefined, launcher, main: main || undefined } });
      starting.add(r.session.session_id);
      // the prompt shows in the conversation at once (it started with it), until its transcript has it
      noteSent(r.session.machine_id, r.session.session_id, out);
      if (launcher) pickLauncher(NEW_CONVERSATION, undefined);
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
        {/* a phone's 助手 page: 「助手 | Agents」 between the same side slots as ChatView's and the Agents page's */}
        {main ? (
          <ShellHeaderSlots page="assistant" onToggleSidebar={onToggleSidebar} end={<IconButton icon="info" label="资料" onClick={() => navigate({ name: "profile" })} />} />
        ) : (
          <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        )}
        <span className={cx("flex-1", main && "max-lg:hidden")} />
        <button type="button" onClick={() => navigate({ name: "profile" })} className={cx("flex h-7 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 shadow-btn hover:bg-hover hover:text-ink", main && "max-lg:hidden")}>
          <PersonaAvatar size={16} fallback={<Icon name="info" size={13} />} /> 资料
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-5 pb-[10vh]">
        <div className="w-full max-w-[640px]" style={{ animation: `fade-up 450ms ${EASE} both` }}>
          <div className="mb-6 flex flex-col items-center text-center">
            <PersonaAvatar
              size={56}
              className="mb-4 shadow-raised"
              fallback={
                <span className="mb-4 flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
                  <Icon name="spark" size={20} />
                </span>
              }
            />
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">有什么要我做的？</h1>
            <p className="mt-1.5 text-[13px] text-ink-2">能看你的日程、飞书、邮件、知识库，也能到{online.length ? ` ${online.map((m) => m.name).join("、")} ` : "各台电脑"}上动手。</p>
          </div>
          <Composer key={seed} draftKey="jarvis-new" placeholder={`问 ${persona.name}…`} onSend={start} autoFocus commands={[]} footer={assistant ? <StarterLauncherChip /> : undefined} />
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
