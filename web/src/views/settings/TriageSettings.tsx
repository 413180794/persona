// 设置 › 会话分诊 (docs/agent-work.md「开关」): when an agent stops, the hub can read its last reply and say whether it is
// done or what it waits for, and act on it. All off until turned on here: the triage calls a model on every stopped
// turn, and 自动继续 / 到点叫醒 run a turn of the agent on its own model. Saved on the hub (PUT /agents/triage), so it is
// the same on every device.
// A read that fails is not "all off" (#4491): the switches are PUT as a whole set, so drawing them off and letting one be
// flipped would write off over every other switch the hub has on. Until the hub answers there are no switches, only
// 加载中 or why it could not read them and 重试. A hub with no such route (404) is older than the triage: it says so,
// without a 重试 that could not help (iOS / Android do the same).
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { api, ApiError, errorText } from "../../lib/api";
import type { TriageSwitches } from "../../lib/types";
import { Button, Loading, toast } from "../../ui/atoms";
import { Group, Row, Switch } from "./SettingsParts";

const OFF: TriageSwitches = { enabled: false, auto_continue: false, wake: false, decide: false };

export function TriageSettings({ onModel }: { onModel?: () => void }) {
  const [sw, setSw] = useState<TriageSwitches | null>(null);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let dead = false;
    setError("");
    api<TriageSwitches>("/agents/triage")
      .then((r) => !dead && setSw({ ...OFF, ...r }))
      .catch((e) => {
        if (dead) return;
        if (e instanceof ApiError && e.status === 404) setUnsupported(true);
        else setError(errorText(e));
      });
    return () => {
      dead = true;
    };
  }, [reload]);

  const change = async (next: TriageSwitches) => {
    const before = sw;
    setSw(next);
    setBusy(true);
    try {
      setSw({ ...OFF, ...(await api<TriageSwitches>("/agents/triage", { method: "PUT", json: next })) });
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
      {!sw ? (
        unsupported ? (
          <p className="px-4 py-3 text-[12.5px] text-ink-3">{t("settings-triagesettings.unsupported")}</p>
        ) : error ? (
          <div className="flex items-center gap-3 px-4 py-3">
            <p role="alert" className="min-w-0 flex-1 text-[12.5px] text-red">{t("settings-triagesettings.load_failed", { error })}</p>
            <Button tone="secondary" icon="retry" onClick={() => setReload((n) => n + 1)}>
              {t("settings-triagesettings.retry")}
            </Button>
          </div>
        ) : (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )
      ) : (
        <>
          <Row label={t("settings-triagesettings.title")} hint={t("settings-triagesettings.enabled_hint")}>
            <Switch on={sw.enabled} onChange={flip("enabled")} disabled={busy} label={t("settings-triagesettings.title")} />
          </Row>
          <Row label={t("settings-triagesettings.decide")} hint={t("settings-triagesettings.decide_hint")}>
            <Switch on={sw.decide} onChange={flip("decide")} disabled={busy || off} label={t("settings-triagesettings.decide")} />
          </Row>
          <Row label={t("settings-triagesettings.auto_continue")} hint={t("settings-triagesettings.auto_continue_hint")}>
            <Switch on={sw.auto_continue} onChange={flip("auto_continue")} disabled={busy || off} label={t("settings-triagesettings.auto_continue")} />
          </Row>
          <Row label={t("settings-triagesettings.wake")} hint={t("settings-triagesettings.wake_hint")}>
            <Switch on={sw.wake} onChange={flip("wake")} disabled={busy || off} label={t("settings-triagesettings.wake")} />
          </Row>
        </>
      )}
    </Group>
  );
}
