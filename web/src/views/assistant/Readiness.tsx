// What the main conversation still needs, above its composer (docs/onboarding.md 4.4): the first missing step as a card
// (a title, one line why, one button), 「只能聊天」's note, a refused start or send as a card with the original text under
// 详情 (4.6), and the help those buttons and the local example questions open in place. 添加电脑 and 确认电脑 open the
// machines page's own flow (views/machines/AddComputer.tsx) in place where the hub has it (machines_add, machine_trust).
import { useState } from "react";
import { canAddComputer, canConfirmComputer, pendingToConfirm } from "../../lib/add-computer";
import {
  actionLabel, ADMIN_MACHINES, blockingStep, knownAction, readinessChanged, runAction, sendLabel, useReadiness, type CodedFailure, type HelpTopic,
  type Readiness, type ReadinessStep,
} from "../../lib/readiness";
import { navigate } from "../../lib/router";
import { useStore } from "../../lib/store";
import { AddComputer, type PendingComputer } from "../machines/AddComputer";
import { Button, Collapse, copyText, cx, Dialog, IconButton } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { t } from "../../i18n";

// ── the gate ────────────────────────────────────────────

export interface ReadinessGate {
  readiness: Readiness | null;
  /** The step the card shows; null: nothing missing (or a hub without readiness). */
  step: ReadinessStep | null;
  /** The send button's stand-in while a step is missing: its words and what it opens. Nothing is queued (4.4). */
  blocked?: { label: string; onClick?: () => void };
  help: (topic: HelpTopic) => void;
  /** 确认电脑: the confirm screen of the computer waiting to be confirmed (the machines page on an older hub). */
  confirm: () => void;
  /** 试一句 is running (the hub's probe takes up to ~30 s). */
  probing: boolean;
  /** The help dialog and the add / confirm computer flow, to render once beside the card. */
  dialog: React.ReactNode;
}

/** The main conversation's gate: the readiness, its card's step, the send button's stand-in and the help dialog. */
export function useReadinessGate(on = true): ReadinessGate {
  const { readiness, probing } = useReadiness(on);
  const [topic, setTopic] = useState<HelpTopic | null>(null);
  // the add computer flow: a new one ({}), or the one waiting to be confirmed
  const [computer, setComputer] = useState<{ confirm?: PendingComputer } | null>(null);
  const canAdd = useStore((s) => canAddComputer(s.features));
  const canConfirm = useStore((s) => canConfirmComputer(s.features));
  const machines = useStore((s) => s.machines);
  const r = on ? readiness : null;
  const step = blockingStep(r);
  const machine = r?.machine?.name ?? "";
  // 添加电脑 is the in-app flow where a signed-in device can add one; the how-to pointing at /admin on an older hub
  const help = (h: HelpTopic) => (h === "add_computer" && canAdd ? setComputer({}) : setTopic(h));
  const confirm = () => {
    const m = canConfirm ? pendingToConfirm(machines, step?.machine) : undefined;
    if (m) setComputer({ confirm: { id: m.id, name: m.name } });
    else navigate("/machines");
  };
  const act = (id: string) => runAction(id, { launcher: step?.launcher, onHelp: help, onConfirm: confirm });
  const blocked = step
    ? {
        label: sendLabel(step),
        onClick: step.action && knownAction(step.action.id) ? () => act(step.action!.id) : undefined,
      }
    : undefined;
  return {
    readiness: r,
    step,
    blocked,
    help,
    confirm,
    probing: on && probing,
    dialog: (
      <>
        {topic && <HelpDialog topic={topic} machine={machine} step={step} onClose={() => setTopic(null)} onAction={act} />}
        {on && (
          <AddComputer
            open={!!computer}
            confirm={computer?.confirm}
            onClose={() => {
              setComputer(null);
              void readinessChanged(); // added, confirmed or removed: the card follows now
            }}
          />
        )}
      </>
    ),
  };
}

// ── the card ────────────────────────────────────────────

function stepIcon(step: ReadinessStep): IconName {
  if (step.id === "computer") return "machine";
  if (step.id === "claude") return "terminal";
  return "key";
}

/** The first missing step: its title and line from the hub, its button and the other ways (alt_actions). */
export function ReadinessCard({ gate }: { gate: ReadinessGate }) {
  const step = gate.step;
  if (!step) return null;
  const act = (id: string) => runAction(id, { launcher: step.launcher, onHelp: gate.help, onConfirm: gate.confirm });
  const main = step.action && knownAction(step.action.id) ? step.action.id : undefined;
  const alts = (step.alt_actions ?? []).map((a) => a.id).filter((id) => knownAction(id) && id !== main);
  const problem = step.state === "problem";
  const probe = gate.readiness?.probe;
  return (
    <div
      role="status"
      data-testid="readiness-card"
      data-step={step.id}
      data-code={step.code}
      className="flex items-start gap-3 rounded-card bg-surface px-3.5 py-3 shadow-card"
      style={{ animation: "fade-up 220ms ease-out both" }}
    >
      <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-control", problem ? "bg-orange-tint text-orange" : "bg-accent-tint text-accent-ink")}>
        <Icon name={stepIcon(step)} size={15} stroke={2} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[13.5px] font-semibold text-ink">{step.title || t("assistant-readiness.one_more_step")}</span>
        {step.detail && <span className="text-[12.5px] leading-relaxed text-ink-2">{step.detail}</span>}
        {probe && !gate.probing && (
          <span data-testid="readiness-probe" className={cx("text-[12.5px] leading-relaxed", probe.ok ? "text-ink-2" : "text-red")}>
            {probe.ok ? t("assistant-readiness.probe_ok", { answer: probe.answer ?? "" }) : t("assistant-readiness.probe_failed", { error: probe.error ?? "" })}
          </span>
        )}
        {(main || alts.length > 0) && (
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            {main && (
              <Button tone="primary" busy={main === "test_assistant" && gate.probing} onClick={() => act(main)}>
                {actionLabel(main)}
              </Button>
            )}
            {alts.map((id) => (
              <Button key={id} tone="quiet" busy={id === "test_assistant" && gate.probing} onClick={() => act(id)}>
                {actionLabel(id)}
              </Button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/** 「只能聊天」 (P1, 4.4): the hub answers by itself without a computer; sending stays as it is. */
export function ChatOnlyNote({ gate }: { gate: ReadinessGate }) {
  if (gate.readiness?.mode !== "chat_only") return null;
  return (
    <div role="status" className="flex min-h-9 items-center gap-2 rounded-[12px] bg-accent-tint px-3 py-1.5 text-[12.5px] text-ink-2">
      <Icon name="chat" size={13} className="shrink-0 text-accent-ink" />
      <span className="min-w-0 flex-1">{t("assistant-readiness.chat_only")}</span>
      <Button tone="quiet" onClick={() => gate.help("add_computer")}>
        {actionLabel("add_computer")}
      </Button>
    </div>
  );
}

// ── a refused start or send ─────────────────────────────

/** A start or a send that did not go: the hub's words, a button for its code, the original under 详情 (4.6). */
export function FailureCard({ failure, onClose, onHelp, onConfirm }: { failure: CodedFailure; onClose: () => void; onHelp: (topic: HelpTopic) => void; onConfirm?: () => void }) {
  const [open, setOpen] = useState(false);
  const action = failure.action && knownAction(failure.action) ? failure.action : undefined;
  const raw = [failure.detail, failure.requestId && t("assistant-readiness.request_id", { id: failure.requestId })].filter(Boolean).join("\n");
  return (
    <div role="alert" data-testid="readiness-failure" data-code={failure.code} className="flex flex-col gap-2 rounded-card bg-red-tint px-3.5 py-3" style={{ animation: "fade-up 200ms ease-out both" }}>
      <div className="flex items-start gap-2">
        <Icon name="warning" size={14} stroke={2.2} className="mt-[3px] shrink-0 text-red" />
        <span className="min-w-0 flex-1 text-[13px] leading-relaxed font-medium text-ink">{failure.error}</span>
        <IconButton icon="x" label={t("assistant-readiness.close")} onClick={onClose} size={24} iconSize={12} />
      </div>
      <div className="flex flex-wrap items-center gap-1.5 pl-[22px]">
        {action && (
          <Button tone="primary" onClick={() => runAction(action, { launcher: failure.launcher, onHelp, onConfirm })}>
            {actionLabel(action)}
          </Button>
        )}
        {raw && (
          <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex h-8 items-center gap-1 rounded-control px-2 text-[12px] font-medium whitespace-nowrap text-ink-2 hover:bg-hover">
            {t("assistant-readiness.details")}
            <Icon name="chevron" size={12} stroke={2.2} className="transition-transform duration-200" style={{ transform: open ? "rotate(180deg)" : "none" }} />
          </button>
        )}
      </div>
      {raw && (
        <Collapse open={open}>
          <div className="flex flex-col gap-1.5 pt-0.5 pl-[22px]">
            <pre className="max-h-48 overflow-auto rounded-[8px] bg-surface px-2.5 py-2 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap text-ink-2 shadow-hairline [overflow-wrap:anywhere]">{raw}</pre>
            <button type="button" onClick={() => void copyText(raw)} className="flex w-fit items-center gap-1 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline">
              <Icon name="copy" size={12} />
              {t("assistant-readiness.copy_details")}
            </button>
          </div>
        </Collapse>
      )}
    </div>
  );
}

// ── help ────────────────────────────────────────────────

/** A command to copy in the help (what to type on that computer). */
function Command({ text }: { text: string }) {
  return (
    <div className="mt-2 flex items-center gap-2 rounded-[8px] bg-inset px-2.5 py-1.5 shadow-hairline">
      <code className="min-w-0 flex-1 font-mono text-[12px] break-all text-ink">{text}</code>
      <IconButton icon="copy" label={t("assistant-readiness.copy")} onClick={() => void copyText(text)} size={24} iconSize={12} />
    </div>
  );
}

function P({ children }: { children: React.ReactNode }) {
  return <p className="text-[13px] leading-relaxed text-ink-2">{children}</p>;
}

/** How to install Claude Code on that computer: the line to paste in its terminal (the help dialog, the provider wizard). */
export function InstallClaudeHelp({ machine }: { machine: string }) {
  return (
    <div>
      <P>{t("assistant-readiness.help_install_body", { machine: machine || t("assistant-readiness.that_computer") })}</P>
      {/* i18n-ignore: Claude Code's own installer, typed as it is */}
      <Command text="curl -fsSL https://claude.ai/install.sh | bash" />
      <p className="mt-2 text-[12px] leading-relaxed text-ink-3">{t("assistant-readiness.help_install_after")}</p>
    </div>
  );
}

/** The help a card's button or a local example question opens (wake, install, sign in, add a computer, what Persona
 *  does, how to connect a model): local words, nothing sent to the model. */
/** Without `onAction` the help is only words (no 改用 API Key). */
export function HelpDialog({ topic, machine, step, onClose, onAction }: { topic: HelpTopic; machine: string; step: ReadinessStep | null; onClose: () => void; onAction?: (id: string) => void }) {
  const where = machine || t("assistant-readiness.that_computer");
  const then = (id: string) => {
    onClose();
    onAction?.(id);
  };
  const useKey = onAction ? <Button tone="secondary" onClick={() => then("connect_model")}>{t("assistant-readiness.use_api_key")}</Button> : undefined;
  const footer = (main?: React.ReactNode) => (
    <>
      <Button tone="quiet" onClick={onClose}>
        {t("assistant-readiness.got_it")}
      </Button>
      {main}
    </>
  );
  switch (topic) {
    case "add_computer":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_add_title")} footer={footer(<Button tone="primary" href={ADMIN_MACHINES} icon="external">{t("assistant-readiness.open_admin_machines")}</Button>)}>
          <div className="flex flex-col gap-2">
            <P>{t("assistant-readiness.help_add_why")}</P>
            <P>{t("assistant-readiness.help_add_how")}</P>
            <p className="text-[12px] leading-relaxed text-ink-3">{t("assistant-readiness.help_add_note")}</p>
          </div>
        </Dialog>
      );
    case "wake_computer":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_wake_title", { machine: where })} footer={footer()}>
          <div className="flex flex-col gap-2">
            <P>{t("assistant-readiness.help_wake_body", { machine: where })}</P>
            <P>{t("assistant-readiness.help_wake_after")}</P>
          </div>
        </Dialog>
      );
    case "install_claude":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_install_title")} footer={footer(useKey)}>
          <InstallClaudeHelp machine={machine} />
        </Dialog>
      );
    case "login_claude":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_login_title")} footer={footer(useKey)}>
          <P>{t("assistant-readiness.help_login_body", { machine: where })}</P>
          <Command text="claude" />
          <p className="mt-2 text-[12px] leading-relaxed text-ink-3">{t("assistant-readiness.help_login_after")}</p>
        </Dialog>
      );
    case "what_can_you_do":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_what_title")} footer={footer(step?.action && knownAction(step.action.id) ? <Button tone="primary" onClick={() => then(step.action!.id)}>{actionLabel(step.action.id)}</Button> : undefined)}>
          <div className="flex flex-col gap-2">
            <P>{t("assistant-readiness.help_what_body")}</P>
            <ul className="flex list-disc flex-col gap-1 pl-5 text-[13px] leading-relaxed text-ink-2">
              <li>{t("assistant-readiness.help_what_computer")}</li>
              <li>{t("assistant-readiness.help_what_data")}</li>
              <li>{t("assistant-readiness.help_what_memory")}</li>
            </ul>
            {step && <p className="text-[12px] leading-relaxed text-ink-3">{t("assistant-readiness.help_what_missing", { step: step.title || sendLabel(step) })}</p>}
          </div>
        </Dialog>
      );
    case "how_to_connect_model":
      return (
        <Dialog open onClose={onClose} title={t("assistant-readiness.help_model_title")} footer={footer(<Button tone="primary" onClick={() => then("connect_model")}>{actionLabel("connect_model")}</Button>)}>
          <div className="flex flex-col gap-2">
            <P>{t("assistant-readiness.help_model_body")}</P>
            <ol className="flex list-decimal flex-col gap-1 pl-5 text-[13px] leading-relaxed text-ink-2">
              <li>{t("assistant-readiness.help_model_key")}</li>
              <li>{t("assistant-readiness.help_model_claude")}</li>
            </ol>
          </div>
        </Dialog>
      );
  }
}
