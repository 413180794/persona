// Persona's profile (/assistant, the app's AssistantProfileView), opened from the Persona page and a conversation's menu:
// who it is and where it runs, what it will do on its own next, its persona, then what shapes it (设置 › 助手's
// groups: 主动做事, 了解你, 运行在) and what it can reach.
import { useEffect, useMemo, useState } from "react";
import { api } from "../../lib/api";
import { navigate } from "../../lib/router";
import { dayLabel, hm, untilLabel, upcoming, type Task } from "../../lib/routines";
import { useStore } from "../../lib/store";
import { EASE, IconButton, useNow } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { AssistantSettings } from "../settings/AssistantSettings";
import { Group, Pill } from "../settings/SettingsParts";
import { BuiltinFace, PersonaAvatar, PersonaPicker, usePersona } from "./Persona";

const REACH: { text: string; icon: IconName }[] = [
  { text: "各台电脑上的 Agent：看状态、读对话、发消息、开新的", icon: "layers" },
  { text: "飞书：消息、日历、文档、会议纪要、任务、邮件", icon: "chat" },
  { text: "iPhone：通讯录、日历、提醒、位置、健康、照片", icon: "monitor" },
  { text: "你添加的所有邮箱", icon: "mail" },
  { text: "在手机上问你、给你推送", icon: "bell" },
];

export function Profile({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const persona = usePersona();
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const working = useStore((s) => (s.assistant?.conversations ?? []).some((c) => c.state === "working"));
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const nowMs = useNow(30_000);
  const now = useMemo(() => new Date(nowMs), [nowMs]);

  useEffect(() => {
    api<{ tasks: Task[] }>("/assistant/tasks").then((r) => setTasks(r.tasks ?? []), () => setTasks([]));
  }, []);

  const machine = machines.find((m) => m.id === assistant?.machine_id);
  const machineName = machine?.name ?? assistant?.machine_id ?? "hub 所在的机器";
  const next = tasks ? upcoming(tasks, 3) : [];
  const reach = [{ text: machines.length ? `${machines.map((m) => m.name).join("、")}（ssh）` : "hub 接入的各台电脑（ssh）", icon: "machine" as IconName }, ...REACH];

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink">{persona.name}</h1>
        {assistant?.main && <IconButton icon="chat" label="主对话" onClick={() => navigate({ name: "jarvis", sid: assistant.main })} />}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[760px] px-5 py-8 sm:px-8" style={{ animation: `fade-up 320ms ${EASE} both` }}>
          <div className="mb-8 flex flex-col items-center gap-2 text-center">
            <PersonaAvatar size={88} fallback={<BuiltinFace size={88} />} className="shadow-raised" />
            <h2 className="mt-2 text-[24px] font-semibold tracking-tight text-ink">{persona.name}</h2>
            <p className="max-w-full truncate text-[13px] text-ink-2">你的私人助手 · 运行在 {machineName}</p>
            <div className="mt-1 flex flex-wrap justify-center gap-1.5">
              {machine && <Pill tone={machine.online ? "green" : undefined}>{machine.online ? "在线" : "离线"}</Pill>}
              {working && <Pill tone="accent">工作中</Pill>}
              {next[0] && <Pill>下一件 {dayLabel(next[0].at, now)} {hm(next[0].at)}</Pill>}
            </div>
          </div>

          {next.length > 0 && (
            <Group title="接下来">
              {next.map((u) => (
                <button key={u.id} type="button" onClick={() => navigate({ name: "routines" })} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover">
                  <span className="w-12 shrink-0">
                    <span className="block text-[14px] font-semibold text-ink tabular-nums">{hm(u.at)}</span>
                    <span className="block text-[11px] whitespace-nowrap text-ink-3">{dayLabel(u.at, now)}</span>
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-1.5">
                      <Icon name={u.repeating ? "loop" : "bell"} size={12} className="shrink-0 text-accent-ink" />
                      <span className="truncate text-[13px] font-medium text-ink">{u.title}</span>
                    </span>
                    <span className="block truncate text-[11.5px] text-ink-3">{u.detail} · {untilLabel(u.at, now)}</span>
                  </span>
                </button>
              ))}
            </Group>
          )}

          <Group title="形象" footer="换形象只换 Persona 的名字、头像和对话里会动的状态，只在这个浏览器里生效；网页的颜色不变。">
            <PersonaPicker builtin={<BuiltinFace size={56} />} />
          </Group>

          <AssistantSettings bare onConfigure={() => navigate("/settings?tab=model-config&purpose=assistant")} />

          <Group title="能访问" footer="发飞书消息、发邮件、新建日程这类会替你出面的操作，都先在手机上问你。">
            {reach.map((r) => (
              <div key={r.text} className="flex items-center gap-3 px-4 py-2.5">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-3">
                  <Icon name={r.icon} size={14} />
                </span>
                <span className="min-w-0 flex-1 text-[13px] leading-snug text-ink">{r.text}</span>
              </div>
            ))}
          </Group>
        </div>
      </div>
    </div>
  );
}
