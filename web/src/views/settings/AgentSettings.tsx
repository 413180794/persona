// 设置 › Claude Code / Codex (iOS ClaudeSettingsView): the node's whitelist of each agent's own settings file on one
// computer, picked at the top (online ones; the choice is remembered). A change is sent at once (PUT {key, value},
// null = back to the default) and rolled back if that computer refuses; 「同时改所有在线机器」 sends it to every online
// computer, and a row's ⋯ pushes its value to all of them. The footer says which file changes and when it applies.
import { useEffect, useMemo, useRef, useState } from "react";
import {
  AGENT_TEXT,
  applySummary,
  effective,
  isUnset,
  pickMachine,
  textValue,
  valueText,
  withValue,
  type AgentKind,
  type AgentSetting,
  type ApplyResult,
  type SettingValue,
} from "../../lib/agent-settings";
import { api, enc, errorText } from "../../lib/api";
import { machineWithHub } from "../../lib/mesh";
import { useStore } from "../../lib/store";
import { Button, cx, EASE, IconButton, Loading, Menu, Spinner, toast, useMenu } from "../../ui/atoms";
import { ClaudeDirs } from "./ClaudeDirs";
import { Group, Switch } from "./SettingsParts";

const MACHINE_KEY = "jarvis.agentSettings.machine";
const ALL_KEY = "jarvis.agentSettings.applyAll";

const controlClass = "h-8 rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

export function AgentSettings({ agent }: { agent: AgentKind }) {
  const text = AGENT_TEXT[agent];
  const machines = useStore((s) => s.machines);
  const online = useMemo(() => machines.filter((m) => m.online), [machines]);
  const [mid, setMid] = useState(() => localStorage.getItem(MACHINE_KEY) ?? "");
  const [applyAll, setApplyAll] = useState(() => localStorage.getItem(ALL_KEY) !== "0"); // on by default, as on iOS
  const [settings, setSettings] = useState<AgentSetting[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const current = useRef(mid);

  // The remembered computer while it is online, else the first online one.
  const shown = pickMachine(machines, mid);
  current.current = shown;
  const name = (id: string) => machineWithHub(id, machines);

  useEffect(() => {
    if (!shown) return;
    let dead = false;
    setSettings(null);
    setError("");
    api<{ settings: AgentSetting[] }>(`/machines/${enc(shown)}/${text.path}`)
      .then((r) => !dead && setSettings(r.settings ?? []))
      .catch((e) => !dead && setError(errorText(e)));
    return () => {
      dead = true;
    };
  }, [shown, text.path, reload]);

  const choose = (id: string) => {
    setMid(id);
    localStorage.setItem(MACHINE_KEY, id);
  };
  const toggleAll = (on: boolean) => {
    setApplyAll(on);
    localStorage.setItem(ALL_KEY, on ? "1" : "0");
  };

  const put = (id: string, key: string, value: SettingValue) =>
    api<{ settings?: AgentSetting[] }>(`/machines/${enc(id)}/${text.path}`, { method: "PUT", json: { key, value } });

  // Sends one value to the shown computer (and, with 同时改, every other online one); rolls the row back if the
  // shown computer refuses, and says what happened.
  const change = async (s: AgentSetting, value: SettingValue) => {
    const before = s.value ?? null;
    const target = shown;
    setSettings((cur) => cur && withValue(cur, s.key, value));
    setBusy(s.key);
    const ids = applyAll ? [target, ...online.map((m) => m.id).filter((id) => id !== target)] : [target];
    const results: ApplyResult[] = await Promise.all(
      ids.map(async (id) => {
        try {
          const r = await put(id, s.key, value);
          if (id === target && current.current === target && r.settings) setSettings(r.settings);
          return { id, name: name(id) };
        } catch (e) {
          if (id === target && current.current === target) setSettings((cur) => cur && withValue(cur, s.key, before));
          return { id, name: name(id), error: errorText(e) };
        }
      }),
    );
    setBusy(null);
    const sum = applySummary(results);
    toast(sum.ok ? `${s.title}：${valueText(value)} · ${sum.text}` : `${s.title}：${sum.text}`, sum.ok ? "ok" : "error");
  };

  // ⋯ › 应用到所有在线机器: this computer's value (unset = the default) on every other online one.
  const pushToAll = async (s: AgentSetting) => {
    const others = online.filter((m) => m.id !== shown);
    if (!others.length) return toast("没有别的在线机器", "info");
    setBusy(s.key);
    const results: ApplyResult[] = await Promise.all(
      others.map((m) =>
        put(m.id, s.key, s.value ?? null).then(
          () => ({ id: m.id, name: name(m.id) }),
          (e) => ({ id: m.id, name: name(m.id), error: errorText(e) }),
        ),
      ),
    );
    setBusy(null);
    const sum = applySummary(results);
    toast(sum.ok ? `${s.title}：${valueText(s.value ?? null)} · 已应用到另外 ${results.length} 台机器` : `${s.title}：${sum.text}`, sum.ok ? "ok" : "error");
  };

  if (!online.length) {
    return (
      <Group title="机器" footer={text.footer}>
        <p className="px-4 py-3 text-[12.5px] text-ink-3">没有在线的机器：这些设置存在各台电脑上，连上后再改。</p>
      </Group>
    );
  }

  return (
    <>
      <Group title="机器" footer={text.footer}>
        <div className="flex items-center gap-3 px-4 py-2.5">
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">改哪台电脑</span>
          <select value={shown} onChange={(e) => choose(e.target.value)} className={cx(controlClass, "max-w-[60%] min-w-[140px]")} aria-label="改哪台电脑">
            {online.map((m) => (
              <option key={m.id} value={m.id}>
                {name(m.id)}
              </option>
            ))}
          </select>
        </div>
        <div className="flex items-center gap-3 px-4 py-2.5">
          <div className="min-w-0 flex-1">
            <div className="truncate text-[13px] text-ink">同时改所有在线机器</div>
            <div className="mt-0.5 text-[11.5px] text-ink-3">{applyAll ? `改一项会写到这 ${online.length} 台在线的电脑` : "只改上面选的这一台"}</div>
          </div>
          <Switch on={applyAll} onChange={toggleAll} label="同时改所有在线机器" />
        </div>
      </Group>

      <Group title={`${text.name} 设置 · ${name(shown)}`}>
        {error ? (
          <div className="flex items-center gap-3 px-4 py-3">
            <p className="min-w-0 flex-1 text-[12.5px] text-red">读不到这台电脑的设置：{error}</p>
            <Button tone="secondary" icon="retry" onClick={() => setReload((n) => n + 1)}>
              重试
            </Button>
          </div>
        ) : !settings ? (
          <div className="px-4 py-3">
            <Loading />
          </div>
        ) : settings.length === 0 ? (
          <p className="px-4 py-3 text-[12.5px] text-ink-3">这台电脑上没有能改的设置。</p>
        ) : (
          settings.map((s, i) => (
            <SettingRow key={`${shown}/${s.key}`} s={s} i={i} busy={busy === s.key} others={online.length - 1} onChange={(v) => void change(s, v)} onPushAll={() => void pushToAll(s)} />
          ))
        )}
      </Group>
      {agent === "claude" && <ClaudeDirs key={shown} mid={shown} name={name(shown)} />}
    </>
  );
}

function SettingRow({ s, i, busy, others, onChange, onPushAll }: { s: AgentSetting; i: number; busy: boolean; others: number; onChange: (v: SettingValue) => void; onPushAll: () => void }) {
  const menu = useMenu();
  const v = effective(s);
  const unset = isUnset(s);
  return (
    <div className="flex items-center gap-3 px-4 py-2.5" style={{ animation: `fade-up 320ms ${EASE} ${Math.min(i, 8) * 30}ms both` }}>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-2">
          <span className="truncate text-[13px] text-ink">{s.title}</span>
          {unset && <span className="shrink-0 text-[11px] text-ink-3">默认</span>}
          <span className="hidden truncate font-mono text-[10.5px] text-ink-3 sm:inline">{s.key}</span>
        </div>
        {s.detail && <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{s.detail}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {busy && <Spinner />}
        {s.type === "bool" ? (
          <Switch on={v === true} onChange={(on) => onChange(on)} disabled={busy} label={s.title} />
        ) : s.type === "enum" ? (
          <select value={typeof v === "string" ? v : ""} disabled={busy} onChange={(e) => onChange(e.target.value)} className={cx(controlClass, "max-w-[180px]")} aria-label={s.title}>
            {typeof v !== "string" && <option value="">（未设置）</option>}
            {(s.options ?? []).map((o) => (
              <option key={o} value={o}>
                {o === s.default ? `${o}（默认）` : o}
              </option>
            ))}
          </select>
        ) : (
          <TextSetting s={s} disabled={busy} onSave={onChange} />
        )}
        <IconButton icon="more" label={`${s.title}：更多`} onClick={menu.open} active={!!menu.anchor} />
        {menu.anchor && (
          <Menu
            anchor={menu.anchor}
            onClose={menu.close}
            align="right"
            width={220}
            items={[
              { key: "reset", label: "恢复默认", hint: s.default !== undefined && s.default !== "" ? valueText(s.default ?? null) : undefined, icon: "retry", disabled: unset || busy, onSelect: () => onChange(null) },
              { key: "all", label: "应用到所有在线机器", hint: others > 0 ? `另外 ${others} 台` : "没有别的", icon: "machine", disabled: others < 1 || busy, onSelect: onPushAll },
            ]}
          />
        )}
      </div>
    </div>
  );
}

// A text option: saved on Enter or when the field loses focus with a change; Esc puts the saved value back.
function TextSetting({ s, disabled, onSave }: { s: AgentSetting; disabled: boolean; onSave: (v: string | null) => void }) {
  const saved = typeof s.value === "string" ? s.value : "";
  const [text, setText] = useState(saved);
  useEffect(() => setText(saved), [saved]);
  const commit = () => {
    if (text.trim() !== saved) onSave(textValue(text));
  };
  return (
    <input
      value={text}
      disabled={disabled}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
        else if (e.key === "Escape") setText(saved);
      }}
      placeholder={typeof s.default === "string" && s.default ? s.default : "未设置"}
      spellCheck={false}
      autoComplete="off"
      aria-label={s.title}
      className={cx(controlClass, "w-[180px] placeholder:text-ink-3")}
    />
  );
}
