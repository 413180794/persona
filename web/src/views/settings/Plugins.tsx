// 设置 › 插件 (docs/plugins.md「App：设置 › 插件」, iOS PluginsView / PluginDetailView): the hub's plugins (built in,
// from hub.json, installed on the hub's machine) and their states; a plugin's page has its switch, its settings as a
// form drawn from its schema (`form.fields`, saved as a whole), its secrets (typed in, never shown again), what it
// contributes, what it may do and where it came from. A plugin written in hub.json is only looked at here. Installing,
// upgrading and removing are done on the hub's side (`personad plugin` on its machine, or its admin UI at /admin).
import { useEffect, useMemo, useState } from "react";
import { errorText } from "../../lib/api";
import { useStore } from "../../lib/store";
import { marketOf, pluginsRoute, routeSub } from "../../lib/plugin-market";
import {
  choiceLabel, clearPluginSecret, compact, configError, fieldLabel, fieldTitle, isDirty, listItems, listPluginsInfo, listSummary, numberProblems,
  placeholder, pluginDetail, pluginState, pluginTone, pointText, readNumber, same, secretState, setAt, setPluginConfig, setPluginEnabled,
  machineStatus, saveFooter, setPluginSecret, sourceText, stateLine, valueAt,
  type HubPlugin, type HubPluginDetail, type Json, type JsonObject, type PluginField, type PluginSecret,
} from "../../lib/plugins";
import { Button, cx, Dialog, EASE, IconButton, Loading, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { MarketCatalog, MarketItem, Markets, UninstallDialog } from "./PluginMarket";
import { PluginUI } from "./PluginView";
import { ChannelList, ChannelPage, ChannelsEntry } from "./Channels";
import { settingsTarget } from "../../lib/channels";
import { Group, Pill, Row, Switch } from "./SettingsParts";

/**
 * The tab's pages, by its `plugin` parameter (lib/plugin-market.ts pluginsRoute): the list, a plugin, a page a plugin
 * draws, and the market (its catalog, one plugin of it, the markets).
 */
export function Plugins({ open, onOpen, onTab }: { open: string | null; onOpen: (id: string | null) => void; onTab?: (tab: string, sub: string | null) => void }) {
  const r = pluginsRoute(open);
  const go = (to: Parameters<typeof routeSub>[0]) => onOpen(routeSub(to));
  switch (r.kind) {
    case "channels":
      return <ChannelList onBack={() => onOpen(null)} onOpen={(id) => go({ kind: "channel", id })} onMarketItem={(market, id) => go({ kind: "item", market, id })} />;
    case "channel":
      return (
        <ChannelPage
          key={r.id}
          id={r.id}
          onBack={() => go({ kind: "channels" })}
          onSettings={(url) => {
            const t = settingsTarget(url);
            if (t?.tab === "plugins") onOpen(t.sub);
            else if (t) onTab?.(t.tab, t.sub);
          }}
        />
      );
    case "plugin":
      return <PluginDetail key={r.id} id={r.id} onBack={() => onOpen(null)} onPage={(ui) => go({ kind: "page", id: r.id, ui })} />;
    case "page":
      return <PluginPage key={open} id={r.id} ui={r.ui} onBack={() => go({ kind: "plugin", id: r.id })} />;
    case "catalog":
      return <MarketCatalog onBack={() => onOpen(null)} onOpen={(it) => go({ kind: "item", market: it.market, id: it.id })} onMarkets={() => go({ kind: "markets" })} />;
    case "item":
      return <MarketItem key={open} market={r.market} id={r.id} onBack={() => go({ kind: "catalog" })} onInstalled={(id) => go({ kind: "plugin", id })} />;
    case "markets":
      return <Markets onBack={() => go({ kind: "catalog" })} />;
    default:
      return <PluginList onOpen={onOpen} onMarket={() => go({ kind: "catalog" })} onMarkets={() => go({ kind: "markets" })} onChannels={() => go({ kind: "channels" })} />;
  }
}

/** A page a plugin draws (placement plugin_page), under the plugin's name. */
function PluginPage({ id, ui, onBack }: { id: string; ui: string; onBack: () => void }) {
  const [d, setD] = useState<HubPluginDetail | null>(null);
  const [n, setN] = useState(0);
  useEffect(() => {
    pluginDetail(id).then(setD, () => {});
  }, [id]);
  const title = d?.ui?.find((u) => u.id === ui)?.title;
  return (
    <>
      <div className="mb-3 flex items-center gap-2">
        <Button tone="quiet" icon="chevronLeft" onClick={onBack} className="-ml-2">
          <span className="max-w-[240px] truncate">{d?.name ?? id}</span>
        </Button>
        <span className="flex-1" />
        <IconButton icon="retry" label="重新画" onClick={() => setN((x) => x + 1)} />
      </div>
      <PluginUI key={n} plugin={id} ui={ui} title={title} />
    </>
  );
}

const DOT: Record<ReturnType<typeof pluginTone>, string> = { red: "bg-red", orange: "bg-orange", green: "bg-green", idle: "bg-line-strong" };
const TILE: Record<ReturnType<typeof pluginTone>, string> = {
  red: "bg-red-tint text-red",
  orange: "bg-orange-tint text-orange",
  green: "bg-accent-tint text-accent-ink",
  idle: "bg-inset text-ink-3 shadow-hairline",
};

function StateDot({ plugin }: { plugin?: HubPlugin | null }) {
  return <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", DOT[pluginTone(plugin)])} />;
}

// ── the list ────────────────────────────────────────────

function PluginList({ onOpen, onMarket, onMarkets, onChannels }: { onOpen: (id: string) => void; onMarket: () => void; onMarkets: () => void; onChannels: () => void }) {
  const hasChannels = useStore((s) => s.features.includes("channels"));
  const [plugins, setPlugins] = useState<HubPlugin[] | null>(null);
  const [markets, setMarkets] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const load = () => {
    setLoading(true);
    listPluginsInfo()
      .then((r) => (setPlugins(r.plugins), setMarkets(r.markets), setError("")))
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);

  return (
    <>
      <p className="mb-5 px-1 text-[12.5px] leading-relaxed text-ink-3">
        插件给 Persona 加上新的连接器、语音对话等能力：hub 自带的、写在 hub.json 里的和装在 hub 机器上的都列在这里。
      </p>
      {hasChannels && <ChannelsEntry onOpen={onChannels} />}
      <Group
        title={
          <span className="flex items-center gap-2">
            插件
            {plugins && <span className="font-normal">{listSummary(plugins)}</span>}
          </span>
        }
        right={<IconButton icon="retry" label="刷新插件" onClick={load} disabled={loading} size={24} iconSize={13} className="-mb-1" />}
        footer="插件在 hub 的管理界面（/admin）或 hub 所在的机器上（personad plugin）安装、升级和卸载，这里可以开关、改设置、填密钥。"
      >
        {!plugins && !error && (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )}
        {error && (
          <div className="flex items-center gap-3 px-4 py-3">
            <p className="min-w-0 flex-1 text-[12.5px] break-words text-red">读不到插件：{error}</p>
            <Button tone="secondary" icon="retry" onClick={load} busy={loading}>
              重试
            </Button>
          </div>
        )}
        {plugins?.length === 0 && (
          <div className="flex flex-col items-center gap-2 px-6 py-8 text-center">
            <span className="flex size-10 items-center justify-center rounded-[10px] bg-inset text-ink-3 shadow-hairline">
              <Icon name="plugin" size={18} />
            </span>
            <p className="text-[13px] font-medium text-ink">没有插件</p>
            <p className="max-w-[420px] text-[12px] leading-relaxed text-ink-3">插件在 hub 的管理界面（/admin）或 hub 所在的机器上用 personad plugin install 安装，装好后在这里开关和设置。</p>
          </div>
        )}
        {plugins?.map((p, i) => (
          <button
            key={p.id}
            type="button"
            onClick={() => onOpen(p.id)}
            className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover"
            style={{ animation: `fade-up 320ms ${EASE} ${Math.min(i, 8) * 30}ms both` }}
          >
            <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-[8px]", TILE[pluginTone(p)])}>
              <Icon name="plugin" size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-[13px] text-ink">{p.name}</span>
                {p.source && p.source !== "installed" && <Pill>{sourceText(p.source)}</Pill>}
              </div>
              {(p.needs_setup?.length || p.error || p.description) && (
                <div className={cx("mt-0.5 truncate text-[11.5px]", p.error ? "text-red" : p.needs_setup?.length ? "text-orange" : "text-ink-3")}>
                  {p.error || (p.needs_setup?.length ? `还缺：${p.needs_setup.join("、")}` : p.description)}
                </div>
              )}
            </div>
            <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[12px] text-ink-2">
              <StateDot plugin={p} />
              {pluginState(p)}
            </span>
            <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
          </button>
        ))}
      </Group>
      {markets && (
        <Group title="插件市场" footer="市场是网站或 GitHub 仓库发布的插件索引：在这里搜索、安装、升级，从市场装的也能在这里卸载。">
          <Row
            label={
              <span className="flex items-center gap-2">
                <Icon name="search" size={13} className="shrink-0 text-ink-3" />
                浏览插件市场
              </span>
            }
            onClick={onMarket}
          />
          <Row
            label={
              <span className="flex items-center gap-2">
                <Icon name="gear" size={13} className="shrink-0 text-ink-3" />
                管理市场
              </span>
            }
            onClick={onMarkets}
          />
        </Group>
      )}
    </>
  );
}

// ── one plugin ──────────────────────────────────────────

/** A number field as typed, by dotted path, until it reads as one. */
type Typed = Record<string, { text: string; integer: boolean }>;

function PluginDetail({ id, onBack, onPage }: { id: string; onBack: () => void; onPage: (ui: string) => void }) {
  const [removing, setRemoving] = useState(false);
  const [detail, setDetail] = useState<HubPluginDetail | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [draft, setDraft] = useState<JsonObject>({});
  const [typed, setTyped] = useState<Typed>({});
  // The hub's word on a field it refused, by dotted path ("" = the whole form).
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [secret, setSecret] = useState<PluginSecret | null>(null);
  const [leaving, setLeaving] = useState(false);

  const apply = (d: HubPluginDetail) => {
    setDetail(d);
    setDraft(d.config ?? {});
    setError("");
  };
  const load = () => {
    setLoading(true);
    pluginDetail(id)
      .then((d) => (apply(d), setTyped({}), setFieldErrors({})))
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, [id]);

  const problems = useMemo(() => numberProblems(typed), [typed]);
  const dirty = !!detail && (isDirty(draft, detail.config) || Object.keys(problems).length > 0);
  const canSave = dirty && !Object.keys(problems).length && !saving;
  const fields = detail?.form?.fields ?? [];
  const editable = !!detail && !detail.read_only;

  const edit = (path: string[], value: Json | undefined) => {
    setDraft((d) => setAt(d, path, value));
    const key = path.join(".");
    if (fieldErrors[key] || fieldErrors[""]) setFieldErrors(({ [key]: _a, [""]: _b, ...rest }) => rest);
  };
  const type = (path: string[], text: string, integer: boolean) => {
    const key = path.join(".");
    setTyped((t) => ({ ...t, [key]: { text, integer } }));
    const n = readNumber(text, integer);
    if (!n.error) edit(path, n.value);
  };
  const revert = () => {
    if (!detail) return;
    setDraft(detail.config ?? {});
    setTyped({});
    setFieldErrors({});
  };

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setFieldErrors({});
    try {
      apply(await setPluginConfig(id, compact(draft)));
      setTyped({});
      toast("已保存，插件会重新启动", "ok");
    } catch (e) {
      const ce = configError(e);
      if (ce) setFieldErrors({ [ce.field]: ce.message });
      else toast(`没有保存：${errorText(e)}`, "error");
    } finally {
      setSaving(false);
    }
  };

  const toggle = async (on: boolean) => {
    setToggling(true);
    try {
      const d = await setPluginEnabled(id, on);
      // the switch is its own call: an unsaved draft of the form stays
      setDetail(d);
      toast(on ? `已启用 ${d.name}` : `已停用 ${d.name}`, "ok");
    } catch (e) {
      toast(`没有${on ? "启用" : "停用"}：${errorText(e)}`, "error");
    } finally {
      setToggling(false);
    }
  };

  // A machine's switch (format machine) is saved at once, on top of what the hub has; the rest of the draft stays.
  const saveNow = async (path: string[], value: Json | undefined) => {
    if (!detail) return;
    const saved = detail.config ?? {};
    setDraft((d) => setAt(d, path, value));
    try {
      const d = await setPluginConfig(id, compact(setAt(saved, path, value)));
      setDetail(d);
      setDraft((cur) => setAt(cur, path, valueAt(d.config ?? {}, path)));
    } catch (e) {
      setDraft((cur) => setAt(cur, path, valueAt(saved, path)));
      toast(`没有保存：${errorText(e)}`, "error");
    }
  };
  const computers = !!detail?.contributes?.computer;

  // A secret's answer is a whole detail too; keep the form's draft as it is.
  const secretDone = (d: HubPluginDetail) => setDetail(d);

  const back = () => (dirty ? setLeaving(true) : onBack());
  const p = detail;

  return (
    <div className={cx(dirty && "pb-16")}>
      <div className="relative mb-3 flex items-center gap-2">
        <Button tone="quiet" icon="chevronLeft" onClick={back} className="-ml-2">
          插件
        </Button>
        {leaving && (
          <span className="absolute top-full left-0 z-10 mt-1 flex items-center gap-1 rounded-control bg-surface p-1 shadow-overlay" style={{ animation: `pop-in 160ms ${EASE} both` }}>
            <span className="px-2 text-[12px] whitespace-nowrap text-ink-2">设置改了还没保存</span>
            <Button tone="quiet" onClick={() => setLeaving(false)}>
              接着改
            </Button>
            <Button tone="danger" onClick={onBack}>
              不保存
            </Button>
          </span>
        )}
        <span className="flex-1" />
        <IconButton icon="retry" label="重新读取" onClick={load} disabled={loading} />
      </div>

      {/* what it is and how it is doing */}
      <section className="mb-6 overflow-hidden rounded-card bg-surface shadow-card">
        <div className="flex items-center gap-3 px-4 py-3.5">
          <span className={cx("flex size-11 shrink-0 items-center justify-center rounded-[11px]", TILE[pluginTone(p)])}>
            <Icon name="plugin" size={20} />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[15px] font-semibold text-ink">{p?.name ?? id}</h2>
            <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12px] text-ink-3">
              <StateDot plugin={p} />
              <span className="truncate">{p ? stateLine(p) : loading ? "正在读取…" : "读不到"}</span>
            </div>
          </div>
          {loading && !p && <Spinner />}
        </div>
        {(p?.description || p?.needs_setup?.length || p?.error || error) && (
          <div className="flex flex-col gap-1.5 border-t border-line px-4 py-3">
            {p?.description && <p className="text-[12.5px] leading-relaxed text-ink-2">{p.description}</p>}
            {!!p?.needs_setup?.length && <p className="text-[12px] text-orange">还缺：{p.needs_setup.join("、")}</p>}
            {p?.error && <p className="text-[12px] break-words text-red select-text">{p.error}</p>}
            {error && <p className="text-[12px] break-words text-red select-text">读不到这个插件：{error}</p>}
          </div>
        )}
      </section>

      {p && (
        <>
          {p.read_only ? (
            <Group title="开关和设置">
              <div className="flex items-start gap-3 px-4 py-3">
                <Icon name="lock" size={14} className="mt-0.5 shrink-0 text-ink-3" />
                <p className="text-[12.5px] leading-relaxed text-ink-2">这个插件写在 hub.json 里：开关和设置在 hub 所在的机器上改（hub.json 或管理界面），改完重启 hub。</p>
              </div>
            </Group>
          ) : (
            <Group footer="停用后它不再运行，提供的连接器等也一起关掉；设置和密钥留着。">
              <div className="flex items-center gap-3 px-4 py-2.5">
                <span className="min-w-0 flex-1 truncate text-[13px] text-ink">启用</span>
                {toggling && <Spinner />}
                <Switch on={p.enabled ?? true} onChange={(on) => void toggle(on)} disabled={toggling} label="启用" />
              </div>
            </Group>
          )}

          {!!p.ui?.length && (
            <Group title="页面" footer="这些页面由插件画，按钮的动作交给插件处理。">
              {p.ui.map((u) => (
                <Row
                  key={u.id}
                  label={
                    <span className="flex items-center gap-2">
                      <Icon name="layers" size={13} className="shrink-0 text-ink-3" />
                      {u.title || u.id}
                    </span>
                  }
                  onClick={() => onPage(u.id)}
                />
              ))}
            </Group>
          )}

          {editable && fields.length > 0 && (
            <>
              {fieldErrors[""] && <p role="alert" className="mb-3 rounded-[8px] bg-red-tint px-3 py-2 text-[12px] break-words text-red">没有保存：{fieldErrors[""]}</p>}
              {fields.some((f) => f.type !== "object") && (
                <Group title="设置" footer={saveFooter(fields)}>
                  {fields
                    .filter((f) => f.type !== "object")
                    .map((f) => (
                      <FieldRow key={f.name} field={f} path={[f.name]} draft={draft} typed={typed} errors={fieldErrors} problems={problems} onEdit={edit} onType={type} onSaveNow={saveNow} computers={computers} />
                    ))}
                </Group>
              )}
              {fields
                .filter((f) => f.type === "object")
                .map((g) => (
                  <Group key={g.name} title={fieldLabel(g)} footer={g.description}>
                    {(g.fields ?? []).map((f) => (
                      <FieldRow key={f.name} field={f} path={[g.name, f.name]} draft={draft} typed={typed} errors={fieldErrors} problems={problems} onEdit={edit} onType={type} onSaveNow={saveNow} computers={computers} />
                    ))}
                  </Group>
                ))}
            </>
          )}

          {editable && !!p.secrets?.length && (
            <Group title="密钥" footer="密钥加密存在 hub 上，只交给这个插件；存好后不再显示，只能换新的或清除。">
              {p.secrets.map((s) => (
                <Row
                  key={s.name}
                  label={
                    <span className="flex items-center gap-2">
                      <Icon name="key" size={13} className="shrink-0 text-ink-3" />
                      {s.title}
                    </span>
                  }
                  hint={s.description}
                  onClick={() => setSecret(s)}
                >
                  <span className={cx(!s.set && s.required && "text-orange")}>{secretState(s)}</span>
                </Row>
              ))}
            </Group>
          )}

          {p.contributes && Object.keys(p.contributes).length > 0 && (
            <Group title="提供">
              {Object.keys(p.contributes)
                .sort()
                .map((point) => (
                  <Row key={point} label={pointText(point)} mono>
                    <span className="truncate">{(p.contributes?.[point] ?? []).join("、")}</span>
                  </Row>
                ))}
            </Group>
          )}

          {!!p.permissions?.length && (
            <Group title="它可以" footer="安装时你同意过这些；新版本要得更多时，升级前会再问一次。">
              {p.permissions.map((perm) => (
                <div key={perm.key} className="px-4 py-2.5">
                  <div className="text-[13px] text-ink">{perm.title}</div>
                  {perm.detail && <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{perm.detail}</div>}
                </div>
              ))}
            </Group>
          )}

          <About d={p} />

          {marketOf(p.origin) && (
            <Group footer="这个插件是从插件市场装的：卸载后可以在市场里重新安装。">
              <button type="button" onClick={() => setRemoving(true)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-[13px] whitespace-nowrap text-red transition-colors hover:bg-red-tint">
                <Icon name="trash" size={13} className="shrink-0" />
                卸载
              </button>
            </Group>
          )}
        </>
      )}
      {removing && p && <UninstallDialog id={p.id} name={p.name} onClose={() => setRemoving(false)} onDone={onBack} />}

      {dirty && (
        <div className="sticky bottom-3 z-10 mt-2 flex items-center gap-2 rounded-card bg-surface px-3 py-2 shadow-overlay" style={{ animation: `fade-up 220ms ${EASE} both` }}>
          <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-2">
            {Object.keys(problems).length ? "有一项不是数，改好才能保存" : "设置改了还没保存"}
          </span>
          <Button tone="quiet" onClick={revert} disabled={saving}>
            还原
          </Button>
          <Button tone="accent" busy={saving} disabled={!canSave} onClick={() => void save()}>
            保存
          </Button>
        </div>
      )}

      {secret && <SecretDialog plugin={id} secret={secret} onClose={() => setSecret(null)} onDone={secretDone} />}
    </div>
  );
}

function About({ d }: { d: HubPluginDetail }) {
  const source = sourceText(d.source);
  let home = "";
  try {
    home = d.homepage && new URL(d.homepage).protocol === "https:" ? d.homepage : "";
  } catch {
    home = "";
  }
  return (
    <Group title="关于">
      {d.version && <Row label="版本" mono>{d.version}</Row>}
      {d.previous_version && <Row label="上一个版本" mono>{d.previous_version}</Row>}
      {source && <Row label="来源">{source}</Row>}
      {d.origin && (
        <Row label="安装自" mono>
          <span className="max-w-[360px] truncate select-text" title={d.origin}>
            {d.origin}
          </span>
        </Row>
      )}
      {d.publisher && <Row label="发布者">{d.publisher}</Row>}
      {home && (
        <a href={home} target="_blank" rel="noreferrer noopener" className="flex items-center gap-3 px-4 py-2.5 whitespace-nowrap transition-colors hover:bg-hover">
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">主页</span>
          <span className="min-w-0 truncate text-[12.5px] text-ink-3">{new URL(home).host}</span>
          <Icon name="external" size={12} className="shrink-0 text-ink-3" />
        </a>
      )}
      {(d.warnings ?? []).map((w) => (
        <div key={w} className="flex items-start gap-2.5 px-4 py-2.5">
          <Icon name="info" size={14} className="mt-0.5 shrink-0 text-orange" />
          <span className="min-w-0 text-[12px] leading-snug break-words text-ink-2">{w}</span>
        </div>
      ))}
    </Group>
  );
}

// ── the form ────────────────────────────────────────────

const inputClass = "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

interface FieldProps {
  field: PluginField;
  path: string[];
  draft: JsonObject;
  typed: Typed;
  errors: Record<string, string>;
  problems: Record<string, string>;
  onEdit: (path: string[], value: Json | undefined) => void;
  onType: (path: string[], text: string, integer: boolean) => void;
  /** Saves one field at once (a machine's switch). */
  onSaveNow: (path: string[], value: Json | undefined) => void;
  /** The plugin operates computers: machine rows say how Cua Driver is there. */
  computers: boolean;
}

/** One field of the form, by its type; its title above (or beside a switch), its description and error below. */
function FieldRow(props: FieldProps) {
  const { field: f, path, draft, errors, problems, onEdit } = props;
  const key = path.join(".");
  const err = errors[key] ?? problems[key];
  const title = fieldTitle(f);
  const id = `plugin-field-${key}`;
  const below = (
    <>
      {f.description && <p className="mt-1 text-[11.5px] leading-snug text-ink-3">{f.description}</p>}
      {err && (
        <p role="alert" className="mt-1 text-[11.5px] text-red">
          {err}
        </p>
      )}
    </>
  );

  if (f.type === "boolean") {
    const v = valueAt(draft, path);
    const on = typeof v === "boolean" ? v : f.default === true;
    return (
      <div className="px-4 py-2.5" data-field={key}>
        <div className="flex items-center gap-3">
          <span className="min-w-0 flex-1 text-[13px] text-ink">{title}</span>
          <Switch on={on} onChange={(x) => onEdit(path, x)} label={fieldLabel(f)} />
        </div>
        {below}
      </div>
    );
  }

  return (
    <div className="px-4 py-2.5" data-field={key}>
      <label htmlFor={id} className="mb-1.5 block text-[12px] font-medium text-ink-2">
        {title}
      </label>
      <Control {...props} id={id} invalid={!!err} />
      {below}
    </div>
  );
}

function Control({ field: f, path, draft, typed, onEdit, onType, onSaveNow, computers, id, invalid }: FieldProps & { id: string; invalid: boolean }) {
  const ring = invalid ? "shadow-[inset_0_0_0_1px_var(--red)]" : "shadow-inset-field";
  const v = valueAt(draft, path);

  if (f.type === "array" && f.items?.format === "machine") {
    return <MachineSwitches field={f} path={path} draft={draft} onSaveNow={onSaveNow} computers={computers} id={id} />;
  }
  if (f.type === "array") return <ListControl field={f} path={path} draft={draft} onEdit={onEdit} id={id} ring={ring} />;

  if (f.enum?.length) {
    const at = f.enum.findIndex((c) => v !== undefined && same(c.value, v));
    return (
      <select id={id} value={at < 0 ? "" : String(at)} onChange={(e) => onEdit(path, e.target.value === "" ? undefined : f.enum![Number(e.target.value)].value)} className={cx(inputClass, ring, "max-w-[320px]")}>
        <option value="">{placeholder(f)}</option>
        {f.enum.map((c, i) => (
          <option key={i} value={String(i)}>
            {choiceLabel(c)}
          </option>
        ))}
      </select>
    );
  }

  if (f.type === "integer" || f.type === "number") {
    const key = path.join(".");
    const text = typed[key]?.text ?? (typeof v === "number" ? String(v) : "");
    const range = [f.minimum !== undefined ? `最小 ${f.minimum}` : "", f.maximum !== undefined ? `最大 ${f.maximum}` : ""].filter(Boolean).join("，");
    return (
      <div className="flex items-center gap-2">
        <input
          id={id}
          value={text}
          inputMode={f.type === "integer" ? "numeric" : "decimal"}
          onChange={(e) => onType(path, e.target.value, f.type === "integer")}
          placeholder={placeholder(f)}
          spellCheck={false}
          autoComplete="off"
          className={cx(inputClass, ring, "max-w-[200px] font-mono")}
        />
        {range && <span className="text-[11px] whitespace-nowrap text-ink-3">{range}</span>}
      </div>
    );
  }

  return (
    <input
      id={id}
      value={typeof v === "string" ? v : ""}
      onChange={(e) => onEdit(path, e.target.value === "" ? undefined : e.target.value)}
      type={f.format === "email" ? "email" : f.format === "uri" ? "url" : "text"}
      placeholder={placeholder(f)}
      maxLength={f.max_length}
      spellCheck={false}
      autoComplete="off"
      autoCapitalize="off"
      className={cx(inputClass, ring, f.format && "font-mono")}
    />
  );
}

/** A list of texts (each removable, more added), or of choices (each one pressed in or out). */
/** A list of the hub's machines (format machine): a switch per machine of the systems the field names, by name and how
 *  it is, in effect at once; a value naming no machine still shows, to be turned off. */
function MachineSwitches({ field: f, path, draft, onSaveNow, computers, id }: Pick<FieldProps, "field" | "path" | "draft" | "onSaveNow" | "computers"> & { id: string }) {
  const all = useStore((s) => s.machines);
  const items = listItems(draft, path, f);
  const os = f.items?.os ?? [];
  const machines = all.filter((m) => !os.length || os.includes(m.os ?? ""));
  const unknown = items.filter((x) => !all.some((m) => m.id === x));
  const flip = (mid: string, on: boolean) => {
    const next = items.filter((x) => x !== mid);
    if (on) next.push(mid);
    onSaveNow(path, next.length ? next : undefined);
  };
  const rows = [...machines.map((m) => ({ id: m.id, name: m.name, status: machineStatus(m, computers) })), ...unknown.map((x) => ({ id: x, name: x, status: "找不到这台机器" }))];
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{os.join() === "darwin" ? "这个 hub 上还没有 Mac。" : "这个 hub 上还没有机器。"}</p>;
  return (
    <div id={id} role="group" className="flex flex-col divide-y divide-line rounded-control shadow-hairline">
      {rows.map((r) => (
        <div key={r.id} className="flex items-center gap-3 px-3 py-2">
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] text-ink">{r.name}</p>
            <p className="truncate text-[11.5px] text-ink-3">{r.status}</p>
          </div>
          <Switch on={items.includes(r.id)} onChange={(on) => flip(r.id, on)} label={r.name} />
        </div>
      ))}
    </div>
  );
}

function ListControl({ field: f, path, draft, onEdit, id, ring }: Pick<FieldProps, "field" | "path" | "draft" | "onEdit"> & { id: string; ring: string }) {
  const [adding, setAdding] = useState("");
  const items = listItems(draft, path, f);
  const store = (list: string[]) => onEdit(path, list.length ? list : undefined);
  const choices = f.items?.enum;

  if (choices?.length) {
    return (
      <div id={id} role="group" className="flex flex-wrap gap-1.5">
        {choices.map((c) => {
          const value = typeof c.value === "string" ? c.value : String(c.value);
          const on = items.includes(value);
          return (
            <button
              key={value}
              type="button"
              aria-pressed={on}
              onClick={() => store(on ? items.filter((x) => x !== value) : [...items, value])}
              className={cx(
                "flex h-7 items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 text-[12px] transition-[background-color,box-shadow,color] duration-150",
                on ? "bg-accent-tint font-medium text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "text-ink-2 shadow-hairline hover:bg-hover",
              )}
            >
              {on && <Icon name="check" size={12} stroke={2.2} />}
              {choiceLabel(c)}
            </button>
          );
        })}
      </div>
    );
  }

  const add = () => {
    const v = adding.trim();
    if (!v) return;
    store([...items, v]);
    setAdding("");
  };
  return (
    <div className="flex flex-col gap-1.5">
      {items.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {items.map((item, i) => (
            <li key={`${i}-${item}`} className="flex h-7 items-center gap-1 rounded-full bg-inset pr-1 pl-2.5 text-[12px] text-ink shadow-hairline">
              <span className="max-w-[240px] truncate">{item}</span>
              <IconButton icon="x" label={`删除 ${item}`} size={20} iconSize={11} onClick={() => store(items.filter((_, j) => j !== i))} />
            </li>
          ))}
        </ul>
      )}
      <div className="flex max-w-[360px] items-center gap-1.5">
        <input
          id={id}
          value={adding}
          onChange={(e) => setAdding(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add();
            }
          }}
          placeholder="添加一项，回车确认"
          spellCheck={false}
          autoComplete="off"
          className={cx(inputClass, ring)}
        />
        <Button tone="secondary" icon="plus" onClick={add} disabled={!adding.trim()}>
          添加
        </Button>
      </div>
    </div>
  );
}

// ── a secret ────────────────────────────────────────────

/** Sets one secret: typed in hidden, saved to the hub, never shown again; one that is set can be cleared. */
function SecretDialog({ plugin, secret, onClose, onDone }: { plugin: string; secret: PluginSecret; onClose: () => void; onDone: (d: HubPluginDetail) => void }) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "clear">("");
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const run = async (kind: "save" | "clear") => {
    const v = value.trim();
    if (kind === "save" && !v) return;
    setBusy(kind);
    setError("");
    try {
      const d = kind === "save" ? await setPluginSecret(plugin, secret.name, v) : await clearPluginSecret(plugin, secret.name);
      onDone(d);
      toast(kind === "save" ? `已保存「${secret.title}」` : `已清除「${secret.title}」`, "ok");
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  };
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      width={440}
      title={secret.title}
      footer={
        <>
          {secret.set && (
            <span className="mr-auto">
              {confirm ? (
                <Button tone="danger" busy={busy === "clear"} disabled={!!busy} onClick={() => void run("clear")}>
                  确认清除
                </Button>
              ) : (
                <Button tone="quiet" icon="trash" disabled={!!busy} onClick={() => setConfirm(true)} className="text-red enabled:hover:text-red">
                  清除
                </Button>
              )}
            </span>
          )}
          <Button tone="quiet" onClick={onClose} disabled={!!busy}>
            取消
          </Button>
          <Button tone="primary" busy={busy === "save"} disabled={!value.trim() || !!busy} onClick={() => void run("save")}>
            保存
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void run("save");
        }}
      >
        <div className="flex items-center gap-2 text-[12px] text-ink-3">
          <Icon name="lock" size={13} className="shrink-0" />
          {secretState(secret)}
        </div>
        <input
          type="password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          autoFocus
          autoComplete="new-password"
          spellCheck={false}
          placeholder={secret.set ? "输入新的值替换" : "输入"}
          aria-label={secret.title}
          disabled={!!busy}
          className={cx(inputClass, "h-9 font-mono shadow-inset-field")}
        />
        <p className="text-[11.5px] leading-relaxed text-ink-3">{secret.description || "存在 hub 上，只交给这个插件。"}</p>
        {error && <p className="rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] break-words text-red">没有保存：{error}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
