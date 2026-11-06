// An extension's sign-in (#4507, docs/protocol.md「扩展的登录」): the 「登录」 group on its detail (state, 登录 / 重新登录,
// 退出登录) and the dialog of steps the extension's program asks for: fields to fill in, a device code (a link to open,
// the code to enter, and the link's QR code for another phone), or a QR code to scan. A device code / QR step is asked
// again every interval (lib/extension-auth.ts pollAuth) until it is done or another step comes. The rules and words are
// in lib/extension-auth.ts; the QR code is the one the connectors' sign-in draws (LoginSteps.tsx).
import { useCallback, useEffect, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import { fieldInput } from "../../lib/connectors";
import {
  authLogout, authStateText, authStep, authStepView, authTone, canSignOut, isPolled, pollAuth, safeLink, signInLabel,
  type AuthPrompt, type ExtensionAuth,
} from "../../lib/extension-auth";
import { t } from "../../i18n";
import { Button, copyText, cx, Dialog, IconButton, Spinner, toast } from "../../ui/atoms";
import { QRCode, saveQR, UnsupportedStep } from "./LoginSteps";
import { Group } from "./SettingsParts";

const DOT: Record<ReturnType<typeof authTone>, string> = { red: "bg-red", orange: "bg-orange", green: "bg-green", idle: "bg-line-strong" };
const TEXT: Record<ReturnType<typeof authTone>, string> = { red: "text-red", orange: "text-orange", green: "text-ink", idle: "text-ink-3" };

const fieldClass = "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

/**
 * The 「登录」 group: the state, 登录 (重新登录 once signed in to) and 退出登录. `enabled` false: a switched-off extension
 * cannot sign in. `start` opens the dialog once (an install whose next step is to sign in). `onChange` takes the new state.
 */
export function AuthGroup({ id, auth, enabled, start, onChange }: {
  id: string;
  auth: ExtensionAuth;
  enabled: boolean;
  start?: boolean;
  onChange: (a?: ExtensionAuth) => void;
}) {
  const [open, setOpen] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const started = useRef(false);
  useEffect(() => {
    if (start && enabled && !started.current) {
      started.current = true;
      setOpen(true);
    }
  }, [start, enabled]);
  const tone = authTone(auth);
  const signOut = async () => {
    setLeaving(true);
    try {
      onChange(await authLogout(id));
      toast(t("settings-extensionauth.signed_out"), "ok");
    } catch (e) {
      toast(t("settings-extensionauth.sign_out_failed", { error: errorText(e) }), "error");
    } finally {
      setLeaving(false);
    }
  };
  return (
    <Group title={t("settings-extensionauth.title")} footer={enabled ? auth.description : t("settings-extensionauth.enable_first")}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", DOT[tone])} />
          <span className="min-w-0">
            <span className="block truncate text-[13px] text-ink">{auth.title}</span>
            <span className={cx("block truncate text-[11.5px]", TEXT[tone])} title={auth.message}>{authStateText(auth)}</span>
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          {canSignOut(auth) && (
            <Button tone="quiet" icon="logout" busy={leaving} disabled={!enabled} onClick={() => void signOut()}>
              {t("settings-extensionauth.sign_out")}
            </Button>
          )}
          <Button tone={auth.state === "signed_in" ? "secondary" : "accent"} icon="key" disabled={!enabled || leaving} onClick={() => setOpen(true)}>
            {signInLabel(auth)}
          </Button>
        </span>
      </div>
      {open && <SignInDialog id={id} title={auth.title} onClose={() => setOpen(false)} onDone={() => onChange()} />}
    </Group>
  );
}

/** The sign-in, one step at a time; closes itself when the extension says it is done (onDone asks for the new state). */
export function SignInDialog({ id, title, onClose, onDone }: { id: string; title: string; onClose: () => void; onDone: () => void }) {
  const [prompt, setPrompt] = useState<AuthPrompt | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(true);
  const [failure, setFailure] = useState("");
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => void (live.current = false);
  }, []);

  const take = useCallback((p: AuthPrompt) => {
    if (!live.current) return;
    setFailure("");
    if (p.done) {
      toast(p.account ? t("settings-extensionauth.signed_in_as", { account: p.account }) : t("settings-extensionauth.signed_in"), "ok");
      onDone();
      onClose();
      return;
    }
    setPrompt((cur) => {
      if ((p.step ?? "") !== (cur?.step ?? "") || !cur) setValues({}); // a new step: its fields start empty
      return p;
    });
  }, [onClose, onDone]);

  const send = useCallback(async (step: string, vals: Record<string, string>) => {
    setBusy(true);
    try {
      take(await authStep(id, step, vals));
    } catch (e) {
      if (live.current) setFailure(errorText(e));
    } finally {
      if (live.current) setBusy(false);
    }
  }, [id, take]);

  useEffect(() => {
    void send("", {});
  }, [send]);

  // a device code or QR code: the same step again every interval, until it is done or another step comes
  const view = prompt ? authStepView(prompt) : null;
  const polled = isPolled(prompt);
  useEffect(() => {
    if (!prompt || !polled) return;
    return pollAuth(prompt, (step) => authStep(id, step), take, (e) => live.current && setFailure(errorText(e)));
  }, [prompt?.step, view, polled, id]); // eslint-disable-line react-hooks/exhaustive-deps

  const fields = prompt?.fields ?? [];
  const canSend = view === "fields" && !busy && fields.every((f) => (values[f.name] ?? "").trim().length > 0);
  const submit = () => {
    if (canSend && prompt) void send(prompt.step ?? "", values);
  };
  const restart = () => {
    setPrompt(null);
    setValues({});
    void send("", {});
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title={title}
      width={400}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>{t("common.cancel")}</Button>
          {view === "fields" && (
            <Button tone="accent" disabled={!canSend} busy={busy} onClick={submit}>
              {busy ? t("settings-extensionauth.verifying") : t("settings-extensionauth.next")}
            </Button>
          )}
        </>
      }
    >
      {!prompt && busy && (
        <p className="flex items-center gap-2 py-6 text-[12.5px] text-ink-3"><Spinner size={11} />{t("settings-extensionauth.starting")}</p>
      )}
      {prompt?.hint && <p className="mb-3 text-[12.5px] leading-relaxed text-ink-2">{prompt.hint}</p>}
      {prompt && view === "device_code" && <DeviceCode p={prompt} />}
      {prompt && view === "qr" && (
        <div className="flex flex-col items-center gap-3 py-1">
          <QRCode rows={prompt.qr!.rows} label={t("settings-extensionauth.qr_label")} />
          <Waiting text={t("settings-extensionauth.waiting_scan")} />
          <Button tone="secondary" icon="download" onClick={() => saveQR(prompt.qr!.rows, title)}>{t("settings-loginsteps.save_qr")}</Button>
        </div>
      )}
      {prompt && view === "qr_link" && (
        <div className="flex flex-col gap-3">
          <p className="text-[12.5px] leading-relaxed text-ink-2">{t("settings-extensionauth.qr_link")}</p>
          <Link url={prompt.url!} />
          <Waiting text={t("settings-extensionauth.waiting")} />
        </div>
      )}
      {prompt && view === "unsupported" && (
        <div className="-mx-4">
          <UnsupportedStep onRestart={restart} onCancel={onClose} />
        </div>
      )}
      {prompt && view === "fields" && (
        <form
          className="flex flex-col gap-2.5"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {fields.map((f, i) => {
            const input = fieldInput(f);
            return (
              <label key={f.name} className="flex flex-col gap-1">
                <span className="text-[12px] text-ink-2">{f.label}</span>
                <input
                  value={values[f.name] ?? ""}
                  onChange={(e) => setValues((cur) => ({ ...cur, [f.name]: e.target.value }))}
                  type={input.type}
                  inputMode={input.inputMode}
                  autoComplete={input.autoComplete}
                  placeholder={f.placeholder ?? f.label}
                  spellCheck={false}
                  autoCapitalize="off"
                  autoFocus={i === 0}
                  disabled={busy}
                  className={fieldClass}
                />
              </label>
            );
          })}
          {/* Enter sends the form; the dialog's footer has the button */}
          <button type="submit" hidden aria-hidden tabIndex={-1} />
        </form>
      )}
      {(prompt?.error || failure) && (
        <p role="alert" className="mt-2.5 text-[12px] leading-relaxed break-words text-red">{prompt?.error || failure}</p>
      )}
    </Dialog>
  );
}

function Waiting({ text }: { text: string }) {
  return (
    <p className="flex items-center gap-2 text-[12.5px] text-ink-3" aria-live="polite">
      <Spinner size={11} />
      {text}
    </p>
  );
}

/** A link to open in a new tab (https only), copyable. */
function Link({ url }: { url: string }) {
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-control bg-inset px-3 py-1.5 shadow-hairline">
      <a href={url} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate font-mono text-[12px] text-accent-ink hover:underline">
        {url}
      </a>
      <IconButton
        icon="copy"
        label={t("settings-extensionauth.copy_link")}
        onClick={() => void copyText(url, { done: t("settings-extensionauth.link_copied"), failed: t("settings-extensionauth.copy_failed") })}
      />
    </div>
  );
}

/** A device code: the page to open, the code to enter there (when there is one), and the page's QR code for another phone. */
function DeviceCode({ p }: { p: AuthPrompt }) {
  const url = safeLink(p.url);
  return (
    <div className="flex flex-col gap-3">
      {url && (
        <div className="flex flex-col gap-2">
          <div>
            <Button tone="accent" icon="external" href={url}>{t("settings-extensionauth.open_page")}</Button>
          </div>
          <Link url={url} />
        </div>
      )}
      {p.user_code && (
        <div className="flex flex-col gap-1">
          <span className="text-[12px] text-ink-3">{t("settings-extensionauth.code_label")}</span>
          <div className="flex items-center gap-3">
            <span className="min-w-0 flex-1 truncate font-mono text-[26px] font-semibold tracking-[0.12em] text-ink select-all">{p.user_code}</span>
            <IconButton
              icon="copy"
              label={t("settings-extensionauth.copy_code")}
              onClick={() => void copyText(p.user_code ?? "", { done: t("settings-extensionauth.code_copied"), failed: t("settings-extensionauth.copy_failed") })}
            />
          </div>
        </div>
      )}
      {!!p.qr?.rows?.length && (
        <div className="flex flex-col items-center gap-1.5 pt-1">
          <QRCode rows={p.qr.rows} size={152} label={t("settings-extensionauth.qr_label")} />
          <span className="text-[11.5px] text-ink-3">{t("settings-extensionauth.qr_other_phone")}</span>
        </div>
      )}
      <Waiting text={t("settings-extensionauth.waiting")} />
    </div>
  );
}
