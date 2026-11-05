// 新建 worktree… (#2431, lib/worktree.ts): a Git worktree of a workspace's repository, opened as a workspace. The
// repository is read when the dialog opens; the checkout path follows the branch until it is edited.
import { useEffect, useId, useRef, useState } from "react";
import { t } from "../i18n";
import {
  createWorktree, emptyForm, formProblem, onOpenNewWorktree, openCreated, prepareWorktree, typeBranch, typePath, WORKTREE_COPY, worktreeProblem,
  type WorktreeForm, type WorktreePrepare, type WorktreeRequest,
} from "../lib/worktree";
import { Button, cx, Dialog, Spinner, toast } from "../ui/atoms";

/** Mounted once, with the app shell: the dialog opens for whichever workspace's menu or palette command asked. */
export function NewWorktreeHost() {
  const [request, setRequest] = useState<WorktreeRequest | null>(null);
  useEffect(() => onOpenNewWorktree(setRequest), []);
  if (!request) return null;
  return <NewWorktree key={`${request.target.mid}/${request.target.workspaceId}`} request={request} onClose={() => setRequest(null)} />;
}

type Read = { state: "reading" } | { state: "ready"; info: WorktreePrepare } | { state: "failed"; text: string; needsUpdate: boolean };

export function NewWorktree({ request, onClose }: { request: WorktreeRequest; onClose: () => void }) {
  const { target } = request;
  const [read, setRead] = useState<Read>({ state: "reading" });
  const [form, setForm] = useState<WorktreeForm>(emptyForm);
  const [shown, setShown] = useState(false); // a problem with the branch is told once it was typed in or sent
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ text: string; needsUpdate: boolean } | null>(null);
  const closed = useRef(false);

  useEffect(() => {
    closed.current = false;
    prepareWorktree(target).then(
      (info) => {
        if (closed.current) return;
        setRead({ state: "ready", info });
      },
      (e) => {
        if (closed.current) return;
        setRead({ state: "failed", ...worktreeProblem(e, WORKTREE_COPY.readFailed) });
      },
    );
    return () => {
      closed.current = true;
    };
  }, [target]);

  const base = read.state === "ready" ? read.info.path : "";
  const problem = formProblem(form);
  const ready = read.state === "ready";
  const branchError = shown && (problem === WORKTREE_COPY.pathRequired ? "" : problem);
  const pathError = shown && problem === WORKTREE_COPY.pathRequired ? problem : "";

  const submit = async () => {
    if (!ready || busy) return;
    if (problem) {
      setShown(true);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const created = await createWorktree(target, form);
      if (closed.current) return;
      closed.current = true;
      toast(t("worktree.created"), "ok");
      request.changed();
      onClose();
      openCreated(target.mid, created);
    } catch (e) {
      if (closed.current) return;
      setError(worktreeProblem(e, WORKTREE_COPY.failed));
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onClose={busy ? () => {} : onClose}
      title={t("worktree.dialog_title")}
      width={440}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button tone="primary" busy={busy} disabled={!ready || Boolean(error?.needsUpdate)} onClick={() => void submit()}>
            {t("worktree.create_open")}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {read.state === "reading" && (
          <p className="flex items-center gap-2 text-[12.5px] text-ink-3" role="status">
            <Spinner />
            {WORKTREE_COPY.reading}
          </p>
        )}
        {read.state === "failed" && (
          <p className="text-[12.5px] text-red" role="alert">
            {read.text}
          </p>
        )}
        {read.state === "ready" && (
          <>
            <p className="truncate text-[12.5px] text-ink-2" title={read.info.root}>
              {read.info.branch ? t("worktree.repo_on_branch", { repo: read.info.repoName, branch: read.info.branch }) : t("worktree.repo_detached", { repo: read.info.repoName })}
            </p>
            <Input label={t("worktree.branch_label")} value={form.branch} placeholder={t("worktree.branch_placeholder")} autoFocus error={branchError || undefined} onChange={(v) => setForm((f) => typeBranch(f, base, v))} />
            <Input label={t("worktree.path_label")} value={form.path} mono error={pathError || undefined} onChange={(v) => setForm((f) => typePath(f, base, v))} hint={t("worktree.path_hint")} />
            <p className="text-[11px] text-ink-3">{t("worktree.note")}</p>
          </>
        )}
        {error && (
          <p className="text-[12.5px] text-red" role="alert">
            {error.text}
          </p>
        )}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function Input({ label, value, onChange, placeholder, hint, error, mono, autoFocus }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string; hint?: string; error?: string; mono?: boolean; autoFocus?: boolean }) {
  const id = useId();
  const noteId = `${id}-note`;
  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-[12px] font-medium text-ink-2">{label}</label>
      <input
        id={id}
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        aria-invalid={error ? true : undefined}
        aria-describedby={error || hint ? noteId : undefined}
        className={cx(
          "h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]",
          mono && "font-mono",
        )}
      />
      {error ? <span id={noteId} className="mt-1 block text-[11px] text-red" role="alert">{error}</span> : hint && <span id={noteId} className="mt-1 block text-[11px] text-ink-3">{hint}</span>}
    </div>
  );
}
