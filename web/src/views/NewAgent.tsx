// Start an agent on one of your computers: machine, folder (recent ones first), launcher, the first message.
import { useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, enc, errorText } from "../lib/api";
import { pickAttachment, releaseAttachments, type LocalAttachment } from "../lib/attachments";
import { launchAgent } from "../lib/new-agent";
import { AttachmentTray } from "../ui/AttachmentTray";
import { modelPurpose, modelSummary } from "../lib/model-config";
import { baseName, shortPath } from "../lib/format";
import { useStore } from "../lib/store";
import type { Launcher } from "../lib/types";
import { Button, cx, Dialog, Spinner, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

interface Dirs {
  path: string;
  parent?: string;
  entries: { name: string; path: string; is_git?: boolean }[];
  recent?: { path: string; name: string; last_used?: string }[];
}

export function NewAgent({ open, onClose }: { open: boolean; onClose: () => void }) {
  const machines = useStore((s) => s.machines);
  const online = useMemo(() => machines.filter((m) => m.online), [machines]);
  const [mid, setMid] = useState(() => localStorage.getItem("new.machine") ?? "");
  const [cwd, setCwd] = useState("");
  const [launcher, setLauncher] = useState("");
  const [launchers, setLaunchers] = useState<Launcher[]>([]);
  const [unified, setUnified] = useState(true);
  const [modelDefault, setModelDefault] = useState("");
  const [loadingModels, setLoadingModels] = useState(false);
  const [dirs, setDirs] = useState<Dirs | null>(null);
  const [browse, setBrowse] = useState("");
  const [loadingDirs, setLoadingDirs] = useState(false);
  const [prompt, setPrompt] = useState(() => localStorage.getItem("draft:new-agent") ?? "");
  const [attachments, setAttachments] = useState<LocalAttachment[]>([]);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const submitted = useRef(false);
  const attachmentsRef = useRef(attachments);
  attachmentsRef.current = attachments;

  useEffect(() => { if (open) submitted.current = false; }, [open]);
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
    if (open && (!mid || !online.some((m) => m.id === mid)) && online[0]) setMid(online[0].id);
  }, [open, online, mid]);

  useEffect(() => {
    if (!open || !mid) return;
    localStorage.setItem("new.machine", mid);
    setLaunchers([]);
    const controller = new AbortController();
    setLauncher("");
    setModelDefault("");
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
        setUnified(config !== null);
        if (config) {
          setModelDefault(modelSummary(config.purpose, config.providers));
          setLauncher("");
        } else {
          const saved = localStorage.getItem(`new.launcher.${mid}`);
          setLauncher(ls.some((l) => l.name === saved) ? saved! : (ls[0]?.name ?? ""));
        }
      })
      .catch((e) => { if (!controller.signal.aborted) toast(errorText(e), "error"); })
      .finally(() => { if (!controller.signal.aborted) setLoadingModels(false); });
    setCwd(localStorage.getItem(`new.cwd.${mid}`) ?? "");
    setBrowse("");
    return () => controller.abort();
  }, [open, mid]);

  useEffect(() => {
    if (!open || !mid) return;
    setLoadingDirs(true);
    api<Dirs>(`/machines/${enc(mid)}/dirs?path=${enc(browse)}`)
      .then(setDirs)
      .catch(() => setDirs(null))
      .finally(() => setLoadingDirs(false));
  }, [open, mid, browse]);

  const start = () => {
    if (submitted.current || loadingModels || !online.some((m) => m.id === mid) || !cwd.trim()) return;
    submitted.current = true;
    localStorage.setItem(`new.cwd.${mid}`, cwd.trim());
    if (launcher && !unified) localStorage.setItem(`new.launcher.${mid}`, launcher);
    launchAgent({ machineId: mid, cwd: cwd.trim(), launcher, text: prompt.trim(), attachments });
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
              {unified && <button type="button" onClick={() => setLauncher("")} title={modelDefault}
                className={cx("h-7 rounded-control px-2.5 text-[12px] whitespace-nowrap transition-colors", launcher === "" ? "bg-ink text-surface" : "bg-surface text-ink-2 shadow-btn hover:bg-hover hover:text-ink")}>
                跟随模型配置
              </button>}
              {launchers.map((l) => (
                <button
                  key={l.name}
                  type="button"
                  onClick={() => setLauncher(l.name)}
                  title={l.model}
                  className={cx("h-7 rounded-control px-2.5 font-mono text-[12px] whitespace-nowrap transition-colors", launcher === l.name ? "bg-ink text-surface" : "bg-surface text-ink-2 shadow-btn hover:bg-hover hover:text-ink")}
                >
                  {l.name}
                </button>
              ))}
            </div>
            {unified && <p className="mt-1.5 text-[11.5px] text-ink-3">{launcher ? "只覆盖这次新会话，不改变统一默认。" : modelDefault || "使用统一模型配置中的新 Agent 默认值。"}</p>}
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
