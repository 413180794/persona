// The live screen of a Mac (docs/computer-use.md「实时画面」): macOS's Screen Sharing on its 127.0.0.1:5900, through the
// hub's port relay, drawn by noVNC. View only until 接管; Screen Sharing asks for the Mac account's name and password
// (Apple's ARD sign-in), which go to the Mac only and are not kept. The app shows this page in its web view.
import RFB from "@novnc/novnc";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, errorText } from "../lib/api";
import { cx, Spinner } from "../ui/atoms";
import "../views/theme"; // follows the system's light / dark while open (index.html picked the first one)
import { ensureSubtleAES } from "./aes";
import { accountOf, screenSocket, type ScreenTarget } from "./screen";

type Phase =
  | { kind: "checking" }
  | { kind: "blocked"; why: string }
  | { kind: "connecting" }
  | { kind: "signin"; failed?: string }
  | { kind: "live" }
  | { kind: "closed"; why: string };

type Computer = { enabled?: boolean; screen_sharing?: boolean };
type MachineRow = { id: string; name: string; home?: string; online: boolean };

export function ScreenView({ target }: { target: ScreenTarget }) {
  const [phase, setPhase] = useState<Phase>({ kind: "checking" });
  const [name, setName] = useState(target.name);
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [control, setControl] = useState(false);
  const [attempt, setAttempt] = useState(0);
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
        const c = await api<Computer>(`/machines/${encodeURIComponent(target.mid)}/computer`, { token: target.token });
        if (!c.enabled) throw new Error("这台 Mac 还没有打开「操控电脑」：在它的机器页打开。");
        if (!c.screen_sharing) throw new Error("这台 Mac 的屏幕共享没开：在它的「系统设置 › 通用 › 共享」里打开「屏幕共享」。");
        if (!gone) {
          setPhase({ kind: "connecting" });
          setGo(true);
        }
      } catch (e) {
        const why = e instanceof ApiError && e.status === 404 ? "操控电脑的插件没打开，或这台不是 Mac。"
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
    r.addEventListener("connect", () => setPhase({ kind: "live" }));
    r.addEventListener("credentialsrequired", () => setPhase((p) => (p.kind === "signin" ? p : { kind: "signin" })));
    r.addEventListener("securityfailure", (e) => {
      const reason = (e as CustomEvent<{ reason?: string }>).detail?.reason;
      setPhase({ kind: "signin", failed: reason ? `没登录上：${reason}` : "账号或密码不对" });
    });
    r.addEventListener("disconnect", (e) => {
      const clean = (e as CustomEvent<{ clean: boolean }>).detail?.clean;
      setPhase((p) => (p.kind === "signin" && p.failed ? p : { kind: "closed", why: clean ? "已断开。" : "连接断了：那台 Mac 的屏幕共享没有回应，或网络断了。" }));
    });
    rfb.current = r;
    return () => {
      rfb.current = null;
      r.disconnect();
    };
  }, [go, attempt, target]);

  useEffect(() => {
    if (rfb.current) rfb.current.viewOnly = !control;
  }, [control, phase.kind]);

  // In the app (#1343) the app draws the name, the state and 只看 / 接管 around the page: it hears the phase, and
  // says 接管 through window.personaScreen.
  useEffect(() => {
    if (!target.embedded) return;
    const w = window as unknown as { webkit?: { messageHandlers?: { screen?: { postMessage(m: unknown): void } } } };
    w.webkit?.messageHandlers?.screen?.postMessage({ phase: phase.kind });
  }, [target.embedded, phase.kind]);
  useEffect(() => {
    if (!target.embedded) return;
    (window as unknown as { personaScreen?: unknown }).personaScreen = { setControl: (on: boolean) => setControl(on) };
  }, [target.embedded]);

  const signIn = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      if (!rfb.current) {
        setAttempt((n) => n + 1);
        setPhase({ kind: "connecting" });
        return;
      }
      rfb.current.sendCredentials({ username: account.trim(), password });
      setPassword("");
      setPhase({ kind: "connecting" });
    },
    [account, password],
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
                  <p className="mt-1 text-[12.5px] text-ink-2">用 {name} 上的 macOS 账号和密码。只发给那台 Mac，不保存。</p>
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
