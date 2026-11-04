// What a Persona conversation adds to the chat page (the app's AssistantTopBar): the persona's dot in the header,
// moving with the conversation's state; the header menu's 详细信息 / 知识库 / 回到主对话 / 拷贝信息; and the
// launcher chip under the composer that switches the model for the next messages (AssistantTopBar's 启动器 menu).
import { useEffect, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import {
  assistantInfoText, conversationKey, effectiveLauncher, NEW_CONVERSATION, pickLauncher, pickModel, runsAssistant, shortModel, usePickedLauncher,
  usePickedModel, type AssistantLauncher,
} from "../../lib/assistant-chat";
import { launcherModels, type AgentModel } from "../../lib/launcher-models";
import { personaState } from "../../lib/persona";
import { navigate } from "../../lib/router";
import { useStore } from "../../lib/store";
import type { Item, Session } from "../../lib/types";
import { copyText, Menu, toast, useMenu, type MenuItem } from "../../ui/atoms";
import { Chip } from "../chat/SessionDialogs";
import { PersonaDot, usePersona } from "./Persona";

// ── launchers ───────────────────────────────────────────

const cache = new Map<string, { at: number; list: AssistantLauncher[] }>();

/** The launchers the assistant's computer can run it with (Claude Code or Codex), fetched when a menu opens. */
function useAssistantLaunchers(mid: string | undefined, wanted: boolean): { list: AssistantLauncher[] | null; error: string } {
  const hit = mid ? cache.get(mid) : undefined;
  const [list, setList] = useState<AssistantLauncher[] | null>(hit?.list ?? null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!mid || !wanted) return;
    const c = cache.get(mid);
    if (c && Date.now() - c.at < 60_000) {
      setList(c.list);
      return;
    }
    let live = true;
    api<{ launchers: AssistantLauncher[] }>(`/machines/${enc(mid)}/launchers`)
      .then((r) => {
        const l = (r.launchers ?? []).filter(runsAssistant);
        cache.set(mid, { at: Date.now(), list: l });
        if (live) {
          setList(l);
          setError("");
        }
      })
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [mid, wanted]);
  return { list, error };
}

function launcherItems(list: AssistantLauncher[] | null, error: string, current: string, onPick: (name: string) => void): MenuItem[] {
  if (!list) return [{ key: "loading", label: error ? `读不到启动器：${error}` : "正在读取启动器…", disabled: true, onSelect: () => {} }];
  if (!list.length) return [{ key: "none", label: "这台电脑上没有能运行 Persona 的启动器", disabled: true, onSelect: () => {} }];
  return list.map((l) => ({ key: l.name, label: l.name, hint: shortModel(l.model) || undefined, checked: l.name === current, onSelect: () => onPick(l.name) }));
}

/** The composer's 启动器 chip in a Persona conversation: the next message (and the ones after it) uses the pick.
 *  Picking a launcher opens its model submenu in the same menu. */
export function AssistantLauncherChip({ session }: { session: Session }) {
  const menu = useMenu();
  const assistant = useStore((s) => s.assistant);
  const key = conversationKey(session.machine_id, session.session_id);
  const picked = usePickedLauncher(key);
  const pickedM = usePickedModel(key);
  const [modelsFor, setModelsFor] = useState<{ name: string; options: AgentModel[]; loading: boolean } | null>(null);
  const current = effectiveLauncher({ isMain: assistant?.main === session.session_id, picked, session: session.launcher, config: assistant?.launcher });
  const { list, error } = useAssistantLaunchers(session.machine_id, Boolean(menu.anchor));
  const model = shortModel(pickedM ?? (picked ? list?.find((l) => l.name === picked)?.model : session.model));

  // Picking the launcher already applies; the submenu only offers that launcher's models.
  const openModels = (name: string) => {
    pickLauncher(key, name);
    setModelsFor({ name, options: [], loading: true });
    void launcherModels(session.machine_id, name)
      .then((r) => setModelsFor((cur) => (cur?.name === name ? { name, options: r?.models ?? [], loading: false } : cur)))
      .catch(() => setModelsFor((cur) => (cur?.name === name ? { name, options: [], loading: false } : cur)));
  };
  const pick = (name: string, id: string | undefined) => {
    pickModel(key, id);
    toast(id ? `下一条消息起使用 ${name} · ${id}` : `下一条消息起使用 ${name} 的默认模型`, "ok");
  };

  const items: MenuItem[] = modelsFor
    ? [
        { key: "back", label: "← 启动器", onSelect: () => setModelsFor(null), keepOpen: true },
        { key: "default", label: "默认模型", checked: !pickedM, onSelect: () => pick(modelsFor.name, undefined) },
        ...(modelsFor.loading
          ? [{ key: "loading", label: "正在读取模型…", disabled: true, onSelect: () => {} }]
          : modelsFor.options.map((m) => ({
              key: m.id,
              label: m.name || m.id,
              hint: m.description,
              checked: m.default ? !pickedM : pickedM === m.id,
              onSelect: () => pick(modelsFor.name, m.default ? undefined : m.id),
            }))),
      ]
    : launcherItems(list, error, current, openModels).map((it) => ({ ...it, submenu: true }));

  return (
    <>
      <Chip label={[current, model].filter(Boolean).join(" · ")} onClick={menu.open} active={Boolean(menu.anchor)} title="启动器：下一条消息起使用" />
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={() => {
            menu.close();
            setModelsFor(null);
          }}
          side="top"
          width={280}
          header={modelsFor ? `模型：${modelsFor.name}` : "下一条消息起使用"}
          items={items}
        />
      )}
    </>
  );
}

/** The new-conversation page's chip: what the conversation starts with (the default unless picked). */
export function StarterLauncherChip() {
  const menu = useMenu();
  const assistant = useStore((s) => s.assistant);
  const picked = usePickedLauncher(NEW_CONVERSATION);
  const current = effectiveLauncher({ isNew: true, picked, config: assistant?.launcher });
  const { list, error } = useAssistantLaunchers(assistant?.machine_id, Boolean(menu.anchor));
  if (!assistant) return null;
  return (
    <>
      <Chip label={picked ? current : `默认 · ${current}`} onClick={menu.open} active={Boolean(menu.anchor)} title="新对话用的启动器" />
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          side="top"
          width={280}
          header="新对话使用"
          items={[
            { key: "default", label: "默认", hint: assistant.launcher || undefined, checked: !picked, onSelect: () => pickLauncher(NEW_CONVERSATION, undefined) },
            ...launcherItems(list, error, picked ?? "", (name) => pickLauncher(NEW_CONVERSATION, name)),
          ]}
        />
      )}
    </>
  );
}

// ── the header ──────────────────────────────────────────

/** The persona's dot beside a Persona conversation's title, in the conversation's state; nothing for the page's mark. */
export function AssistantFace({ session, last }: { session?: Session; last?: Item }) {
  const persona = usePersona();
  const conn = useStore((s) => s.conn);
  const machineOnline = useStore((s) => s.machines.find((m) => m.id === (session?.machine_id ?? s.assistant?.machine_id))?.online);
  const waiting = useStore((s) => (session ? s.interactions.filter((i) => i.machine_id === session.machine_id && i.session_id === session.session_id).length : 0));
  if (!persona.info && !persona.mochi) return null;
  const state = personaState({ online: conn === "online", machineOnline, state: session?.state, interactions: waiting, last });
  return <PersonaDot size={30} state={state} fallback={null} className="-my-1" />;
}

/** The header menu's Persona part: 详细信息 (the profile), 知识库, 回到主对话, 拷贝信息. Empty for an agent. */
export function useAssistantMenu(session: Session | undefined): MenuItem[] {
  const assistant = useStore((s) => s.assistant);
  const machine = useStore((s) => (session ? s.machines.find((m) => m.id === session.machine_id)?.name ?? "" : ""));
  const picked = usePickedLauncher(session ? conversationKey(session.machine_id, session.session_id) : "");
  if (!session) return [];
  const isMain = assistant?.main === session.session_id;
  const launcher = effectiveLauncher({ isMain, picked, session: session.launcher, config: assistant?.launcher });
  const cwd = session.cwd || assistant?.cwd || "~/.jarvis/assistant";
  const title = !isMain && session.title && session.title !== "Claude Code" ? session.title : undefined;
  return [
    { key: "profile", label: "详细信息", icon: "info", onSelect: () => navigate({ name: "profile" }) },
    { key: "kb", label: "知识库", icon: "book", onSelect: () => navigate({ name: "kb" }) },
    ...(assistant?.main && !isMain ? [{ key: "main", label: "回到主对话", icon: "home" as const, onSelect: () => navigate({ name: "jarvis", sid: assistant.main }) }] : []),
    {
      key: "copyinfo",
      label: "拷贝信息",
      icon: "copy",
      onSelect: () => void copyText(assistantInfoText({ title, isMain, sessionId: session.session_id, machine, machineId: session.machine_id, cwd, launcher, model: shortModel(session.model) || undefined })),
    },
  ];
}
