// Start an agent on one of your computers: machine, folder (recent ones first), launcher, model and effort for this
// start, the first message.
import { useEffect, useMemo, useRef, useState } from "react";
import { hubLabel } from "../lib/mesh";
import { api, ApiError, enc, errorText } from "../lib/api";
import { pickAttachment, releaseAttachments, type LocalAttachment } from "../lib/attachments";
import { launchAgent, presetPrompt, type NewAgentPreset } from "../lib/new-agent";
import { AttachmentTray } from "../ui/AttachmentTray";
import { modelPurpose, modelSummary, type ModelBinding } from "../lib/model-config";
import {
  defaultDetail, defaultModel, effortChoices, fitEffort, followedBinding, launcherAgent, launcherModels, modelDetail, modelName, modelRows,
  pickerLauncher, startPicks, takesEffort, type LauncherModels,
} from "../lib/launcher-models";
import { EFFORTS, effortTitle } from "../lib/session-actions";
import { baseName, shortPath } from "../lib/format";
import { useStore } from "../lib/store";
import type { Launcher } from "../lib/types";
import { Button, Collapse, cx, Dialog, IconButton, Spinner, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

interface Dirs {
  path: string;
  parent?: string;
  entries: { name: string; path: string; is_git?: boolean }[];
  recent?: { path: string; name: string; last_used?: string }[];
}

/** The picker's list for the launcher that runs: idle (no launcher to ask), loading, the answer (null: an older hub), failed. */
interface ModelList {
  status: "idle" | "loading" | "ready" | "error";
  list?: LauncherModels | null;
  error?: string;
}

export function NewAgent({ open, onClose, preset }: { open: boolean; onClose: () => void; /** Filled in when another page opens it (知识库's 开 Agent). */ preset?: NewAgentPreset | null }) {
  const machines = useStore((s) => s.machines);
  const online = useMemo(() => machines.filter((m) => m.online), [machines]);
  const [mid, setMid] = useState(() => localStorage.getItem("new.machine") ?? "");
  const [cwd, setCwd] = useState("");
  const [launcher, setLauncher] = useState("");
  const [launchers, setLaunchers] = useState<Launcher[]>([]);
  const [unified, setUnified] = useState(true);
  const [modelDefault, setModelDefault] = useState("");
  // 模型配置's 新建 Agent as it applies now (provider, model, effort); null: the hub has no 模型配置
  const [configured, setConfigured] = useState<ModelBinding | null>(null);
  const [loadingModels, setLoadingModels] = useState(false);
  const [launchersOf, setLaunchersOf] = useState(""); // the machine `launchers` and `configured` are for
  // this start's model (an id from the list; "": the default) and effort ("": the launcher's), never saved
  const [models, setModels] = useState<ModelList>({ status: "idle" });
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [picking, setPicking] = useState(false);
  const [query, setQuery] = useState("");
  const [reload, setReload] = useState(0);
  const refreshModels = useRef(false);
  const search = useRef<HTMLInputElement>(null);
  const [dirs, setDirs] = useState<Dirs | null>(null);
  const [browse, setBrowse] = useState("");
  const [loadingDirs, setLoadingDirs] = useState(false);
  const [prompt, setPrompt] = useState(() => localStorage.getItem("draft:new-agent") ?? "");
  const [attachments, setAttachments] = useState<LocalAttachment[]>([]);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const submitted = useRef(false);
  // a preset's folder, put in once its computer is the chosen one (choosing a computer loads that one's last folder)
  const presetCwd = useRef<{ mid: string; cwd: string } | null>(null);
  const attachmentsRef = useRef(attachments);
  attachmentsRef.current = attachments;

  useEffect(() => {
    if (!open) return;
    submitted.current = false;
    setEffort("");
  }, [open]);
  useEffect(() => {
    if (!open || !preset) return;
    if (preset.prompt) setPrompt((draft) => presetPrompt(preset.prompt, draft));
    if (preset.machineId && preset.cwd && online.some((m) => m.id === preset.machineId)) {
      presetCwd.current = { mid: preset.machineId, cwd: preset.cwd };
      setMid(preset.machineId);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, preset]);
  useEffect(() => {
    if (prompt) localStorage.setItem("draft:new-agent", prompt);
    else localStorage.removeItem("draft:new-agent");
  }, [prompt]);
  useEffect(() => () => releaseAttachments(attachmentsRef.current), []);

  const addFiles = (files: FileList | File[]) => {
    const picked: LocalAttachment[] = [];
    for (const file of Array.from(files)) {
      try { picked.push(pickAttachment(file)); }
      catch (e) { toast(errorText(e), "error"); }
    }
    setAttachments((current) => [...current, ...picked]);
  };

  useEffect(() => {
    // a preset's computer (set just above, in the same commit) is not overridden by the first one online
    if (open && !presetCwd.current && (!mid || !online.some((m) => m.id === mid)) && online[0]) setMid(online[0].id);
  }, [open, online, mid]);

  useEffect(() => {
    if (!open || !mid) return;
    localStorage.setItem("new.machine", mid);
    setLaunchers([]);
    const controller = new AbortController();
    setLauncher("");
    setModelDefault("");
    setConfigured(null);
    setLoadingModels(true);
    Promise.all([
      api<{ launchers: Launcher[] }>(`/machines/${enc(mid)}/launchers`, { signal: controller.signal }),
      modelPurpose("agent.new").catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }),
    ])
      .then(([r, config]) => {
        if (controller.signal.aborted) return;
        const ls = r.launchers ?? [];
        setLaunchers(ls);
        setLaunchersOf(mid);
        setUnified(config !== null);
        if (config) {
          setModelDefault(modelSummary(config.purpose, config.providers));
          setConfigured(config.purpose.effective ?? {});
          setLauncher("");
        } else {
          const saved = localStorage.getItem(`new.launcher.${mid}`);
          setLauncher(ls.some((l) => l.name === saved) ? saved! : (ls[0]?.name ?? ""));
        }
      })
      .catch((e) => { if (!controller.signal.aborted) toast(errorText(e), "error"); })
      .finally(() => { if (!controller.signal.aborted) setLoadingModels(false); });
    const p = presetCwd.current;
    if (p?.mid === mid) presetCwd.current = null;
    setCwd(p?.mid === mid ? p.cwd : (localStorage.getItem(`new.cwd.${mid}`) ?? ""));
    setBrowse("");
    return () => {
      controller.abort();
      setLaunchersOf("");
    };
  }, [open, mid]);

  useEffect(() => {
    if (!open || !mid) return;
    setLoadingDirs(true);
    api<Dirs>(`/machines/${enc(mid)}/dirs?path=${enc(browse)}`)
      .then(setDirs)
      .catch(() => setDirs(null))
      .finally(() => setLoadingDirs(false));
  }, [open, mid, browse]);

  // The model list of the launcher that will run (the chosen one, or 模型配置's); a new machine or launcher starts over
  // from the default. Starting never waits for it.
  const followed = followedBinding(launcher, unified ? configured : null);
  const listLauncher = launchersOf === mid ? pickerLauncher(launcher, followed) : "";
  useEffect(() => {
    setModel("");
    setPicking(false);
    setQuery("");
  }, [open, mid, listLauncher]);
  useEffect(() => {
    if (!open || !mid || !listLauncher) {
      setModels({ status: "idle" });
      return;
    }
    const controller = new AbortController();
    const refresh = refreshModels.current;
    refreshModels.current = false;
    setModels((m) => ({ status: "loading", list: refresh ? m.list : undefined }));
    launcherModels(mid, listLauncher, { refresh, signal: controller.signal })
      .then((list) => {
        if (controller.signal.aborted) return;
        setModels({ status: "ready", list });
        setModel((id) => (list?.models.some((m) => m.id === id) ? id : ""));
      })
      .catch((e) => { if (!controller.signal.aborted) setModels({ status: "error", error: errorText(e) }); });
    return () => controller.abort();
  }, [open, mid, listLauncher, reload]);
  const reloadModels = () => {
    refreshModels.current = true;
    setReload((n) => n + 1);
  };
  useEffect(() => { if (picking) search.current?.focus(); }, [picking]);

  const agent = launcherAgent(listLauncher, launchers, models.list);
  const pickedModel = models.list?.models.find((m) => m.id === model);
  const efforts = effortChoices(models.list, model, followed);
  const effortsKey = efforts.join(" ");
  useEffect(() => { setEffort((e) => fitEffort(e, effortsKey ? effortsKey.split(" ") : [])); }, [effortsKey]);

  const start = () => {
    if (submitted.current || loadingModels || !online.some((m) => m.id === mid) || !cwd.trim()) return;
    submitted.current = true;
    localStorage.setItem(`new.cwd.${mid}`, cwd.trim());
    if (launcher && !unified) localStorage.setItem(`new.launcher.${mid}`, launcher);
    const picks = startPicks({ agent, list: models.list, model, effort, followed });
    launchAgent({ machineId: mid, cwd: cwd.trim(), launcher, ...picks, text: prompt.trim(), attachments });
    attachmentsRef.current = [];
    setAttachments([]);
    setPrompt("");
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="新建 Agent"
      width={520}
      footer={
        <>
          <span className="mr-auto min-w-0 truncate text-[11.5px] text-ink-3">{cwd ? shortPath(cwd) : "选一个目录"}</span>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" disabled={loadingModels || !online.some((m) => m.id === mid) || !cwd.trim()} onClick={start}>
            启动
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="电脑">
          <div className="flex flex-wrap gap-1.5">
            {machines.map((m) => (
              <button
                key={m.id}
                type="button"
                disabled={!m.online}
                onClick={() => setMid(m.id)}
                className={cx(
                  "flex h-8 items-center gap-1.5 rounded-control px-2.5 text-[12.5px] whitespace-nowrap transition-[background-color,box-shadow,color] disabled:opacity-40",
                  mid === m.id ? "bg-ink font-medium text-surface" : "bg-surface text-ink-2 shadow-btn enabled:hover:bg-hover enabled:hover:text-ink",
                )}
              >
                <span className={cx("size-1.5 rounded-full", m.online ? "bg-green" : "bg-ink-3")} />
                {m.name}
                {hubLabel(m, machines) && <span className={mid === m.id ? "opacity-70" : "text-ink-3"}>{hubLabel(m, machines)}</span>}
              </button>
            ))}
          </div>
        </Field>

        <Field label="目录">
          <input
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="~/code/项目"
            spellCheck={false}
            className="h-9 w-full rounded-control bg-inset px-3 font-mono text-[12.5px] text-ink shadow-hairline outline-none placeholder:font-sans placeholder:text-ink-3 focus:shadow-[0_0_0_1px_var(--line-strong)]"
          />
          <div className="mt-1.5 max-h-52 overflow-y-auto rounded-control bg-surface p-1 shadow-hairline">
            {!browse && (dirs?.recent?.length ?? 0) > 0 && (
              <>
                <div className="px-2 pt-1 pb-0.5 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">最近用过</div>
                {dirs!.recent!.slice(0, 6).map((d) => (
                  <DirRow key={d.path} name={d.name || baseName(d.path)} path={d.path} sub={shortPath(d.path)} selected={cwd === d.path} onPick={() => setCwd(d.path)} />
                ))}
              </>
            )}
            <div className="flex items-center gap-1 px-2 pt-1.5 pb-0.5 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">
              {browse && dirs?.parent !== undefined && (
                <button type="button" aria-label="上一级" onClick={() => setBrowse(dirs!.parent!)} className="-ml-1 flex size-4.5 items-center justify-center rounded-[4px] hover:bg-hover hover:text-ink">
                  <Icon name="chevronLeft" size={12} stroke={2.4} />
                </button>
              )}
              <span className="truncate normal-case tracking-normal">{dirs?.path ? shortPath(dirs.path) : "主目录"}</span>
              {loadingDirs && <Spinner size={10} />}
            </div>
            {dirs?.entries?.map((d) => (
              <DirRow key={d.path} name={d.name} path={d.path} git={d.is_git} selected={cwd === d.path} onPick={() => setCwd(d.path)} onOpen={() => setBrowse(d.path)} />
            ))}
          </div>
        </Field>

        {(unified || launchers.length > 1) && (
          <Field label={unified ? "启动器（本次覆盖）" : "启动器"}>
            <div className="flex flex-wrap gap-1.5">
              {unified && <Choice selected={launcher === ""} onClick={() => setLauncher("")} title={modelDefault}>跟随模型配置</Choice>}
              {launchers.map((l) => (
                <Choice key={l.name} mono selected={launcher === l.name} onClick={() => setLauncher(l.name)} title={l.model}>
                  {l.name}
                </Choice>
              ))}
            </div>
            {unified && <p className="mt-1.5 text-[11.5px] text-ink-3">{launcher ? "只覆盖这次新会话，不改变统一默认。" : modelDefault || "使用统一模型配置中的新 Agent 默认值。"}</p>}
          </Field>
        )}

        {listLauncher && models.status !== "idle" && !(models.status === "ready" && !models.list?.models.length) && (
          <ModelPicker
            models={models}
            model={model}
            followed={followed}
            ownModel={launchers.find((l) => l.name === listLauncher)?.model}
            open={picking}
            onToggle={() => setPicking((o) => !o)}
            query={query}
            onQuery={setQuery}
            search={search}
            onPick={(id) => {
              setModel(id);
              setPicking(false);
              setQuery("");
            }}
            onReload={reloadModels}
          />
        )}

        {!loadingModels && takesEffort(agent) && (
          <Field label="思考强度">
            {efforts.length > 0 ? (
              <div className="flex flex-wrap gap-1.5">
                <Choice selected={effort === ""} onClick={() => setEffort("")}>{followed.effort ? "跟随模型配置" : "跟随启动器"}</Choice>
                {efforts.map((id) => (
                  <Choice key={id} selected={effort === id} onClick={() => setEffort(id)} title={EFFORTS.find((e) => e.id === id)?.detail}>
                    {effortTitle(id)}
                  </Choice>
                ))}
              </div>
            ) : (
              <p className="flex h-7 items-center text-[11.5px] text-ink-3">
                {pickedModel ? `${modelName(pickedModel)} 不支持设置思考强度。` : "默认模型不支持设置思考强度。"}
              </p>
            )}
          </Field>
        )}

        <Field label="第一条消息（可选）">
          <div className={cx("flex flex-col gap-2 rounded-control bg-inset shadow-hairline focus-within:shadow-[0_0_0_1px_var(--line-strong)]", dragging && "outline-2 outline-accent")}
            onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setDragging(true); } }}
            onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false); }}
            onDrop={(e) => { e.preventDefault(); setDragging(false); addFiles(e.dataTransfer.files); }}>
            {attachments.length > 0 && <div className="px-3 pt-2"><AttachmentTray attachments={attachments} onRemove={(id) => {
              releaseAttachments(attachments.filter((a) => a.id === id));
              setAttachments((current) => current.filter((a) => a.id !== id));
            }} /></div>}
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onPaste={(e) => { if (e.clipboardData.files.length) { e.preventDefault(); addFiles(e.clipboardData.files); } }}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing || e.keyCode === 229) return;
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); start(); }
              }}
              rows={3}
              aria-label="第一条消息"
              placeholder="要它做什么…（⌘↵ 启动）"
              className="w-full resize-none bg-transparent px-3 py-2 text-[13px] leading-relaxed text-ink outline-none placeholder:text-ink-3"
            />
            <div className="flex items-center gap-2 px-2 pb-2">
              <input ref={fileInput} type="file" multiple hidden onChange={(e) => { if (e.currentTarget.files) addFiles(e.currentTarget.files); e.currentTarget.value = ""; }} />
              <button type="button" aria-label="添加图片或文件" title="添加图片或文件" onClick={() => fileInput.current?.click()} className="flex size-7 shrink-0 items-center justify-center rounded-control text-ink-3 hover:bg-hover hover:text-ink"><Icon name="plus" size={16} stroke={2} /></button>
              <span className="min-w-0 text-[11.5px] text-ink-3">粘贴、拖入图片或文件 · 启动后在后台发送</span>
            </div>
          </div>
        </Field>
      </div>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 text-[12px] font-medium text-ink-2">{label}</div>
      {children}
    </div>
  );
}

function Choice({ selected, onClick, title, mono, children }: { selected: boolean; onClick: () => void; title?: string; mono?: boolean; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      title={title}
      className={cx("h-7 rounded-control px-2.5 text-[12px] whitespace-nowrap transition-colors", mono && "font-mono", selected ? "bg-ink text-surface" : "bg-surface text-ink-2 shadow-btn hover:bg-hover hover:text-ink")}
    >
      {children}
    </button>
  );
}

// The model for this start: a row showing the pick, opening into a searchable list (80–110 models through a gateway).
function ModelPicker({ models, model, followed, ownModel, open, onToggle, query, onQuery, search, onPick, onReload }: {
  models: ModelList;
  model: string;
  followed: ModelBinding;
  /** What the launcher passes as its own --model (shown when it is locked). */
  ownModel?: string;
  open: boolean;
  onToggle: () => void;
  query: string;
  onQuery: (q: string) => void;
  search: React.RefObject<HTMLInputElement | null>;
  onPick: (id: string) => void;
  onReload: () => void;
}) {
  const all = models.list?.models ?? [];
  const locked = models.list?.locked;
  const loading = models.status === "loading";
  const picked = all.find((m) => m.id === model);
  const { showDefault, rows } = modelRows(all, query, followed);
  const pickFirst = () => {
    if (showDefault) onPick("");
    else if (rows[0]) onPick(rows[0].id);
  };
  const detail = picked ? modelDetail(picked) : locked ? (ownModel ?? "") : defaultDetail(all, followed);
  return (
    <Field label="模型">
      <button
        type="button"
        disabled={!!locked}
        aria-expanded={open}
        onClick={onToggle}
        title={picked?.description ?? (picked ? undefined : defaultModel(all)?.description)}
        className="flex h-9 w-full items-center gap-2 rounded-control bg-inset px-3 text-left shadow-hairline outline-none transition-colors enabled:hover:bg-hover focus-visible:shadow-[0_0_0_1px_var(--line-strong)] disabled:cursor-not-allowed"
      >
        <span className={cx("max-w-[60%] shrink-0 truncate text-[12.5px]", locked ? "text-ink-3" : "text-ink")}>{picked ? modelName(picked) : "默认"}</span>
        {loading && !all.length ? (
          <span className="flex min-w-0 flex-1 items-center gap-1.5 text-[11.5px] text-ink-3"><Spinner size={10} /><span className="truncate">正在读取模型列表…</span></span>
        ) : models.status === "error" ? (
          <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">没读到模型列表</span>
        ) : detail ? (
          <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-3">{detail}</span>
        ) : (
          <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">{(picked ?? (locked ? undefined : defaultModel(all)))?.description}</span>
        )}
        <Icon name={locked ? "lock" : "chevron"} size={13} className={cx("shrink-0 text-ink-3 transition-transform duration-200", open && !locked && "rotate-180")} />
      </button>
      {locked && <p className="mt-1.5 text-[11.5px] text-ink-3">{locked}</p>}
      {models.status === "error" && (
        <p className="mt-1.5 flex items-center gap-2 text-[11.5px] text-ink-3">
          <span className="min-w-0 truncate" title={models.error}>{models.error}</span>
          <button type="button" onClick={onReload} className="shrink-0 font-medium whitespace-nowrap text-accent-ink hover:underline">重试</button>
        </p>
      )}
      {/* -mx-px / px-px pb-px: room for the box's hairline inside Collapse's clip */}
      <Collapse open={open && !locked} className="-mx-px">
        <div inert={!open || !!locked} className="px-px pb-px"><div className="mt-1.5 rounded-control bg-surface shadow-hairline">
          <div className="flex items-center gap-2 border-b border-line pr-1 pl-2.5">
            <Icon name="search" size={13} className="shrink-0 text-ink-3" />
            <input
              ref={search}
              value={query}
              onChange={(e) => onQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing || e.keyCode === 229) return;
                if (e.key === "Enter") { e.preventDefault(); pickFirst(); }
                if (e.key === "Escape") { e.stopPropagation(); onToggle(); }
              }}
              aria-label="搜索模型"
              placeholder="搜索名称、ID 或描述"
              spellCheck={false}
              autoComplete="off"
              className="h-8 min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3"
            />
            {all.length > 0 && (
              <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">
                {query.trim() ? `${rows.length + (showDefault ? 1 : 0)} / ${all.length}` : `${all.length} 个`}
              </span>
            )}
            <IconButton icon="retry" label="重新读取模型列表" size={24} iconSize={12} disabled={loading} onClick={onReload} />
          </div>
          <div role="listbox" aria-label="模型" className="max-h-60 overflow-y-auto p-1">
            {showDefault && <ModelRow name="默认" detail={defaultDetail(all, followed)} note={defaultModel(all)?.description} selected={!picked} onPick={() => onPick("")} />}
            {rows.map((m) => (
              <ModelRow key={m.id} name={modelName(m)} detail={modelDetail(m)} note={m.description} selected={m.id === model} onPick={() => onPick(m.id)} />
            ))}
            {loading && (
              <div className="flex h-8 items-center gap-2 px-2 text-[12px] text-ink-3"><Spinner size={10} />正在向启动器读取模型列表，第一次要几秒…</div>
            )}
            {models.status === "error" && (
              <div className="flex h-8 items-center px-2 text-[12px] text-ink-3"><span className="truncate">没读到模型列表，仍可以用默认模型启动</span></div>
            )}
            {models.status === "ready" && !showDefault && rows.length === 0 && (
              <div className="flex h-8 items-center px-2 text-[12px] text-ink-3"><span className="truncate">没有和「{query.trim()}」匹配的模型</span></div>
            )}
          </div>
        </div></div>
      </Collapse>
    </Field>
  );
}

// A row of the list: the model's name, then the id / concrete model it stands for, or its description when the name
// already says that (Codex's GPT-5.5-Mini).
function ModelRow({ name, detail, note, selected, onPick }: { name: string; detail?: string; note?: string; selected: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={selected}
      onClick={onPick}
      title={note}
      className={cx("flex h-8 w-full items-center gap-2 rounded-[6px] px-2 text-left transition-colors", selected ? "bg-accent-tint" : "hover:bg-hover")}
    >
      <span className={cx("max-w-[60%] shrink-0 truncate text-[12.5px]", selected ? "font-medium text-accent-ink" : "text-ink")}>{name}</span>
      {detail ? <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-3">{detail}</span> : <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">{note}</span>}
      {selected && <Icon name="check" size={13} stroke={2.5} className="shrink-0 text-accent-ink" />}
    </button>
  );
}

function DirRow({ name, path, sub, git, selected, onPick, onOpen }: { name: string; path: string; sub?: string; git?: boolean; selected: boolean; onPick: () => void; onOpen?: () => void }) {
  return (
    <div className={cx("group/dir flex h-8 items-center gap-2 rounded-[6px] px-2 transition-colors", selected ? "bg-accent-tint" : "hover:bg-hover")}>
      <button type="button" onClick={onPick} onDoubleClick={onOpen} className="flex min-w-0 flex-1 items-center gap-2 text-left" title={path}>
        <Icon name={git ? "git" : "folder"} size={13} className={selected ? "text-accent-ink" : "text-ink-3"} />
        <span className={cx("shrink-0 truncate text-[12.5px]", selected ? "font-medium text-accent-ink" : "text-ink")}>{name}</span>
        {sub && <span className="min-w-0 truncate font-mono text-[11px] text-ink-3">{sub}</span>}
      </button>
      {onOpen && (
        <button type="button" aria-label={`打开 ${name}`} onClick={onOpen} className="flex size-5 items-center justify-center rounded-[4px] text-ink-3 opacity-0 transition-opacity group-hover/dir:opacity-100 hover:bg-hover-2 hover:text-ink focus-visible:opacity-100">
          <Icon name="chevronRight" size={12} stroke={2.4} />
        </button>
      )}
    </div>
  );
}
