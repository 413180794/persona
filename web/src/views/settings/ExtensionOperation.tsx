// An install, upgrade or uninstall in steps (docs/protocol.md「扩展的安装、升级、卸载进度」, #3277): every step with its
// state, the download's bytes as a bar, where it stopped and why, and a retry. The hub pushes each change; the page
// only draws what lib/extension-operations.ts keeps.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import {
  latestFor, opLine, operations, shownSteps, stepDetail, stepFraction, stepLabel, stepsDone, watchOperations,
  type ExtensionOperation, type ExtensionOpStep,
} from "../../lib/extension-operations";
import { Button, cx, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** The newest operation of extension id (of market), kept up to date. */
export function useOperation(id: string, market?: string): ExtensionOperation | undefined {
  const [op, setOp] = useState(() => latestFor(operations(), id, market));
  useEffect(() => {
    const read = () => setOp(latestFor(operations(), id, market));
    read();
    return watchOperations(read);
  }, [id, market]);
  return op;
}

function StepMark({ s }: { s: ExtensionOpStep }) {
  switch (s.state) {
    case "running":
      return <Spinner size={13} />;
    case "done":
      return <Icon name="check" size={13} stroke={2.6} className="text-green" />;
    case "failed":
      return <Icon name="x" size={13} stroke={2.6} className="text-red" />;
    default:
      return <span aria-hidden className="size-[7px] rounded-full bg-line-strong" />;
  }
}

function StepRow({ s, kind }: { s: ExtensionOpStep; kind: ExtensionOperation["kind"] }) {
  const detail = stepDetail(s);
  const fraction = stepFraction(s);
  return (
    <li className="flex flex-col gap-1.5 py-1.5" aria-current={s.state === "running" ? "step" : undefined}>
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="flex size-4 shrink-0 items-center justify-center">
          <StepMark s={s} />
        </span>
        <span className={cx("min-w-0 flex-1 truncate text-[12.5px]", s.state === "pending" ? "text-ink-3" : s.state === "failed" ? "font-medium text-red" : "text-ink")}>
          {stepLabel(s.id, kind)}
        </span>
        {detail && <span className="shrink-0 font-mono text-[11px] text-ink-3 tabular-nums">{detail}</span>}
      </div>
      {fraction !== undefined && (
        <div
          role="progressbar"
          aria-label={stepLabel(s.id, kind)}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(fraction * 100)}
          className="ml-6.5 h-1 overflow-hidden rounded-full bg-inset"
        >
          <div className="h-full rounded-full bg-accent transition-[width] duration-300" style={{ width: `${Math.max(2, fraction * 100)}%` }} />
        </div>
      )}
      {s.state === "running" && fraction === undefined && s.id === "download" && (
        <div aria-hidden className="ml-6.5 h-1 overflow-hidden rounded-full bg-inset">
          <div className="h-full w-1/3 rounded-full bg-accent" style={{ animation: "indeterminate 1.2s ease-in-out infinite" }} />
        </div>
      )}
    </li>
  );
}

/**
 * The steps of op, with what it can do now: retry a failed one, cancel a download, review the permissions it waits
 * for. Shown in the item's card, under its buttons.
 */
export function OperationSteps({ op, onRetry, onCancel, onReview, onDismiss }: {
  op: ExtensionOperation;
  onRetry?: () => Promise<void>;
  onCancel?: () => Promise<void>;
  onReview?: () => void;
  onDismiss?: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const steps = shownSteps(op);
  const { done, total } = stepsDone(op);
  const title = op.kind === "uninstall" ? t("settings-extensionmarket.op_title_uninstall") : op.kind === "upgrade" ? t("settings-extensionmarket.op_title_upgrade") : t("settings-extensionmarket.op_title");
  const run = (f?: () => Promise<void>) => async () => {
    if (!f) return;
    setBusy(true);
    try {
      await f();
    } finally {
      setBusy(false);
    }
  };
  const failed = op.state === "failed";
  const nodeErrors = Object.entries(steps.find((s) => s.id === "nodes")?.machines?.errors ?? {});
  return (
    <div className="border-t border-line px-4 py-3" aria-live="polite">
      <div className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-[12.5px] font-medium text-ink">{title}</span>
        <span className={cx("shrink-0 truncate text-[11.5px]", failed ? "text-red" : op.state === "needs_consent" ? "text-orange" : "text-ink-3")}>{opLine(op)}</span>
        {op.state === "running" && <span className="shrink-0 font-mono text-[11px] text-ink-3 tabular-nums">{t("settings-extensionmarket.op_progress", { done, total })}</span>}
      </div>
      <ol className="mt-1.5">
        {steps.map((s) => (
          <StepRow key={s.id} s={s} kind={op.kind} />
        ))}
      </ol>
      {failed && op.error && <p className="mt-1.5 rounded-[8px] bg-red-tint px-3 py-2 text-[12px] leading-relaxed break-words text-red">{t("settings-extensionmarket.op_error", { error: op.error })}</p>}
      {nodeErrors.length > 0 && (
        <ul className="mt-1.5 flex flex-col gap-1">
          {nodeErrors.map(([machine, error]) => (
            <li key={machine} className="text-[11.5px] break-words text-orange">
              {t("settings-extensionmarket.op_machine_error", { machine, error })}
            </li>
          ))}
        </ul>
      )}
      {(failed || op.state === "needs_consent" || (op.state === "running" && onCancel)) && (
        <div className="mt-2.5 flex items-center justify-end gap-2">
          {failed && onDismiss && (
            <Button tone="quiet" onClick={onDismiss} disabled={busy}>
              {t("settings-extensionmarket.op_dismiss")}
            </Button>
          )}
          {op.state !== "failed" && onCancel && (
            <Button tone="quiet" onClick={() => void run(onCancel)()} disabled={busy}>
              {t("settings-extensionmarket.op_cancel")}
            </Button>
          )}
          {op.state === "needs_consent" && onReview && (
            <Button tone="primary" icon="shield" onClick={onReview} disabled={busy}>
              {t("settings-extensionmarket.op_review")}
            </Button>
          )}
          {failed && op.retryable && onRetry && (
            <Button tone="primary" icon="retry" busy={busy} onClick={() => void run(onRetry)()}>
              {t("settings-extensionmarket.op_retry")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
