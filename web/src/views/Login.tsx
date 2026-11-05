import { useEffect, useState } from "react";
import { errorText } from "../lib/api";
import { pair } from "../lib/auth";
import { rememberHub } from "../lib/hubs";
import { Button, Spinner } from "../ui/atoms";
import { AppMark } from "../ui/AppMark";
import { Icon } from "../ui/icons";
import { OtherHubs } from "./Hubs";
import { t } from "../i18n";
import { tRich } from "../i18n/rich";

// Pair with the link `personad hub pair` prints, the hub's token or a device token (lib/auth.ts pairToken). `error` is what went
// wrong with a token that came in the link.
export function Login({ onDone, error: linkError = "" }: { onDone: () => void; error?: string }) {
  const [token, setTok] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(linkError);
  useEffect(() => void rememberHub(location.origin), []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const tok = token.trim();
    if (!tok) return;
    setBusy(true);
    setError("");
    try {
      await pair(tok);
      onDone();
    } catch (err) {
      setError(errorText(err) === "invalid token" ? t("login.invalid_token") : errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center bg-page px-5 py-16">
      <form onSubmit={submit} className="w-full max-w-[360px]" style={{ animation: "fade-up 450ms cubic-bezier(0.23,1,0.32,1) both" }}>
        <div className="mb-7 flex flex-col items-center gap-3 text-center">
          <AppMark size={44} radius={12} className="shadow-raised" />
          <div>
            <h1 className="text-[18px] font-semibold tracking-tight text-ink">{t("login.title")}</h1>
            <p className="mt-1 text-[13px] leading-relaxed text-ink-2">
              {tRich("login.how_to_pair", {
                command: <code className="rounded-[5px] bg-field px-1 py-0.5 font-mono text-[12px] shadow-hairline">personad hub pair</code>,
                link: <code className="font-mono text-[12px]">jarvis://pair?…</code>,
                legacy: <code className="font-mono text-[12px]">--legacy</code>,
                jq: <code className="font-mono text-[12px]">jq -r .token ~/.persona/hub.json</code>,
              })}
            </p>
            <p className="mt-1.5 text-[12px] leading-relaxed text-ink-3">{t("login.phone_hint")}</p>
          </div>
        </div>
        <div className="rounded-card bg-surface p-1.5 shadow-card">
          <label className="flex h-10 items-center gap-2 rounded-control bg-inset px-3 shadow-hairline focus-within:shadow-[0_0_0_1px_var(--line-strong)]">
            <Icon name="key" size={14} className="text-ink-3" />
            <input
              autoFocus
              type="password"
              value={token}
              onChange={(e) => setTok(e.target.value)}
              placeholder={t("login.token_placeholder")}
              aria-label={t("login.token_placeholder")}
              autoComplete="current-password"
              className="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-ink outline-none placeholder:font-sans placeholder:text-ink-3"
            />
          </label>
          <Button type="submit" tone="primary" disabled={!token.trim()} className="mt-1.5 h-9 w-full">
            {busy ? <Spinner /> : t("login.connect")}
          </Button>
        </div>
        <p className="mt-3 h-5 text-center text-[12.5px] text-red" role="alert">
          {error}
        </p>
        <p className="text-center text-[11.5px] leading-relaxed text-ink-3">{t("login.credential_note")}</p>
        <OtherHubs />
      </form>
    </div>
  );
}
