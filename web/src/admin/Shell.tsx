// The admin shell: navigation (left on wide screens, a strip on top on narrow ones), the restart banner, the page.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Button, cx, Dialog, EASE, toast } from "../ui/atoms";
import { setTheme, useTheme, type Theme } from "../views/theme";
import { adminApi, errorText } from "./api";
import { AdminIcon, type AdminIconName } from "./icons";
import { memberNotice, restartText } from "./logic";
import { NAV, type Page } from "./nav";
import { navigate, pathOf, usePage } from "./router";
import { refreshMesh, share, useShared } from "./store";
import type { Overview } from "./types";
import { Devices } from "./pages/Devices";
import { Machines } from "./pages/Machines";
import { Network } from "./pages/Network";
import { OverviewPage } from "./pages/Overview";
import { Peers } from "./pages/Peers";

export function Shell({ hubName, onLogout }: { hubName: string; onLogout: () => void }) {
  const page = usePage();
  const shared = useShared();
  const name = shared.hubName || hubName;

  // The banner and TLS state are needed on every page: one overview fetch when the shell opens.
  useEffect(() => {
    adminApi<Overview>("/overview").then(
      (o) => share({ restart: o.restart, canRestart: o.hub.can_restart, tls: o.tls, hubName: o.hub.name, startedAt: o.hub.started_at, hubId: o.hub.hub_id }),
      () => {},
    );
  }, []);

  // Center or member: asked again on every page change (the center may have changed meanwhile, or gone offline).
  useEffect(() => {
    void refreshMesh();
  }, [page]);

  useEffect(() => {
    document.title = `${NAV.find((n) => n.page === page)?.label ?? "管理"} · ${name || "Persona"} 管理`;
  }, [page, name]);

  const logout = async () => {
    try {
      await adminApi("/logout", "POST");
    } catch {
      // the session is gone either way
    }
    onLogout();
  };

  return (
    <div className="flex h-full flex-col bg-page lg:flex-row">
      <aside className="hidden w-[232px] shrink-0 flex-col border-r border-line bg-canvas lg:flex">
        <Brand name={name} />
        <SideNav page={page} />
        <div className="mt-auto border-t border-line p-2.5">
          <ThemeSwitch />
          <button type="button" onClick={logout} className="mt-1 flex h-8 w-full items-center gap-2 whitespace-nowrap rounded-control px-2 text-[12.5px] text-ink-2 transition-colors hover:bg-hover hover:text-ink">
            <AdminIcon name="logout" size={14} />
            退出登录
          </button>
        </div>
      </aside>
      <header className="shrink-0 border-b border-line bg-canvas lg:hidden">
        <div className="flex h-12 items-center gap-2 px-3">
          <Brand name={name} compact />
          <div className="ml-auto flex items-center gap-1">
            <ThemeSwitch compact />
            <button type="button" onClick={logout} aria-label="退出登录" title="退出登录" className="flex size-8 items-center justify-center rounded-[8px] text-ink-3 hover:bg-hover hover:text-ink">
              <AdminIcon name="logout" size={15} />
            </button>
          </div>
        </div>
        <nav aria-label="管理分类" className="flex items-center gap-0.5 overflow-x-auto px-2 pb-2">
          {NAV.map((n, i) => [
            n.advanced && !NAV[i - 1]?.advanced && <span key="advanced" aria-hidden className="mx-1 h-4 w-px shrink-0 bg-line-strong" />,
            <a
              key={n.page}
              href={pathOf(n.page)}
              onClick={(e) => go(e, n.page)}
              aria-current={page === n.page ? "page" : undefined}
              className={cx(
                "flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-control px-2.5 text-[12.5px] transition-colors",
                page === n.page ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
              )}
            >
              <AdminIcon name={n.icon} size={14} />
              {n.label}
            </a>,
          ])}
        </nav>
      </header>
      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[860px] px-4 py-6 sm:px-8 sm:py-8">
          <RestartBanner />
          <MemberBanner />
          <div key={page} style={{ animation: `fade-up 320ms ${EASE} both` }}>
            {page === "overview" && <OverviewPage />}
            {page === "machines" && <Machines />}
            {page === "peers" && <Peers />}
            {page === "devices" && <Devices />}
            {page === "network" && <Network />}
          </div>
        </div>
      </main>
    </div>
  );
}

function go(e: React.MouseEvent, page: Page) {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  navigate(page);
}

function Brand({ name, compact }: { name: string; compact?: boolean }) {
  return (
    <div className={cx("flex min-w-0 items-center gap-2.5", !compact && "px-3.5 pt-4 pb-3")}>
      <span className={cx("flex shrink-0 items-center justify-center rounded-[9px] bg-ink text-surface shadow-raised", compact ? "size-7" : "size-8")}>
        <AdminIcon name="shield" size={compact ? 14 : 16} />
      </span>
      <div className="min-w-0">
        <div className="truncate text-[13px] font-semibold text-ink">{name || "Persona hub"}</div>
        <div className="truncate text-[11px] text-ink-3">hub 管理</div>
      </div>
    </div>
  );
}

// SideNav is Beautiful UI's sidebar nav: one highlight that glides to the hovered row, resting on the current page.
function SideNav({ page }: { page: Page }) {
  const box = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<Page, HTMLAnchorElement>());
  const [hover, setHover] = useState<Page | null>(null);
  const [pos, setPos] = useState<{ top: number; height: number } | null>(null);
  useLayoutEffect(() => {
    const el = rows.current.get(hover ?? page);
    const next = el ? { top: el.offsetTop, height: el.offsetHeight } : null;
    setPos((p) => (p?.top === next?.top && p?.height === next?.height ? p : next));
  });
  return (
    <nav ref={box} aria-label="管理分类" onMouseLeave={() => setHover(null)} className="relative flex flex-col gap-px px-2.5">
      <span
        aria-hidden
        className="pointer-events-none absolute inset-x-2.5 rounded-[7px] bg-hover"
        style={{ top: pos?.top ?? 0, height: pos?.height ?? 0, opacity: pos ? 1 : 0, transition: `top 220ms ${EASE}, height 220ms ${EASE}, opacity 150ms ease` }}
      />
      {NAV.map((n, i) => [
        n.advanced && !NAV[i - 1]?.advanced && (
          <div key="advanced" className="px-2 pt-4 pb-1 text-[11px] font-medium text-ink-3">
            高级
          </div>
        ),
        <a
          key={n.page}
          ref={(el) => {
            if (el) rows.current.set(n.page, el);
          }}
          href={pathOf(n.page)}
          onClick={(e) => go(e, n.page)}
          onMouseEnter={() => setHover(n.page)}
          aria-current={page === n.page ? "page" : undefined}
          className={cx("relative z-10 flex h-8 items-center gap-2.5 whitespace-nowrap rounded-[7px] px-2 text-[13px]", page === n.page ? "font-medium text-ink" : "text-ink-2")}
        >
          <AdminIcon name={n.icon} size={15} className={page === n.page ? "text-ink" : "text-ink-3"} />
          {n.label}
        </a>,
      ])}
    </nav>
  );
}

const THEMES: { t: Theme; icon: AdminIconName; label: string }[] = [
  { t: "system", icon: "monitor", label: "跟随系统" },
  { t: "light", icon: "sun", label: "浅色" },
  { t: "dark", icon: "moon", label: "深色" },
];

function ThemeSwitch({ compact }: { compact?: boolean }) {
  const theme = useTheme();
  return (
    <div role="radiogroup" aria-label="外观" className={cx("flex items-center gap-0.5 rounded-control bg-inset p-0.5 shadow-hairline", !compact && "w-full")}>
      {THEMES.map((o) => (
        <button
          key={o.t}
          type="button"
          role="radio"
          aria-checked={theme === o.t}
          aria-label={o.label}
          title={o.label}
          onClick={() => setTheme(o.t)}
          className={cx(
            "flex h-7 items-center justify-center rounded-[7px] transition-[background-color,color] duration-150",
            compact ? "w-7" : "flex-1",
            theme === o.t ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
          )}
        >
          <AdminIcon name={o.icon} size={13} />
        </button>
      ))}
    </div>
  );
}

function RestartBanner() {
  const { restart, canRestart, startedAt } = useShared();
  const [asking, setAsking] = useState(false);
  const [phase, setPhase] = useState<"idle" | "restarting" | "slow">("idle");
  if (!restart.required && phase === "idle") return null;

  const run = async () => {
    setAsking(false);
    setPhase("restarting");
    try {
      await adminApi("/restart", "POST");
    } catch (e) {
      setPhase("idle");
      toast(errorText(e), "error");
      return;
    }
    // Back when the overview answers with a newer start time; give up waiting after 90 seconds.
    const until = Date.now() + 90_000;
    let down = false;
    while (Date.now() < until) {
      await new Promise((r) => setTimeout(r, 1500));
      try {
        const o = await adminApi<Overview>("/overview");
        if (startedAt ? o.hub.started_at !== startedAt : down) {
          share({ restart: o.restart, canRestart: o.hub.can_restart, tls: o.tls, hubName: o.hub.name, startedAt: o.hub.started_at });
          setPhase("idle");
          toast("hub 已重启", "ok");
          return;
        }
      } catch {
        down = true; // still on its way back
      }
    }
    setPhase("slow");
  };

  if (phase === "restarting")
    return (
      <Banner tone="accent" title="正在重启 hub…" detail="通常几秒钟就好；这期间 App 会短暂断开再自己连上。" />
    );
  if (phase === "slow")
    return <Banner tone="red" title="hub 还没回来" detail="已经等了 90 秒。到 hub 机器上看看日志：systemd 用 journalctl --user -u jarvisd，Docker 用 docker compose logs hub。" action={<Button onClick={() => setPhase("idle")}>知道了</Button>} />;

  return (
    <>
      <Banner
        tone="orange"
        title={restartText(restart)}
        detail={canRestart ? restart.reasons.join("；") : `${restart.reasons.join("；")}。在 hub 机器上重启：systemctl --user restart jarvisd，或 docker compose restart hub。`}
        action={canRestart && (
          <Button tone="primary" onClick={() => setAsking(true)}>
            <AdminIcon name="power" size={13} stroke={2.2} />
            重启 hub
          </Button>
        )}
      />
      <Dialog
        open={asking}
        onClose={() => setAsking(false)}
        title="重启 hub？"
        footer={
          <>
            <Button tone="quiet" onClick={() => setAsking(false)}>
              取消
            </Button>
            <Button tone="primary" onClick={run}>
              重启
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">
          hub 会退出，由 systemd 或 Docker 马上拉起来。重启期间手机和网页会断开几秒钟，正在进行的 Agent 不受影响（它们跑在各自的电脑上）。
        </p>
      </Dialog>
    </>
  );
}

// MemberBanner: on a member hub every page says the group is the center's to run (docs/plan-hub-center.md 3).
function MemberBanner() {
  const { mesh } = useShared();
  const page = usePage();
  const n = memberNotice(mesh);
  if (!n) return null;
  return (
    <Banner
      tone={n.offline ? "orange" : "accent"}
      icon="mesh"
      title={n.title}
      detail={n.detail}
      action={n.offline && page !== "peers" && <Button onClick={() => navigate("peers")}>换中心</Button>}
    />
  );
}

function Banner({ tone, title, detail, action, icon }: { tone: "orange" | "accent" | "red"; title: string; detail?: string; action?: React.ReactNode; icon?: AdminIconName }) {
  const bg = { orange: "bg-orange-tint", accent: "bg-accent-tint", red: "bg-red-tint" }[tone];
  const ink = { orange: "text-orange", accent: "text-accent-ink", red: "text-red" }[tone];
  const spin = !icon && tone === "accent"; // the restart in progress
  return (
    <div role="status" className={cx("mb-6 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-card px-3.5 py-3", bg)} style={{ animation: `fade-up 300ms ${EASE} both` }}>
      <AdminIcon name={icon ?? (spin ? "retry" : "alert")} size={16} className={cx("shrink-0 self-start", ink)} style={spin ? { animation: "spin 1.2s linear infinite" } : undefined} />
      <div className="min-w-[200px] flex-1">
        <div className={cx("text-[13px] font-medium", ink)}>{title}</div>
        {detail && <div className="mt-0.5 text-[12px] leading-relaxed text-ink-2">{detail}</div>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}
