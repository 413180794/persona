// 设置 › 连接器's hub connectors (iOS ConnectorsSection's rows, HubConnectorView, ConnectorSignInSections,
// GitHubConnectorSections, GitHubInboxSettings; docs/connectors.md): GitHub, Telegram and any plugin's connector, as
// GET /api/v1/connectors lists them. A page shows how one is doing, as whom, where its sign-in comes from and what is
// wrong (with the steps to fix it); one the user signs in to here (`login`) also gets its switch, the sign-in a step
// at a time (fields the hub asks for, or GitHub's device code), 进知识库 / 进收件箱, sign-out and delete. GitHub adds
// where its token comes from (a computer's gh, or the device flow), the scopes it lacks, and its inbox options.
import { useCallback, useEffect, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import {
  canSendStep, CI_MODES, connectorBrand, connectorLogin, connectorLogout, connectorState, connectorSummary, connectorTone, deleteConnectorArchive,
  deleteConnectorData, deletedLine, feedsFooter, fieldInput, githubAuth, githubInboxFooter, githubMachineLine, githubMachineWarns, githubOptions,
  githubScopeAdvice, isDeviceCode, isEnabled, isSignedIn, isStarting, listConnectors, mutableRepo, otherConnectors, setConnector, setGitHubAuth,
  signInFooter, signsInHere, switchFooter, undescribed,
  type ConnectorPrompt, type ConnectorTone, type GitHubAuth, type GitHubInboxOptions, type HubConnector,
} from "../../lib/connectors";
import { ago } from "../../lib/format";
import { Button, cx, Dialog, IconButton, Loading, Spinner, toast } from "../../ui/atoms";
import { BrandMark } from "../../ui/BrandMark";
import { Icon } from "../../ui/icons";
import { Group, Row, Switch } from "./SettingsParts";

const DOT: Record<ConnectorTone, string> = { green: "bg-green", orange: "bg-orange", red: "bg-red", idle: "bg-line-strong" };
const fieldClass = "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

function Mark({ id, size }: { id: string; size: number }) {
  const brand = connectorBrand(id);
  if (brand) {
    return (
      <span className="flex shrink-0 items-center justify-center rounded-[9px] bg-surface shadow-hairline" style={{ width: size, height: size }}>
        <BrandMark brand={brand} size={Math.round(size * (brand === "telegram" ? 0.78 : 0.6))} />
      </span>
    );
  }
  return (
    <span className="flex shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent-ink" style={{ width: size, height: size }}>
      <Icon name={id === "imessage" ? "chat" : "link"} size={Math.round(size * 0.45)} />
    </span>
  );
}

// ── the rows on 设置 › 连接器 ────────────────────────────

export function ConnectorList({ onOpen }: { onOpen: (id: string) => void }) {
  const [list, setList] = useState<HubConnector[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    listConnectors()
      .then((l) => (setList(otherConnectors(l)), setError("")))
      .catch((e) => setError(errorText(e)));
  }, []);
  if (list?.length === 0) return null;
  return (
    <Group title="更多连接器" footer="GitHub、Telegram 和插件提供的连接器：点进去看状态、登录和设置。">
      {!list && !error && <div className="px-4 py-3"><Loading /></div>}
      {error && <p className="px-4 py-3 text-[12.5px] text-red">读不到连接器：{error}</p>}
      {list?.map((c) => (
        <button key={c.id} type="button" onClick={() => onOpen(c.id)} className="flex w-full items-center gap-3 px-4 py-2.5 text-left whitespace-nowrap transition-colors hover:bg-hover">
          <Mark id={c.id} size={32} />
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{c.name}</span>
          <span className="flex min-w-0 items-center gap-1.5 text-[12.5px] text-ink-2">
            <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", DOT[connectorTone(c)])} />
            <span className="truncate">{connectorSummary(c)}</span>
          </span>
          <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
        </button>
      ))}
    </Group>
  );
}

// ── one connector's page ─────────────────────────────────

export function ConnectorDetail({ id, onBack }: { id: string; onBack: () => void }) {
  const [c, setC] = useState<HubConnector | null>(null);
  const [failure, setFailure] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async (quietly = false) => {
    if (!quietly) setLoading(true);
    try {
      const found = (await listConnectors()).find((x) => x.id === id) ?? null;
      setC(found);
      setFailure(found ? "" : "hub 上没有这个连接器");
    } catch (e) {
      setFailure(`读不到连接器的状态：${errorText(e)}`);
    } finally {
      setLoading(false);
    }
  }, [id]);
  useEffect(() => void load(), [load]);

  // Switched on, its program takes a moment to start and say it signs in: asked again every second (15 s at most).
  const starting = isStarting(c);
  useEffect(() => {
    if (!starting) return;
    let n = 0;
    const t = setInterval(() => {
      if (++n > 15) clearInterval(t);
      else void load(true);
    }, 1000);
    return () => clearInterval(t);
  }, [starting, load]);

  const reload = () => load(true);
  const title = c?.name ?? (id === "github" ? "GitHub" : id);
  const rateLimited = c?.status === "rate_limited";
  const err = c?.error || failure;

  return (
    <div>
      <div className="mb-3 flex items-center gap-2">
        <Button tone="quiet" icon="chevronLeft" onClick={onBack} className="-ml-2">连接器</Button>
        <span className="flex-1" />
        <IconButton icon="retry" label="重新检查" onClick={() => void load()} disabled={loading} />
      </div>

      <section className="mb-6 overflow-hidden rounded-card bg-surface shadow-card">
        <div className="flex items-center gap-3 px-4 py-3.5">
          <Mark id={id} size={44} />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[15px] font-semibold text-ink">{c?.account || title}</h2>
            <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] text-ink-3">
              <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", DOT[connectorTone(c)])} />
              <span className="truncate">{c ? connectorState(c) : loading ? "正在检查…" : "读不到状态"}</span>
            </div>
          </div>
          {loading && <Spinner />}
        </div>
        {c?.via && <InfoRow k="登录" v={c.via} />}
        {c?.last_sync && <InfoRow k="最近一次同步" v={ago(c.last_sync)} />}
        {err && <p className={cx("border-t border-line px-4 py-2.5 text-[12px] break-words select-text", rateLimited ? "text-ink-2" : "text-red")}>{err}</p>}
      </section>
      {c && !signsInHere(c) && !undescribed(c) && (
        <p className="-mt-4 mb-6 px-1 text-[11.5px] leading-relaxed text-ink-3">
          {id === "imessage" ? "「交给 Persona」和删除已存的数据在手机 App 的 设置 › 连接器 › 信息 里。" : "登录在 hub 一侧设置（hub.json，见 docs/config.md），这里只看状态。"}
        </p>
      )}

      {c && id === "github" && signsInHere(c) && <GitHubSections c={c} reload={reload} />}
      {c && id !== "github" && (signsInHere(c) || undescribed(c)) && <SignInSections c={c} reload={reload} />}

      {c && c.status !== "ok" && !!c.guide?.length && (
        <Group title="怎么修好">
          {c.guide.map((step, i) => (
            <div key={i} className="flex items-baseline gap-3 px-4 py-2.5">
              <span className="w-4 shrink-0 text-[12px] font-semibold text-accent-ink tabular-nums">{i + 1}</span>
              <span className="min-w-0 text-[13px] leading-relaxed text-ink select-text">{step}</span>
            </div>
          ))}
          <div className="px-4 py-2.5">
            <Button tone="secondary" icon="retry" onClick={() => void load()} busy={loading}>重新检查</Button>
          </div>
        </Group>
      )}

      {c && id === "github" && githubOptions(c.options) && <GitHubInbox key={JSON.stringify(c.options)} options={githubOptions(c.options)!} onSaved={setC} />}
    </div>
  );
}

function InfoRow({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-center gap-3 border-t border-line px-4 py-2.5">
      <span className="shrink-0 text-[13px] text-ink">{k}</span>
      <span className="min-w-0 flex-1 truncate text-right text-[12.5px] text-ink-2" title={v}>{v}</span>
    </div>
  );
}

// ── switch, feeds, sign-out and delete ───────────────────

/** What a page's actions share: busy while one runs, the hub's errors as toasts. */
function useActions(id: string, reload: () => Promise<void>) {
  const [busy, setBusy] = useState("");
  const run = async (what: string, fn: () => Promise<unknown>, done?: (r: unknown) => void) => {
    setBusy(what);
    try {
      const r = await fn();
      done?.(r);
      await reload();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };
  return { busy, run, id };
}
type Actions = ReturnType<typeof useActions>;

function SwitchGroup({ c, a }: { c: HubConnector; a: Actions }) {
  return (
    <Group footer={switchFooter(c)}>
      <div className="flex items-center gap-3 px-4 py-2.5">
        <span className="min-w-0 flex-1 truncate text-[13px] text-ink">同步 {c.name}</span>
        {a.busy === "enabled" && <Spinner />}
        <Switch on={isEnabled(c)} disabled={!!a.busy} label={`同步 ${c.name}`} onChange={(on) => void a.run("enabled", () => setConnector(c.id, { enabled: on }))} />
      </div>
    </Group>
  );
}

function FeedsGroup({ c, a }: { c: HubConnector; a: Actions }) {
  const [confirm, setConfirm] = useState(false);
  const [deleted, setDeleted] = useState("");
  if (c.archive === undefined && c.inbox === undefined) return null;
  const toggle = (title: string, on: boolean, feed: "archive" | "inbox") => (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{title}</span>
      {a.busy === feed && <Spinner />}
      <Switch on={on} disabled={!!a.busy} label={title} onChange={(v) => void a.run(feed, () => setConnector(c.id, { [feed]: v }))} />
    </div>
  );
  return (
    <Group footer={<span className="whitespace-pre-line">{deleted || feedsFooter(c)}</span>}>
      {c.archive !== undefined && toggle("进知识库", c.archive, "archive")}
      {c.inbox !== undefined && toggle("进收件箱", c.inbox, "inbox")}
      {c.archive !== undefined && (
        <div className="px-4 py-2.5">
          <button type="button" disabled={!!a.busy} onClick={() => setConfirm(true)} className="text-[13px] whitespace-nowrap text-red hover:underline disabled:opacity-50">删除已存的知识库内容</button>
        </div>
      )}
      <Confirm
        open={confirm}
        title={`删除 ${c.name} 存进知识库的内容？`}
        message={`知识库里从 ${c.name} 来的会话和人都会删掉，「进知识库」会关掉。收件箱和助手查 ${c.name} 不受影响。`}
        action="删除"
        busy={a.busy === "deleteArchive"}
        onClose={() => setConfirm(false)}
        onConfirm={() => void a.run("deleteArchive", () => deleteConnectorArchive(c.id), (r) => (setDeleted(deletedLine(r as object, false)), setConfirm(false)))}
      />
    </Group>
  );
}

function DataGroup({ c, a, logout }: { c: HubConnector; a: Actions; logout: boolean }) {
  const [confirm, setConfirm] = useState<"" | "logout" | "delete">("");
  const [deleted, setDeleted] = useState("");
  return (
    <Group footer={deleted || "删除：知识库里从它来的会话和人、收件箱里它的条目，并关掉同步；登录状态不受影响。"}>
      {logout && (isSignedIn(c) || !!c.via) && (
        <div className="px-4 py-2.5">
          <button type="button" disabled={!!a.busy} onClick={() => setConfirm("logout")} className="text-[13px] whitespace-nowrap text-red hover:underline disabled:opacity-50">退出登录</button>
        </div>
      )}
      <div className="px-4 py-2.5">
        <button type="button" disabled={!!a.busy} onClick={() => setConfirm("delete")} className="text-[13px] whitespace-nowrap text-red hover:underline disabled:opacity-50">删除已存的数据</button>
      </div>
      <Confirm
        open={confirm === "logout"}
        title={`退出 ${c.name} 登录？`}
        message={`hub 上存的 API 凭据和登录状态都会删掉，${c.name} 那边这个设备会话也会结束。再用要重新登录。`}
        action="退出登录"
        busy={a.busy === "logout"}
        onClose={() => setConfirm("")}
        onConfirm={() => void a.run("logout", () => connectorLogout(c.id), () => setConfirm(""))}
      />
      <Confirm
        open={confirm === "delete"}
        title={`删除 ${c.name} 存下的数据？`}
        message={`知识库里从 ${c.name} 来的会话和人、收件箱里它的条目都会删掉，同步会关掉。项目的总结在下次更新时不再包含它们。`}
        action="删除"
        busy={a.busy === "delete"}
        onClose={() => setConfirm("")}
        onConfirm={() => void a.run("delete", () => deleteConnectorData(c.id), (r) => (setDeleted(deletedLine(r as object)), setConfirm("")))}
      />
    </Group>
  );
}

function Confirm({ open, title, message, action, busy, onClose, onConfirm }: {
  open: boolean;
  title: string;
  message: string;
  action: string;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={title}
      width={420}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>取消</Button>
          <Button tone="danger" busy={busy} onClick={onConfirm}>{action}</Button>
        </>
      }
    >
      <p className="text-[12.5px] leading-relaxed text-ink-2">{message}</p>
    </Dialog>
  );
}

// ── signing in, a step at a time ─────────────────────────

/** A connector the user signs in to here (Telegram, a plugin's): switch, sign-in, feeds, sign-out and delete. */
function SignInSections({ c, reload }: { c: HubConnector; reload: () => Promise<void> }) {
  const a = useActions(c.id, reload);
  return (
    <>
      <SwitchGroup c={c} a={a} />
      {isEnabled(c) && !isSignedIn(c) && (signsInHere(c) ? (
        <SignInSteps c={c} reload={reload} footer={signInFooter(c)} />
      ) : (
        // Just switched on: its program has not started yet (the page asks again every second).
        <Group><p className="px-4 py-3 text-[12.5px] text-ink-3">{c.name} 启动后就能登录。</p></Group>
      ))}
      <FeedsGroup c={c} a={a} />
      <DataGroup c={c} a={a} logout />
    </>
  );
}

function SignInSteps({ c, reload, footer }: { c: HubConnector; reload: () => Promise<void>; footer: string }) {
  const [prompt, setPrompt] = useState<ConnectorPrompt | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true; // again after StrictMode's extra unmount in development
    return () => void (live.current = false);
  }, []);

  /** True when signing in is done. */
  const send = useCallback(async (step: string, vals: Record<string, string>, quiet = false): Promise<boolean> => {
    if (!quiet) setBusy(true);
    try {
      const p = await connectorLogin(c.id, step, vals);
      if (!live.current) return false;
      if (p.done) {
        setPrompt(null);
        setValues({});
        toast(p.account ? `已登录 ${p.account}` : `已登录 ${c.name}`, "ok");
        await reload();
        return true;
      }
      setPrompt((cur) => {
        if (p.step !== cur?.step) setValues({}); // a new step: its fields start empty
        return p;
      });
      if (!quiet) await reload(); // the status above follows (credentials saved: api_id shown)
    } catch (e) {
      if (live.current) toast(errorText(e), "error");
    } finally {
      if (!quiet && live.current) setBusy(false);
    }
    return false;
  }, [c.id, c.name, reload]);

  // A code on screen (GitHub's device flow): ask again every interval the hub gave until the user has entered it.
  const code = isDeviceCode(prompt) ? prompt : null;
  useEffect(() => {
    if (!code) return;
    let stop = false;
    let t: ReturnType<typeof setTimeout>;
    const tick = () => {
      t = setTimeout(async () => {
        if (stop) return;
        if (!(await send(code.step ?? "", {}, true)) && !stop) tick();
      }, Math.max(code.interval ?? 5, 1) * 1000);
    };
    tick();
    return () => {
      stop = true;
      clearTimeout(t);
    };
  }, [code?.step, code?.user_code]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!prompt) {
    return (
      <Group title="登录" footer={footer}>
        <div className="px-4 py-3">
          <Button tone="accent" busy={busy} onClick={() => void send("", {})}>登录 {c.name}</Button>
        </div>
      </Group>
    );
  }
  const canSend = canSendStep(prompt, values) && !busy;
  return (
    <Group title="登录" footer={footer}>
      {(prompt.hint || prompt.error) && (
        <div className="flex flex-col gap-1 px-4 py-2.5">
          {prompt.hint && <p className="text-[12.5px] leading-relaxed text-ink-2">{prompt.hint}</p>}
          {prompt.error && <p role="alert" className="text-[12px] text-red">{prompt.error}</p>}
        </div>
      )}
      {code ? (
        <div className="flex flex-col gap-3 px-4 py-3">
          <div className="flex items-center gap-3">
            <span className="min-w-0 flex-1 truncate font-mono text-[26px] font-semibold tracking-[0.12em] text-ink select-all">{code.user_code}</span>
            <IconButton
              icon="copy"
              label="复制验证码"
              onClick={() => void navigator.clipboard?.writeText(code.user_code ?? "").then(() => toast("已复制验证码", "ok"), () => toast("没复制上", "error"))}
            />
          </div>
          <p className="flex items-center gap-2 text-[12.5px] text-ink-3"><Spinner size={11} />等你在 {c.name} 上授权…</p>
          {code.url && (
            <div>
              <Button tone="accent" icon="external" onClick={() => window.open(code.url, "_blank", "noopener,noreferrer")}>打开 {c.name} 输入验证码</Button>
            </div>
          )}
        </div>
      ) : (
        <form
          className="flex flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSend) void send(prompt.step ?? "", values);
          }}
        >
          {(prompt.fields ?? []).map((f) => {
            const input = fieldInput(f);
            return (
              <label key={f.name} className="flex items-center gap-3 border-b border-line px-4 py-2">
                <span className="w-[96px] shrink-0 truncate text-[13px] text-ink">{f.label}</span>
                <input
                  value={values[f.name] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [f.name]: e.target.value }))}
                  type={input.type}
                  inputMode={input.inputMode}
                  autoComplete={input.autoComplete}
                  placeholder={f.placeholder ?? f.label}
                  spellCheck={false}
                  autoCapitalize="off"
                  disabled={busy}
                  className={cx(fieldClass, "font-mono")}
                />
              </label>
            );
          })}
          <div className="flex items-center gap-2 px-4 py-2.5">
            <Button type="submit" tone="accent" disabled={!canSend} busy={busy}>{busy ? "正在验证…" : "下一步"}</Button>
            <Button tone="quiet" onClick={() => (setPrompt(null), setValues({}))} disabled={busy}>取消</Button>
          </div>
        </form>
      )}
    </Group>
  );
}

// ── GitHub ───────────────────────────────────────────────

function GitHubSections({ c, reload }: { c: HubConnector; reload: () => Promise<void> }) {
  const a = useActions(c.id, reload);
  const [auth, setAuth] = useState<GitHubAuth | null>(null);
  const [authError, setAuthError] = useState("");
  const [choosing, setChoosing] = useState<string | null>(null);
  const [signIn, setSignIn] = useState(0); // remounts the sign-in when the source changes

  const loadAuth = useCallback(() => {
    githubAuth().then((x) => (setAuth(x), setAuthError("")), (e) => setAuthError(errorText(e)));
  }, []);
  useEffect(loadAuth, [loadAuth]);

  const choose = async (machine: string | null) => {
    setChoosing(machine ?? "device");
    try {
      setAuth(await setGitHubAuth(machine));
      setSignIn((n) => n + 1);
      await reload();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setChoosing(null);
    }
  };

  const row = (key: string, icon: "machine" | "key", title: string, sub: string, warn: boolean, selected: boolean, onPick: () => void) => (
    <button
      key={key}
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={choosing !== null}
      onClick={onPick}
      className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors enabled:hover:bg-hover disabled:cursor-default"
    >
      <Icon name={icon} size={15} className="shrink-0 text-ink-3" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-ink">{title}</span>
        <span className={cx("mt-0.5 block truncate text-[11.5px]", warn ? "text-orange" : "text-ink-3")}>{sub}</span>
      </span>
      {choosing === key ? <Spinner /> : selected ? <Icon name="check" size={14} stroke={2.6} className="shrink-0 text-accent-ink" /> : null}
    </button>
  );

  const missing = auth?.missing ?? [];
  return (
    <>
      <SwitchGroup c={c} a={a} />
      <Group title="从哪里登录" footer="选一台电脑就用它上面 gh 的登录；设备码登录不依赖任何电脑。换一个就是退出上一个。">
        <div role="radiogroup" aria-label="从哪里登录" className="divide-y divide-line">
          {!auth && !authError && <p className="flex items-center gap-2 px-4 py-3 text-[12.5px] text-ink-3"><Spinner size={11} />正在看各台电脑上的 gh 登录…</p>}
          {authError && <p className="px-4 py-3 text-[12.5px] text-red">读不到登录方式：{authError}</p>}
          {auth?.machines.map((m) => row(m.id, "machine", m.name, githubMachineLine(m), githubMachineWarns(m), auth.source === "machine" && auth.machine === m.id, () => void choose(m.id)))}
          {auth && row("device", "key", "设备码登录", "不用电脑：在 GitHub 上输入验证码授权", false, auth.source === "device", () => void choose(null))}
        </div>
      </Group>
      {auth?.source === "device" && isEnabled(c) && !isSignedIn(c) && (
        <SignInSteps key={signIn} c={c} reload={async () => (await reload(), loadAuth())} footer="设备码登录的授权加密存在 hub 上；改用电脑上的 gh 登录时会忘掉。" />
      )}
      {auth && missing.length > 0 && isSignedIn(c) && (
        <Group title="权限不够">
          <div className="flex items-start gap-2.5 px-4 py-3">
            <Icon name="info" size={14} className="mt-0.5 shrink-0 text-orange" />
            <p className="min-w-0 text-[13px] leading-relaxed text-ink select-text">{githubScopeAdvice(auth, missing)}</p>
          </div>
          {missing.flatMap((m) => m.affects).map((x) => <p key={x} className="px-4 py-2 text-[12.5px] text-ink-2">{x}</p>)}
        </Group>
      )}
      <FeedsGroup c={c} a={a} />
      <DataGroup c={c} a={a} logout={false} />
    </>
  );
}

/** GitHub's 收件箱: which CI failures come in, and repositories that bring nothing in (PUT …/github/settings {options}). */
function GitHubInbox({ options, onSaved }: { options: GitHubInboxOptions; onSaved: (c: HubConnector) => void }) {
  const [o, setO] = useState(options);
  const [repo, setRepo] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const save = async (next: GitHubInboxOptions) => {
    const before = o;
    setO(next);
    setSaving(true);
    try {
      onSaved(await setConnector("github", { options: next }));
      setError("");
    } catch (e) {
      setO(before);
      setError(`没存上：${errorText(e)}`);
    } finally {
      setSaving(false);
    }
  };
  const add = () => {
    const r = mutableRepo(repo, o.muted_repos);
    if (!r) return;
    setRepo("");
    void save({ ...o, muted_repos: [...o.muted_repos, r] });
  };
  return (
    <Group title="收件箱" footer={githubInboxFooter(o)}>
      <Row label="CI 失败">
        {saving && <Spinner />}
        <select value={o.ci} disabled={saving} onChange={(e) => void save({ ...o, ci: e.target.value })} aria-label="CI 失败" className={cx(fieldClass, "w-auto min-w-[140px]")}>
          {CI_MODES.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
        </select>
      </Row>
      {o.muted_repos.map((r) => (
        <div key={r} className="flex items-center gap-3 px-4 py-2">
          <Icon name="bellOff" size={14} className="shrink-0 text-ink-3" />
          <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink">{r}</span>
          <IconButton icon="trash" label={`不再静音 ${r}`} disabled={saving} onClick={() => void save({ ...o, muted_repos: o.muted_repos.filter((x) => x !== r) })} />
        </div>
      ))}
      <form
        className="flex items-center gap-2 px-4 py-2.5"
        onSubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <input value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="静音仓库：owner/name" aria-label="静音仓库" spellCheck={false} autoCapitalize="off" className={cx(fieldClass, "font-mono")} />
        <Button type="submit" tone="secondary" icon="plus" disabled={!mutableRepo(repo, o.muted_repos) || saving}>添加</Button>
      </form>
      {error && <p className="px-4 py-2.5 text-[12px] text-red">{error}</p>}
    </Group>
  );
}
