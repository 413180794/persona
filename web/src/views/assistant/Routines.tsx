// 定时任务 (docs/assistant.md「定时任务」, the iOS app's AssistantRoutinesView): everything Persona does on its own on
// one page — what repeats (every day at a time, every N minutes) and the one-offs it scheduled from a conversation, each
// with its rule in words, next and last run, what it runs with and an on/off switch; what comes next and the history
// of runs beside them. A row opens the editor; 新建 starts from a template or blank.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { navigate } from "../../lib/router";
import {
  fromTemplate, hm, blankTask, dayLabel, lastRunText, nextText, ruleText, runDays, runsWith, splitTasks, taskBody, taskTitle, TEMPLATES,
  untilLabel, upcoming, type Run, type Task,
} from "../../lib/routines";
import { onHubEvent, useStore } from "../../lib/store";
import { Button, cx, IconButton, Loading, Menu, toast, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Pill } from "../settings/SettingsParts";
import { Empty, IconTile, SectionTitle, Switch } from "./parts";
import { RunLine, TaskEditor } from "./TaskEditor";
import { t } from "../../i18n";

/** A task's mark, from its title (the iOS app's AssistantScheduleText.symbol). */
function taskIcon(task: Task): IconName {
  // i18n-ignore below: keywords in the title the user or the assistant wrote (iOS AssistantScheduleText.symbol)
  const s = task.title ?? "";
  if (s.includes("早")) return "sun"; // i18n-ignore
  if (s.includes("晚") || s.includes("回顾")) return "moon"; // i18n-ignore
  if (s.includes("周报") || s.includes("报告")) return "read"; // i18n-ignore
  if (s.includes("会")) return "chat"; // i18n-ignore
  if (s.includes("盘点") || s.includes("待办")) return "todo"; // i18n-ignore
  if (s.includes("巡检") || s.includes("检查")) return "search"; // i18n-ignore
  return task.repeat === "once" ? "bell" : task.repeat === "interval" ? "loop" : "clock";
}

const RUNS_SHOWN = 30;

export function Routines({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const timezone = (assistant as { timezone?: string } | null)?.timezone;
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Task | null>(null);
  const [allRuns, setAllRuns] = useState(false);
  const menu = useMenu();
  const nowMs = useNow(30_000);
  const now = useMemo(() => new Date(nowMs), [nowMs]);
  const seq = useRef(0);

  const load = useCallback(async () => {
    const mine = ++seq.current;
    const [task, r] = await Promise.allSettled([api<{ tasks: Task[] }>("/assistant/tasks"), api<{ runs: Run[] }>("/assistant/runs")]);
    if (mine !== seq.current) return; // a newer load (a toggle's) won
    if (task.status === "fulfilled") {
      setTasks(task.value.tasks ?? []);
      setError("");
    } else setError(errorText(task.reason));
    if (r.status === "fulfilled") setRuns(r.value.runs ?? []);
  }, []);

  useEffect(() => {
    void load();
    // the hub says when the list changed (a run, Persona scheduling something, another device)
    let timer: number | undefined;
    const off = onHubEvent("assistant_schedule", () => {
      clearTimeout(timer);
      timer = window.setTimeout(() => void load(), 300);
    });
    return () => {
      off();
      clearTimeout(timer);
    };
  }, [load]);

  const toggle = async (task: Task, on: boolean) => {
    const next = { ...task, enabled: on };
    setTasks((list) => list?.map((x) => (x.id === task.id ? next : x)) ?? list);
    seq.current++; // a load already on its way would put the old value back
    try {
      const r = await api<{ task: Task }>(`/assistant/tasks/${enc(task.id)}`, { method: "PUT", json: taskBody(next, task) });
      setTasks((list) => list?.map((x) => (x.id === task.id ? r.task : x)) ?? list);
    } catch (e) {
      toast(errorText(e), "error");
      void load();
    }
  };

  const saved = (task: Task) => {
    setTasks((list) => (list ? (list.some((x) => x.id === task.id) ? list.map((x) => (x.id === task.id ? task : x)) : [...list, task]) : [task]));
    void load();
  };
  const deleted = (task: Task) => {
    setTasks((list) => list?.filter((x) => x.id !== task.id) ?? list);
    void load();
  };

  const items: MenuItem[] = [
    ...TEMPLATES.map((tpl) => ({ key: tpl.key, label: tpl.title, icon: taskIcon(fromTemplate(tpl)), hint: tpl.repeat === "interval" ? t("assistant-routines.every_30_min") : undefined, onSelect: () => setEditing(fromTemplate(tpl)) })),
    { key: "blank", label: t("assistant-routines.blank"), icon: "pencil" as IconName, onSelect: () => setEditing(blankTask()) },
  ];

  const { repeating, oneOff } = splitTasks(tasks ?? []);
  const next = upcoming(tasks ?? []);
  const browserTZ = Intl.DateTimeFormat().resolvedOptions().timeZone;

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("assistant-routines.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink">{t("assistant-routines.title")}</h1>
        <IconButton icon="retry" label={t("assistant-routines.refresh")} onClick={() => void load()} />
        <span className="inline-flex" onClick={menu.open}>
          <Button tone="primary" icon="plus">{t("assistant-routines.new")}</Button>
        </span>
        {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} items={items} align="right" header={t("assistant-routines.from_template")} width={220} />}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[1120px] flex-col gap-6 px-4 py-6 sm:px-8 lg:flex-row lg:items-start">
          <div className="min-w-0 flex-1">
            {tasks === null && !error && (
              <div className="flex justify-center py-16">
                <Loading />
              </div>
            )}
            {error && tasks === null && (
              <Empty icon="info" title={t("assistant-routines.load_failed")}>
                {error}
              </Empty>
            )}
            {tasks !== null && tasks.length === 0 && (
              <Empty icon="clock" title={t("assistant-routines.empty_title")}>
                {t("assistant-routines.empty_body")}
              </Empty>
            )}
            {repeating.length > 0 && (
              <section className="mb-6">
                <SectionTitle>{t("assistant-routines.section_scheduled")}</SectionTitle>
                <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                  {repeating.map((task) => (
                    <TaskRow key={task.id} task={task} now={now} onOpen={() => setEditing(task)} onToggle={(on) => void toggle(task, on)} />
                  ))}
                </div>
                <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">
                  {timezone && browserTZ && timezone !== browserTZ
                    ? t("assistant-routines.scheduled_footer_browser", { zone: timezone, browser: browserTZ })
                    : timezone
                      ? t("assistant-routines.scheduled_footer_zone", { zone: timezone })
                      : t("assistant-routines.scheduled_footer_local")}
                </p>
              </section>
            )}
            {oneOff.length > 0 && (
              <section className="mb-6">
                <SectionTitle>{t("assistant-routines.section_follow_ups")}</SectionTitle>
                <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                  {oneOff.map((task) => (
                    <TaskRow key={task.id} task={task} now={now} onOpen={() => setEditing(task)} onToggle={(on) => void toggle(task, on)} />
                  ))}
                </div>
                <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">{t("assistant-routines.follow_ups_footer")}</p>
              </section>
            )}
          </div>

          <aside className="w-full shrink-0 lg:sticky lg:top-6 lg:w-[340px]">
            {next.length > 0 && (
              <section className="mb-6">
                <SectionTitle>{t("assistant-routines.section_upcoming")}</SectionTitle>
                <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                  {next.map((u) => (
                    <div key={u.id} className="flex items-center gap-3 px-3.5 py-2.5">
                      <div className="w-[52px] shrink-0">
                        <div className="text-[15px] leading-tight font-semibold text-ink tabular-nums">{hm(u.at)}</div>
                        <div className="text-[11px] text-ink-3">{dayLabel(u.at, now)}</div>
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <Icon name={u.repeating ? "loop" : "bell"} size={12} stroke={2.2} className="shrink-0 text-accent-ink" />
                          <span className="truncate text-[13px] font-medium text-ink">{u.title}</span>
                        </div>
                        <div className="truncate text-[11.5px] text-ink-3">{u.detail} · {untilLabel(u.at, now)}</div>
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            )}
            <section>
              <SectionTitle right={runs && runs.length > 0 ? <span className="text-[11px] text-ink-3 tabular-nums">{t("assistant-routines.run_count", { count: runs.length })}</span> : undefined}>{t("assistant-routines.section_runs")}</SectionTitle>
              {runs === null ? (
                <div className="rounded-card bg-surface px-4 py-6 shadow-card">
                  <Loading />
                </div>
              ) : runs.length === 0 ? (
                <div className="rounded-card bg-surface px-4 py-6 text-center text-[12.5px] leading-relaxed text-ink-3 shadow-card">
                  {t("assistant-routines.runs_empty")}
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  {runDays(allRuns ? runs : runs.slice(0, RUNS_SHOWN), now).map((d) => (
                    <div key={d.day}>
                      <div className="mb-1 px-1 text-[11px] font-medium text-ink-3">{d.day}</div>
                      <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                        {d.runs.map((r) => (
                          <RunLine key={r.id} run={r} now={now} showTitle onOpen={r.session_id ? () => navigate({ name: "jarvis", sid: r.session_id! }) : undefined} />
                        ))}
                      </div>
                    </div>
                  ))}
                  {!allRuns && runs.length > RUNS_SHOWN && (
                    <Button tone="quiet" onClick={() => setAllRuns(true)}>{t("assistant-routines.show_all_runs", { count: runs.length })}</Button>
                  )}
                </div>
              )}
            </section>
          </aside>
        </div>
      </div>

      {editing && (
        <TaskEditor
          key={editing.id || "new"}
          task={editing}
          machineID={assistant?.machine_id}
          defaultLauncher={assistant?.launcher}
          onClose={() => setEditing(null)}
          onSaved={saved}
          onDeleted={deleted}
        />
      )}
    </div>
  );
}

/** One task: its mark, title, rule in words, next and last run, what it runs with when not the default, a switch. */
function TaskRow({ task, now, onOpen, onToggle }: { task: Task; now: Date; onOpen: () => void; onToggle: (on: boolean) => void }) {
  const with_ = runsWith(task);
  const last = lastRunText(task, now);
  const next = nextText(task, now);
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      aria-label={t("assistant-routines.row_label", { title: taskTitle(task), rule: ruleText(task), next })}
      className="flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors hover:bg-hover focus-visible:bg-hover focus-visible:outline-none"
    >
      <span className={cx("mt-0.5", !task.enabled && "opacity-60")}>
        <IconTile icon={taskIcon(task)} tone={with_?.danger ? "red" : task.enabled ? "accent" : "muted"} />
      </span>
      <div className={cx("min-w-0 flex-1", !task.enabled && "opacity-75")}>
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{taskTitle(task)}</span>
          {task.created_by === "assistant" && task.repeat !== "once" && <Pill tone="accent">{t("assistant-routines.created_by_persona")}</Pill>}
        </div>
        <div className="mt-0.5 truncate text-[12px] text-ink-2">
          {ruleText(task)}
          {/* 跑完推送 on the rule's line, not as a pill: two pills squeezed the title away on a phone */}
          {task.push === "asked" && ` · ${t("assistant-routines.push_asked")}`}
        </div>
        {next && <div className="mt-0.5 truncate text-[11.5px] text-ink-3 tabular-nums">{next}</div>}
        {with_ && (
          <div className={cx("mt-0.5 flex items-center gap-1 truncate text-[11.5px]", with_.danger ? "font-medium text-red" : "text-ink-3")}>
            <Icon name={with_.danger ? "info" : "gear"} size={11} className="shrink-0" />
            <span className="truncate">{with_.text}</span>
          </div>
        )}
        {last && (
          <div className={cx("mt-0.5 flex items-center gap-1 text-[11.5px]", last.failed ? "text-red" : "text-ink-3")}>
            <Icon name={last.failed ? "x" : "check"} size={11} stroke={2.4} className="shrink-0" />
            <span className="truncate tabular-nums">{last.failed && task.last_error ? t("assistant-routines.last_failed", { text: last.text, error: task.last_error }) : last.text}</span>
          </div>
        )}
      </div>
      <span className="mt-1.5">
        <Switch checked={task.enabled} label={task.enabled ? t("assistant-routines.turn_off", { title: taskTitle(task) }) : t("assistant-routines.turn_on", { title: taskTitle(task) })} onChange={onToggle} />
      </span>
    </div>
  );
}
