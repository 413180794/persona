import { useEffect, useMemo, useState } from "react";
import { api, ApiError, enc, errorText } from "../../lib/api";
import {
  chooseProvider, draftFor, modelConfiguration, modelPurpose, modelSummary, providersFor, purposeGroups, saveModelPurpose, supportsField,
  type ModelConfigurationBody, type ModelDraft, type ModelPurposeBody,
} from "../../lib/model-config";
import { Button, Dialog, IconButton, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Group, Pill, Row } from "./SettingsParts";

export function ModelConfiguration({ open, onOpen, onProviders }: { open: string | null; onOpen: (id: string | null) => void; onProviders: () => void }) {
  const [data, setData] = useState<ModelConfigurationBody | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const load = async () => {
    try {
      setData(await modelConfiguration());
      setError("");
    } catch (e) {
      setError(e instanceof ApiError && e.status === 404 ? "这个 hub 还不支持统一模型配置，请先更新服务器。" : errorText(e));
    }
  };
  useEffect(() => { void load(); }, []);
  const groups = useMemo(() => purposeGroups(data?.purposes ?? [], query), [data, query]);
  const sourceName = (id?: string) => data?.purposes.find((p) => p.id === id)?.name ?? id;

  return (
    <>
      <div className="mb-4 flex items-center gap-2">
        <label className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control bg-surface px-3 shadow-btn focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
          <Icon name="search" size={13} stroke={2} className="text-ink-3" />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索用途或模型" aria-label="搜索模型用途"
            className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3" />
          {query && <IconButton icon="x" label="清除" size={22} iconSize={12} onClick={() => setQuery("")} />}
        </label>
        <IconButton icon="retry" label="刷新模型配置" onClick={() => void load()} />
      </div>
      <p className="mb-5 px-1 text-[12px] leading-relaxed text-ink-3">各功能下一次运行使用这里的配置；正在工作的任务不打断。连接和凭据在「模型供应商」中管理。</p>
      {!data && !error && <Loading />}
      {error && <p role="alert" className="mb-4 text-[13px] text-red">{error}</p>}
      {data && groups.length === 0 && <p className="py-10 text-center text-[13px] text-ink-3">{query ? `没有和「${query}」相关的用途` : "hub 暂无可配置的模型用途"}</p>}
      {groups.map(([group, purposes]) => (
        <Group key={group} title={group}>
          {purposes.map((p) => (
            <Row key={p.id} label={p.name} hint={modelSummary(p, data?.providers)} onClick={() => onOpen(p.id)}>
              <span className="hidden max-w-[180px] truncate text-[11.5px] text-ink-3 sm:inline">{p.status || (p.inherited_from ? `跟随 ${sourceName(p.inherited_from)}` : "自定义")}</span>
            </Row>
          ))}
        </Group>
      ))}
      <Button tone="quiet" onClick={onProviders}>管理模型供应商</Button>
      <PurposeEditor key={open} id={open} sourceName={sourceName} onClose={() => onOpen(null)} onSaved={() => void load()} />
    </>
  );
}

const inputClass = "h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

function PurposeEditor({ id, sourceName, onClose, onSaved }: { id: string | null; sourceName: (id?: string) => string | undefined; onClose: () => void; onSaved: () => void }) {
  const [data, setData] = useState<ModelPurposeBody | null>(null);
  const [draft, setDraft] = useState<ModelDraft>({ provider: "", model: "", effort: "" });
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [models, setModels] = useState<{ id: string; name?: string }[]>([]);
  const [modelsError, setModelsError] = useState("");
  const load = async () => {
    if (!id) return;
    try {
      const r = await modelPurpose(id);
      setData(r);
      setDraft(draftFor(r.purpose));
      setError("");
      setConflict(false);
    } catch (e) {
      setError(errorText(e));
    }
  };
  useEffect(() => { void load(); }, [id]);
  useEffect(() => {
    if (!id?.startsWith("plugin.")) return;
    const controller = new AbortController();
    api<{ models: { id: string; name?: string }[] }>(`/plugins/${enc(id.slice("plugin.".length))}/models`, { signal: controller.signal })
      .then((r) => setModels(r.models ?? []))
      .catch((e) => { if (!controller.signal.aborted) setModelsError(errorText(e)); });
    return () => controller.abort();
  }, [id]);

  const p = data?.purpose;
  const candidates = p ? providersFor(p, data.providers) : [];
  const chosen = data?.providers.find((v) => v.id === (draft.provider || p?.effective?.provider));
  const effortAllowed = p && supportsField(p, "effort") && !(chosen?.kind === "api" && chosen.api !== "responses");
  const save = async (restore = false) => {
    if (!p || busy || conflict) return;
    setBusy(true);
    setError("");
    try {
      const r = await saveModelPurpose(p, restore ? { provider: "", model: "", effort: "" } : draft);
      setData(r);
      setDraft(draftFor(r.purpose));
      toast(restore ? "已恢复继承" : "已保存，下一次运行生效", "ok");
      onSaved();
      onClose();
    } catch (e) {
      const changed = e instanceof ApiError && e.status === 409;
      setConflict(changed);
      setError(changed ? "配置已在别处修改。你的编辑已保留；重新加载后再修改并保存。" : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={id !== null} onClose={() => { if (!busy) onClose(); }} width={480} title={p?.name ?? "模型配置"}
      footer={<>
        <Button tone="quiet" disabled={busy || !p || conflict} onClick={() => void save(true)}>恢复继承</Button>
        <span className="flex-1" />
        <Button tone="quiet" disabled={busy} onClick={onClose}>取消</Button>
        <Button tone="primary" busy={busy} disabled={!p || conflict} onClick={() => void save()}>保存</Button>
      </>}
    >
      {!p && !error && <Loading />}
      {p && <div className="flex flex-col gap-4">
        <Group title="当前生效" footer={p.inherited_from ? `继承来源：${sourceName(p.inherited_from)}` : "用途的配置优先于默认值；会话或任务的临时选择仍可覆盖。"}>
          <Row label="供应商" mono><span className="max-w-[220px] truncate">{data?.providers.find((v) => v.id === p.effective?.provider)?.name ?? p.effective?.provider ?? "未配置"}</span></Row>
          <Row label="模型" mono><span className="max-w-[220px] truncate">{p.effective?.model || "供应商默认"}</span></Row>
          {p.status && <Row label="状态"><Pill>{p.status}</Pill></Row>}
        </Group>
        {supportsField(p, "provider") && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">模型供应商</span>
          <select className={inputClass} value={draft.provider} disabled={busy} onChange={(e) => setDraft((d) => chooseProvider(d, e.target.value))}>
            <option value="">跟随默认</option>
            {draft.provider && !candidates.some((v) => v.id === draft.provider) && <option value={draft.provider}>{draft.provider}（当前引用不可用）</option>}
            {candidates.map((v) => <option key={v.id} value={v.id}>{v.name}{v.hidden ? "（已隐藏）" : ""}</option>)}
          </select>
        </label>}
        {supportsField(p, "model") && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">具体模型</span>
          {p.kind === "plugin" ? <select className={inputClass} value={draft.model} disabled={busy || (!!modelsError && models.length === 0)} onChange={(e) => setDraft((d) => ({ ...d, model: e.target.value }))}>
            <option value="">跟随插件默认</option>
            {draft.model && !models.some((m) => m.id === draft.model) && <option value={draft.model}>{draft.model}（当前引用）</option>}
            {models.map((m) => <option key={m.id} value={m.id}>{m.name || m.id}</option>)}
          </select> : <input className={`${inputClass} font-mono`} value={draft.model} disabled={busy} placeholder={chosen?.model || p.effective?.model || "跟随默认"}
            spellCheck={false} autoComplete="off" onChange={(e) => setDraft((d) => ({ ...d, model: e.target.value }))} />}
          <span className="mt-1 block text-[11px] text-ink-3">留空恢复继承；指定模型 ID 不代表已经通过真实请求验证。</span>
          {modelsError && <span role="alert" className="mt-1 block text-[11px] text-red">插件模型列表未读取：{modelsError}</span>}
        </label>}
        {effortAllowed && <label className="block">
          <span className="mb-1 block text-[12px] font-medium text-ink-2">思考强度</span>
          <input className={inputClass} value={draft.effort} disabled={busy} placeholder={p.effective?.effort || "跟随默认"} list="model-efforts"
            onChange={(e) => setDraft((d) => ({ ...d, effort: e.target.value }))} />
          <datalist id="model-efforts">{["low", "medium", "high", "xhigh", "max"].map((v) => <option key={v} value={v} />)}</datalist>
        </label>}
        <p className="text-[11.5px] leading-relaxed text-ink-3">保存只修改配置，不会发起模型请求或打断正在运行的任务。{p.kind === "plugin" && "语音模型是 Jarvis 的默认选择，不改变插件自己的供应商或凭据。"}</p>
      </div>}
      {error && <p role="alert" className="mt-3 text-[12px] text-red">{error}</p>}
      {(conflict || (!p && error)) && <Button tone="secondary" disabled={busy} onClick={() => void load()}>{conflict ? "重新加载（丢弃编辑）" : "重试"}</Button>}
    </Dialog>
  );
}
