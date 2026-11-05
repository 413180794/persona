// 设置 › 模型供应商 › 新增 / 编辑 (#2455, after cindy's AddProviderWizard): ① 选择供应商 (Anthropic 官方, a compatible
// gateway, or one an extension offers) ② 连接 (address, auth, key; 测试连接 and 获取模型列表 through the hub) ③ 选择模型
// (context window, thinking levels, the default), the rest under 高级. Editing opens at ② with the same form. The logic
// is lib/providers.ts.
import { useEffect, useId, useMemo, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { launcherNameProblem, loadPresets, presetBody, presetChoices, presetDefaultModel, presetModelLabel, presetSubtitle, type LauncherPreset } from "../../lib/launcher-presets";
import {
  EFFORT_LEVELS,
  formFromView,
  formModel,
  formProblems,
  mergeDiscovered,
  newForm,
  presetOf,
  probeBody,
  providerBody,
  testModel,
  testText,
  type FormModel,
  type ProviderForm,
  type ProviderModel,
  type ProviderView,
  type TestResult,
} from "../../lib/providers";
import { effortLabel } from "../../lib/routines";
import { Button, cx, Dialog, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Segmented } from "../assistant/parts";
import { Switch } from "./SettingsParts";
import { CheckMark, Disclosure, FIELD, INPUT, Labelled, SecretField, SMALL, TextField } from "./ProviderFields";
import { t } from "../../i18n";

type Step = 1 | 2 | 3;
type Source = "official" | "custom" | "extension";
const stepTitle = (s: Step) => ({ 1: t("settings-providerwizard.step_pick"), 2: t("settings-providerwizard.step_connect"), 3: t("settings-providerwizard.step_models") })[s];

export function ProviderWizard({ existing, onClose, onSaved }: { existing: ProviderView | "new" | null; onClose: () => void; onSaved: () => void }) {
  const isNew = existing === "new";
  const old = existing && existing !== "new" ? existing : null;
  const [step, setStep] = useState<Step>(1);
  const [f, setF] = useState<ProviderForm>(() => newForm());
  const [source, setSource] = useState<Source>("custom");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [testing, setTesting] = useState(false);
  const [test, setTest] = useState<TestResult | null>(null);
  const [probeModel, setProbeModel] = useState("");
  const [discovering, setDiscovering] = useState(false);
  const [found, setFound] = useState<{ n: number; error?: string } | null>(null);
  // 从扩展添加 (docs/extensions.md「模型供应商」): only when adding, and only when the hub's extensions offer providers.
  const [presets, setPresets] = useState<LauncherPreset[]>([]);
  const [pick, setPick] = useState("");
  const [pf, setPf] = useState({ name: "", api_key: "", model: "" });
  const preset = presets.find((p) => p.id === pick);

  useEffect(() => {
    if (existing === null) return;
    setError("");
    setAdvanced(false);
    setTest(null);
    setFound(null);
    setProbeModel("");
    setF(old ? formFromView(old) : newForm("custom"));
    setSource(old ? presetOf(old.base_url) : "custom");
    setStep(old ? 2 : 1);
    if (!isNew) return;
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

  const set = <K extends keyof ProviderForm>(k: K) => (v: ProviderForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const problems = formProblems(f, isNew, !!old?.has_key);
  const responses = f.kind === "api" && f.api === "responses";
  const fromExtension = isNew && step === 1 && source === "extension" && !!preset;
  const presetReady = !!preset && !!pf.name.trim() && !launcherNameProblem(pf.name) && !!pf.api_key.trim();
  const canSave = fromExtension ? presetReady : !problems.connect && !problems.models && !problems.advanced;

  const choose = (s: Source) => {
    setSource(s);
    if (s !== "extension") setF((x) => ({ ...newForm(s), name: x.name, api_key: x.api_key, models: x.models, model: x.model }));
  };

  const runTest = async () => {
    const model = probeModel.trim() || testModel(f);
    setTesting(true);
    setTest(null);
    try {
      setTest(await api<TestResult>("/launchers/test", { method: "POST", json: probeBody(f, old?.name ?? null, model) }));
    } catch (e) {
      setTest({ ok: false, ms: 0, error: errorText(e) });
    } finally {
      setTesting(false);
    }
  };
  const runDiscover = async () => {
    setDiscovering(true);
    setFound(null);
    try {
      const r = await api<{ models?: ProviderModel[]; error?: string }>("/launchers/discover", { method: "POST", json: probeBody(f, old?.name ?? null) });
      const list = r.models ?? [];
      setF((x) => {
        const models = mergeDiscovered(x.models, list);
        return { ...x, models, model: x.model || models.find((m) => m.checked)?.id || "" };
      });
      setFound({ n: list.length, error: r.error });
    } catch (e) {
      setFound({ n: 0, error: errorText(e) });
    } finally {
      setDiscovering(false);
    }
  };

  const save = async () => {
    if (busy || !canSave) return;
    setBusy(true);
    setError("");
    try {
      if (fromExtension && preset) {
        const body = presetBody(preset, pf.name, pf.api_key, pf.model);
        await api("/launchers", { method: "POST", json: body });
        toast(t("settings-providerwizard.added", { name: body.name }), "ok");
      } else {
        const body = providerBody(f, old);
        await api(isNew ? "/launchers" : `/launchers/${enc(String(body.name))}`, { method: isNew ? "POST" : "PUT", json: body });
        toast(isNew ? t("settings-providerwizard.added", { name: String(body.name) }) : t("settings-providerwizard.saved", { name: String(body.name) }), "ok");
      }
      onSaved();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const steps: Step[] = isNew ? [1, 2, 3] : [2, 3];
  const stepProblem = step === 1 ? "" : step === 2 ? problems.connect : problems.models || problems.advanced;
  const next = () => {
    if (stepProblem) return setError(stepProblem);
    setError("");
    setStep((s) => (s + 1) as Step);
  };
  const reachable = (s: Step) => s <= step || !isNew || (s === 3 && !problems.connect);

  return (
    <Dialog
      open={existing !== null}
      onClose={onClose}
      width={580}
      title={isNew ? t("settings-providerwizard.title_new") : t("settings-providerwizard.title_edit", { name: old?.name ?? "" })}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <span className="flex-1" />
          {step > steps[0] && (
            <Button tone="secondary" onClick={() => (setError(""), setStep((s) => (s - 1) as Step))}>
              {t("settings-providerwizard.back")}
            </Button>
          )}
          {step < 3 && !fromExtension && (
            <Button tone={isNew ? "primary" : "secondary"} onClick={next}>
              {t("settings-providerwizard.next")}
            </Button>
          )}
          {(step === 3 || fromExtension || !isNew) && (
            <Button tone="primary" busy={busy} disabled={!canSave} onClick={() => void save()}>
              {t("common.save")}
            </Button>
          )}
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (step === 3 || fromExtension) void save();
          else next();
        }}
      >
        <ol aria-label={t("settings-providerwizard.steps")} className="flex items-center gap-2">
          {steps.map((s, i) => (
            <li key={s} className="flex min-w-0 items-center gap-2">
              {i > 0 && <span aria-hidden className="h-px w-4 shrink-0 bg-line-strong sm:w-8" />}
              <button
                type="button"
                disabled={!reachable(s)}
                aria-current={s === step ? "step" : undefined}
                onClick={() => (setError(""), setStep(s))}
                className="touch-hit flex min-w-0 items-center gap-1.5 rounded-control text-[12px] disabled:opacity-50"
              >
                <span className={cx("flex size-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold tabular-nums", s === step ? "bg-accent text-white" : s < step ? "bg-accent-tint text-accent" : "bg-inset text-ink-3 shadow-hairline")}>
                  {s < step ? <Icon name="check" size={10} stroke={3} /> : i + 1}
                </span>
                <span className={cx("truncate whitespace-nowrap", s === step ? "font-medium text-ink" : "text-ink-3")}>{stepTitle(s)}</span>
              </button>
            </li>
          ))}
        </ol>

        {step === 1 && (
          <PickStep source={source} onChoose={choose} presets={presets}>
            {source === "extension" && preset && <PresetFields presets={presets} preset={preset} onPick={(p) => (setPick(p.id), setPf((x) => ({ ...x, model: presetDefaultModel(p) })))} pf={pf} setPf={setPf} />}
          </PickStep>
        )}

        {step === 2 && (
          <div className="flex flex-col gap-3">
            <TextField label={t("settings-providerwizard.name")} value={f.name} onChange={set("name")} disabled={!isNew} mono placeholder={t("settings-providerwizard.name_placeholder", { example: "my_gateway" })}
              error={isNew ? launcherNameProblem(f.name) : undefined} hint={t("settings-providerwizard.name_hint")} />
            {isNew && (
              <Labelled label={t("settings-providerwizard.kind")} hint={f.kind === "api" ? t("settings-providerwizard.kind_api_hint") : t("settings-providerwizard.kind_launcher_hint")}>
                <Segmented<ProviderForm["kind"]> label={t("settings-providerwizard.kind")} value={f.kind} onChange={(v) => setF((x) => ({ ...x, kind: v, api: "messages" }))}
                  options={[{ v: "launcher", label: "Claude Code" }, { v: "api", label: t("settings-providerwizard.direct_api") }]} />
              </Labelled>
            )}
            {isNew && f.kind === "api" && (
              <Labelled label={t("settings-providerwizard.api_protocol")}>
                <Segmented<ProviderForm["api"]> label={t("settings-providerwizard.api_protocol")} value={f.api} onChange={set("api")} options={[{ v: "messages", label: "Anthropic Messages" }, { v: "responses", label: "OpenAI Responses" }]} />
              </Labelled>
            )}
            <TextField label={t("settings-providerwizard.base_url")} value={f.base_url} onChange={set("base_url")} mono inputMode="url" placeholder="https://…"
              hint={source === "official" ? t("settings-providerwizard.url_official_hint") : responses ? t("settings-providerwizard.url_responses_hint") : t("settings-providerwizard.url_gateway_hint")} />
            {!responses && (
              <Labelled label={t("settings-providerwizard.auth")} hint={f.auth === "bearer" ? t("settings-providerwizard.auth_bearer_hint") : t("settings-providerwizard.auth_key_hint")}>
                <Segmented<ProviderForm["auth"]> label={t("settings-providerwizard.auth")} value={f.auth} onChange={set("auth")} options={[{ v: "bearer", label: "Bearer" }, { v: "api_key", label: "x-api-key" }]} />
              </Labelled>
            )}
            <SecretField label={isNew || !old?.has_key ? "API Key" : t("settings-providerwizard.key_keep")} value={f.api_key} onChange={set("api_key")} placeholder={old?.has_key ? t("settings-providerwizard.key_set") : "sk-…"} hint={t("settings-providerwizard.key_hint")} />
            {responses ? (
              <p className="text-[11.5px] text-ink-3">{t("settings-providerwizard.responses_note")}</p>
            ) : (
              <div className="flex flex-col gap-2 rounded-card bg-inset p-3 shadow-hairline">
                <div className="flex flex-wrap items-center gap-2">
                  <input value={probeModel} onChange={(e) => setProbeModel(e.target.value)} placeholder={testModel(f) || t("settings-providerwizard.test_model")} aria-label={t("settings-providerwizard.test_model")} autoComplete="off" spellCheck={false}
                    className={cx(FIELD, "h-8 w-full min-w-0 px-2.5 font-mono text-[12.5px] sm:w-auto sm:flex-1")} />
                  <Button tone="secondary" icon="run" busy={testing} disabled={!!problems.connect} onClick={() => void runTest()}>
                    {t("settings-providerwizard.test")}
                  </Button>
                  <Button tone="secondary" icon="download" busy={discovering} disabled={!!problems.connect} onClick={() => void runDiscover()}>
                    {t("settings-providerwizard.discover")}
                  </Button>
                </div>
                <p className="text-[11px] text-ink-3">{t("settings-providerwizard.test_cost")}</p>
                {test && (
                  <p role="status" className={cx("flex items-start gap-1.5 text-[12px]", test.ok ? "text-green" : "text-red")}>
                    <Icon name={test.ok ? "check" : "warning"} size={12} stroke={2.4} className="mt-0.5 shrink-0" />
                    <span className="min-w-0 break-words">{testText(test)}</span>
                  </p>
                )}
                {found && (
                  <p role="status" className={cx("text-[12px]", found.error ? "text-orange" : "text-ink-2")}>
                    {found.error ? t("settings-providerwizard.discover_failed", { error: found.error }) : t("settings-providerwizard.discover_found", { count: found.n })}
                  </p>
                )}
              </div>
            )}
          </div>
        )}

        {step === 3 && (
          <div className="flex flex-col gap-3">
            <ModelList f={f} setF={setF} />
            <Disclosure title={t("settings-providerwizard.advanced")} open={advanced} onToggle={() => setAdvanced((o) => !o)} badge={advanced ? undefined : t("settings-providerwizard.advanced_badge")}>
              <Advanced f={f} setF={setF} set={set} hadHeaders={!!old?.has_headers} />
            </Disclosure>
          </div>
        )}

        {(error || (step === 3 && !canSave && (problems.models || problems.advanced))) && <p role="alert" className="text-[12px] text-red">{error || problems.models || problems.advanced}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function PickStep({ source, onChoose, presets, children }: { source: Source; onChoose: (s: Source) => void; presets: LauncherPreset[]; children?: React.ReactNode }) {
  const options: { v: Source; title: string; detail: string }[] = [
    { v: "official", title: t("settings-providerwizard.official"), detail: t("settings-providerwizard.official_detail") },
    { v: "custom", title: t("settings-providerwizard.custom"), detail: t("settings-providerwizard.custom_detail") },
    ...(presets.length ? [{ v: "extension" as const, title: t("settings-providerwizard.extension"), detail: t("settings-providerwizard.extension_detail", { count: presets.length }) }] : []),
  ];
  return (
    <div className="flex flex-col gap-3">
      <div role="radiogroup" aria-label={t("settings-providerwizard.provider_type")} className="grid gap-2 sm:grid-cols-2">
        {options.map((o) => (
          <button key={o.v} type="button" role="radio" aria-checked={source === o.v} onClick={() => onChoose(o.v)}
            className={cx("touch-hit flex items-start gap-2.5 rounded-card p-3 text-left transition-[background-color,box-shadow] duration-150",
              source === o.v ? "bg-accent-tint shadow-[inset_0_0_0_1.5px_var(--accent)]" : "bg-surface shadow-btn hover:bg-hover")}>
            <span className={cx("mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full", source === o.v ? "bg-accent text-white" : "shadow-[inset_0_0_0_1.5px_var(--line-strong)]")}>
              {source === o.v && <Icon name="check" size={9} stroke={3.2} />}
            </span>
            <span className="min-w-0">
              <span className="block text-[13px] font-medium text-ink">{o.title}</span>
              <span className="mt-0.5 block text-[11.5px] leading-relaxed text-ink-3">{o.detail}</span>
            </span>
          </button>
        ))}
      </div>
      {children}
    </div>
  );
}

/** ③: the models this provider offers. Checked ones are saved; the default is one of them. */
function ModelList({ f, setF }: { f: ProviderForm; setF: React.Dispatch<React.SetStateAction<ProviderForm>> }) {
  const [adding, setAdding] = useState("");
  const update = (i: number, patch: Partial<FormModel>) =>
    setF((x) => {
      const models = x.models.map((m, j) => (j === i ? { ...m, ...patch } : m));
      // unchecking the default moves it to the first checked one
      const model = models.some((m) => m.checked && m.id === x.model) ? x.model : models.find((m) => m.checked)?.id ?? "";
      return { ...x, models, model };
    });
  const add = () => {
    const id = adding.trim();
    if (!id) return;
    setF((x) => (x.models.some((m) => m.id === id) ? { ...x, models: x.models.map((m) => (m.id === id ? { ...m, checked: true } : m)) } : { ...x, models: [...x.models, formModel({ id })], model: x.model || id }));
    setAdding("");
  };
  const checked = f.models.filter((m) => m.checked).length;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[12px] font-medium text-ink-2">{t("settings-providerwizard.models")}</span>
        <span className="text-[11px] text-ink-3 tabular-nums">{t("settings-providerwizard.checked_count", { checked, total: f.models.length })}</span>
      </div>
      {f.models.length === 0 ? (
        <p className="rounded-card bg-inset px-3 py-4 text-center text-[12px] text-ink-3 shadow-hairline">{t("settings-providerwizard.no_models")}</p>
      ) : (
        <ul aria-label={t("settings-providerwizard.models")} className="overflow-hidden rounded-card bg-surface shadow-btn">
          {f.models.map((m, i) => (
            <ModelRow key={m.id + i} m={m} isDefault={m.checked && f.model === m.id} onChange={(p) => update(i, p)} onDefault={() => setF((x) => ({ ...x, model: m.id }))} />
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        <input value={adding} onChange={(e) => setAdding(e.target.value)} onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), add())} placeholder={t("settings-providerwizard.add_model_id")} aria-label={t("settings-providerwizard.add_model_id")} autoComplete="off" spellCheck={false} className={cx(INPUT, "font-mono")} />
        <Button tone="secondary" icon="plus" disabled={!adding.trim()} onClick={add}>
          {t("settings-providerwizard.add")}
        </Button>
      </div>
    </div>
  );
}

function ModelRow({ m, isDefault, onChange, onDefault }: { m: FormModel; isDefault: boolean; onChange: (p: Partial<FormModel>) => void; onDefault: () => void }) {
  const toggleEffort = (e: string) => {
    const efforts = m.efforts.includes(e) ? m.efforts.filter((x) => x !== e) : EFFORT_LEVELS.filter((x) => x === e || m.efforts.includes(x));
    onChange({ efforts, default_effort: efforts.length && !efforts.includes(m.default_effort) ? "" : m.default_effort });
  };
  const levels = m.efforts.length ? m.efforts : [...EFFORT_LEVELS];
  return (
    <li data-model={m.id} className="border-b border-line px-3 py-2.5 last:border-b-0">
      <div className="flex items-center gap-2.5">
        <button type="button" role="checkbox" aria-checked={m.checked} aria-label={t("settings-providerwizard.use_model", { id: m.id })} onClick={() => onChange({ checked: !m.checked })}
          className="touch-hit shrink-0">
          <CheckMark on={m.checked} />
        </button>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-mono text-[12.5px] text-ink">{m.id}</span>
          {m.name && <span className="block truncate text-[11px] text-ink-3">{m.name}</span>}
        </span>
        {m.checked &&
          (isDefault ? (
            <span className="shrink-0 whitespace-nowrap rounded-chip bg-accent-tint px-2 py-0.5 text-[11px] font-medium text-accent">{t("settings-providerwizard.default")}</span>
          ) : (
            <button type="button" onClick={onDefault} className="touch-hit shrink-0 whitespace-nowrap rounded-chip px-2 py-0.5 text-[11px] text-ink-3 hover:bg-hover hover:text-ink">
              {t("settings-providerwizard.set_default")}
            </button>
          ))}
      </div>
      {m.checked && (
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2 pl-6.5">
          <label className="flex items-center gap-1.5 text-[11.5px] text-ink-3">
            <span className="whitespace-nowrap">{t("settings-providerwizard.context")}</span>
            <input value={m.context_window} onChange={(e) => onChange({ context_window: e.target.value })} inputMode="numeric" placeholder="200000" aria-label={t("settings-providerwizard.context_window", { id: m.id })}
              className={cx(SMALL, "w-[104px] font-mono tabular-nums")} />
          </label>
          <div role="group" aria-label={t("settings-providerwizard.efforts_supported", { id: m.id })} className="flex flex-wrap gap-1">
            {EFFORT_LEVELS.map((e) => (
              <button key={e} type="button" aria-pressed={m.efforts.includes(e)} onClick={() => toggleEffort(e)}
                className={cx("touch-hit h-6 whitespace-nowrap rounded-chip px-2 text-[11px] transition-colors duration-150", m.efforts.includes(e) ? "bg-accent-tint font-medium text-accent" : "bg-inset text-ink-3 shadow-hairline hover:text-ink-2")}>
                {effortLabel(e)}
              </button>
            ))}
          </div>
          <label className="flex items-center gap-1.5 text-[11.5px] text-ink-3">
            <span className="whitespace-nowrap">{t("settings-providerwizard.default_effort_short")}</span>
            <select value={m.default_effort} onChange={(e) => onChange({ default_effort: e.target.value })} aria-label={t("settings-providerwizard.model_default_effort", { id: m.id })} className={SMALL}>
              <option value="">{t("settings-providerwizard.unspecified")}</option>
              {levels.map((e) => (
                <option key={e} value={e}>
                  {effortLabel(e)}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
    </li>
  );
}

function Advanced({ f, setF, set, hadHeaders }: {
  f: ProviderForm;
  setF: React.Dispatch<React.SetStateAction<ProviderForm>>;
  set: <K extends keyof ProviderForm>(k: K) => (v: ProviderForm[K]) => void;
  hadHeaders: boolean;
}) {
  const list = useId();
  const ids = useMemo(() => f.models.filter((m) => m.checked).map((m) => m.id), [f.models]);
  const launcher = f.kind !== "api";
  const tier = (k: "opus_model" | "sonnet_model" | "haiku_model" | "fable_model" | "small_model", label: string, hint?: string) => (
    <Labelled label={label} hint={hint}>
      <input value={f[k]} onChange={(e) => set(k)(e.target.value)} list={list} placeholder={t("settings-providerwizard.same_as_default")} aria-label={label} autoComplete="off" spellCheck={false} className={cx(INPUT, "font-mono")} />
    </Labelled>
  );
  return (
    <>
      <datalist id={list}>
        {ids.map((id) => (
          <option key={id} value={id} />
        ))}
      </datalist>
      {launcher && (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            {tier("opus_model", t("settings-providerwizard.tier_model", { tier: "Opus" }))}
            {tier("sonnet_model", t("settings-providerwizard.tier_model", { tier: "Sonnet" }))}
            {tier("haiku_model", t("settings-providerwizard.tier_model", { tier: "Haiku" }))}
            {tier("fable_model", t("settings-providerwizard.tier_model", { tier: "Fable" }))}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {tier("small_model", t("settings-providerwizard.small_model"), t("settings-providerwizard.small_model_hint"))}
            <Labelled label={t("settings-providerwizard.subagent_model")} hint={t("settings-providerwizard.subagent_hint")}>
              <div className="flex items-center gap-2">
                <input value={f.subagent_model} onChange={(e) => set("subagent_model")(e.target.value)} list={list} placeholder={t("settings-providerwizard.same_as_default")} aria-label={t("settings-providerwizard.subagent_model")} autoComplete="off" spellCheck={false} className={cx(INPUT, "font-mono")} />
                <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[12px] text-ink-2">
                  {t("settings-providerwizard.force")}
                  <Switch on={f.subagent_force} onChange={set("subagent_force")} label={t("settings-providerwizard.force_label")} />
                </span>
              </div>
            </Labelled>
          </div>
        </>
      )}
      <SecretField
        label={t("settings-providerwizard.headers")}
        multiline
        value={f.headers}
        onChange={(v) => setF((x) => ({ ...x, headers: v, headers_touched: true }))}
        placeholder={hadHeaders && !f.headers_touched ? t("settings-providerwizard.headers_set") : t("settings-providerwizard.headers_placeholder")}
        hint={
          hadHeaders && !f.headers_touched ? (
            <button type="button" onClick={() => setF((x) => ({ ...x, headers: "", headers_touched: true }))} className="touch-hit text-red hover:underline">
              {t("settings-providerwizard.headers_clear")}
            </button>
          ) : f.headers_touched && hadHeaders && !f.headers.trim() ? (
            t("settings-providerwizard.headers_cleared")
          ) : (
            t("settings-providerwizard.headers_hint")
          )
        }
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <TextField label={t("settings-providerwizard.proxy")} value={f.proxy} onChange={set("proxy")} mono placeholder="http://127.0.0.1:7890" hint="HTTP_PROXY / HTTPS_PROXY" />
        <TextField label="NO_PROXY" value={f.no_proxy} onChange={set("no_proxy")} mono placeholder="localhost,127.0.0.1" />
        <TextField label={t("settings-providerwizard.timeout")} value={f.timeout_ms} onChange={set("timeout_ms")} mono inputMode="numeric" placeholder="3000000" />
        <TextField label={t("settings-providerwizard.compact_window")} value={f.compact_window} onChange={set("compact_window")} mono inputMode="numeric" placeholder={t("settings-providerwizard.same_as_model_context")} />
        <TextField label={t("settings-providerwizard.max_context")} value={f.max_context} onChange={set("max_context")} mono inputMode="numeric" placeholder={t("settings-providerwizard.same_as_model_context")} />
        <Labelled label={t("settings-providerwizard.default_effort")}>
          <select value={f.effort} onChange={(e) => set("effort")(e.target.value)} aria-label={t("settings-providerwizard.default_effort")} className={INPUT}>
            <option value="">{t("settings-providerwizard.follow_model")}</option>
            {EFFORT_LEVELS.map((e) => (
              <option key={e} value={e}>
                {effortLabel(e)}
              </option>
            ))}
          </select>
        </Labelled>
      </div>
      {launcher && (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            <TextField label={t("settings-providerwizard.custom_model")} value={f.custom_model} onChange={set("custom_model")} mono placeholder={t("settings-providerwizard.custom_model_placeholder")} />
            <TextField label={t("settings-providerwizard.custom_model_name")} value={f.custom_model_name} onChange={set("custom_model_name")} />
          </div>
          <TextField label={t("settings-providerwizard.custom_model_description")} value={f.custom_model_description} onChange={set("custom_model_description")} hint={t("settings-providerwizard.custom_model_hint")} />
          <TextField label={t("settings-providerwizard.args")} value={f.args} onChange={set("args")} mono placeholder="--dangerously-skip-permissions" hint={t("settings-providerwizard.args_hint")} />
        </>
      )}
    </>
  );
}

/** 从扩展添加: pick one of the extensions' providers, then only the name, the key and maybe another of its models. */
function PresetFields({ presets, preset, onPick, pf, setPf }: {
  presets: LauncherPreset[];
  preset: LauncherPreset;
  onPick: (p: LauncherPreset) => void;
  pf: { name: string; api_key: string; model: string };
  setPf: React.Dispatch<React.SetStateAction<{ name: string; api_key: string; model: string }>>;
}) {
  const choices = presetChoices(preset);
  const nameProblem = launcherNameProblem(pf.name);
  return (
    <>
      <div>
        <span className="mb-1 block text-[12px] font-medium text-ink-2">{t("settings-providerwizard.extension_providers")}</span>
        <div role="radiogroup" aria-label={t("settings-providerwizard.extension_providers")} className="overflow-hidden rounded-control bg-field shadow-inset-field">
          {presets.map((p) => (
            <button key={p.id} type="button" role="radio" aria-checked={p.id === preset.id} onClick={() => onPick(p)}
              className="flex w-full items-center gap-3 border-b border-line px-3 py-2 text-left transition-colors last:border-b-0 hover:bg-hover">
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px] font-medium text-ink">{p.name}</div>
                {p.description && <div className="line-clamp-2 text-[11.5px] text-ink-2">{p.description}</div>}
                <div className="truncate text-[11px] text-ink-3">{presetSubtitle(p)}</div>
              </div>
              {p.id === preset.id && <Icon name="check" size={13} stroke={2.2} className="shrink-0 text-accent" />}
            </button>
          ))}
        </div>
        <span className="mt-1 block text-[11px] text-ink-3">{t("settings-providerwizard.extension_note", { plugin: preset.plugin })}</span>
      </div>
      <TextField label={t("settings-providerwizard.name")} value={pf.name} onChange={(v) => setPf((x) => ({ ...x, name: v }))} mono placeholder={t("settings-providerwizard.name_placeholder", { example: "jv_team" })} error={nameProblem} hint={t("settings-providerwizard.name_hint")} />
      <SecretField label="API Key" value={pf.api_key} onChange={(v) => setPf((x) => ({ ...x, api_key: v }))} hint={preset.key_hint ? t("settings-providerwizard.key_hint_with", { hint: preset.key_hint }) : t("settings-providerwizard.key_hint")} />
      <Labelled label={t("settings-providerwizard.default_model")} hint={preset.models_error ? t("settings-providerwizard.preset_models_error", { error: preset.models_error }) : t("settings-providerwizard.default_model_hint")}>
        {choices.length ? (
          <select value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} aria-label={t("settings-providerwizard.default_model")} className={cx(INPUT, "font-mono")}>
            {choices.map((m) => (
              <option key={m.id} value={m.id}>
                {presetModelLabel(m)}
              </option>
            ))}
          </select>
        ) : (
          <input value={pf.model} onChange={(e) => setPf((x) => ({ ...x, model: e.target.value }))} placeholder={t("settings-providerwizard.main_model")} aria-label={t("settings-providerwizard.default_model")} autoComplete="off" spellCheck={false} className={cx(INPUT, "font-mono")} />
        )}
      </Labelled>
    </>
  );
}

