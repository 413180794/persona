// The live screen of a Mac (docs/computer-use.md「实时画面」): macOS's Screen Sharing on its 127.0.0.1:5900, through the
// hub's port relay, drawn by noVNC. View only until 接管; Screen Sharing asks for the Mac account's name and password
// (Apple's ARD sign-in), which go to the Mac only and are not kept. The app shows this page in its web view.
import RFB from "@novnc/novnc";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, errorText } from "../lib/api";
import { cx, Spinner } from "../ui/atoms";
import "../views/theme"; // follows the system's light / dark while open (index.html picked the first one)
import { ensureSubtleAES } from "./aes";
import { accountOf, keepLabel, keysymsOf, screenSocket, toApp, type ScreenTarget } from "./screen";

type Phase =
  | { kind: "checking" }
  | { kind: "blocked"; why: string }
  | { kind: "connecting" }
  | { kind: "signin"; failed?: string }
  | { kind: "live" }
  | { kind: "closed"; why: string };

type Computer = { enabled?: boolean; screen_sharing?: boolean };
/** A sign-in sent to the Mac: typed (and to keep or not), or the one the app kept (saved). */
type Credentials = { account: string; password: string; remember: boolean; saved: boolean };
type MachineRow = { id: string; name: string; home?: string; online: boolean };

export function ScreenView({ target }: { target: ScreenTarget }) {
  const [phase, setPhase] = useState<Phase>({ kind: "checking" });
  const [name, setName] = useState(target.name);
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [control, setControl] = useState(false);
  const controlRef = useRef(false);
  controlRef.current = control;
  const [attempt, setAttempt] = useState(0);
  // The app keeps the Mac's sign-in (#1322): 「在这台手机上记住」, on unless unticked.
  const canKeep = target.embedded && target.keep !== "";
  const [remember, setRemember] = useState(true);
  const queued = useRef<Credentials | null>(null); // typed while there was no connection waiting: sent on the next
  const sent = useRef<Credentials | null>(null); // sent on this connection, until the Mac says yes or no
  const waiting = useRef(false); // this connection asked for a sign-in and has none yet
  const answer = useRef<((c: Credentials) => void) | null>(null);
  const unlockAsked = useRef(false); // 一键解锁 asked for its password once per page
  // Set once the hub says there is a screen to connect to: the connection lives from then on, whatever the page
  // shows (asking to sign in is part of it), until 重新连接 or the page goes.
  const [go, setGo] = useState(false);
  const screen = useRef<HTMLDivElement>(null);
  const rfb = useRef<RFB | null>(null);

  // What the hub says first: why there would be nothing to connect to.
  useEffect(() => {
    let gone = false;
    (async () => {
      if (!target.token) return setPhase({ kind: "blocked", why: "先在网页上登录 hub，再打开这一页。" });
      try {
        const { machines } = await api<{ machines: MachineRow[] }>("/machines", { token: target.token });
        const m = machines.find((x) => x.id === target.mid);
        if (m) {
          setName(m.name);
          setAccount((a) => a || accountOf(m.home));
        }
        if (m && !m.online) throw new Error(`${m.name} 离线，连上后再看。`);
        // The plugin cua or ego, on for this Mac, lets its screen through (#1507); each asked fresh, 404 while off.
        const fresh = async (what: "computer" | "browser") => {
          try {
            return await api<Computer>(`/machines/${encodeURIComponent(target.mid)}/${what}`, { token: target.token });
          } catch (e) {
            if (e instanceof ApiError && e.status === 404) return null;
            throw e;
          }
        };
        const c = await fresh("computer");
        const b = c?.enabled ? null : await fresh("browser");
        const on = c?.enabled ? c : b?.enabled ? b : null;
        if (!c && !b) throw new Error("操控电脑和浏览器（ego lite）的插件都没打开，或这台不是 Mac。");
        if (!on) throw new Error("这台 Mac 还没有打开「操控电脑」或「浏览器」：在它的机器页打开。");
        if (!on.screen_sharing) throw new Error("这台 Mac 的屏幕共享没开：在它的「系统设置 › 通用 › 共享」里打开「屏幕共享」。");
        if (!gone) {
          setPhase({ kind: "connecting" });
          setGo(true);
        }
      } catch (e) {
        const why = e instanceof ApiError && e.status === 404 ? "这台机器不在 hub 上，或不是 Mac。"
          : e instanceof ApiError && e.status === 401 ? (target.embedded ? "hub 没认这台设备的凭据：在 App 的「设置 › hub」里重新配对。" : "hub 没认这个凭据：在网页上重新登录 hub。")
          : errorText(e);
        if (!gone) setPhase({ kind: "blocked", why });
      }
    })();
    return () => {
      gone = true;
    };
  }, [target]);

  // The connection, once there is something to connect to; again on 重新连接.
  useEffect(() => {
    if (!go || !screen.current) return;
    ensureSubtleAES(); // an http page has no crypto.subtle, and the ARD sign-in needs AES
    const r = new RFB(screen.current, screenSocket(window.location, target.mid, target.token), { shared: true });
    r.viewOnly = true;
    r.scaleViewport = true;
    r.resizeSession = false;
    r.background = "transparent";
    waiting.current = false;
    sent.current = null;
    let askedApp = false;
    let appSilent: ReturnType<typeof setTimeout> | undefined;
    const send = (c: Credentials) => {
      waiting.current = false;
      sent.current = c;
      r.sendCredentials({ username: c.account, password: c.password });
      setPhase({ kind: "connecting" });
    };
    const showForm = () => setPhase((p) => (p.kind === "signin" ? p : { kind: "signin" }));
    answer.current = (c) => {
      clearTimeout(appSilent);
      if (waiting.current) send(c);
    };
    r.addEventListener("connect", () => {
      setPhase({ kind: "live" });
      // 一键解锁 (#1507 a-2): in, so the app hands over the kept password for the lock screen (personaScreen.unlock).
      if (target.unlock && !unlockAsked.current) {
        unlockAsked.current = true;
        toApp({ credentials: "unlock" });
      }
      const c = sent.current;
      sent.current = null;
      // What worked goes to the app's keychain when 记住 was on; off, what it kept goes.
      if (c && canKeep && !c.saved) toApp(c.remember ? { credentials: "worked", account: c.account, password: c.password } : { credentials: "forget" });
    });
    r.addEventListener("credentialsrequired", () => {
      waiting.current = true;
      const q = queued.current;
      queued.current = null;
      if (q) return send(q);
      if (canKeep && !askedApp) {
        // The app answers with what it kept (personaScreen.signIn), or null for the form; the form if it says nothing.
        askedApp = true;
        appSilent = setTimeout(showForm, 1500);
        return toApp({ credentials: "wanted" });
      }
      showForm();
    });
    r.addEventListener("securityfailure", (e) => {
      const reason = (e as CustomEvent<{ reason?: string }>).detail?.reason;
      const kept = sent.current?.saved ? sent.current : null;
      sent.current = null;
      if (kept) setAccount(kept.account);
      setPhase({
        kind: "signin",
        failed: kept ? "记住的密码不对了（那台 Mac 可能改过密码）：重新输入，连上后替换记住的那个" : reason ? `没登录上：${reason}` : "账号或密码不对",
      });
    });
    r.addEventListener("disconnect", (e) => {
      const clean = (e as CustomEvent<{ clean: boolean }>).detail?.clean;
      setPhase((p) => (p.kind === "signin" && p.failed ? p : { kind: "closed", why: clean ? "已断开。" : "连接断了：那台 Mac 的屏幕共享没有回应，或网络断了。" }));
    });
    rfb.current = r;
    return () => {
      clearTimeout(appSilent);
      answer.current = null;
      rfb.current = null;
      r.disconnect();
    };
  }, [go, attempt, target, canKeep]);

  useEffect(() => {
    if (rfb.current) rfb.current.viewOnly = !control;
  }, [control, phase.kind]);

  // In the app (#1343) the app draws the name, the state and 只看 / 接管 around the page: it hears the phase, and
  // says 接管 through window.personaScreen.
  useEffect(() => {
    if (!target.embedded) return;
    toApp({ phase: phase.kind });
  }, [target.embedded, phase.kind]);
  useEffect(() => {
    if (!target.embedded) return;
    (window as unknown as { personaScreen?: unknown }).personaScreen = {
      setControl: (on: boolean) => setControl(on),
      // The app's answer to { credentials: "unlock" }: a key wakes the lock screen and puts the cursor in its password
      // field, then the password and Return; 只看 again after.
      unlock: (kept: { password?: string } | null) => {
        const r = rfb.current;
        if (!r || !kept?.password) return;
        const password = kept.password;
        r.viewOnly = false;
        r.sendKey(0xffe1, "ShiftLeft");
        setTimeout(() => {
          for (const k of keysymsOf(password)) r.sendKey(k, null);
          r.viewOnly = !controlRef.current;
        }, 800);
      },
      // The app's answer to { credentials: "wanted" }: the Mac's sign-in it kept, or null (the form).
      signIn: (kept: { account?: string; password?: string } | null) => {
        if (kept?.account && kept.password) {
          setAccount(kept.account);
          answer.current?.({ account: kept.account, password: kept.password, remember: true, saved: true });
        } else {
          answer.current = null;
          setPhase((p) => (p.kind === "signin" ? p : { kind: "signin" }));
        }
      },
    };
  }, [target.embedded]);

  const signIn = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      const c: Credentials = { account: account.trim(), password, remember: canKeep && remember, saved: false };
      setPassword("");
      setPhase({ kind: "connecting" });
      if (rfb.current && waiting.current) {
        waiting.current = false;
        sent.current = c;
        rfb.current.sendCredentials({ username: c.account, password: c.password });
        return;
      }
      // A refused sign-in closes the connection: a new one takes this when it asks.
      queued.current = c;
      setAttempt((n) => n + 1);
    },
    [account, password, canKeep, remember],
  );

  const reconnect = () => {
    setAttempt((n) => n + 1);
    setPhase({ kind: "connecting" });
  };

  const live = phase.kind === "live";
  return (
    <div className={cx("flex h-dvh flex-col text-ink", target.embedded ? "bg-transparent" : "bg-canvas")}>
      {!target.embedded && (
      <header className="flex h-11 shrink-0 items-center gap-2 border-b border-line bg-surface px-3">
        {!target.embedded && (
          <a href={`/machines/${encodeURIComponent(target.mid)}`} className="whitespace-nowrap rounded-control px-2 py-1 text-[12.5px] text-ink-2 hover:bg-hover hover:text-ink">
            返回
          </a>
        )}
        <p className="min-w-0 flex-1 truncate text-[13px] font-medium">
          {name} 的屏幕
          <span className="ml-2 text-[11.5px] font-normal text-ink-3">{live ? (control ? "你在操作" : "只看") : phaseLabel(phase)}</span>
        </p>
        {live && (
          <div role="group" aria-label="只看或接管" className="flex shrink-0 rounded-control bg-field p-0.5">
            {[false, true].map((on) => (
              <button
                key={String(on)}
                type="button"
                aria-pressed={control === on}
                onClick={() => setControl(on)}
                className={cx(
                  "h-7 whitespace-nowrap rounded-[7px] px-3 text-[12px] font-medium transition-colors",
                  control === on ? "bg-surface text-ink shadow-btn" : "text-ink-2 hover:text-ink",
                )}
              >
                {on ? "接管" : "只看"}
              </button>
            ))}
          </div>
        )}
      </header>
      )}
      <main className="relative min-h-0 flex-1">
        <div ref={screen} className={cx("absolute inset-0", !live && "invisible")} data-testid="screen" />
        {!live && (
          <div className="absolute inset-0 flex items-center justify-center p-6">
            {phase.kind === "signin" ? (
              <form onSubmit={signIn} className="flex w-full max-w-[340px] flex-col gap-3 rounded-card bg-surface p-5 shadow-card">
                <div>
                  <p className="text-[14px] font-medium">登录这台 Mac 的屏幕共享</p>
                  <p className="mt-1 text-[12.5px] text-ink-2">
                    用 {name} 上的 macOS 账号和密码。只发给那台 Mac{canKeep && remember ? "；记住的话存在这台设备的钥匙串里，不上传。" : "，不保存。"}
                  </p>
                </div>
                {phase.failed && <p className="text-[12.5px] text-red">{phase.failed}</p>}
                <input
                  value={account}
                  onChange={(e) => setAccount(e.target.value)}
                  placeholder="账号"
                  autoComplete="username"
                  autoCapitalize="none"
                  className="h-9 rounded-control bg-field px-3 text-[13px] outline-none focus:ring-2 focus:ring-accent/40"
                />
                <input
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="密码"
                  type="password"
                  autoComplete="current-password"
                  className="h-9 rounded-control bg-field px-3 text-[13px] outline-none focus:ring-2 focus:ring-accent/40"
                />
                {canKeep && (
                  <label className="flex items-center gap-2 whitespace-nowrap text-[12.5px] text-ink-2">
                    <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} className="size-3.5 shrink-0 accent-[var(--accent)]" />
                    {keepLabel(target.keep)}
                  </label>
                )}
                <button
                  type="submit"
                  disabled={!account.trim() || !password}
                  className="h-9 whitespace-nowrap rounded-control bg-accent text-[13px] font-medium text-white disabled:opacity-45"
                >
                  连接
                </button>
              </form>
            ) : (
              <div className="flex max-w-[360px] flex-col items-center gap-3 text-center">
                {(phase.kind === "checking" || phase.kind === "connecting") && <Spinner size={18} />}
                <p className="text-[13px] text-ink-2">{phaseText(phase, name)}</p>
                {phase.kind === "closed" && (
                  <button type="button" onClick={reconnect} className="h-8 whitespace-nowrap rounded-control bg-surface px-3 text-[12.5px] font-medium shadow-btn hover:bg-hover">
                    重新连接
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </main>
    </div>
  );
}

function phaseLabel(p: Phase): string {
  switch (p.kind) {
    case "signin":
      return "要登录";
    case "closed":
    case "blocked":
      return "没连上";
    default:
      return "连接中";
  }
}

function phaseText(p: Phase, name: string): string {
  switch (p.kind) {
    case "checking":
      return `正在找 ${name}…`;
    case "connecting":
      return `正在连接 ${name} 的屏幕…`;
    case "blocked":
    case "closed":
      return p.why;
    default:
      return "";
  }
}
