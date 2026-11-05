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
import { ExtensionThemePicker } from "../ui/ExtensionThemePicker";
import { setTheme, useTheme, type Theme } from "./theme";
import { Group, Row, Pill, RowConfirm, Switch } from "./settings/SettingsParts";
import { LanguageRow } from "./settings/LanguageRow";
import { setReplyMotionEnabled, useReplyMotionSetting } from "../ui/stream/motion";
import { setSidebarTerminals, useSidebarTerminals } from "../lib/sidebar-terminals";
import { setAgentDetails, setContextAsUsed, useAgentDetails, useContextAsUsed } from "../lib/context-usage";
import { setAttentionSoundEnabled, useAttentionSoundSetting } from "../lib/attention";
import { HubList } from "./Hubs";
import { ModelConfiguration } from "./settings/ModelConfiguration";
import { AgentSettings } from "./settings/AgentSettings";
import { AgentGroup } from "./settings/AgentGroup";
import { AssistantSettings } from "./settings/AssistantSettings";
import { BuiltinFace, PersonaPicker } from "./assistant/Persona";
import { Connectors } from "./settings/Connectors";
import { TriageSettings } from "./settings/TriageSettings";
import { TranslationSettings } from "./settings/TranslationSettings";
import { Extensions } from "./settings/Extensions";
import { ShortcutSettings } from "./settings/ShortcutSettings";
import { ChannelList, ChannelPage } from "./settings/Channels";
import { settingsTarget } from "../lib/channels";
import { routeSub } from "../lib/extension-market";
import { HubSettings } from "./settings/HubSettings";
import { LauncherQuota } from "./settings/LauncherQuota";
import { LauncherExtensionSections } from "./settings/ExtensionView";
import { hubHasExt } from "../lib/extension-api";
import { launcherNameProblem, loadPresets, presetBody, presetChoices, presetDefaultModel, presetModelLabel, presetSubtitle, type LauncherPreset } from "../lib/launcher-presets";
import { Segmented } from "./assistant/parts";
import { FROM_LIST, LIST_GROUPS, openedFromList, settingsAt, settingsHref, TAB_INFO, TABS, type Tab } from "./settings/nav";
import { SHEET_QUERY, useMedia } from "../ui/overlay";
import { formatList, t } from "../i18n";

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

export function Settings({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const phone = useMedia(SHEET_QUERY);
  // The category (null: none named, the phone's list) and the purpose (模型配置) or extension (扩展) it has open.
  const [at, setAt] = useState(() => settingsAt(location.search));
  // Back from a category opened from the list: the URL went back, the page follows it.
  useEffect(() => {
    const back = () => {
      if (location.pathname !== "/settings") return;
      const now = settingsAt(location.search);
      setAt((was) => (was.tab === now.tab && was.sub === now.sub ? was : now));
    };
    addEventListener("popstate", back);
    return () => removeEventListener("popstate", back);
  }, []);
  const listed = phone && !at.tab;
  const tab: Tab = at.tab ?? "general";
  const { sub } = at;
  // 扩展 only on a hub with the extension base (docs/extensions.md「hub 的接口」); a link to it still opens it.
  const hasExtensions = useStore((s) => hubHasExt(s.features, "manager"));
  const hasChannels = useStore((s) => s.features.includes("channels"));
  const shown = (k: Tab) => (k !== "extensions" || hasExtensions || tab === "extensions") && (k !== "channels" || hasChannels || tab === "channels");
  const tabs = TABS.filter(shown).map((k) => ({ k, ...TAB_INFO[k] }));
  const pick = (k: Tab, id: string | null = null) => {
    setAt({ tab: k, sub: id });
    const to = settingsHref(k, id, phone);
    // from the phone's list the category goes on top of it, so Back returns to the list
    if (listed) history.pushState({ [FROM_LIST]: true }, "", to);
    else history.replaceState(openedFromList(history.state) ? history.state : null, "", to);
  };
  const toList = () => {
    if (openedFromList(history.state)) return history.back();
    history.replaceState(null, "", "/settings");
    setAt({ tab: null, sub: null });
  };
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        {phone && at.tab ? (
          <>
            <IconButton icon="chevronLeft" label={t("settings.back")} onClick={toList} />
            <h1 className="min-w-0 truncate text-[14px] font-semibold text-ink">{TAB_INFO[tab].label}</h1>
          </>
        ) : (
          <>
            <IconButton icon="sidebar" label={t("settings.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
            <h1 className="shrink-0 whitespace-nowrap text-[14px] font-semibold text-ink">{t("settings.title")}</h1>
          </>
        )}
        {!phone && (
          <nav className="ml-1 flex min-w-0 items-center gap-0.5 overflow-x-auto rounded-control bg-inset p-0.5 shadow-hairline sm:ml-3" aria-label={t("settings.categories")}>
            {tabs.map((it) => (
              <button
                key={it.k}
                type="button"
                onClick={() => pick(it.k)}
                aria-current={tab === it.k ? "page" : undefined}
                // the bar scrolls when the labels don't fit (English, Japanese): keep the open category in view
                ref={tab === it.k ? (el) => el?.scrollIntoView?.({ block: "nearest", inline: "nearest" }) : undefined}
                className={cx(
                  "flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[7px] px-2.5 text-[12.5px] transition-[background-color,color] duration-150",
                  tab === it.k ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
                )}
              >
                <Icon name={it.icon} size={13} />
                {it.label}
              </button>
            ))}
          </nav>
        )}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {listed ? (
          <div className="px-4 py-5" style={{ animation: `fade-up 320ms ${EASE} both` }}>
            <SettingsList shown={shown} onPick={pick} />
          </div>
        ) : (
          <div key={tab} className="mx-auto max-w-[760px] px-5 py-6 sm:px-8" style={{ animation: `fade-up 320ms ${EASE} both` }}>
            {tab === "general" && <General onTriageModel={() => pick("model-config", "agent.triage")} />}
            {tab === "models" && <Models />}
            {tab === "model-config" && <ModelConfiguration open={sub} onOpen={(id) => pick("model-config", id)} onProviders={() => pick("models")} />}
            {tab === "assistant" && <AssistantSettings />}
            {(tab === "claude" || tab === "codex") && <AgentSettings key={tab} agent={tab} />}
            {tab === "channels" && (sub ? (
              <ChannelPage key={sub} id={sub} onBack={() => pick("channels")} onSettings={(url) => {
                const target = settingsTarget(url);
                if (target && TABS.includes(target.tab as Tab)) pick(target.tab as Tab, target.sub);
              }} />
            ) : (
              <ChannelList onOpen={(id) => pick("channels", id)} onMarketItem={(market, id) => pick("extensions", routeSub({ kind: "item", market, id }))} />
            ))}
            {tab === "connectors" && <Connectors open={sub} onOpen={(id) => pick("connectors", id)} onChannel={(id) => pick("channels", id)} />}
            {tab === "extensions" && <Extensions open={sub} onOpen={(id) => pick("extensions", id)} />}
            {tab === "translate" && <TranslationSettings onConfigure={() => pick("model-config", "translate")} />}
            {tab === "shortcuts" && <ShortcutSettings />}
            {tab === "devices" && <Devices />}
            {tab === "hub" && <HubSettings />}
          </div>
        )}
      </div>
    </div>
  );
}

/** The phone's first page of 设置: the categories in grouped cards, each opening its page (as on iOS). */
function SettingsList({ shown, onPick }: { shown: (k: Tab) => boolean; onPick: (k: Tab) => void }) {
  return (
    <nav aria-label={t("settings.categories")}>
      {LIST_GROUPS.map((g) => {
        const tabs = g.tabs.filter(shown);
        if (!tabs.length) return null;
        return (
          <Group key={g.title} title={g.title}>
            {tabs.map((k) => (
              <Row key={k} icon={TAB_INFO[k].icon} label={TAB_INFO[k].label} onClick={() => onPick(k)} />
            ))}
          </Group>
        );
      })}
    </nav>
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
  const replyMotion = useReplyMotionSetting();
  const sidebarTerminals = useSidebarTerminals();
  const agentDetails = useAgentDetails();
  const contextAsUsed = useContextAsUsed();
  const attentionSound = useAttentionSoundSetting();
  const machines = useStore((s) => s.machines);
  const [assistant, setAssistant] = useState<AssistantState | null>(null);

  useEffect(() => {
    api<AssistantState>("/assistant").then(setAssistant).catch((e) => toast(errorText(e), "error"));
  }, []);

  const themes: { k: Theme; label: string; icon: IconName }[] = [
    { k: "system", label: t("language.system"), icon: "monitor" },
    { k: "light", label: t("settings.theme_light"), icon: "sun" },
    { k: "dark", label: t("settings.theme_dark"), icon: "moon" },
  ];
  const machineName = machines.find((m) => m.id === assistant?.machine_id)?.name ?? assistant?.machine_id;

  return (
    <>
      <Group title={t("settings.appearance")}>
        <div className="flex gap-2 p-3">
          {themes.map((th) => (
            <button
              key={th.k}
              type="button"
              onClick={() => setTheme(th.k)}
              aria-pressed={theme === th.k}
              className={cx(
                "flex flex-1 items-center justify-center gap-2 whitespace-nowrap rounded-control px-3 py-2 text-[12.5px] transition-[background-color,box-shadow,color] duration-150",
                theme === th.k ? "bg-accent-tint font-medium text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "text-ink-2 shadow-hairline hover:bg-hover",
              )}
            >
              <Icon name={th.icon} size={14} />
              {th.label}
            </button>
          ))}
        </div>
        <ExtensionThemePicker />
        <LanguageRow />
        <Row label={t("settings.reply_motion")} hint={t("settings.reply_motion_hint")}>
          <Switch on={replyMotion} onChange={setReplyMotionEnabled} label={t("settings.reply_motion")} />
        </Row>
        <Row label={t("settings.sound")} hint={t("settings.sound_hint")}>
          <Switch on={attentionSound} onChange={setAttentionSoundEnabled} label={t("settings.sound")} />
        </Row>
        <Row label={t("settings.sidebar_terminals")} hint={t("settings.sidebar_terminals_hint")}>
          <Switch on={sidebarTerminals} onChange={setSidebarTerminals} label={t("settings.sidebar_terminals")} />
        </Row>
        <Row label={t("context.details")} hint={t("context.details_hint")}>
          <Switch on={agentDetails} onChange={setAgentDetails} label={t("context.details")} />
        </Row>
        <Row label={t("context.as_used")} hint={t("context.as_used_hint")}>
          <Switch on={contextAsUsed} onChange={setContextAsUsed} label={t("context.as_used")} />
        </Row>
      </Group>

      <AgentGroup />

      <Group title={t("settings.avatar")} footer={t("settings.avatar_footer")}>
        <PersonaPicker builtin={<BuiltinFace size={56} />} />
      </Group>

      <Group title={t("settings.assistant")} footer={t("settings.assistant_footer", { machine: machineName ?? t("settings.hub_machine"), cwd: assistant?.cwd ?? "~/.persona/assistant" })}>
        <Row label={t("settings.runs_on")}>{machineName ?? "—"}</Row>
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
    <Group title={t("settings.this_browser")} footer={t("settings.this_browser_footer")}>
      <Row label={t("settings.hub_address")} mono>
        {location.host}
      </Row>
      {hubName && <Row label={t("settings.hub")}>{hubName}</Row>}
      {others.length > 0 && (
        <Row label={t("settings.linked_hubs")} hint={t("settings.linked_hubs_hint")}>
          {others.map((h) => (
            <span key={h.hub_id} className="flex items-center gap-1.5">
              <span className={cx("size-1.5 rounded-full", h.online === false ? "bg-ink-3" : "bg-green")} />
              {h.name || h.hub_id}
            </span>
          ))}
        </Row>
      )}
      <Row label={t("settings.computers")}>{t("settings.computers_online", { online: machines.filter((m) => m.online).length, total: machines.length })}</Row>
      <InstallRow />
      <div className="flex justify-end px-4 py-2.5">
        <Button tone="quiet" icon="logout" onClick={() => void logout()} className="text-red enabled:hover:text-red">
          {t("settings.disconnect_browser")}
        </Button>
      </div>
    </Group>
  );
}

// Installing the web client as an app (lib/pwa.ts): the browser's prompt where it has one, else where to find it.
const INSTALL_HINT: Record<InstallState, string> = {
  installed: t("settings.install_installed"),
  prompt: t("settings.install_prompt"),
  ios: t("settings.install_ios"),
  safari: t("settings.install_safari"),
  insecure: t("settings.install_insecure"),
  menu: t("settings.install_menu"),
};

function InstallRow() {
  const state = useInstall();
  return (
    <Row label={t("settings.install_app")} hint={INSTALL_HINT[state]}>
      {state === "installed" && <Pill tone="green">{t("settings.installed")}</Pill>}
      {state === "prompt" && (
        <Button icon="download" onClick={() => void install()}>
          {t("settings.install")}
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
          {l.kind === "api" && <Pill>{t("settings.direct_api")}</Pill>}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[11.5px] text-ink-3">
          {l.model && <span className="truncate font-mono text-ink-2">{l.model}</span>}
          {host(l.base_url) && <span className="truncate">{host(l.base_url)}</span>}
        </div>
      </div>
      {l.machines && l.machines.length > 0 && <span className="hidden max-w-[200px] shrink-0 truncate text-[11.5px] text-ink-3 sm:block">{formatList(l.machines.map(name), { type: "conjunction", style: "narrow" })}</span>}
      <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
    </button>
  );

  return (
    <>
      <div className="mb-4 flex items-center gap-2">
        <label className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control bg-surface px-3 shadow-btn focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
          <Icon name="search" size={13} stroke={2} className="text-ink-3" />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("settings.search_placeholder")} className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3" aria-label={t("settings.search_providers")} />
          {query && <IconButton icon="x" label={t("settings.clear")} size={22} iconSize={12} onClick={() => setQuery("")} />}
        </label>
        <Button tone="accent" icon="plus" onClick={() => setEditing("new")}>
          {t("settings.add_provider")}
        </Button>
      </div>
      {!list && !error && <Loading />}
      {error && <p className="text-[13px] text-red">{error}</p>}
      {list && query && shown.length === 0 && <p className="py-10 text-center text-[13px] text-ink-3">{t("settings.no_matching_providers", { query })}</p>}
      {list && (!query || managed.length > 0) && (
        <Group title={t("settings.managed_title", { count: managed.length })} footer={managed.length ? t("settings.managed_footer") : undefined}>
          {managed.length ? managed.map(row) : <p className="px-4 py-3 text-[12.5px] text-ink-3">{t("settings.managed_empty")}</p>}
        </Group>
      )}
      {zshrc.length > 0 && (
        <Group title={t("settings.zshrc_title", { count: zshrc.length })} footer={t("settings.zshrc_footer")}>
          {zshrc.map(row)}
        </Group>
      )}
      {hidden.length > 0 && (
        <Group title={t("settings.hidden_title", { count: hidden.length })} footer={t("settings.hidden_footer")}>
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
  // extensions draw a section here (docs/extensions.md「声明式 UI」 launcher_section)
  const hasExtensions = useStore((s) => hubHasExt(s.features, "manager"));

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
      toast(l.source === "jarvis" ? t("settings.deleted", { name: l.name }) : t("settings.hid", { name: l.name }), "ok");
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
      toast(t("settings.restored", { name: l.name }), "ok");
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
                {t("settings.restore")}
              </Button>
            ) : (
              <span className="relative">
                <Button tone="quiet" icon={l.source === "jarvis" ? "x" : undefined} onClick={() => setConfirm((c) => !c)} className="text-red enabled:hover:text-red">
                  {l.source === "jarvis" ? t("settings.delete_provider") : t("settings.hide_in_persona")}
                </Button>
                {/* the confirmation comes up right at the button (like the app), not as another dialog */}
                {confirm && (
                  <span className="absolute bottom-full left-0 mb-1.5 rounded-control bg-surface p-1 shadow-overlay" style={{ animation: `pop-in 160ms ${EASE} both` }}>
                    <Button tone="danger" onClick={() => void remove()}>
                      {l.source === "jarvis" ? t("settings.confirm_delete") : t("settings.confirm_hide")}
                    </Button>
                  </span>
                )}
              </span>
            )}
            <span className="flex-1" />
            {l.source === "jarvis" && (
              <Button tone="secondary" onClick={() => onEdit(l)}>
                {t("settings.edit")}
              </Button>
            )}
            <Button tone="primary" icon={running ? undefined : "run"} busy={running} disabled={testOn.length === 0} onClick={() => void check()}>
              {Object.keys(checks).length ? t("settings.test_again") : t("settings.test")}
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
            <Pill tone={l.source === "jarvis" ? "accent" : undefined}>{l.source === "jarvis" ? t("settings.managed") : t("settings.zshrc_function")}</Pill>
            {(l.machines ?? []).map((id) => (
              <Pill key={id}>{mname(id)}</Pill>
            ))}
            {l.hidden && <Pill tone="red">{t("settings.hidden")}</Pill>}
          </div>

          {/* how much is left comes before trying it, as in the app */}
          {l.kind !== "api" && <LauncherQuota name={l.name} />}

          {Object.keys(checks).length > 0 && (
            <KV title={t("settings.availability")}>
              {Object.entries(checks).map(([id, c]) => (
                <div key={id} className="flex items-start gap-2.5 py-1.5">
                  <span className="mt-0.5 flex size-4 shrink-0 items-center justify-center">{c === null ? <Spinner /> : <Icon name={c.ok ? "check" : "x"} size={13} stroke={2.6} className={c.ok ? "text-green" : "text-red"} />}</span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2 text-[12.5px]">
                      <span className="font-medium text-ink">{id ? mname(id) : t("settings.hub_machine_title")}</span>
                      {c && <span className="text-ink-3 tabular-nums">{t("settings.elapsed_seconds", { seconds: (c.millis / 1000).toFixed(c.millis >= 10_000 ? 0 : 1) })}</span>}
                    </div>
                    {c === null ? (
                      <div className="text-[11.5px] text-ink-3">{t("settings.requesting")}</div>
                    ) : c.ok ? (
                      <div className="truncate text-[11.5px] text-ink-3">{[c.model, c.answer && t("settings.answered", { answer: c.answer })].filter(Boolean).join(" · ")}</div>
                    ) : (
                      <pre className="mt-0.5 whitespace-pre-wrap break-all font-mono text-[11px] text-red">{c.error}</pre>
                    )}
                  </div>
                </div>
              ))}
            </KV>
          )}

          {l.source === "jarvis" && (
            <KV title={t("settings.config")}>
              <KVRow k={t("settings.access")} v={l.kind === "api" ? t("settings.direct_api") : t("settings.launcher")} />
              {l.kind === "api" && <KVRow k={t("settings.protocol")} v={l.api === "responses" ? "Responses" : "Messages"} />}
              <KVRow k="Base URL" v={l.base_url} mono />
              <KVRow k={t("settings.model")} v={l.model} mono />
              {l.kind !== "api" && l.small_model && <KVRow k={t("settings.small_model")} v={l.small_model} mono />}
              {l.kind !== "api" && l.subagent_model && <KVRow k={t("settings.subagent_model")} v={l.subagent_model} mono />}
              {l.kind !== "api" && l.custom_model && <KVRow k={t("settings.custom_model")} v={l.custom_model} mono />}
              {l.kind !== "api" && l.custom_model_name && <KVRow k={t("settings.custom_model_name")} v={l.custom_model_name} />}
              <KVRow k="API Key" v={l.has_key ? t("settings.key_set_hidden") : t("settings.not_set")} />
              {l.api !== "responses" && <KVRow k={t("settings.auth")} v={l.auth === "bearer" ? "Bearer" : "x-api-key"} />}
              {l.kind !== "api" && l.args?.length ? <KVRow k={t("settings.extra_args")} v={l.args.join(" ")} mono /> : null}
            </KV>
          )}

          {hasExtensions && <LauncherExtensionSections launcher={l.name} machine={l.source === "jarvis" ? undefined : testOn[0] || undefined} />}

          {groups.map((g) => (
            <DetailBlock key={g.ids.join()} d={g.d} place={groups.length > 1 ? formatList(g.ids.map(mname), { type: "conjunction", style: "narrow" }) : undefined} />
          ))}

          {l.definitions && Object.keys(l.definitions).length > 0 && (
            <div>
              <button type="button" onClick={() => setShowDef((x) => !x)} className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2 hover:text-ink">
                <Icon name="chevron" size={10} stroke={2.4} style={{ transform: showDef ? "none" : "rotate(-90deg)", transition: "transform 150ms" }} />
                {t("settings.function_source")}
                <span className="font-normal text-ink-3">{t("settings.function_source_note")}</span>
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
  const titled = (s: string) => (place ? `${s} · ${place}` : s);
  if (d.error) return <KV title={titled(t("settings.run"))}><p className="py-2 text-[12px] text-ink-3">{d.error}</p></KV>;
  const agent = d.agent === "codex" ? "Codex" : d.agent === "traex" ? "TRAEX" : "Claude Code";
  return (
    <>
      <KV title={titled(t("settings.run"))}>
        <KVRow k="Agent" v={agent} />
        {d.base_url && <KVRow k={t("settings.gateway")} v={d.base_url} mono />}
        {d.auth && <KVRow k="Key" v={t("settings.key_set", { auth: d.auth })} mono />}
      </KV>
      {d.models?.length ? (
        <KV title={titled(t("settings.model"))}>
          {d.models.map((m) => (
            <KVRow key={m.role + m.env} k={m.role} sub={m.env} v={m.model} mono />
          ))}
        </KV>
      ) : null}
      {(d.context_window || d.compact_window || d.effort || d.timeout_ms) && (
        <KV title={titled(t("settings.context"))}>
          {d.context_window ? <KVRow k={t("settings.context_window")} v={`${tokens(d.context_window)} tokens`} /> : null}
          {d.compact_window ? <KVRow k={t("settings.compact_window")} v={`${tokens(d.compact_window)} tokens`} /> : null}
          {d.effort ? <KVRow k={t("settings.effort")} v={d.effort} /> : null}
          {d.timeout_ms ? <KVRow k={t("settings.request_timeout")} v={d.timeout_ms >= 60_000 ? t("settings.minutes", { count: Math.round(d.timeout_ms / 60_000) }) : t("settings.seconds", { count: Math.round(d.timeout_ms / 1000) })} /> : null}
        </KV>
      )}
      {d.args?.length ? (
        <KV title={titled(t("settings.launch_args"))}>
          <p className="py-2 font-mono text-[11.5px] break-all text-ink">{d.args.join(" ")}</p>
        </KV>
      ) : null}
      {d.env?.length ? (
        <KV title={titled(t("settings.other_settings", { count: d.env.length }))}>
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
  // 从扩展添加 (docs/extensions.md「模型供应商」): only when adding, and only when the hub's extensions offer providers.
  const [presets, setPresets] = useState<LauncherPreset[]>([]);
  const [source, setSource] = useState<"manual" | "extension">("manual");
  const [pick, setPick] = useState("");
  const [pf, setPf] = useState({ name: "", api_key: "", model: "" });
  const preset = presets.find((p) => p.id === pick);
  const fromExtension = isNew && source === "extension" && !!preset;

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
  const ready = fromExtension ? presetReady : f.name.trim() && f.base_url.trim() && f.model.trim() && (!isNew || f.api_key.trim());
  const save = async () => {
    if (busy || !ready) return;
    setBusy(true);
    setError("");
    try {
      if (fromExtension && preset) {
        const body = presetBody(preset, pf.name, pf.api_key, pf.model);
        await api("/launchers", { method: "POST", json: body });
        toast(t("settings.added", { name: body.name }), "ok");
        onSaved();
        onClose();
        return;
      }
      const body = { name: f.name.trim(), kind: f.kind, api: f.kind === "api" ? f.api : "messages", base_url: f.base_url.trim(), model: f.model.trim(), small_model: f.kind === "api" ? "" : f.small_model.trim(), subagent_model: f.kind === "api" ? "" : f.subagent_model.trim(), custom_model: f.kind === "api" ? "" : f.custom_model.trim(), custom_model_name: f.kind === "api" ? "" : f.custom_model_name.trim(), custom_model_description: f.kind === "api" ? "" : f.custom_model_description.trim(), args: f.kind === "api" ? [] : f.args.split(/\s+/).filter(Boolean), api_key: f.api_key || undefined, auth: responses ? "api_key" : f.auth };
      await api(isNew ? "/launchers" : enLauncher(body.name), { method: isNew ? "POST" : "PUT", json: body });
      toast(isNew ? t("settings.added", { name: body.name }) : t("settings.saved", { name: body.name }), "ok");
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
      title={isNew ? t("settings.add_provider") : t("settings.edit_named", { name: old?.name ?? "" })}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button tone="primary" busy={busy} disabled={!ready} onClick={() => void save()}>
            {t("common.save")}
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
          <Segmented<"manual" | "extension">
            label={t("settings.source")}
            value={source}
            onChange={setSource}
            options={[{ v: "manual", label: t("settings.source_manual") }, { v: "extension", label: t("settings.source_extension") }]}
          />
        )}
        {fromExtension && preset ? (
          <PresetFields presets={presets} preset={preset} onPick={choose} pf={pf} setPf={setPf} nameProblem={nameProblem} />
        ) : (
        <>
        <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.access")}</span>
          <select value={f.kind} onChange={(e) => setF((x) => ({ ...x, kind: e.target.value, api: "messages" }))} disabled={!isNew}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none disabled:opacity-60">
            <option value="launcher">{t("settings.claude_launcher")}</option>
            <option value="api">{t("settings.direct_api")}</option>
          </select>
        </label>
        {f.kind === "api" && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.api_protocol")}</span>
          <select value={f.api} onChange={(e) => setF((x) => ({ ...x, api: e.target.value }))}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none">
            <option value="messages">Anthropic Messages</option>
            <option value="responses">OpenAI Responses</option>
          </select>
        </label>}
        {field("name", t("settings.name"), { placeholder: t("settings.name_placeholder", { example: "jv_strong" }), hint: t("settings.name_hint"), mono: true, disabled: !isNew })}
        {field("base_url", "Base URL", { placeholder: "https://…", hint: f.api === "responses" ? t("settings.base_url_responses") : t("settings.base_url_messages"), mono: true })}
        {field("api_key", isNew || !old?.has_key ? "API Key" : t("settings.api_key_keep"), { type: "password", hint: t("settings.key_stored") })}
        {!responses && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.auth")}</span>
          <select value={f.auth} onChange={(e) => setF((x) => ({ ...x, auth: e.target.value }))}
            className="h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none">
            <option value="api_key">{t("settings.auth_api_key")}</option>
            <option value="bearer">{t("settings.auth_bearer")}</option>
          </select>
          <span className="mt-1 block text-[11px] text-ink-3">{t("settings.auth_hint")}</span>
        </label>}
        <div className={f.kind === "api" ? "" : "grid grid-cols-2 gap-3"}>
          {field("model", t("settings.default_model"), { placeholder: t("settings.main_model"), mono: true })}
          {f.kind !== "api" && field("small_model", t("settings.small_model_optional"), { placeholder: t("settings.same_as_model"), mono: true })}
          {f.kind !== "api" && field("subagent_model", t("settings.subagent_model_optional"), { placeholder: t("settings.same_as_model"), mono: true, hint: t("settings.subagent_model_hint") })}
        </div>
        {f.kind !== "api" && (
          <div className="grid grid-cols-2 gap-3">
            {field("custom_model", t("settings.custom_model_optional"), { placeholder: t("settings.gateway_model_id"), mono: true })}
            {field("custom_model_name", t("settings.display_name_optional"), {})}
          </div>
        )}
        {f.kind !== "api" && field("custom_model_description", t("settings.custom_model_description_optional"), { hint: t("settings.custom_model_description_hint") })}
        {f.kind !== "api" && field("args", t("settings.extra_args_optional"), { placeholder: "--dangerously-skip-permissions", hint: t("settings.extra_args_hint"), mono: true })}
        </>
        )}
        {error && <p className="text-[12px] text-red">{error}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

/** 从扩展添加: pick one of the extensions' providers, then only the name, the key and maybe another of its models. */
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
        <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.provider")}</span>
        <div role="radiogroup" aria-label={t("settings.provider")} className="overflow-hidden rounded-control bg-field shadow-inset-field">
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
        <span className="mt-1 block text-[11px] text-ink-3">{t("settings.preset_hint", { plugin: preset.plugin })}</span>
      </div>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.name")}</span>
        <input value={pf.name} onChange={(e) => setPf((x) => ({ ...x, name: e.target.value }))} placeholder={t("settings.name_placeholder", { example: "jv_team" })} autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        <span className={cx("mt-1 block text-[11px]", nameProblem ? "text-red" : "text-ink-3")}>{nameProblem || t("settings.name_hint")}</span>
      </label>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">API Key</span>
        <input value={pf.api_key} onChange={(e) => setPf((x) => ({ ...x, api_key: e.target.value }))} type="password" autoComplete="off" spellCheck={false} className={input} />
        <span className="mt-1 block text-[11px] text-ink-3">{preset.key_hint ? `${preset.key_hint} ` : ""}{t("settings.key_stored")}</span>
      </label>
      <label className="block">
        <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings.default_model")}</span>
        {choices.length ? (
          <select value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} className={cx(input, "font-mono")}>
            {choices.map((m) => (
              <option key={m.id} value={m.id}>
                {presetModelLabel(m)}
              </option>
            ))}
          </select>
        ) : (
          <input value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} placeholder={t("settings.main_model")} autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        )}
        <span className="mt-1 block text-[11px] text-ink-3">
          {preset.models_error ? t("settings.preset_models_error", { error: preset.models_error }) : t("settings.preset_models_hint")}
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
      toast(t("settings.revoked_toast"), "ok");
      setConfirm(null);
      void load();
    } catch (e) {
      toast(errorText(e), "error");
    }
  };
  const kind: Record<string, string> = { phone: t("settings.kind_phone"), watch: t("settings.kind_watch"), other: t("settings.kind_cli"), web: t("settings.kind_web") };
  const { active, revoked } = splitDevices(data?.tokens ?? [], data?.current);

  // A revoked credential is shown greyed, with when it was revoked and no 撤销 (iOS DeviceCredentials.swift).
  const row = (tok: DeviceToken, gone: boolean) => (
    <div key={tok.id} className="flex items-center gap-3 px-4 py-2.5">
      <Icon name={tok.kind === "phone" ? "machine" : tok.kind === "watch" ? "monitor" : "web"} size={15} className="shrink-0 text-ink-3" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={cx("truncate text-[13px]", gone ? "text-ink-3" : "text-ink")}>{tok.name || tok.id}</span>
          {tok.id === data?.current && <Pill tone="green">{t("settings.this_browser")}</Pill>}
          {tok.role !== "full" && <Pill>{tok.role === "answer" ? t("settings.role_answer") : tok.role}</Pill>}
        </div>
        <div className="mt-0.5 truncate text-[11.5px] text-ink-3">
          {kind[tok.kind] ?? tok.kind}
          {gone && tok.revoked_at ? ` · ${t("settings.revoked_ago", { time: ago(tok.revoked_at) })}` : ""}
          {!gone && tok.last_seen ? ` · ${t("settings.used_ago", { time: ago(tok.last_seen) })}` : ""}
          {tok.created_at ? ` · ${t("settings.added_ago", { time: ago(tok.created_at) })}` : ""}
        </div>
      </div>
      {!gone && tok.id !== data?.current && (
        <RowConfirm
          open={confirm === tok.id}
          confirm={t("settings.confirm_revoke")}
          onCancel={() => setConfirm(null)}
          onConfirm={() => void revoke(tok.id)}
          trigger={
            <Button tone="quiet" onClick={() => setConfirm(tok.id)} className="text-red enabled:hover:text-red">
              {t("settings.revoke")}
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
        <Group title={t("settings.devices_title", { count: active.length })} footer={t("settings.devices_footer")}>
          {active.length === 0 && <p className="px-4 py-3 text-[12.5px] text-ink-3">{t("settings.no_devices")}</p>}
          {active.map((tok) => row(tok, false))}
        </Group>
      )}
      {data && revoked.length > 0 && <Group title={t("settings.revoked_title", { count: revoked.length })}>{revoked.map((tok) => row(tok, true))}</Group>}
    </>
  );
}
