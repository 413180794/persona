// 设置 (docs/web.md): appearance, where the assistant runs, 模型 (every launcher: ~/.zshrc ones synced, Persona ones
// kept by the hub — docs/launchers.md), this browser and the other devices, about. The same data as the app's 设置.
import { useEffect, useMemo, useState } from "react";
import { api, ApiError, enc, errorText, setToken } from "../lib/api";
import { splitDevices } from "../lib/devices";
import { ago } from "../lib/format";
import { install, useInstall, type InstallState } from "../lib/pwa";
import { reset, useStore } from "../lib/store";
import { Button, cx, Dialog, EASE, IconButton, Loading, Spinner, toast } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { setTheme, useTheme, type Theme } from "./theme";
import { Group, Row, Pill, RowConfirm } from "./settings/SettingsParts";
import { HubList } from "./Hubs";
import { ModelConfiguration } from "./settings/ModelConfiguration";
import { AgentSettings } from "./settings/AgentSettings";
import { AssistantSettings } from "./settings/AssistantSettings";
import { BuiltinFace, PersonaPicker } from "./assistant/Persona";
import { Connectors } from "./settings/Connectors";
import { TriageSettings } from "./settings/TriageSettings";
import { TranslationSettings } from "./settings/TranslationSettings";
import { Plugins } from "./settings/Plugins";
import { HubSettings } from "./settings/HubSettings";
import { LauncherQuota } from "./settings/LauncherQuota";
import { LauncherPluginSections } from "./settings/PluginView";
import { launcherNameProblem, loadPresets, presetBody, presetChoices, presetDefaultModel, presetModelLabel, presetSubtitle, type LauncherPreset } from "../lib/launcher-presets";
import { Segmented } from "./assistant/parts";

// ── types (docs/launchers.md) ───────────────────────────

interface LauncherDetail {
  agent?: string;
  base_url?: string;
  auth?: string;
  models?: { role: string; env: string; model: string }[];
  context_window?: number;
  compact_window?: number;
  effort?: string;
  timeout_ms?: number;
  args?: string[];
  env?: { key: string; value: string }[];
  error?: string;
}

interface LauncherView {
  name: string;
  source: "zshrc" | "jarvis";
  kind?: "launcher" | "api";
  api?: "messages" | "responses";
  base_url?: string;
  model?: string;
  small_model?: string;
  /** Subagents (workflows, multi-agent runs): "" follows the model. */
  subagent_model?: string;
  /** An extra /model entry at the gateway. */
  custom_model?: string;
  custom_model_name?: string;
  custom_model_description?: string;
  args?: string[];
  has_key?: boolean;
  /** Managed: how the key goes to the gateway (x-api-key by default, or Authorization: Bearer). */
  auth?: "api_key" | "bearer";
  machines?: string[];
  hidden?: boolean;
  definitions?: Record<string, string>;
  details?: Record<string, LauncherDetail>;
}

interface LauncherCheck {
  ok: boolean;
  agent?: string;
  model?: string;
  answer?: string;
  millis: number;
  error?: string;
}

interface DeviceToken {
  id: string;
  name: string;
  kind: string;
  role: string;
  created_at?: string;
  last_seen?: string;
  /** Set once revoked: the hub keeps listing it (lib/devices.ts). */
  revoked_at?: string;
}

const enLauncher = (n: string) => `/launchers/${enc(n)}`;

export function tokens(n: number): string {
  if (n >= 1_000_000 && n % 100_000 === 0) return `${n % 1_000_000 === 0 ? n / 1_000_000 : (n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.floor(n / 1000)}K`;
  return String(n);
}

// ── page ────────────────────────────────────────────────

type Tab = "general" | "assistant" | "models" | "model-config" | "claude" | "codex" | "connectors" | "plugins" | "translate" | "devices" | "hub";
const TABS: Tab[] = ["general", "assistant", "models", "model-config", "claude", "codex", "connectors", "plugins", "translate", "devices", "hub"];
// The tab whose page opens one of its items, and the query parameter that names it.
const SUB: Partial<Record<Tab, string>> = { "model-config": "purpose", plugins: "plugin", connectors: "connector" };

export function Settings({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const [tab, setTab] = useState<Tab>(() => {
    const t = new URLSearchParams(location.search).get("tab");
    return TABS.includes(t as Tab) ? (t as Tab) : "general";
  });
  // The purpose (模型配置) or plugin (插件) the tab has open.
  const [sub, setSub] = useState<string | null>(() => {
    const q = new URLSearchParams(location.search);
    const param = SUB[q.get("tab") as Tab];
    return param ? q.get(param) : null;
  });
  // 插件 only on a hub with the plugin base (docs/plugins.md「hub 的接口」); a link to it still opens it.
  const hasPlugins = useStore((s) => s.features.includes("plugin_manager"));
  const tabs: { k: Tab; label: string; icon: IconName }[] = [
    { k: "general", label: "通用", icon: "gear" },
    { k: "assistant", label: "助手", icon: "spark" },
    { k: "models", label: "模型供应商", icon: "layers" },
    { k: "model-config", label: "模型配置", icon: "gear" },
    { k: "claude", label: "Claude Code", icon: "terminal" },
    { k: "codex", label: "Codex", icon: "terminal" },
    { k: "connectors", label: "连接器", icon: "link" },
    ...(hasPlugins || tab === "plugins" ? [{ k: "plugins" as Tab, label: "插件", icon: "plugin" as IconName }] : []),
    { k: "translate", label: "翻译", icon: "translate" },
    { k: "devices", label: "设备", icon: "key" },
    { k: "hub", label: "hub", icon: "hub" },
  ];
  const pick = (k: Tab, id: string | null = null) => {
    setTab(k);
    setSub(id);
    const param = SUB[k];
    history.replaceState(null, "", k === "general" ? "/settings" : `/settings?tab=${k}${id && param ? `&${param}=${enc(id)}` : ""}`);
  };
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="shrink-0 whitespace-nowrap text-[14px] font-semibold text-ink">设置</h1>
        <nav className="ml-1 flex min-w-0 items-center gap-0.5 overflow-x-auto rounded-control bg-inset p-0.5 shadow-hairline sm:ml-3" aria-label="设置分类">
          {tabs.map((t) => (
            <button
              key={t.k}
              type="button"
              onClick={() => pick(t.k)}
              aria-current={tab === t.k ? "page" : undefined}
              className={cx(
                "flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[7px] px-2.5 text-[12.5px] transition-[background-color,color] duration-150",
                tab === t.k ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
              )}
            >
              <Icon name={t.icon} size={13} />
              {t.label}
            </button>
          ))}
        </nav>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div key={tab} className="mx-auto max-w-[760px] px-5 py-6 sm:px-8" style={{ animation: `fade-up 320ms ${EASE} both` }}>
          {tab === "general" && <General onTriageModel={() => pick("model-config", "agent.triage")} />}
          {tab === "models" && <Models />}
          {tab === "model-config" && <ModelConfiguration open={sub} onOpen={(id) => pick("model-config", id)} onProviders={() => pick("models")} />}
          {tab === "assistant" && <AssistantSettings />}
          {(tab === "claude" || tab === "codex") && <AgentSettings key={tab} agent={tab} />}
          {tab === "connectors" && <Connectors open={sub} onOpen={(id) => pick("connectors", id)} onChannel={(id) => pick("plugins", `-channels/${id}`)} />}
          {tab === "plugins" && <Plugins open={sub} onOpen={(id) => pick("plugins", id)} onTab={(t, id) => TABS.includes(t as Tab) && pick(t as Tab, id)} />}
          {tab === "translate" && <TranslationSettings onConfigure={() => pick("model-config", "translate")} />}
          {tab === "devices" && <Devices />}
          {tab === "hub" && <HubSettings />}
        </div>
      </div>
    </div>
  );
}

const host = (u?: string) => {
  try {
    return u ? new URL(u).host : "";
  } catch {
    return u ?? "";
  }
};

// ── 通用 ────────────────────────────────────────────────

interface AssistantState {
  machine_id?: string;
  launcher?: string;
  cwd?: string;
}

function General({ onTriageModel }: { onTriageModel: () => void }) {
  const theme = useTheme();
  const machines = useStore((s) => s.machines);
  const [assistant, setAssistant] = useState<AssistantState | null>(null);

  useEffect(() => {
    api<AssistantState>("/assistant").then(setAssistant).catch((e) => toast(errorText(e), "error"));
  }, []);

  const themes: { k: Theme; label: string; icon: IconName }[] = [
    { k: "system", label: "跟随系统", icon: "monitor" },
    { k: "light", label: "浅色", icon: "sun" },
    { k: "dark", label: "深色", icon: "moon" },
  ];
  const machineName = machines.find((m) => m.id === assistant?.machine_id)?.name ?? assistant?.machine_id;

  return (
    <>
      <Group title="外观">
        <div className="flex gap-2 p-3">
          {themes.map((t) => (
            <button
              key={t.k}
              type="button"
              onClick={() => setTheme(t.k)}
              aria-pressed={theme === t.k}
              className={cx(
                "flex flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-control px-3 py-2 text-[12.5px] transition-[background-color,box-shadow,color] duration-150",
                theme === t.k ? "bg-accent-tint font-medium text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "text-ink-2 shadow-hairline hover:bg-hover",
              )}
            >
              <Icon name={t.icon} size={14} />
              {t.label}
            </button>
          ))}
        </div>
      </Group>

      <Group title="形象" footer="Persona 的名字、头像和对话里会动的状态，只在这个浏览器里生效；网页的颜色不变。形象包放在 hub 机器上。">
        <PersonaPicker builtin={<BuiltinFace size={56} />} />
      </Group>

      <Group title="Persona 助手" footer={`每段对话在 ${machineName ?? "hub 所在的机器"} 的 ${assistant?.cwd ?? "~/.persona/assistant"} 里后台运行。模型与启动器统一在「模型配置」中选择，连接与凭据在「模型供应商」中管理。`}>
        <Row label="运行在">{machineName ?? "—"}</Row>
      </Group>

      <TriageSettings onModel={onTriageModel} />

      <HubList />
      <About />
    </>
  );
}

function About() {
  const machines = useStore((s) => s.machines);
  const hubName = useStore((s) => s.hubName);
  // interconnected hubs (docs/plan-hub-mesh.md 2.6): this page talks to one hub and sees its whole group
  const mesh = useStore((s) => s.mesh);
  const others = mesh.filter((h) => !h.self);
  const logout = async () => {
    const id = localStorage.getItem("jarvis.tokenId");
    if (id) await api(`/auth/tokens/${enc(id)}`, { method: "DELETE" }).catch(() => {});
    localStorage.removeItem("jarvis.tokenId");
    setToken("");
    reset();
    location.href = "/";
  };
  return (
    <Group title="这个浏览器" footer="断开会撤销这个浏览器的 token，下次要重新贴 hub 的 token。hub 默认是明文 HTTP，只在内网 / VPN 里用。">
      <Row label="hub 地址" mono>
        {location.host}
      </Row>
      {hubName && <Row label="hub">{hubName}</Row>}
      {others.length > 0 && (
        <Row label="互联的 hub" hint="这些 hub 上的电脑也列在这里，经这台 hub 访问">
          {others.map((h) => (
            <span key={h.hub_id} className="flex items-center gap-1.5">
              <span className={cx("size-1.5 rounded-full", h.online === false ? "bg-ink-3" : "bg-green")} />
              {h.name || h.hub_id}
            </span>
          ))}
        </Row>
      )}
      <Row label="电脑">{`${machines.filter((m) => m.online).length} / ${machines.length} 在线`}</Row>
      <InstallRow />
      <div className="flex justify-end px-4 py-2.5">
        <Button tone="quiet" icon="logout" onClick={() => void logout()} className="text-red enabled:hover:text-red">
          断开这个浏览器
        </Button>
      </div>
    </Group>
  );
}

// Installing the web client as an app (lib/pwa.ts): the browser's prompt where it has one, else where to find it.
const INSTALL_HINT: Record<InstallState, string> = {
  installed: "已经在独立窗口里打开",
  prompt: "装到桌面或主屏幕，在独立窗口里打开",
  ios: "在 Safari 的分享菜单里选「添加到主屏幕」",
  safari: "在 Safari 的「文件」菜单里选「添加到程序坞」",
  insecure: "用 HTTPS 地址打开 hub 才能安装",
  menu: "在浏览器菜单里选「安装」或「添加到主屏幕」",
};

function InstallRow() {
  const state = useInstall();
  return (
    <Row label="安装为 App" hint={INSTALL_HINT[state]}>
      {state === "installed" && <Pill tone="green">已安装</Pill>}
      {state === "prompt" && (
        <Button icon="download" onClick={() => void install()}>
          安装
        </Button>
      )}
    </Row>
  );
}

// ── 模型 ────────────────────────────────────────────────

function Models() {
  const machines = useStore((s) => s.machines);
  const [list, setList] = useState<LauncherView[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [editing, setEditing] = useState<LauncherView | "new" | null>(null);

  const load = () =>
    api<{ launchers: LauncherView[] }>("/launchers")
      .then((r) => (setList(r.launchers ?? []), setError("")))
      .catch((e) => setError(errorText(e)));
  useEffect(() => void load(), []);

  const name = (id: string) => machines.find((m) => m.id === id)?.name ?? id;
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!list || !q) return list ?? [];
    return list.filter((l) => [l.name, l.model, l.small_model, l.base_url, ...(l.machines ?? []).map(name)].some((v) => v?.toLowerCase().includes(q)));
  }, [list, query, machines]);
  const managed = shown.filter((l) => l.source === "jarvis");
  const zshrc = shown.filter((l) => l.source !== "jarvis" && !l.hidden);
  const hidden = shown.filter((l) => l.source !== "jarvis" && l.hidden);

  const row = (l: LauncherView) => (
    <button key={l.name} type="button" onClick={() => setOpen(l.name)} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-[13px] font-medium text-ink">{l.name}</span>
          {l.source === "jarvis" && <Pill tone="accent">Persona</Pill>}
          {l.kind === "api" && <Pill>直接 API</Pill>}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[11.5px] text-ink-3">
          {l.model && <span className="truncate font-mono text-ink-2">{l.model}</span>}
          {host(l.base_url) && <span className="truncate">{host(l.base_url)}</span>}
        </div>
      </div>
      {l.machines && l.machines.length > 0 && <span className="hidden max-w-[200px] shrink-0 truncate text-[11.5px] text-ink-3 sm:block">{l.machines.map(name).join("、")}</span>}
      <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
    </button>
  );

  return (
    <>
      <div className="mb-4 flex items-center gap-2">
        <label className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control bg-surface px-3 shadow-btn focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
          <Icon name="search" size={13} stroke={2} className="text-ink-3" />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="名字、模型、网关或电脑" className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3" aria-label="搜索供应商" />
          {query && <IconButton icon="x" label="清除" size={22} iconSize={12} onClick={() => setQuery("")} />}
        </label>
        <Button tone="accent" icon="plus" onClick={() => setEditing("new")}>
          新增供应商
        </Button>
      </div>
      {!list && !error && <Loading />}
      {error && <p className="text-[13px] text-red">{error}</p>}
      {list && query && shown.length === 0 && <p className="py-10 text-center text-[13px] text-ink-3">没有和「{query}」相关的供应商</p>}
      {list && (!query || managed.length > 0) && (
        <Group title={`Persona 管理 ${managed.length}`} footer={managed.length ? "Key 加密存在 hub；直接 API 由 hub 调用，启动器以 jarvisd launch <名字> 运行。" : undefined}>
          {managed.length ? managed.map(row) : <p className="px-4 py-3 text-[12.5px] text-ink-3">还没有。点「新增供应商」：填协议、网关地址、Key 和默认模型；用途在「模型配置」中选择。</p>}
        </Group>
      )}
      {zshrc.length > 0 && (
        <Group title={`来自 ~/.zshrc ${zshrc.length}`} footer="各台电脑 ~/.zshrc 里调用 claude 的函数，每次打开这页时同步；新加的函数会自动出现。">
          {zshrc.map(row)}
        </Group>
      )}
      {hidden.length > 0 && (
        <Group title={`已隐藏 ${hidden.length}`} footer="只是在 Persona 里不显示，~/.zshrc 没有改。点进去可以恢复。">
          {hidden.map(row)}
        </Group>
      )}
      <LauncherDetailDialog
        name={open}
        onClose={() => setOpen(null)}
        onChanged={() => void load()}
        onEdit={(l) => {
          setOpen(null);
          setEditing(l);
        }}
      />
      <LauncherEditor existing={editing} onClose={() => setEditing(null)} onSaved={() => void load()} />
    </>
  );
}

function LauncherDetailDialog({ name, onClose, onChanged, onEdit }: { name: string | null; onClose: () => void; onChanged: () => void; onEdit: (l: LauncherView) => void }) {
  const machines = useStore((s) => s.machines);
  const [l, setL] = useState<LauncherView | null>(null);
  const [error, setError] = useState("");
  const [checks, setChecks] = useState<Record<string, LauncherCheck | null>>({});
  const [confirm, setConfirm] = useState(false);
  const [showDef, setShowDef] = useState(false);
  const mname = (id: string) => machines.find((m) => m.id === id)?.name ?? id;
  // plugins draw a section here (docs/plugins.md「声明式 UI」 launcher_section)
  const hasPlugins = useStore((s) => s.features.includes("plugin_manager"));

  useEffect(() => {
    setL(null);
    setError("");
    setChecks({});
    setConfirm(false);
    setShowDef(false);
    if (!name) return;
    api<{ launcher: LauncherView }>(enLauncher(name))
      .then((r) => setL(r.launcher))
      .catch((e) => setError(errorText(e)));
  }, [name]);

  // machines whose launcher sets the same, once
  const groups = useMemo(() => {
    const out: { ids: string[]; d: LauncherDetail }[] = [];
    for (const [id, d] of Object.entries(l?.details ?? {}).sort()) {
      const g = out.find((x) => JSON.stringify(x.d) === JSON.stringify(d));
      if (g) g.ids.push(id);
      else out.push({ ids: [id], d });
    }
    return out;
  }, [l]);

  const testOn = l?.source === "jarvis" ? [""] : (l?.machines ?? []).filter((id) => machines.find((m) => m.id === id)?.online !== false);
  const running = Object.values(checks).some((c) => c === null);
  const check = async () => {
    if (!l) return;
    setChecks(Object.fromEntries(testOn.map((id) => [id, null])));
    await Promise.all(
      testOn.map(async (id) => {
        let c: LauncherCheck;
        try {
          c = (await api<{ check: LauncherCheck }>(`${enLauncher(l.name)}/check${id ? `?machine=${enc(id)}` : ""}`, { method: "POST" })).check;
        } catch (e) {
          c = { ok: false, millis: 0, error: errorText(e) };
        }
        setChecks((x) => ({ ...x, [id]: c }));
      }),
    );
  };

  const remove = async () => {
    if (!l) return;
    try {
      await api(enLauncher(l.name), { method: "DELETE" });
      toast(l.source === "jarvis" ? `已删除 ${l.name}` : `已隐藏 ${l.name}`, "ok");
      onChanged();
      onClose();
    } catch (e) {
      toast(errorText(e), "error");
    }
  };
  const restore = async () => {
    if (!l) return;
    try {
      await api(`${enLauncher(l.name)}/restore`, { method: "POST" });
      toast(`已恢复 ${l.name}`, "ok");
      onChanged();
      onClose();
    } catch (e) {
      toast(errorText(e), "error");
    }
  };

  return (
    <Dialog
      open={name !== null}
      onClose={onClose}
      width={620}
      title={<span className="font-mono">{name}</span>}
      footer={
        l && (
          <>
            {l.hidden ? (
              <Button tone="secondary" onClick={() => void restore()}>
                恢复显示
              </Button>
            ) : (
              <span className="relative">
                <Button tone="quiet" icon={l.source === "jarvis" ? "x" : undefined} onClick={() => setConfirm((c) => !c)} className="text-red enabled:hover:text-red">
                  {l.source === "jarvis" ? "删除供应商" : "在 Persona 里隐藏"}
                </Button>
                {/* the confirmation comes up right at the button (like the app), not as another dialog */}
                {confirm && (
                  <span className="absolute bottom-full left-0 mb-1.5 rounded-control bg-surface p-1 shadow-overlay" style={{ animation: `pop-in 160ms ${EASE} both` }}>
                    <Button tone="danger" onClick={() => void remove()}>
                      {l.source === "jarvis" ? "确认删除" : "确认隐藏"}
                    </Button>
                  </span>
                )}
              </span>
            )}
            <span className="flex-1" />
            {l.source === "jarvis" && (
              <Button tone="secondary" onClick={() => onEdit(l)}>
                编辑
              </Button>
            )}
            <Button tone="primary" icon={running ? undefined : "run"} busy={running} disabled={testOn.length === 0} onClick={() => void check()}>
              {Object.keys(checks).length ? "再测一次" : "测试可用性"}
            </Button>
          </>
        )
      }
    >
      {!l && !error && <Loading />}
      {error && <p className="text-[13px] text-red">{error}</p>}
      {l && (
        <div className="flex max-h-[62vh] flex-col gap-4 overflow-y-auto pr-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <Pill tone={l.source === "jarvis" ? "accent" : undefined}>{l.source === "jarvis" ? "Persona 管理" : "~/.zshrc 里的函数"}</Pill>
            {(l.machines ?? []).map((id) => (
              <Pill key={id}>{mname(id)}</Pill>
            ))}
            {l.hidden && <Pill tone="red">已隐藏</Pill>}
          </div>

          {/* how much is left comes before trying it, as in the app */}
          {l.kind !== "api" && <LauncherQuota name={l.name} />}

          {Object.keys(checks).length > 0 && (
            <KV title="可用性">
              {Object.entries(checks).map(([id, c]) => (
                <div key={id} className="flex items-start gap-2.5 py-1.5">
                  <span className="mt-0.5 flex size-4 shrink-0 items-center justify-center">{c === null ? <Spinner /> : <Icon name={c.ok ? "check" : "x"} size={13} stroke={2.6} className={c.ok ? "text-green" : "text-red"} />}</span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2 text-[12.5px]">
                      <span className="font-medium text-ink">{id ? mname(id) : "hub 所在的机器"}</span>
                      {c && <span className="text-ink-3 tabular-nums">{(c.millis / 1000).toFixed(c.millis >= 10_000 ? 0 : 1)} 秒</span>}
                    </div>
                    {c === null ? (
                      <div className="text-[11.5px] text-ink-3">正在请求…</div>
                    ) : c.ok ? (
                      <div className="truncate text-[11.5px] text-ink-3">{[c.model, c.answer && `回答「${c.answer}」`].filter(Boolean).join(" · ")}</div>
                    ) : (
                      <pre className="mt-0.5 whitespace-pre-wrap break-all font-mono text-[11px] text-red">{c.error}</pre>
                    )}
                  </div>
                </div>
              ))}
            </KV>
          )}

          {l.source === "jarvis" && (
            <KV title="设置">
              <KVRow k="接入方式" v={l.kind === "api" ? "直接 API" : "启动器"} />
              {l.kind === "api" && <KVRow k="协议" v={l.api === "responses" ? "Responses" : "Messages"} />}
              <KVRow k="Base URL" v={l.base_url} mono />
              <KVRow k="模型" v={l.model} mono />
              {l.kind !== "api" && l.small_model && <KVRow k="小模型" v={l.small_model} mono />}
              {l.kind !== "api" && l.subagent_model && <KVRow k="子 Agent 模型" v={l.subagent_model} mono />}
              {l.kind !== "api" && l.custom_model && <KVRow k="自定义模型" v={l.custom_model} mono />}
              {l.kind !== "api" && l.custom_model_name && <KVRow k="自定义模型名" v={l.custom_model_name} />}
              <KVRow k="API Key" v={l.has_key ? "已设置（不显示）" : "未设置"} />
              {l.api !== "responses" && <KVRow k="认证方式" v={l.auth === "bearer" ? "Bearer" : "x-api-key"} />}
              {l.kind !== "api" && l.args?.length ? <KVRow k="额外参数" v={l.args.join(" ")} mono /> : null}
            </KV>
          )}

          {hasPlugins && <LauncherPluginSections launcher={l.name} machine={l.source === "jarvis" ? undefined : testOn[0] || undefined} />}

          {groups.map((g) => (
            <DetailBlock key={g.ids.join()} d={g.d} place={groups.length > 1 ? g.ids.map(mname).join("、") : undefined} />
          ))}

          {l.definitions && Object.keys(l.definitions).length > 0 && (
            <div>
              <button type="button" onClick={() => setShowDef((x) => !x)} className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2 hover:text-ink">
                <Icon name="chevron" size={10} stroke={2.4} style={{ transform: showDef ? "none" : "rotate(-90deg)", transition: "transform 150ms" }} />
                函数原文
                <span className="font-normal text-ink-3">（像 Key、Token 的值已换成 •••）</span>
              </button>
              {showDef &&
                Object.entries(l.definitions).map(([id, def]) => (
                  <div key={id} className="mt-2">
                    {Object.keys(l.definitions!).length > 1 && <div className="mb-1 text-[11px] text-ink-3">{mname(id)}</div>}
                    <pre className="overflow-x-auto rounded-control bg-inset p-3 font-mono text-[11px] leading-relaxed text-ink-2 shadow-hairline">{def}</pre>
                  </div>
                ))}
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
}

function KV({ title, children }: { title: React.ReactNode; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-[11.5px] font-medium text-ink-3">{title}</div>
      <div className="divide-y divide-line rounded-control bg-inset px-3 shadow-hairline">{children}</div>
    </div>
  );
}

function KVRow({ k, v, mono, sub }: { k: React.ReactNode; v?: React.ReactNode; mono?: boolean; sub?: string }) {
  return (
    <div className="flex items-baseline gap-3 py-1.5">
      <div className={cx("shrink-0", sub ? "w-[220px]" : "w-[104px]")}>
        <div className="text-[12px] text-ink-2">{k}</div>
        {sub && <div className="font-mono text-[10px] break-all text-ink-3">{sub}</div>}
      </div>
      <div className={cx("min-w-0 flex-1 break-all text-[12.5px] text-ink", mono && "font-mono")}>{v || "—"}</div>
    </div>
  );
}

function DetailBlock({ d, place }: { d: LauncherDetail; place?: string }) {
  const t = (s: string) => (place ? `${s} · ${place}` : s);
  if (d.error) return <KV title={t("运行")}><p className="py-2 text-[12px] text-ink-3">{d.error}</p></KV>;
  const agent = d.agent === "codex" ? "Codex" : d.agent === "traex" ? "TRAEX" : "Claude Code";
  return (
    <>
      <KV title={t("运行")}>
        <KVRow k="Agent" v={agent} />
        {d.base_url && <KVRow k="网关" v={d.base_url} mono />}
        {d.auth && <KVRow k="Key" v={`已设置 · ${d.auth}`} mono />}
      </KV>
      {d.models?.length ? (
        <KV title={t("模型")}>
          {d.models.map((m) => (
            <KVRow key={m.role + m.env} k={m.role} sub={m.env} v={m.model} mono />
          ))}
        </KV>
      ) : null}
      {(d.context_window || d.compact_window || d.effort || d.timeout_ms) && (
        <KV title={t("上下文")}>
          {d.context_window ? <KVRow k="上下文窗口" v={`${tokens(d.context_window)} tokens`} /> : null}
          {d.compact_window ? <KVRow k="自动压缩窗口" v={`${tokens(d.compact_window)} tokens`} /> : null}
          {d.effort ? <KVRow k="思考强度" v={d.effort} /> : null}
          {d.timeout_ms ? <KVRow k="请求超时" v={d.timeout_ms >= 60_000 ? `${Math.round(d.timeout_ms / 60_000)} 分钟` : `${Math.round(d.timeout_ms / 1000)} 秒`} /> : null}
        </KV>
      )}
      {d.args?.length ? (
        <KV title={t("启动参数")}>
          <p className="py-2 font-mono text-[11.5px] break-all text-ink">{d.args.join(" ")}</p>
        </KV>
      ) : null}
      {d.env?.length ? (
        <KV title={t(`其他设置 ${d.env.length}`)}>
          {d.env.map((e) => (
            <KVRow key={e.key} k={<span className="font-mono text-[11px]">{e.key}</span>} v={e.value} mono />
          ))}
        </KV>
      ) : null}
    </>
  );
}

function LauncherEditor({ existing, onClose, onSaved }: { existing: LauncherView | "new" | null; onClose: () => void; onSaved: () => void }) {
  const isNew = existing === "new";
  const old = existing && existing !== "new" ? existing : null;
  const [f, setF] = useState({ name: "", base_url: "", api_key: "", model: "", small_model: "", subagent_model: "", custom_model: "", custom_model_name: "", custom_model_description: "", args: "", kind: "launcher", api: "messages", auth: "api_key" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // 从插件添加 (docs/plugins.md「模型供应商」): only when adding, and only when the hub's plugins offer providers.
  const [presets, setPresets] = useState<LauncherPreset[]>([]);
  const [source, setSource] = useState<"manual" | "plugin">("manual");
  const [pick, setPick] = useState("");
  const [pf, setPf] = useState({ name: "", api_key: "", model: "" });
  const preset = presets.find((p) => p.id === pick);
  const fromPlugin = isNew && source === "plugin" && !!preset;

  useEffect(() => {
    if (!isNew) return;
    setSource("manual");
    setPf({ name: "", api_key: "", model: "" });
    let live = true;
    loadPresets()
      .then((list) => {
        if (!live) return;
        setPresets(list);
        setPick(list[0]?.id ?? "");
        setPf((x) => ({ ...x, model: list[0] ? presetDefaultModel(list[0]) : "" }));
      })
      .catch(() => live && setPresets([]));
    return () => void (live = false);
  }, [existing]);

  const choose = (p: LauncherPreset) => {
    setPick(p.id);
    setPf((x) => ({ ...x, model: presetDefaultModel(p) }));
  };
  const nameProblem = launcherNameProblem(pf.name);
  const presetReady = !!preset && !!pf.name.trim() && !nameProblem && !!pf.api_key.trim();

  useEffect(() => {
    setError("");
    setF(old ? { name: old.name, base_url: old.base_url ?? "", api_key: "", model: old.model ?? "", small_model: old.small_model ?? "", subagent_model: old.subagent_model ?? "", custom_model: old.custom_model ?? "", custom_model_name: old.custom_model_name ?? "", custom_model_description: old.custom_model_description ?? "", args: (old.args ?? []).join(" "), kind: old.kind ?? "launcher", api: old.api ?? "messages", auth: old.auth ?? "api_key" } : { name: "", base_url: "", api_key: "", model: "", small_model: "", subagent_model: "", custom_model: "", custom_model_name: "", custom_model_description: "", args: "", kind: "launcher", api: "messages", auth: "api_key" });
  }, [existing]);

  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF((x) => ({ ...x, [k]: e.target.value }));
  // The Responses API always takes the key as Bearer: nothing to choose.
  const responses = f.kind === "api" && f.api === "responses";
  const ready = fromPlugin ? presetReady : f.name.trim() && f.base_url.trim() && f.model.trim() && (!isNew || f.api_key.trim());
  const save = async () => {
    if (busy || !ready) return;
    setBusy(true);
    setError("");
    try {
      if (fromPlugin && preset) {
        const body = presetBody(preset, pf.name, pf.api_key, pf.model);
        await api("/launchers", { method: "POST", json: body });
        toast(`已新增 ${body.name}`, "ok");
        onSaved();
        onClose();
        return;
      }
      const body = { name: f.name.trim(), kind: f.kind, api: f.kind === "api" ? f.api : "messages", base_url: f.base_url.trim(), model: f.model.trim(), small_model: f.kind === "api" ? "" : f.small_model.trim(), subagent_model: f.kind === "api" ? "" : f.subagent_model.trim(), custom_model: f.kind === "api" ? "" : f.custom_model.trim(), custom_model_name: f.kind === "api" ? "" : f.custom_model_name.trim(), custom_model_description: f.kind === "api" ? "" : f.custom_model_description.trim(), args: f.kind === "api" ? [] : f.args.split(/\s+/).filter(Boolean), api_key: f.api_key || undefined, auth: responses ? "api_key" : f.auth };
      await api(isNew ? "/launchers" : enLauncher(body.name), { method: isNew ? "POST" : "PUT", json: body });
      toast(isNew ? `已新增 ${body.name}` : `已保存 ${body.name}`, "ok");
      onSaved();
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const field = (k: keyof typeof f, label: string, opts: { placeholder?: string; hint?: string; type?: string; mono?: boolean; disabled?: boolean } = {}) => (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-ink-2">{label}</span>
      <input
        value={f[k]}
        onChange={set(k)}
        type={opts.type ?? "text"}
        disabled={opts.disabled}
        placeholder={opts.placeholder}
        autoComplete="off"
        spellCheck={false}
        className={cx("h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60", opts.mono && "font-mono")}
      />
      {opts.hint && <span className="mt-1 block text-[11px] text-ink-3">{opts.hint}</span>}
    </label>
  );

  return (
    <Dialog
      open={existing !== null}
      onClose={onClose}
      width={480}
      title={isNew ? "新增供应商" : `编辑 ${old?.name ?? ""}`}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!ready} onClick={() => void save()}>
            保存
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (ready) void save();
        }}
      >
        {isNew && presets.length > 0 && (
          <Segmented<"manual" | "plugin">
            label="来源"
            value={source}
            onChange={setSource}
            options={[{ v: "manual", label: "自己填写" }, { v: "plugin", label: "从插件添加" }]}
          />
        )}
        {fromPlugin && preset ? (
          <PresetFields presets={presets} preset={preset} onPick={choose} pf={pf} setPf={setPf} nameProblem={nameProblem} />
        ) : (
        <>
        <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">接入方式</span>
          <select value={f.kind} onChange={(e) => setF((x) => ({ ...x, kind: e.target.value, api: "messages" }))} disabled={!isNew}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none disabled:opacity-60">
            <option value="launcher">Claude Code 启动器</option>
            <option value="api">直接 API</option>
          </select>
        </label>
        {f.kind === "api" && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">API 协议</span>
          <select value={f.api} onChange={(e) => setF((x) => ({ ...x, api: e.target.value }))}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none">
            <option value="messages">Anthropic Messages</option>
            <option value="responses">OpenAI Responses</option>
          </select>
        </label>}
        {field("name", "名字", { placeholder: "比如 jv_strong", hint: "字母、数字、_ 和 -；在模型配置中按这个名字选。", mono: true, disabled: !isNew })}
        {field("base_url", "Base URL", { placeholder: "https://…", hint: f.api === "responses" ? "Responses 兼容 API 的基础地址。" : "Messages 兼容 API 的基础地址。", mono: true })}
        {field("api_key", isNew || !old?.has_key ? "API Key" : "API Key（不改就留空）", { type: "password", hint: "加密存在 hub，接口不会再返回。" })}
        {!responses && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">认证方式</span>
          <select value={f.auth} onChange={(e) => setF((x) => ({ ...x, auth: e.target.value }))}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none">
            <option value="api_key">x-api-key（ANTHROPIC_API_KEY）</option>
            <option value="bearer">Bearer（ANTHROPIC_AUTH_TOKEN）</option>
          </select>
          <span className="mt-1 block text-[11px] text-ink-3">网关只认 Authorization: Bearer 时选 Bearer。</span>
        </label>}
        <div className={f.kind === "api" ? "" : "grid grid-cols-2 gap-3"}>
          {field("model", "默认模型", { placeholder: "主模型", mono: true })}
          {f.kind !== "api" && field("small_model", "小模型（可选）", { placeholder: "默认同模型", mono: true })}
          {f.kind !== "api" && field("subagent_model", "子 Agent 模型（可选）", { placeholder: "默认同模型", mono: true, hint: "workflow 等多 Agent 运行用。" })}
        </div>
        {f.kind !== "api" && (
          <div className="grid grid-cols-2 gap-3">
            {field("custom_model", "自定义模型（可选）", { placeholder: "网关里的模型 ID", mono: true })}
            {field("custom_model_name", "显示名（可选）", {})}
          </div>
        )}
        {f.kind !== "api" && field("custom_model_description", "自定义模型说明（可选）", { hint: "在 /model 里多一个可选项；说明要配一个名字。" })}
        {f.kind !== "api" && field("args", "额外参数（可选）", { placeholder: "--dangerously-skip-permissions", hint: "加在 claude 后面，空格分隔，每个都以 - 开头。", mono: true })}
        </>
        )}
        {error && <p className="text-[12px] text-red">{error}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

/** 从插件添加: pick one of the plugins' providers, then only the name, the key and maybe another of its models. */
function PresetFields({ presets, preset, onPick, pf, setPf, nameProblem }: {
  presets: LauncherPreset[];
  preset: LauncherPreset;
  onPick: (p: LauncherPreset) => void;
  pf: { name: string; api_key: string; model: string };
  setPf: React.Dispatch<React.SetStateAction<{ name: string; api_key: string; model: string }>>;
  nameProblem: string;
}) {
  const input = "h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]";
  const choices = presetChoices(preset);
  return (
    <>
      <div>
        <span className="mb-1 block text-[12px] font-medium text-ink-2">供应商</span>
        <div role="radiogroup" aria-label="供应商" className="overflow-hidden rounded-control bg-field shadow-inset-field">
          {presets.map((p) => (
            <button
              key={p.id}
              type="button"
              role="radio"
              aria-checked={p.id === preset.id}
              onClick={() => onPick(p)}
              className="flex w-full items-center gap-3 border-b border-line px-3 py-2 text-left transition-colors last:border-b-0 hover:bg-hover"
            >
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px] font-medium text-ink">{p.name}</div>
                {p.description && <div className="line-clamp-2 text-[11.5px] text-ink-2">{p.description}</div>}
                <div className="truncate text-[11px] text-ink-3">{presetSubtitle(p)}</div>
              </div>
              {p.id === preset.id && <Icon name="check" size={13} stroke={2.2} className="shrink-0 text-accent" />}
            </button>
          ))}
        </div>
        <span className="mt-1 block text-[11px] text-ink-3">地址、接入方式和模型由插件「{preset.plugin}」提供；加好以后和自己填写的一样可以编辑。</span>
      </div>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">名字</span>
        <input value={pf.name} onChange={(e) => setPf((x) => ({ ...x, name: e.target.value }))} placeholder="比如 jv_team" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        <span className={cx("mt-1 block text-[11px]", nameProblem ? "text-red" : "text-ink-3")}>{nameProblem || "字母、数字、_ 和 -；在模型配置中按这个名字选。"}</span>
      </label>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">API Key</span>
        <input value={pf.api_key} onChange={(e) => setPf((x) => ({ ...x, api_key: e.target.value }))} type="password" autoComplete="off" spellCheck={false} className={input} />
        <span className="mt-1 block text-[11px] text-ink-3">{preset.key_hint ? `${preset.key_hint} ` : ""}加密存在 hub，接口不会再返回。</span>
      </label>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">默认模型</span>
        {choices.length ? (
          <select value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} className={cx(input, "font-mono")}>
            {choices.map((m) => (
              <option key={m.id} value={m.id}>
                {presetModelLabel(m)}
              </option>
            ))}
          </select>
        ) : (
          <input value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} placeholder="主模型" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        )}
        <span className="mt-1 block text-[11px] text-ink-3">
          {preset.models_error ? `读不到插件的模型列表（${preset.models_error}），这里是它声明的。` : "各用途的具体选择在「模型配置」里统一管理；未单独指定时使用这里的默认。"}
        </span>
      </label>
    </>
  );
}

// ── 设备 ────────────────────────────────────────────────

function Devices() {
  const [data, setData] = useState<{ tokens: DeviceToken[]; current?: string } | null>(null);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const load = () =>
    api<{ tokens: DeviceToken[]; current?: string }>("/auth/tokens")
      .then((r) => (setData({ tokens: r.tokens ?? [], current: r.current }), setError("")))
      .catch((e) => setError(errorText(e)));
  useEffect(() => void load(), []);

  const revoke = async (id: string) => {
    try {
      await api(`/auth/tokens/${enc(id)}`, { method: "DELETE" });
      toast("已撤销", "ok");
      setConfirm(null);
      void load();
    } catch (e) {
      toast(errorText(e), "error");
    }
  };
  const kind: Record<string, string> = { phone: "手机", watch: "手表", other: "其他（网页 / 命令行）", web: "网页" };
  const { active, revoked } = splitDevices(data?.tokens ?? [], data?.current);

  // A revoked credential is shown greyed, with when it was revoked and no 撤销 (iOS DeviceCredentials.swift).
  const row = (t: DeviceToken, gone: boolean) => (
    <div key={t.id} className="flex items-center gap-3 px-4 py-2.5">
      <Icon name={t.kind === "phone" ? "machine" : t.kind === "watch" ? "monitor" : "web"} size={15} className="shrink-0 text-ink-3" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={cx("truncate text-[13px]", gone ? "text-ink-3" : "text-ink")}>{t.name || t.id}</span>
          {t.id === data?.current && <Pill tone="green">这个浏览器</Pill>}
          {t.role !== "full" && <Pill>{t.role === "answer" ? "只能回答" : t.role}</Pill>}
        </div>
        <div className="mt-0.5 truncate text-[11.5px] text-ink-3">
          {kind[t.kind] ?? t.kind}
          {gone && t.revoked_at ? ` · ${ago(t.revoked_at)}撤销` : ""}
          {!gone && t.last_seen ? ` · ${ago(t.last_seen)}用过` : ""}
          {t.created_at ? ` · ${ago(t.created_at)}添加` : ""}
        </div>
      </div>
      {!gone && t.id !== data?.current && (
        <RowConfirm
          open={confirm === t.id}
          confirm="确认撤销"
          onCancel={() => setConfirm(null)}
          onConfirm={() => void revoke(t.id)}
          trigger={
            <Button tone="quiet" onClick={() => setConfirm(t.id)} className="text-red enabled:hover:text-red">
              撤销
            </Button>
          }
        />
      )}
    </div>
  );

  return (
    <>
      {!data && !error && <Loading />}
      {error && <p className="text-[13px] text-red">{error}</p>}
      {data && (
        <Group title={`设备 ${active.length}`} footer="每台手机、手表、浏览器各用自己的 token；撤销立刻失效，并断开它的连接。配对用的 hub token 不在这里。">
          {active.length === 0 && <p className="px-4 py-3 text-[12.5px] text-ink-3">还没有设备。</p>}
          {active.map((t) => row(t, false))}
        </Group>
      )}
      {data && revoked.length > 0 && <Group title={`已撤销 ${revoked.length}`}>{revoked.map((t) => row(t, true))}</Group>}
    </>
  );
}
