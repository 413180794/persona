// The Persona tab: the headless assistant on the hub's machine (docs/assistant.md). `/` (and the old `/j`) is the main
// conversation, `/j/<id>` one of them, `/j/new` a new one-off conversation. With no main conversation yet, the first
// message on `/` starts it.
import { useState } from "react";
import type { MentionSource } from "./chat/MentionMenu";
import { api } from "../lib/api";
import { codedFailure, helpTopic, passesBySelf, refreshReadiness, type CodedFailure } from "../lib/readiness";
import { navigate } from "../lib/router";
import { refreshAssistant, useStore } from "../lib/store";
import { NEW_CONVERSATION, pickedLauncher, pickLauncher } from "../lib/assistant-chat";
import { noteSent } from "../lib/outbox";
import type { Session } from "../lib/types";
import { Button, cx, EASE, IconButton, Loading } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { ChatView } from "./chat/ChatView";
import { Composer, type Outgoing } from "./chat/Composer";
import { StarterLauncherChip } from "./assistant/AssistantChat";
import { PersonaAvatar, usePersona } from "./assistant/Persona";
import { ChatOnlyNote, FailureCard, ReadinessCard, useReadinessGate } from "./assistant/Readiness";
import { ShellHeaderSlots, ShellSwitch } from "./ShellSwitch";
import { formatList, t } from "../i18n";

/** The example questions of a hub without readiness (its `suggestions` replace them, docs/onboarding.md 4.7). */
const SUGGESTIONS = [t("personaassistant.suggestion_meetings"), t("personaassistant.suggestion_agents"), t("personaassistant.suggestion_lark"), t("personaassistant.suggestion_projects")];

const starting = new Set<string>();

// @ in a new conversation: other sessions as context; no chat yet, so no files it touched (typed / and ~ paths still browse)
const NEW_MENTION: MentionSource = { self: "jarvis-new", files: () => [], workspaces: true };

export function PersonaAssistant({ sid, fresh, onToggleSidebar }: { sid?: string; fresh?: boolean; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const assistantError = useStore((s) => s.assistantError);
  const loaded = useStore((s) => s.loaded);
  if (fresh) return <Starter onToggleSidebar={onToggleSidebar} />;
  const id = sid ?? assistant?.main;
  if (id) {
    if (!assistant) {
      if (assistantError) return <AssistantLoadFailed error={assistantError} onToggleSidebar={onToggleSidebar} />;
      return <Centered><Loading /></Centered>;
    }
    // the main conversation is the phone's assistant page: "Assistant | Agents" in its top bar (ChatView's `topCenter`, narrow only)
    const topCenter = id === assistant.main ? <ShellSwitch page="assistant" /> : undefined;
    return <ChatView key={id} mid={assistant.machine_id} sid={id} assistant starting={starting.has(id)} onToggleSidebar={onToggleSidebar} topCenter={topCenter} />;
  }
  if (!loaded) return <Centered><Loading /></Centered>;
  if (!sid && assistantError && !assistant) {
    return <AssistantLoadFailed error={assistantError} onToggleSidebar={onToggleSidebar} />;
  }
  return <Starter main onToggleSidebar={onToggleSidebar} />;
}

function AssistantLoadFailed({ error, onToggleSidebar }: { error: string; onToggleSidebar: () => void }) {
  const [retrying, setRetrying] = useState(false);
  const retry = async () => {
    setRetrying(true);
    try {
      await refreshAssistant();
    } catch {
      // Keep the failure displayed with the latest error
    } finally {
      setRetrying(false);
    }
  };
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        <ShellHeaderSlots page="assistant" onToggleSidebar={onToggleSidebar} end={<IconButton icon="info" label={t("personaassistant.profile")} onClick={() => navigate({ name: "profile" })} />} />
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-5 text-center">
        <p className="text-[13px] text-ink-2">{t("personaassistant.load_failed", { error })}</p>
        <Button onClick={retry} busy={retrying} icon="retry">
          {t("personaassistant.retry")}
        </Button>
      </div>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full items-center justify-center">{children}</div>;
}

function Starter({ main, onToggleSidebar }: { main?: boolean; onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const persona = usePersona();
  const [seed, setSeed] = useState(0);

  const gate = useReadinessGate();
  const [failure, setFailure] = useState<CodedFailure | null>(null);

  const start = async (out: Outgoing): Promise<boolean> => {
    setFailure(null);
    const launcher = pickedLauncher(NEW_CONVERSATION);
    const body = { prompt: out.text, images: out.images.length ? out.images : undefined, launcher, main: main || undefined };
    for (let attempt = 0; ; attempt++) {
      try {
        const r = await api<{ session: Session }>("/assistant/conversations", { method: "POST", json: body });
        starting.add(r.session.session_id);
        // the prompt shows in the conversation at once (it started with it), until its transcript has it
        noteSent(r.session.machine_id, r.session.session_id, out);
        if (launcher) pickLauncher(NEW_CONVERSATION, undefined);
        void refreshAssistant().catch(() => {});
        navigate({ name: "jarvis", sid: r.session.session_id });
        return true;
      } catch (e) {
        // the assistant restarting: a few seconds, tried again without a word (docs/onboarding.md 4.6)
        if (passesBySelf(e) && attempt < 3) {
          await new Promise((done) => setTimeout(done, 2000));
          continue;
        }
        // a card, not a toast (4.4); the readiness changed with it, most likely
        setFailure(codedFailure(e));
        void refreshReadiness({ force: true });
        return false;
      }
    }
  };

  const fill = (text: string) => {
    localStorage.setItem("draft:jarvis-new", text);
    setSeed((x) => x + 1);
  };

  const online = machines.filter((m) => m.online);
  // the hub's example questions when it gives them: a local one opens help here and is not sent (4.4)
  const hubSuggestions = gate.readiness?.suggestions;
  const suggestions: { key: string; title: string; onClick: () => void; hint?: string }[] = hubSuggestions
    ? hubSuggestions.map((x) => ({
        key: x.id,
        title: x.title,
        hint: x.needs?.includes("phone_calendar") ? t("personaassistant.needs_calendar") : undefined,
        onClick: () => (x.local ? gate.help(helpTopic(x.id)) : fill(x.prompt || x.title)),
      }))
    : SUGGESTIONS.map((x) => ({ key: x, title: x, onClick: () => fill(x) }));
  const setup = Boolean(gate.step);
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 px-3 sm:px-4">
        {/* a phone's assistant page: "Assistant | Agents" between the same side slots as ChatView's and the Agents page's */}
        {main ? (
          <ShellHeaderSlots page="assistant" onToggleSidebar={onToggleSidebar} end={<IconButton icon="info" label={t("personaassistant.profile")} onClick={() => navigate({ name: "profile" })} />} />
        ) : (
          <IconButton icon="sidebar" label={t("personaassistant.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        )}
        <span className={cx("flex-1", main && "max-lg:hidden")} />
        <button type="button" onClick={() => navigate({ name: "profile" })} className={cx("flex h-7 items-center gap-1.5 rounded-[8px] px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 shadow-btn hover:bg-hover hover:text-ink", main && "max-lg:hidden")}>
          <PersonaAvatar size={16} fallback={<Icon name="info" size={13} />} /> {t("personaassistant.profile")}
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
            <h1 className="text-[22px] font-semibold tracking-tight text-ink">{t("personaassistant.empty_title")}</h1>
            <p className="mt-1.5 text-[13px] text-ink-2">
              {setup ? t("personaassistant.empty_body_setup") : online.length ? t("personaassistant.empty_body_machines", { machines: formatList(online.map((m) => m.name), { type: "conjunction", style: "narrow" }) }) : t("personaassistant.empty_body")}
            </p>
          </div>
          {/* files go to the assistant's machine, as in its conversations (iOS StarterComposer) */}
          {(gate.step || failure || gate.readiness?.mode === "chat_only") && (
            <div className="mb-2.5 flex flex-col gap-2">
              {failure ? <FailureCard failure={failure} onClose={() => setFailure(null)} onHelp={gate.help} onConfirm={gate.confirm} /> : <ReadinessCard gate={gate} />}
              <ChatOnlyNote gate={gate} />
            </div>
          )}
          {gate.dialog}
          <Composer key={seed} blocked={gate.blocked} draftKey="jarvis-new" placeholder={t("personaassistant.ask_placeholder", { name: persona.name })} machineId={assistant?.machine_id} mention={NEW_MENTION} onSend={start} autoFocus commands={[]} footer={assistant ? <StarterLauncherChip /> : undefined} />
          <div className="mt-4 grid grid-cols-1 gap-1.5 sm:grid-cols-2">
            {suggestions.map((x, i) => (
              <button
                key={x.key}
                type="button"
                onClick={x.onClick}
                title={x.hint}
                className="flex items-center gap-2 rounded-[10px] px-3 py-2 text-left text-[12.5px] text-ink-2 shadow-hairline transition-colors hover:bg-hover hover:text-ink"
                style={{ animation: `fade-up 380ms ${EASE} ${120 + i * 60}ms both` }}
              >
                <Icon name={hubSuggestions?.[i]?.local ? "info" : "chevronRight"} size={12} stroke={2.2} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1 truncate">{x.title}</span>
                {x.hint && <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-3">{x.hint}</span>}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
