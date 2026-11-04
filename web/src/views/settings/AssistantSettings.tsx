// 设置 › 助手 (docs/assistant.md「资料页和头像」, the iOS app's AssistantProfileView / AssistantNotesView /
// AssistantMemoryView, Android's 助手设置): what Persona does on its own (a line into 定时任务), what it knows about you
// (关于我 = `notes`, saved on 保存 or when leaving; 记忆 = its memory files: read, edit the whole file, delete), and where
// it runs (read-only here). Its model is 设置 › 模型配置 › 主对话, not here (#1291).
import { useEffect, useMemo, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import { MEMORY_INDEX, memoryEntries, memoryTypeLabel, memoryTypeTone, parseMemory, type MemoryEntry, type MemoryFile } from "../../lib/memory";
import { navigate } from "../../lib/router";
import { momentLabel, upcoming, type Task } from "../../lib/routines";
import { refreshAssistant, useStore } from "../../lib/store";
import { Button, cx, Dialog, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
import { Group, Pill, Row } from "../settings/SettingsParts";
import { SkillsGroup } from "./Skills";
import { Tag, textareaClass } from "../assistant/parts";
import { PersonaAvatar, usePersona } from "../assistant/Persona";

interface AssistantState {
  machine_id?: string;
  launcher?: string;
  cwd?: string;
  timezone?: string;
  /** Only once saved here (or in the app); `notes_effective` is what Persona is told (hub.json's otherwise). */
  notes?: string;
  notes_effective?: string;
}

export function AssistantSettings({ bare }: { /** Without its header (the profile page has its own). */ bare?: boolean } = {}) {
  const persona = usePersona();
  const machines = useStore((s) => s.machines);
  const [info, setInfo] = useState<AssistantState | null>(null);
  const [error, setError] = useState("");
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [files, setFiles] = useState<MemoryFile[] | null>(null);
  const [memoryError, setMemoryError] = useState("");
  const [open, setOpen] = useState<{ file: MemoryFile; index?: boolean } | null>(null);

  const loadMemory = async () => {
    try {
      const r = await api<{ files: MemoryFile[]; dir?: string }>("/assistant/memory");
      setFiles(r.files ?? []);
      setMemoryError("");
    } catch (e) {
      setMemoryError(errorText(e));
      setFiles((f) => f ?? []);
    }
  };

  useEffect(() => {
    api<AssistantState>("/assistant").then(setInfo, (e) => setError(errorText(e)));
    api<{ tasks: Task[] }>("/assistant/tasks").then((r) => setTasks(r.tasks ?? []), () => setTasks([]));
    void loadMemory();
  }, []);

  const machine = machines.find((m) => m.id === info?.machine_id);
  const machineName = machine?.name ?? info?.machine_id;
  const entries = useMemo(() => memoryEntries(files ?? []), [files]);
  const index = files?.find((f) => f.name === MEMORY_INDEX);
  const on = tasks?.filter((t) => t.enabled).length ?? 0;
  const next = tasks ? upcoming(tasks, 1)[0] : undefined;

  if (!info && !error) return <Loading />;

  return (
    <>
      {!bare && <div className="mb-6 flex items-center gap-3 px-1">
        <PersonaAvatar
          size={44}
          fallback={
            <span className="flex size-11 shrink-0 items-center justify-center rounded-full bg-ink text-surface">
              <Icon name="spark" size={18} />
            </span>
          }
        />
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-semibold text-ink">{persona.name}</div>
          <div className="truncate text-[12px] text-ink-3">你的私人助手{machineName ? ` · 运行在 ${machineName}` : ""}</div>
        </div>
        {machine && <Pill tone={machine.online ? "green" : undefined}>{machine.online ? "在线" : "离线"}</Pill>}
      </div>}
      {error && <p role="alert" className="mb-4 text-[13px] text-red">{error}</p>}

      <Group title="主动做事" footer="到点自己开始工作、跟进答应你的事，跑完推送到手机。在对话里说「每天 9 点给我早报」「一小时后提醒我」就会加到这里。">
        <Row
          label="定时任务和运行记录"
          hint={tasks === null ? undefined : next ? `下一件：${momentLabel(next.at, new Date())} ${next.title}` : "现在没有要做的"}
          onClick={() => navigate({ name: "routines" })}
        >
          {tasks !== null && <span>{on === 0 ? "未开启" : `${on} 个开启`}</span>}
        </Row>
      </Group>

      <Group title="了解你" footer="「关于我」是你告诉 Persona 的；「记忆」是它在对话里自己记下的，都可以改。">
        {info && <AboutMe info={info} onSaved={(notes) => setInfo((i) => (i ? { ...i, notes, notes_effective: notes.trim() } : i))} />}
        <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
          <span className="flex-1 text-[13px] font-medium text-ink">记忆</span>
          {files && entries.length > 0 && <span className="text-[11.5px] text-ink-3 tabular-nums">{entries.length} 条</span>}
        </div>
        {files === null ? (
          <div className="px-4 py-4">
            <Loading />
          </div>
        ) : entries.length === 0 ? (
          <p className="px-4 py-4 text-[12.5px] leading-relaxed text-ink-3">{memoryError ? `读不到记忆：${memoryError}` : "还没有记忆。Persona 会把你的偏好、纠正过它的地方记在这里。"}</p>
        ) : (
          entries.map((e) => <MemoryRow key={e.file.name} e={e} onOpen={() => setOpen({ file: e.file })} />)
        )}
        {index && entries.length > 0 && (
          <button type="button" onClick={() => setOpen({ file: index, index: true })} className="flex w-full items-center gap-3 whitespace-nowrap px-4 py-2.5 text-left transition-colors hover:bg-hover">
            <Icon name="list" size={14} className="shrink-0 text-ink-3" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13px] text-ink">索引</span>
              <span className="block truncate text-[11.5px] text-ink-3">MEMORY.md：Persona 自己维护的目录，删除记忆时会一起更新。只读</span>
            </span>
            <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
          </button>
        )}
      </Group>

      <SkillsGroup />

      <Group
        title="运行在"
        footer={`每段对话在 ${machineName ?? "hub 所在的机器"} 的这个目录里后台运行，不进 herdr。会话记录和记忆在这台机器上；关于我、定时任务和设置在 hub。用哪个模型在「模型配置 › 主对话」里选。`}
      >
        <Row label="机器">
          <span className="max-w-[220px] truncate">{machineName ?? "—"}</span>
        </Row>
        <Row label="工作目录" mono>
          <span className="max-w-[300px] truncate" title={info?.cwd}>{info?.cwd || "~/.jarvis/assistant"}</span>
        </Row>
        {info?.timezone && <Row label="时区">{info.timezone}</Row>}
      </Group>

      {open && (
        <MemoryDialog
          key={open.file.name}
          file={open.file}
          title={open.index ? "索引" : entries.find((e) => e.file.name === open.file.name)?.title ?? open.file.name}
          readOnly={open.index}
          onClose={() => setOpen(null)}
          onChanged={() => void loadMemory()}
        />
      )}
    </>
  );
}

/** 关于我: what Persona is told about you. Until saved here it shows hub.json's text; saving (even empty) makes this the
 *  one. Saved with 保存, and when leaving the page with changes. */
function AboutMe({ info, onSaved }: { info: AssistantState; onSaved: (notes: string) => void }) {
  const original = info.notes ?? info.notes_effective ?? "";
  const [text, setText] = useState(original);
  const [base, setBase] = useState(original);
  const [busy, setBusy] = useState(false);
  const changed = text !== base;
  const latest = useRef({ text, changed });
  latest.current = { text, changed };

  const save = async (value = text, quiet = false) => {
    setBusy(true);
    try {
      await api("/assistant", { method: "PUT", json: { notes: value } });
      setBase(value);
      onSaved(value);
      void refreshAssistant().catch(() => {});
      if (!quiet) toast("已保存，新对话生效", "ok");
    } catch (e) {
      toast(`关于我没有保存：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  // leaving the page (another settings tab, another page) keeps what was typed
  useEffect(
    () => () => {
      const { text: t, changed: c } = latest.current;
      if (c) {
        api("/assistant", { method: "PUT", json: { notes: t } }).then(
          () => toast("关于我已保存，新对话生效", "ok"),
          (e) => toast(`关于我没有保存：${errorText(e)}`, "error"),
        );
      }
    },
    [],
  );

  return (
    <div className="px-4 py-3">
      <div className="mb-2 flex items-center gap-2">
        <span className="flex-1 text-[13px] font-medium text-ink">关于我</span>
        {info.notes === undefined && original && <span className="text-[11px] text-ink-3">来自 hub 的配置</span>}
      </div>
      <textarea
        className={cx(textareaClass, "min-h-[132px]")}
        rows={6}
        value={text}
        aria-label="关于我"
        placeholder="比如：代码放在哪、常用哪些 skills、哪些机器或目录别碰、你习惯的汇报方式"
        onChange={(e) => setText(e.target.value)}
      />
      <div className="mt-2 flex items-start gap-3">
        <p className="min-w-0 flex-1 text-[11.5px] leading-relaxed text-ink-3">
          这些内容会附在 Persona 的说明最后；改了以后新对话生效。
          {info.notes === undefined && original ? "现在用的是 hub 配置里的内容，在这里保存后以这里为准。" : ""}
        </p>
        {changed && <Button tone="quiet" disabled={busy} onClick={() => setText(base)}>还原</Button>}
        <Button tone="primary" busy={busy} disabled={!changed} onClick={() => void save()}>保存</Button>
      </div>
    </div>
  );
}

function MemoryRow({ e, onOpen }: { e: MemoryEntry; onOpen: () => void }) {
  const type = memoryTypeLabel(e.type);
  return (
    <button type="button" onClick={onOpen} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover">
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13px] text-ink">{e.title}</span>
          {type && <Tag tone={memoryTypeTone(e.type)}>{type}</Tag>}
        </span>
        {e.detail && <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">{e.detail}</span>}
      </span>
      <span className="shrink-0 text-[11px] text-ink-3">{ago(e.file.updated_at)}</span>
      <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
    </button>
  );
}

/** One memory rendered; 编辑 opens the whole file (frontmatter included) as text, 删除 asks first. The index is read-only. */
function MemoryDialog({ file, title, readOnly, onClose, onChanged }: { file: MemoryFile; title: string; readOnly?: boolean; onClose: () => void; onChanged: () => void }) {
  const [content, setContent] = useState(file.content);
  const [updated, setUpdated] = useState(file.updated_at);
  const [editing, setEditing] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const parsed = parseMemory(content);
  const type = memoryTypeLabel(parsed.type);

  const save = async () => {
    if (editing === null) return;
    setBusy(true);
    try {
      await api(`/assistant/memory/${enc(file.name)}`, { method: "PUT", json: { content: editing } });
      setContent(editing);
      setUpdated(new Date().toISOString());
      setEditing(null);
      toast("记忆已保存", "ok");
      onChanged();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      await api(`/assistant/memory/${enc(file.name)}`, { method: "DELETE" });
      toast(`已删除「${title}」`, "ok");
      onChanged();
      onClose();
    } catch (e) {
      toast(errorText(e), "error");
      setBusy(false);
    }
  };

  const footer = readOnly ? (
    <Button tone="quiet" onClick={onClose}>关闭</Button>
  ) : editing !== null ? (
    <>
      <span className="mr-auto min-w-0 truncate font-mono text-[11px] text-ink-3">{file.name}</span>
      <Button tone="quiet" disabled={busy} onClick={() => setEditing(null)}>取消</Button>
      <Button tone="primary" busy={busy} disabled={editing === content} onClick={() => void save()}>保存</Button>
    </>
  ) : confirm ? (
    <>
      <span className="mr-auto min-w-0 text-[12px] text-ink-2">Persona 以后不再记得「{title}」。</span>
      <Button tone="quiet" disabled={busy} onClick={() => setConfirm(false)}>取消</Button>
      <Button tone="danger" busy={busy} onClick={() => void remove()}>删除</Button>
    </>
  ) : (
    <>
      <Button tone="quiet" icon="trash" className="text-red enabled:hover:text-red" onClick={() => setConfirm(true)}>删除</Button>
      <span className="flex-1" />
      <Button tone="secondary" icon="pencil" onClick={() => setEditing(content)}>编辑</Button>
    </>
  );

  return (
    <Dialog open onClose={() => !busy && onClose()} title={title} width={600} footer={footer}>
      {editing !== null ? (
        <textarea
          className={cx(textareaClass, "min-h-[320px] font-mono text-[12.5px]")}
          value={editing}
          autoFocus
          spellCheck={false}
          aria-label={`编辑 ${file.name}`}
          onChange={(e) => setEditing(e.target.value)}
        />
      ) : (
        <div className="-mx-4 max-h-[min(60vh,560px)] overflow-y-auto px-4">
          {!readOnly && (type || parsed.description || updated) && (
            <div className="mb-3 border-b border-line pb-3">
              <div className="flex flex-wrap items-center gap-2">
                {type && <Tag tone={memoryTypeTone(parsed.type)}>{type}</Tag>}
                {updated && <span className="text-[11.5px] text-ink-3">更新于 {ago(updated)}</span>}
              </div>
              {parsed.description && <p className="mt-1.5 text-[12.5px] text-ink-2">{parsed.description}</p>}
            </div>
          )}
          <Markdown text={(readOnly ? content : parsed.body) || "（空）"} />
          {!readOnly && <p className="mt-3 font-mono text-[11px] text-ink-3">{file.name}</p>}
        </div>
      )}
    </Dialog>
  );
}
