import { useState } from "react";
import { api, errorText, setToken } from "../lib/api";
import { Button, Spinner } from "../ui/atoms";
import { Icon } from "../ui/icons";

function browserName() {
  const ua = navigator.userAgent;
  const b = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "浏览器";
  const os = /Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return [b, os].filter(Boolean).join(" · ");
}

// Pair with the hub's token (`jarvisd hub pair` prints it) or a device token; the pairing token is traded for
// this browser's own one right away (docs/protocol.md「鉴权」), so it can be revoked on its own.
export function Login({ onDone }: { onDone: () => void }) {
  const [token, setTok] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const t = token.trim();
    if (!t) return;
    setBusy(true);
    setError("");
    try {
      await api("/machines", { token: t });
      let use = t;
      if (!t.startsWith("jd_")) {
        try {
          const r = await api<{ token: string; id: string }>("/auth/tokens", { method: "POST", json: { name: `网页（${browserName()}）`, kind: "web" }, token: t });
          use = r.token;
          localStorage.setItem("jarvis.tokenId", r.id);
        } catch {
          // an old hub without device tokens: keep the pairing token
        }
      }
      setToken(use);
      onDone();
    } catch (err) {
      setError(errorText(err) === "invalid token" ? "token 不对" : errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center bg-page px-5 py-16">
      <form onSubmit={submit} className="w-full max-w-[360px]" style={{ animation: "fade-up 450ms cubic-bezier(0.23,1,0.32,1) both" }}>
        <div className="mb-7 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
            <Icon name="spark" size={20} />
          </span>
          <div>
            <h1 className="text-[18px] font-semibold tracking-tight text-ink">连接到 Jarvis</h1>
            <p className="mt-1 text-[13px] leading-relaxed text-ink-2">在 hub 所在的机器上运行 <code className="rounded-[5px] bg-field px-1 py-0.5 font-mono text-[12px] shadow-hairline">jarvisd hub pair</code>，把打印出来的 token 贴到这里。</p>
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
              placeholder="hub token"
              aria-label="hub token"
              autoComplete="current-password"
              className="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-ink outline-none placeholder:font-sans placeholder:text-ink-3"
            />
          </label>
          <Button type="submit" tone="primary" disabled={!token.trim()} className="mt-1.5 h-9 w-full">
            {busy ? <Spinner /> : "连接"}
          </Button>
        </div>
        <p className="mt-3 h-5 text-center text-[12.5px] text-red" role="alert">
          {error}
        </p>
        <p className="text-center text-[11.5px] leading-relaxed text-ink-3">这个浏览器会拿到自己的凭据，可以在手机上的「设置 › 设备」里随时撤销。</p>
      </form>
    </div>
  );
}
