// 设置 › 会话分诊 (docs/agent-work.md「开关」): when an agent stops, the hub can read its last reply and say whether it is
// done or what it waits for, and act on it. All off until turned on here: the triage calls a model on every stopped
// turn, and 自动继续 / 到点叫醒 run a turn of the agent on its own model. Saved on the hub (PUT /agents/triage), so it is
// the same on every device.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { api, errorText } from "../../lib/api";
import type { TriageSwitches } from "../../lib/types";
import { toast } from "../../ui/atoms";
import { Group, Row, Switch } from "./SettingsParts";

const OFF: TriageSwitches = { enabled: false, auto_continue: false, wake: false, decide: false };

export function TriageSettings({ onModel }: { onModel?: () => void }) {
  const [sw, setSw] = useState<TriageSwitches | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let dead = false;
    api<TriageSwitches>("/agents/triage")
      .then((r) => !dead && setSw({ ...OFF, ...r }))
      .catch(() => !dead && setSw(OFF));
    return () => {
      dead = true;
    };
  }, []);

  const change = async (next: TriageSwitches) => {
    const before = sw;
    setSw(next);
    setBusy(true);
    try {
      setSw(await api<TriageSwitches>("/agents/triage", { method: "PUT", json: next }));
    } catch (e) {
      setSw(before);
      toast(t("settings-triagesettings.save_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };
  const flip = (k: keyof TriageSwitches) => (on: boolean) => sw && change({ ...sw, [k]: on });
  const off = !sw?.enabled;

  return (
    <Group
      title={t("settings-triagesettings.title")}
      footer={
        <>
          {tRich("settings-triagesettings.footer", {
            model: onModel ? (
              <button type="button" onClick={onModel} className="mx-0.5 text-accent hover:underline">
                {t("settings-triagesettings.model_path")}
              </button>
            ) : (
              t("settings-triagesettings.model_path_quoted")
            ),
          })}
        </>
      }
    >
      <Row label={t("settings-triagesettings.title")} hint={t("settings-triagesettings.enabled_hint")}>
        <Switch on={Boolean(sw?.enabled)} onChange={flip("enabled")} disabled={!sw || busy} label={t("settings-triagesettings.title")} />
      </Row>
      <Row label={t("settings-triagesettings.decide")} hint={t("settings-triagesettings.decide_hint")}>
        <Switch on={Boolean(sw?.decide)} onChange={flip("decide")} disabled={!sw || busy || off} label={t("settings-triagesettings.decide")} />
      </Row>
      <Row label={t("settings-triagesettings.auto_continue")} hint={t("settings-triagesettings.auto_continue_hint")}>
        <Switch on={Boolean(sw?.auto_continue)} onChange={flip("auto_continue")} disabled={!sw || busy || off} label={t("settings-triagesettings.auto_continue")} />
      </Row>
      <Row label={t("settings-triagesettings.wake")} hint={t("settings-triagesettings.wake_hint")}>
        <Switch on={Boolean(sw?.wake)} onChange={flip("wake")} disabled={!sw || busy || off} label={t("settings-triagesettings.wake")} />
      </Row>
    </Group>
  );
}
