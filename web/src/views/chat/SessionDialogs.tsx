// The chat's ⋯ menu dialogs: session info, rename (the agent / its space), and the run-mode chips under the composer.
import { useEffect, useState } from "react";
import { api, enc, errorText, sessionPath } from "../../lib/api";
import { ago, resumeCommand, shortPath } from "../../lib/format";
import { EFFORTS, effortTitle, modeTitle, renameBody, SWITCHABLE_MODES } from "../../lib/session-actions";
import { refresh, refreshAssistant, upsertSession } from "../../lib/store";
import type { Session } from "../../lib/types";
import { Button, copyText, cx, Dialog, Loading, Menu, toast, useMenu } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

interface SessionInfo {
  session_id?: string;
  transcript_path?: string;
  cwd?: string;
  model?: string;
  launcher?: string;
  git_branch?: string;
  version?: string;
  permission_mode?: string;
  effort?: string;
  context_tokens?: number;
  output_tokens?: number;
  compactions?: number;
  turns?: number;
  started_at?: string;
  last_activity?: string;
}

const count = (n?: number) => (n === undefined ? "" : n >= 1000 ? `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}K` : String(n));

// 会话信息 (iOS SessionInfoSheet): how it runs, how much it used, where it lives; ids and the resume command copy.
export function SessionInfoDialog({ session, machineName, open, onClose }: { session: Session; machineName: string; open: boolean; onClose: () => void }) {
  const [info, setInfo] = useState<SessionInfo | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!open) return;
    let live = true;
    setInfo(null);
    setError("");
    api<SessionInfo>(`${sessionPath(session.machine_id, session.session_id)}/info`)
      .then((r) => live && setInfo(r))
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [open, session.machine_id, session.session_id]);

  const groups: { title: string; rows: [string, string | undefined, boolean?][] }[] = info
    ? [
        { title: "运行", rows: [["模型", info.model], ["启动器", info.launcher || session.launcher], ["权限模式", modeTitle(info.permission_mode)], ["思考强度", effortTitle(info.effort)], ["版本", info.version, true]] },
        { title: "用量", rows: [["上下文", count(info.context_tokens) && `${count(info.context_tokens)} tokens`], ["输出", count(info.output_tokens) && `${count(info.output_tokens)} tokens`], ["轮数", info.turns !== undefined ? String(info.turns) : ""], ["压缩", info.compactions ? `${info.compactions} 次` : ""], ["开始", info.started_at && ago(info.started_at)], ["最后活动", info.last_activity && ago(info.last_activity)]] },
        { title: "位置", rows: [["机器", machineName], ["目录", info.cwd ? shortPath(info.cwd) : undefined, true], ["分支", info.git_branch, true], ["记录", info.transcript_path ? shortPath(info.transcript_path) : undefined, true]] },
      ]
    : [];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="会话信息"
      width={480}
      footer={
        <>
          <Button tone="quiet" icon="copy" onClick={() => copyText(session.session_id)}>
            拷贝会话 ID
          </Button>
          {!session.assistant && (
            <Button tone="secondary" icon="terminal" onClick={() => copyText(resumeCommand(session))}>
              拷贝恢复命令
            </Button>
          )}
        </>
      }
    >
      {!info && !error && (
        <div className="flex justify-center py-6">
          <Loading />
        </div>
      )}
      {error && <p className="py-4 text-[13px] text-red">{error}</p>}
      <div className="flex flex-col gap-3">
        {groups.map((g) => {
          const rows = g.rows.filter(([, v]) => v);
          if (!rows.length) return null;
          return (
            <section key={g.title}>
              <h3 className="mb-1 text-[11.5px] font-medium text-ink-3">{g.title}</h3>
              <div className="divide-y divide-line overflow-hidden rounded-[10px] bg-inset shadow-hairline">
                {rows.map(([k, v, mono]) => (
                  <div key={k} className="flex items-center gap-3 px-3 py-1.5 text-[12.5px]">
                    <span className="w-16 shrink-0 text-ink-3">{k}</span>
                    <span title={v} className={cx("min-w-0 flex-1 truncate text-ink", mono && "font-mono text-[12px]")}>
                      {v}
                    </span>
                  </div>
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </Dialog>
  );
}

// 重命名: the herdr tab and the session's name (Claude's /rename), each only when changed (iOS RenameSessionAlert).
export function RenameDialog({ session, open, onClose }: { session: Session; open: boolean; onClose: () => void }) {
  const [tab, setTab] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!open) return;
    setTab(session.slot_name ?? "");
    setTitle(session.title ?? "");
    // only when it opens: a push while typing must not overwrite the field
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const hasTab = !session.assistant && Boolean(session.slot_id);
  const body = renameBody(session, tab, title);
  const save = async () => {
    if (!body) return onClose();
    setBusy(true);
    try {
      await api(`${sessionPath(session.machine_id, session.session_id)}/rename`, { method: "POST", json: body });
      // the list shows the new name at once; the hub's push confirms it
      upsertSession({ ...session, ...(body.title ? { title: body.title } : {}), ...(body.tab_name ? { slot_name: body.tab_name } : {}) });
      if (session.assistant) void refreshAssistant().catch(() => {});
      toast("已重命名", "ok");
      onClose();
    } catch (e) {
      toast(`没改成：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="重命名"
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!body} onClick={() => void save()}>
            保存
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        {hasTab && <Field label="标签名（herdr tab）" value={tab} onChange={setTab} autoFocus />}
        <Field label="会话名" value={title} onChange={setTitle} autoFocus={!hasTab} />
        <p className="text-[12px] leading-relaxed text-ink-3">{session.assistant ? "只改这段对话在 Jarvis 里的标题。" : "标签名同步到 herdr；会话名用 Claude 的 /rename，电脑上 claude --resume 也看得到。"}</p>
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

// 重命名空间: herdr's workspace (and the pins / mutes that name it).
export function RenameSpaceDialog({ session, open, onClose }: { session: Session; open: boolean; onClose: () => void }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (open) setName(session.space_name ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const next = name.trim();
  const save = async () => {
    if (!session.space_id || !next || next === session.space_name) return onClose();
    setBusy(true);
    try {
      await api(`/machines/${enc(session.machine_id)}/spaces/${enc(session.space_id)}/rename`, { method: "POST", json: { name: next } });
      void refresh().catch(() => {});
      toast("已重命名空间", "ok");
      onClose();
    } catch (e) {
      toast(`没改成：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="重命名空间"
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!next || next === session.space_name} onClick={() => void save()}>
            保存
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <Field label="空间名" value={name} onChange={setName} autoFocus />
        <p className="text-[12px] leading-relaxed text-ink-3">同时改 herdr 里这个 workspace 的名字，置顶和屏蔽跟着改。</p>
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function Field({ label, value, onChange, autoFocus }: { label: string; value: string; onChange: (v: string) => void; autoFocus?: boolean }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[12px] text-ink-3">{label}</span>
      <input
        value={value}
        autoFocus={autoFocus}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 rounded-control bg-surface px-3 text-[13px] text-ink shadow-btn outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]"
      />
    </label>
  );
}

// The two chips under the composer, like Claude Code's bottom bar: permission mode and effort (POST …/modes).
export function ModeChips({ session }: { session: Session }) {
  const modeMenu = useMenu();
  const effortMenu = useMenu();
  const set = async (patch: { permission_mode?: string; effort?: string }) => {
    const before = { permission_mode: session.permission_mode, effort: session.effort };
    upsertSession({ ...session, ...patch });
    try {
      await api(`${sessionPath(session.machine_id, session.session_id)}/modes`, { method: "POST", json: patch });
    } catch (e) {
      upsertSession({ ...session, ...before });
      toast(`没改成：${errorText(e)}`, "error");
    }
  };
  const mode = session.permission_mode;
  const danger = mode === "bypassPermissions";
  return (
    <span className="flex shrink-0 items-center gap-1">
      <Chip label={mode ? modeTitle(mode) : "权限：默认"} danger={danger} onClick={modeMenu.open} active={Boolean(modeMenu.anchor)} title="权限模式" />
      <Chip label={session.effort ? `思考 ${effortTitle(session.effort)}` : "思考：默认"} onClick={effortMenu.open} active={Boolean(effortMenu.anchor)} title="思考强度" />
      {modeMenu.anchor && (
        <Menu
          anchor={modeMenu.anchor}
          onClose={modeMenu.close}
          side="top"
          width={300}
          header="权限模式"
          items={SWITCHABLE_MODES.map((m) => ({ key: m.id, label: m.title, hint: m.detail, checked: mode === m.id, onSelect: () => void set({ permission_mode: m.id }) }))}
        />
      )}
      {effortMenu.anchor && (
        <Menu
          anchor={effortMenu.anchor}
          onClose={effortMenu.close}
          side="top"
          width={280}
          header="思考强度"
          items={EFFORTS.map((m) => ({ key: m.id, label: m.title, hint: m.detail, checked: session.effort === m.id, onSelect: () => void set({ effort: m.id }) }))}
        />
      )}
    </span>
  );
}

export function Chip({ label, onClick, active, danger, title }: { label: string; onClick: (e: React.MouseEvent<HTMLElement>) => void; active?: boolean; danger?: boolean; title: string }) {
  return (
    <button
      type="button"
      title={title}
      aria-haspopup="menu"
      onClick={onClick}
      className={cx(
        "flex h-5.5 items-center gap-0.5 whitespace-nowrap rounded-full px-2 text-[11px] font-medium transition-colors",
        danger ? "bg-red-tint text-red" : active ? "bg-hover text-ink" : "bg-field text-ink-2 hover:bg-hover hover:text-ink",
      )}
    >
      {label}
      <Icon name="chevron" size={9} stroke={2.4} />
    </button>
  );
}
