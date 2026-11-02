// First visit (set the admin password) and sign-in (docs/admin.md「管理员密码和会话」).
import { useState } from "react";
import { Button, EASE } from "../../ui/atoms";
import { adminApi, AdminError, errorText, setCsrf } from "../api";
import { setupRequest } from "../logic";
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

const CMD = "jarvisd hub admin reset-password";

export function Setup({ state, onDone }: { state: AdminState; onDone: (s: AdminState) => void }) {
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const needCode = Boolean(state.setup_code_required);

  if (!state.can_setup)
    return (
      <Frame hubName={state.hub_name} title="还没有管理员密码" subtitle="为了不让别人抢先设置，第一次设置密码只能在 hub 所在的局域网里做。">
        <div className="rounded-card bg-surface p-4 text-[12.5px] leading-relaxed text-ink-2 shadow-card">
          <p>任选一种：</p>
          <ul className="mt-2 list-disc space-y-1.5 pl-4 marker:text-ink-3">
            <li>用和 hub 同一个局域网里的电脑或手机打开这个页面；</li>
            <li>
              在 hub 机器上运行 <code className="whitespace-nowrap rounded-[5px] bg-field px-1 py-0.5 font-mono text-[11.5px] shadow-hairline">{CMD}</code> 直接设密码。
            </li>
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
    <Frame hubName={state.hub_name} title="设置管理员密码" subtitle="第一次打开管理界面，先设一个密码，以后用它登录。">
      <form onSubmit={submit} className="rounded-card bg-surface p-4 shadow-card">
        {needCode && (
          <div className="mb-4 border-b border-line pb-4">
            <div className="mb-3 rounded-control bg-inset px-3 py-2.5 text-[12px] leading-relaxed text-ink-2 shadow-hairline">
              <p>你是在 hub 机器本机上打开的（或经过同一台机器上的反向代理）。为了确认你能操作 hub 机器，先输入一次性设置码：10 分钟内有效，用一次就失效。任选一处取：</p>
              <ul className="mt-1.5 list-disc space-y-1 pl-4 marker:text-ink-3">
                <li>
                  hub 启动日志：<span className="whitespace-nowrap font-mono text-[11.5px]">journalctl --user -u jarvisd</span>，Docker 是{" "}
                  <span className="whitespace-nowrap font-mono text-[11.5px]">docker compose logs hub</span>
                </li>
                <li>
                  在 hub 机器上运行 <span className="whitespace-nowrap font-mono text-[11.5px]">jarvisd hub admin setup-code</span>
                </li>
              </ul>
            </div>
            <Field label="设置码" value={code} onChange={(v) => setCode(v.toUpperCase())} placeholder="ABCD-EFGH" mono autoFocus autoComplete="one-time-code" autoCapitalize="characters" maxLength={32} />
          </div>
        )}
        <div className="space-y-3">
          <Field label="密码" type="password" value={pw} onChange={setPw} autoFocus={!needCode} autoComplete="new-password" error={short ? "至少 8 个字符" : undefined} />
          <Field label="再输一遍" type="password" value={again} onChange={setAgain} autoComplete="new-password" error={mismatch ? "两次输的不一样" : undefined} />
        </div>
        <Button type="submit" tone="primary" busy={busy} disabled={!pw || short || mismatch || again !== pw || (needCode && !code.trim())} className="mt-4 h-9 w-full">
          设置并进入
        </Button>
        <p className="mt-3 min-h-5 text-center text-[12.5px] text-red" role="alert">
          {error}
        </p>
        <p className="text-center text-[11.5px] leading-relaxed text-ink-3">
          以后忘了密码，在 hub 机器上运行 <span className="whitespace-nowrap font-mono">{CMD}</span>。
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
    <Frame hubName={state.hub_name} title="登录 hub 管理" subtitle="输入管理员密码。">
      <form onSubmit={submit} className="rounded-card bg-surface p-1.5 shadow-card">
        <label className="flex h-10 items-center gap-2 rounded-control bg-inset px-3 shadow-hairline focus-within:shadow-[0_0_0_1px_var(--line-strong)]">
          <AdminIcon name="lock" size={14} className="text-ink-3" />
          <input
            autoFocus
            type="password"
            value={pw}
            onChange={(e) => setPw(e.target.value)}
            placeholder="管理员密码"
            aria-label="管理员密码"
            autoComplete="current-password"
            className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
          />
        </label>
        <Button type="submit" tone="primary" busy={busy} disabled={!pw} className="mt-1.5 h-9 w-full">
          登录
        </Button>
      </form>
      <p className="mt-3 min-h-5 text-center text-[12.5px] text-red" role="alert">
        {error}
      </p>
      <p className="text-center text-[11.5px] leading-relaxed text-ink-3">
        忘了密码：在 hub 机器上运行 <span className="whitespace-nowrap font-mono">{CMD}</span>。
      </p>
    </Frame>
  );
}
