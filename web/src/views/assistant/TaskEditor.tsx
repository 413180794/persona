// One scheduled task in a dialog (the iOS app's AssistantTaskEditor): when (once / every day / every N minutes), the
// prompt, what a run starts with (launcher, permission mode, effort; "" = the assistant's default), which conversation
// runs go into, 立即运行, the last result and the task's own runs. A new one has 取消 / 添加, so trying a template and
// backing out creates nothing; an existing one is saved with 保存 and deleted after an inline confirmation.
import { useEffect, useMemo, useState } from "react";
import { api, ApiError, enc, errorText } from "../../lib/api";
import { navigate } from "../../lib/router";
import {
  BYPASS, DAY_NAMES, EFFORTS, everyLabel, fromLocalInput, INTERVALS, lastRunText, MAX_EVERY, MIN_EVERY, momentLabel, nextRun, normalizeDays,
  parseTime, PERMISSION_MODES, repeatsLabel, runDetail, runStatusWord, sameTask, taskBody, taskTitle, toLocalInput, untilLabel, validateTask,
  WEEKEND, withRepeat, WORKDAYS, type Repeat, type Run, type Task,
} from "../../lib/routines";
import { Button, cx, Dialog, Spinner, toast, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Field, inputClass, Segmented, SectionTitle, Switch, textareaClass } from "./parts";

/** Brings an inline confirmation into view once, when it appears (stable, so not on every render). */
const reveal = (el: HTMLElement | null) => el?.scrollIntoView({ block: "nearest", behavior: "smooth" });

const everyOk = (m?: number) => Number.isInteger(m) && (m ?? 0) >= MIN_EVERY && (m ?? 0) <= MAX_EVERY;

interface LauncherOption {
  name: string;
  model?: string;
  agent?: string;
}

export function TaskEditor({
  task,
  machineID,
  defaultLauncher,
  onClose,
  onSaved,
  onDeleted,
}: {
  task: Task;
  /** The assistant's machine: its launchers are the ones a run can start. */
  machineID?: string;
  defaultLauncher?: string;
  onClose: () => void;
  onSaved: (t: Task) => void;
  onDeleted: (t: Task) => void;
}) {
  const creating = !task.id;
  const [draft, setDraft] = useState<Task>(task);
  const [saved, setSaved] = useState<Task | null>(creating ? null : task);
  const [busy, setBusy] = useState<"" | "save" | "run" | "delete" | "fresh">("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [askBypass, setAskBypass] = useState(false);
  const [launchers, setLaunchers] = useState<LauncherOption[] | null>(null);
  const [central, setCentral] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const nowMs = useNow(30_000);
  const now = useMemo(() => new Date(nowMs), [nowMs]);
  /** The conversation it was scheduled from: 接着原来的对话 can be switched off (and back on before saving). */
  const origin = task.session_id;

  useEffect(() => {
    if (!machineID) return;
    let live = true;
    api<{ launchers: LauncherOption[] }>(`/machines/${enc(machineID)}/launchers`)
      .then((r) => live && setLaunchers((r.launchers ?? []).filter((l) => !l.agent || l.agent === "claude" || l.agent === "codex")))
      .catch(() => live && setLaunchers([]));
    return () => {
      live = false;
    };
  }, [machineID]);

  useEffect(() => {
    let live = true;
    api("/model-config").then(() => live && setCentral(true), () => {});
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    if (!saved?.id) return;
    let live = true;
    api<{ runs: Run[] }>(`/assistant/runs?ref=${enc(saved.id)}`)
      .then((r) => live && setRuns(r.runs ?? []))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [saved?.id, saved?.last_run]);

  const problem = validateTask(draft, now);
  const changed = !saved || !sameTask(saved, draft);
  const set = (patch: Partial<Task>) => setDraft((d) => ({ ...d, ...patch }));
  const working = busy !== "";

  /** Saves what changed (adds a new one); returns the task as the hub has it, null when it didn't. */
  const persist = async (): Promise<Task | null> => {
    if (saved && !changed) return saved;
    if (problem) {
      toast(problem, "error");
      return null;
    }
    const body = taskBody(draft, saved);
    const r = saved
      ? await api<{ task: Task }>(`/assistant/tasks/${enc(saved.id)}`, { method: "PUT", json: body })
      : await api<{ task: Task }>("/assistant/tasks", { method: "POST", json: body });
    setSaved(r.task);
    setDraft((d) => ({ ...d, id: r.task.id }));
    onSaved(r.task);
    return r.task;
  };

  const save = async () => {
    setBusy("save");
    try {
      if (await persist()) {
        toast(creating ? `已添加「${taskTitle(draft)}」` : "已保存", "ok");
        onClose();
      }
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };

  const runNow = async () => {
    setBusy("run");
    try {
      const t = await persist();
      if (!t) return;
      const r = await api<{ session_id: string; machine_id: string }>(`/assistant/tasks/${enc(t.id)}/run`, { method: "POST", json: {} });
      toast(`「${taskTitle(t)}」开始运行`, "ok");
      onClose();
      navigate({ name: "jarvis", sid: r.session_id });
    } catch (e) {
      toast(e instanceof ApiError && e.status === 409 ? e.message || "上一轮还在运行，等它跑完再试" : errorText(e), "error");
    } finally {
      setBusy("");
    }
  };

  const startOver = async () => {
    if (!saved) return;
    setBusy("fresh");
    try {
      const r = await api<{ task: Task }>(`/assistant/tasks/${enc(saved.id)}/fresh`, { method: "POST", json: {} });
      setSaved((s) => (s ? { ...s, run_machine_id: r.task.run_machine_id, run_session_id: r.task.run_session_id, run_launcher: r.task.run_launcher } : s));
      onSaved({ ...saved, ...r.task });
      toast("下次运行会开一段新对话", "ok");
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };

  const remove = async () => {
    if (!saved) return;
    setBusy("delete");
    try {
      await api(`/assistant/tasks/${enc(saved.id)}`, { method: "DELETE" });
      toast(`已删除「${taskTitle(saved)}」`, "ok");
      onDeleted(saved);
      onClose();
    } catch (e) {
      toast(errorText(e), "error");
      setBusy("");
    }
  };

  const open = (sid: string) => {
    onClose();
    navigate({ name: "jarvis", sid });
  };

  const close = () => {
    if (!working) onClose();
  };

  const footer = confirmDelete ? (
    <>
      <span className="mr-auto min-w-0 text-[12px] text-ink-2">删除后不再定时运行，已经跑过的对话还在。</span>
      <Button tone="quiet" disabled={working} onClick={() => setConfirmDelete(false)}>取消</Button>
      <Button tone="danger" busy={busy === "delete"} onClick={() => void remove()}>删除</Button>
    </>
  ) : (
    <>
      {!creating && (
        <Button tone="quiet" icon="trash" className="text-red enabled:hover:text-red" disabled={working} onClick={() => setConfirmDelete(true)}>删除</Button>
      )}
      <span className={cx("mr-auto min-w-0 truncate text-[11.5px]", problem ? "text-red" : "text-ink-3")}>{changed && problem ? problem : ""}</span>
      <Button tone="quiet" disabled={working} onClick={onClose}>取消</Button>
      <Button tone="primary" busy={busy === "save"} disabled={working || !!problem || (!creating && !changed)} onClick={() => void save()}>{creating ? "添加" : "保存"}</Button>
    </>
  );

  const next = nextRun(saved ?? draft);
  const last = saved ? lastRunText(saved, now) : null;
  const own = saved?.run_session_id;

  return (
    <Dialog open onClose={close} width={600} title={creating ? "新建定时任务" : taskTitle(saved ?? draft)} footer={footer}>
      <div className="-mx-4 max-h-[min(64vh,680px)] overflow-y-auto px-4 pb-1">
        <div className="flex flex-col gap-5">
          <div className="flex items-end gap-3">
            <div className="min-w-0 flex-1">
              <Field label="标题">
                <input
                  className={inputClass}
                  value={draft.title}
                  autoFocus={creating}
                  placeholder={draft.repeat === "once" ? "可不填" : "比如「早报」"}
                  onChange={(e) => set({ title: e.target.value })}
                />
              </Field>
            </div>
            <label className="flex h-9 shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap text-[12.5px] text-ink-2">
              开启
              <Switch checked={draft.enabled} label="开启" onChange={(on) => set({ enabled: on })} />
            </label>
          </div>

          <section>
            <SectionTitle>什么时候</SectionTitle>
            <div className="flex flex-col gap-3 rounded-card bg-inset p-3 shadow-hairline">
              <Segmented<Repeat>
                label="重复"
                value={draft.repeat}
                onChange={(r) => setDraft((d) => withRepeat(d, r, new Date()))}
                options={[
                  { v: "once", label: "一次" },
                  { v: "daily", label: "每天" },
                  { v: "interval", label: "每隔" },
                ]}
              />
              {draft.repeat === "once" && <OnceWhen draft={draft} now={now} onChange={(at) => set({ at })} />}
              {draft.repeat === "daily" && <DailyWhen draft={draft} onChange={set} />}
              {draft.repeat === "interval" && <IntervalWhen minutes={draft.every_minutes ?? 0} onChange={(m) => set({ every_minutes: m })} />}
            </div>
            <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">
              {draft.enabled && next && saved && !changed
                ? `下次运行：${momentLabel(next, now)}（${untilLabel(next, now)}）`
                : draft.repeat === "once"
                  ? "到点运行一次，跑完就从列表里拿掉（运行记录里还在）。"
                  : draft.repeat === "daily"
                    ? `${repeatsLabel(draft.weekdays)} ${draft.time || "--:--"}，按你所在的时区。`
                    : everyOk(draft.every_minutes)
                      ? `${everyLabel(draft.every_minutes ?? 0)}运行一次，从保存时算起。`
                      : "每隔几分钟运行一次，从保存时算起。"}
            </p>
          </section>

          <section>
            <SectionTitle>提示词</SectionTitle>
            <textarea
              className={textareaClass}
              rows={6}
              value={draft.prompt}
              placeholder="要 Jarvis 做什么"
              aria-label="要 Jarvis 做什么"
              onChange={(e) => set({ prompt: e.target.value })}
            />
            <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">像在对话里一样写：看哪些数据、怎么汇报。它可以用 schedule_followup 再安排一次性的跟进，比如会前 10 分钟。</p>
          </section>

          <section>
            <SectionTitle>运行方式</SectionTitle>
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="模型（启动器）">
                <select className={inputClass} value={draft.launcher ?? ""} onChange={(e) => set({ launcher: e.target.value || undefined })}>
                  <option value="">助手默认{defaultLauncher ? `（${defaultLauncher}）` : ""}</option>
                  {draft.launcher && !launchers?.some((l) => l.name === draft.launcher) && <option value={draft.launcher}>{draft.launcher}</option>}
                  {(launchers ?? []).map((l) => (
                    <option key={l.name} value={l.name}>{l.model ? `${l.name}（${l.model}）` : l.name}</option>
                  ))}
                </select>
              </Field>
              <Field label="权限模式">
                <select
                  className={draft.permission_mode === BYPASS ? inputClass.replace(" text-ink ", " font-medium text-red ") : inputClass}
                  value={askBypass ? BYPASS : draft.permission_mode === "default" ? "" : draft.permission_mode ?? ""}
                  onChange={(e) => {
                    const v = e.target.value;
                    if (v === BYPASS && draft.permission_mode !== BYPASS) setAskBypass(true);
                    else {
                      setAskBypass(false);
                      set({ permission_mode: v || undefined });
                    }
                  }}
                >
                  <option value="">默认（先问你）</option>
                  {draft.permission_mode && draft.permission_mode !== "default" && !PERMISSION_MODES.some((m) => m.id === draft.permission_mode) && (
                    <option value={draft.permission_mode}>{draft.permission_mode}</option>
                  )}
                  {PERMISSION_MODES.map((m) => (
                    <option key={m.id} value={m.id}>{m.title}</option>
                  ))}
                </select>
              </Field>
              <Field label="思考强度">
                <select className={inputClass} value={draft.effort ?? ""} onChange={(e) => set({ effort: e.target.value || undefined })}>
                  <option value="">助手默认</option>
                  {draft.effort && !EFFORTS.some((x) => x.id === draft.effort) && <option value={draft.effort}>{draft.effort}</option>}
                  {EFFORTS.map((x) => (
                    <option key={x.id} value={x.id}>{x.title}</option>
                  ))}
                </select>
              </Field>
            </div>
            {askBypass ? (
              <div ref={reveal} role="alertdialog" aria-label="全部放行？" className="mt-3 rounded-card bg-red-tint p-3">
                <p className="text-[13px] font-medium text-red">全部放行？</p>
                <p className="mt-1 text-[12px] leading-relaxed text-ink-2">
                  这个任务运行时，跑任何命令、改任何文件、用任何工具都不再问你，也没有人能中途拦下。只给你信得过、出了错也能接受的提示词用。
                </p>
                <div className="mt-2.5 flex justify-end gap-2">
                  <Button tone="quiet" onClick={() => setAskBypass(false)}>取消</Button>
                  <Button
                    tone="danger"
                    onClick={() => {
                      setAskBypass(false);
                      set({ permission_mode: BYPASS });
                    }}
                  >
                    全部放行
                  </Button>
                </div>
              </div>
            ) : draft.permission_mode === BYPASS ? (
              <p className="mt-1.5 flex items-center gap-1.5 px-1 text-[11.5px] font-medium text-red">
                <Icon name="info" size={13} />
                全部放行：运行时跑命令、改文件都不再问你。
              </p>
            ) : (
              <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">
                空着就是助手的默认。定时任务在后台跑，要你批准时会推送到手机，没人点就一直等着；要它跑命令也不问你，可以选「自动」（自己判断风险）。
                {draft.session_id ? "接着原来的对话运行时，那段对话也会换成这里的设置。" : ""}
              </p>
            )}
            {central && (
              <button
                type="button"
                onClick={() => {
                  onClose();
                  navigate(`/settings?tab=model-config&purpose=${enc(saved ? `task.${saved.id}` : "schedule")}`);
                }}
                className="mt-1 inline-flex items-center gap-1 whitespace-nowrap px-1 text-[11.5px] text-accent-ink hover:underline"
              >
                {saved ? "在「模型配置」里设置这个任务的具体模型" : "在「模型配置」里设置定时任务的默认模型"}
                <Icon name="chevronRight" size={11} stroke={2.2} />
              </button>
            )}
          </section>

          <section>
            <SectionTitle>对话</SectionTitle>
            <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
              {origin && (
                <div className="flex items-center gap-3 px-3.5 py-2.5">
                  <span className="min-w-0 flex-1 truncate text-[13px] text-ink">接着原来的对话</span>
                  <Switch checked={!!draft.session_id} label="接着原来的对话" onChange={(on) => set({ session_id: on ? origin : undefined })} />
                </div>
              )}
              {!draft.session_id &&
                (own ? (
                  <>
                    <LinkRow icon="chat" label="在同一个对话里运行" onClick={() => open(own)} />
                    <LinkRow icon="plus" label="下次开新对话" busy={busy === "fresh"} disabled={working} onClick={() => void startOver()} chevron={false} />
                  </>
                ) : (
                  <div className="flex items-center gap-2.5 px-3.5 py-2.5 text-[13px] text-ink-2">
                    <Icon name="chat" size={14} className="text-ink-3" />
                    在同一个对话里运行
                  </div>
                ))}
            </div>
            <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">
              {draft.session_id
                ? "每次运行都发进安排它的那段对话，带着那里的上下文接着做。"
                : own
                  ? "每次运行都发进这个任务自己的对话，能看到前几次的结果。对话太长或想重来时，点「下次开新对话」，原来的对话还在。"
                  : "第一次运行时开一段新对话，之后每次都在这段对话里接着跑，能看到前几次的结果。"}
            </p>
          </section>

          <section>
            <div className="flex flex-wrap items-center gap-2">
              <Button tone="secondary" icon="play" busy={busy === "run"} disabled={working || !!problem} onClick={() => void runNow()}>立即运行</Button>
              {saved?.last_session_id && saved.last_session_id !== own && (
                <Button tone="quiet" icon="chat" disabled={working} onClick={() => open(saved.last_session_id!)}>查看上次结果</Button>
              )}
              {last && <span className={cx("text-[11.5px] tabular-nums", last.failed ? "text-red" : "text-ink-3")}>{last.text}</span>}
            </div>
            <p className={cx("mt-1.5 px-1 text-[11.5px] leading-relaxed", saved?.last_status === "failed" && saved.last_error ? "text-red" : "text-ink-3")}>
              {saved?.last_status === "failed" && saved.last_error
                ? `上次没有跑起来：${saved.last_error}`
                : `立即运行不改它的时间，${creating || changed ? "先保存再运行，" : ""}跑完在 Jarvis 的对话里看结果。`}
            </p>
          </section>

          {runs.length > 0 && (
            <section>
              <SectionTitle right={runs.length > 5 ? <span className="text-[11px] text-ink-3">共 {runs.length} 次</span> : undefined}>历史记录</SectionTitle>
              <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                {runs.slice(0, 5).map((r) => (
                  <RunLine key={r.id} run={r} now={now} onOpen={r.session_id ? () => open(r.session_id!) : undefined} />
                ))}
              </div>
            </section>
          )}
        </div>
      </div>
    </Dialog>
  );
}

function LinkRow({ icon, label, onClick, busy, disabled, chevron = true }: { icon: "chat" | "plus"; label: string; onClick: () => void; busy?: boolean; disabled?: boolean; chevron?: boolean }) {
  return (
    <button type="button" disabled={disabled} onClick={onClick} className="flex w-full items-center gap-2.5 whitespace-nowrap px-3.5 py-2.5 text-left text-[13px] text-accent-ink transition-colors enabled:hover:bg-hover disabled:opacity-50">
      <Icon name={icon} size={14} />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {busy ? <Spinner /> : chevron ? <Icon name="chevronRight" size={12} stroke={2} className="text-ink-3" /> : null}
    </button>
  );
}

/** One run in a task's history: when, how it went. */
export function RunLine({ run, now, onOpen, showTitle }: { run: Run; now: Date; onOpen?: () => void; showTitle?: boolean }) {
  const at = parseTime(run.at);
  const failed = run.status !== "ok" && run.status !== "skipped";
  const body = (
    <>
      <RunMark status={run.status} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-ink">{showTitle ? run.title : at ? momentLabel(at, now) : runStatusWord(run)}</span>
        <span className={cx("block truncate text-[11.5px]", failed ? "text-red" : "text-ink-3")}>
          {showTitle && at ? `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")} · ` : ""}
          {runDetail(run)}
        </span>
      </span>
      {onOpen && <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />}
    </>
  );
  return onOpen ? (
    <button type="button" onClick={onOpen} title="打开这次运行的对话" className="flex w-full items-center gap-2.5 px-3.5 py-2 text-left transition-colors hover:bg-hover">
      {body}
    </button>
  ) : (
    <div className="flex items-center gap-2.5 px-3.5 py-2">{body}</div>
  );
}

export function RunMark({ status }: { status: string }) {
  if (status === "ok")
    return (
      <span aria-label="已运行" role="img" className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-green text-white">
        <Icon name="check" size={11} stroke={3} />
      </span>
    );
  if (status === "skipped") return <span aria-label="已跳过" role="img" className="size-[18px] shrink-0 rounded-full border-[1.5px] border-line-strong" />;
  return (
    <span aria-label="失败" role="img" className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-red text-white">
      <Icon name="x" size={11} stroke={3} />
    </span>
  );
}

function OnceWhen({ draft, now, onChange }: { draft: Task; now: Date; onChange: (at: string | undefined) => void }) {
  const value = toLocalInput(parseTime(draft.at));
  return (
    <Field label="时间">
      <input
        type="datetime-local"
        className={cx(inputClass, "tabular-nums")}
        value={value}
        min={toLocalInput(now)}
        onChange={(e) => onChange(fromLocalInput(e.target.value)?.toISOString())}
      />
    </Field>
  );
}

function DailyWhen({ draft, onChange }: { draft: Task; onChange: (p: Partial<Task>) => void }) {
  const days = normalizeDays(draft.weekdays);
  const selected = new Set(days.length ? days : [1, 2, 3, 4, 5, 6, 7]);
  const toggle = (d: number) => {
    const s = new Set(selected);
    if (s.has(d)) {
      if (s.size === 1) return; // no days would read as every day
      s.delete(d);
    } else s.add(d);
    onChange({ weekdays: normalizeDays([...s]) });
  };
  const shortcut = (label: string, value: number[]) => {
    const on = repeatsLabel(days) === repeatsLabel(value);
    return (
      <button
        key={label}
        type="button"
        aria-pressed={on}
        onClick={() => onChange({ weekdays: value })}
        className={cx("h-6 whitespace-nowrap rounded-full px-2.5 text-[11.5px] font-medium transition-colors", on ? "bg-accent-tint text-accent-ink" : "bg-field text-ink-3 hover:text-ink-2")}
      >
        {label}
      </button>
    );
  };
  return (
    <div className="flex flex-col gap-3">
      <Field label="时间">
        <input
          type="time"
          className={cx(inputClass.replace("w-full", "w-36"), "tabular-nums")}
          value={draft.time ?? ""}
          onChange={(e) => onChange({ time: e.target.value.slice(0, 5) })}
        />
      </Field>
      <div>
        <span className="mb-1.5 block text-[12px] font-medium text-ink-2">哪几天</span>
        <div className="flex flex-wrap items-center gap-1.5">
          {DAY_NAMES.map((n, i) => {
            const on = selected.has(i + 1);
            return (
              <button
                key={n}
                type="button"
                aria-pressed={on}
                aria-label={`周${n}`}
                onClick={() => toggle(i + 1)}
                className={cx(
                  "flex size-8 items-center justify-center rounded-full text-[12.5px] font-medium transition-[background-color,color,transform] duration-150 active:scale-[0.94]",
                  on ? "bg-accent text-white" : "bg-field text-ink-2 hover:bg-hover",
                )}
              >
                {n}
              </button>
            );
          })}
          <span className="mx-1 hidden h-4 w-px bg-line sm:block" />
          {shortcut("每天", [])}
          {shortcut("工作日", WORKDAYS)}
          {shortcut("周末", WEEKEND)}
        </div>
      </div>
    </div>
  );
}

function IntervalWhen({ minutes, onChange }: { minutes: number; onChange: (m: number) => void }) {
  const [text, setText] = useState(String(minutes || ""));
  useEffect(() => setText(String(minutes || "")), [minutes]);
  const bad = !everyOk(minutes);
  return (
    <div className="flex flex-col gap-2">
      <Field label="间隔（分钟）" note={bad ? "间隔要在 5 分钟到 7 天（10080 分钟）之间" : undefined} error={bad}>
        <input
          type="number"
          inputMode="numeric"
          min={MIN_EVERY}
          max={MAX_EVERY}
          step={1}
          className={cx(inputClass.replace("w-full", "w-36"), "tabular-nums", bad && "shadow-[inset_0_0_0_1px_var(--red)]")}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            const n = Number(e.target.value);
            onChange(e.target.value.trim() === "" || !Number.isFinite(n) ? 0 : n);
          }}
        />
      </Field>
      <div className="flex flex-wrap gap-1.5">
        {INTERVALS.map((m) => (
          <button
            key={m}
            type="button"
            aria-pressed={m === minutes}
            onClick={() => onChange(m)}
            className={cx("h-6 whitespace-nowrap rounded-full px-2.5 text-[11.5px] font-medium transition-colors", m === minutes ? "bg-accent-tint text-accent-ink" : "bg-field text-ink-3 hover:text-ink-2")}
          >
            {m < 60 ? `${m} 分钟` : m % 1440 === 0 ? `${m / 1440} 天` : `${m / 60} 小时`}
          </button>
        ))}
      </div>
    </div>
  );
}
