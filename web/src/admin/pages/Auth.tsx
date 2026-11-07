// First visit (set the admin password) and sign-in (docs/operations/admin.md "Admin password and sessions").
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { useEffect, useState } from "react";
import { Button, EASE } from "../../ui/atoms";
import { adminApi, AdminError, errorText, setCsrf } from "../api";
import { setupCodeFromHash, setupRequest, withoutSetupCode } from "../logic";
import { AdminIcon } from "../icons";
import type { AdminState } from "../types";
import { Field } from "../parts";

function Frame({ title, subtitle, children, hubName }: { title: string; subtitle: React.ReactNode; children: React.ReactNode; hubName?: string }) {
  return (
    <div className="flex min-h-full items-center justify-center bg-page px-5 py-16">
      <div className="w-full max-w-[380px]" style={{ animation: `fade-up 450ms ${EASE} both` }}>
        <div className="mb-7 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
            <AdminIcon name="shield" size={20} />
          </span>
          <div>
            {hubName && <div className="mb-1 text-[11.5px] font-medium tracking-[0.04em] text-ink-3">{hubName}</div>}
            <h1 className="text-[18px] font-semibold tracking-tight text-ink">{title}</h1>
            <p className="mt-1.5 text-[13px] leading-relaxed text-ink-2">{subtitle}</p>
          </div>
        </div>
        {children}
      </div>
    </div>
  );
}

const CMD = "personad hub admin reset-password";

export function Setup({ state, onDone }: { state: AdminState; onDone: (s: AdminState) => void }) {
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  // The installer's admin URL carries the code in its fragment (/admin#setup=…): filled in, then taken off the URL.
  const [fromLink] = useState(() => setupCodeFromHash(window.location.hash));
  const [code, setCode] = useState(fromLink);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const needCode = Boolean(state.setup_code_required);
  useEffect(() => {
    if (!fromLink) return;
    const { pathname, search, hash } = window.location;
    window.history.replaceState(window.history.state, "", pathname + search + withoutSetupCode(hash));
  }, [fromLink]);
  const prefilled = Boolean(fromLink) && code === fromLink;

  if (!state.can_setup)
    return (
      <Frame hubName={state.hub_name} title={t("admin-pages-auth.no_password_title")} subtitle={t("admin-pages-auth.no_password_subtitle")}>
        <div className="rounded-card bg-surface p-4 text-[12.5px] leading-relaxed text-ink-2 shadow-card">
          <p>{t("admin-pages-auth.pick_either")}</p>
          <ul className="mt-2 list-disc space-y-1.5 pl-4 marker:text-ink-3">
            <li>{t("admin-pages-auth.open_on_lan")}</li>
            <li>{tRich("admin-pages-auth.run_reset", { command: <code className="whitespace-nowrap rounded-[5px] bg-field px-1 py-0.5 font-mono text-[11.5px] shadow-hairline">{CMD}</code> })}</li>
          </ul>
        </div>
      </Frame>
    );

  const short = pw.length > 0 && [...pw].length < 8;
  const mismatch = again.length > 0 && again !== pw;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (short || mismatch || !pw || (needCode && !code.trim())) return;
    setBusy(true);
    setError("");
    try {
      const s = await adminApi<AdminState>("/setup", "POST", setupRequest(pw, code, needCode));
      setCsrf(s.csrf);
      onDone(s);
    } catch (err) {
      setError(errorText(err));
      if (err instanceof AdminError && err.code === "already_set") onDone({ ...state, password_set: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Frame hubName={state.hub_name} title={t("admin-pages-auth.setup_title")} subtitle={t("admin-pages-auth.setup_subtitle")}>
      <form onSubmit={submit} className="rounded-card bg-surface p-4 shadow-card">
        {needCode && (
          <div className="mb-4 border-b border-line pb-4">
            {prefilled ? (
              <p className="mb-3 rounded-control bg-inset px-3 py-2.5 text-[12px] leading-relaxed text-ink-2 shadow-hairline">{t("admin-pages-auth.code_prefilled")}</p>
            ) : (
              <div className="mb-3 rounded-control bg-inset px-3 py-2.5 text-[12px] leading-relaxed text-ink-2 shadow-hairline">
                <p>{t("admin-pages-auth.code_intro")}</p>
                <ul className="mt-1.5 list-disc space-y-1 pl-4 marker:text-ink-3">
                  <li>{tRich("admin-pages-auth.code_from_setup", { command: <span className="whitespace-nowrap font-mono text-[11.5px]">personad hub setup</span> })}</li>
                  <li>{tRich("admin-pages-auth.code_from_command", { command: <span className="whitespace-nowrap font-mono text-[11.5px]">personad hub admin setup-code</span> })}</li>
                  <li>
                    {/* the hub's systemd unit keeps its personad name (docs/operations/rename.md): the command is the real one */}
                    {tRich("admin-pages-auth.code_from_logs", {
                      systemd: <span className="whitespace-nowrap font-mono text-[11.5px]">journalctl --user -u personad</span>,
                      docker: <span className="whitespace-nowrap font-mono text-[11.5px]">docker compose logs hub</span>,
                    })}
                  </li>
                </ul>
              </div>
            )}
            <Field label={t("admin-pages-auth.setup_code")} value={code} onChange={(v) => setCode(v.toUpperCase())} placeholder="ABCD-EFGH" mono autoFocus={!prefilled} autoComplete="one-time-code" autoCapitalize="characters" maxLength={32} />
          </div>
        )}
        <div className="space-y-3">
          <Field label={t("admin-pages-auth.password")} type="password" value={pw} onChange={setPw} autoFocus={!needCode || prefilled} autoComplete="new-password" error={short ? t("admin-pages-auth.too_short") : undefined} />
          <Field label={t("admin-pages-auth.again")} type="password" value={again} onChange={setAgain} autoComplete="new-password" error={mismatch ? t("admin-pages-auth.mismatch") : undefined} />
        </div>
        <Button type="submit" tone="primary" busy={busy} disabled={!pw || short || mismatch || again !== pw || (needCode && !code.trim())} className="mt-4 h-9 w-full">
          {t("admin-pages-auth.set_and_enter")}
        </Button>
        <p className="mt-3 min-h-5 text-center text-[12.5px] text-red" role="alert">
          {error}
        </p>
        <p className="text-center text-[11.5px] leading-relaxed text-ink-3">
          {tRich("admin-pages-auth.forgot_later", { command: <span className="whitespace-nowrap font-mono">{CMD}</span> })}
        </p>
      </form>
    </Frame>
  );
}

export function Login({ state, onDone, notice }: { state: AdminState; onDone: (s: AdminState) => void; notice?: string }) {
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(notice ?? "");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pw) return;
    setBusy(true);
    setError("");
    try {
      const s = await adminApi<AdminState>("/login", "POST", { password: pw });
      setCsrf(s.csrf);
      onDone(s);
    } catch (err) {
      setError(errorText(err));
      setPw("");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Frame hubName={state.hub_name} title={t("admin-pages-auth.login_title")} subtitle={t("admin-pages-auth.login_subtitle")}>
      <form onSubmit={submit} className="rounded-card bg-surface p-1.5 shadow-card">
        <label className="flex h-10 items-center gap-2 rounded-control bg-inset px-3 shadow-hairline focus-within:shadow-[0_0_0_1px_var(--line-strong)]">
          <AdminIcon name="lock" size={14} className="text-ink-3" />
          <input
            autoFocus
            type="password"
            value={pw}
            onChange={(e) => setPw(e.target.value)}
            placeholder={t("admin-pages-auth.admin_password")}
            aria-label={t("admin-pages-auth.admin_password")}
            autoComplete="current-password"
            className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
          />
        </label>
        <Button type="submit" tone="primary" busy={busy} disabled={!pw} className="mt-1.5 h-9 w-full">
          {t("admin-pages-auth.sign_in")}
        </Button>
      </form>
      <p className="mt-3 min-h-5 text-center text-[12.5px] text-red" role="alert">
        {error}
      </p>
      <p className="text-center text-[11.5px] leading-relaxed text-ink-3">
        {tRich("admin-pages-auth.forgot", { command: <span className="whitespace-nowrap font-mono">{CMD}</span> })}
      </p>
    </Frame>
  );
}
