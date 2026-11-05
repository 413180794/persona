// 同时显示 botmux 会话 under 引擎 on a machine's page (#2575, iOS BotmuxSwitchModel): whether this computer's botmux
// sessions are listed beside its engine's. GET / PUT /api/v1/machines/{mid}/botmux {"on"}; the hub keeps it per
// machine (hub.json's machines[].botmux is the default, on when unset) and tells the node at once.
import { useEffect, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { toast } from "../../ui/atoms";
import { t } from "../../i18n";
import { Switch } from "../settings/SettingsParts";

export function BotmuxSwitch({ machineID }: { machineID: string }) {
  const [on, setOn] = useState<boolean | null>(null); // null until the hub answered
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let live = true;
    setOn(null);
    api<{ on: boolean }>(`/machines/${enc(machineID)}/botmux`)
      .then((r) => live && setOn(r.on))
      .catch(() => live && setOn(null)); // an older hub has no such route: no switch
    return () => {
      live = false;
    };
  }, [machineID]);

  if (on === null) return null;

  const change = async (next: boolean) => {
    if (saving) return;
    setSaving(true);
    setOn(next);
    try {
      const r = await api<{ on: boolean }>(`/machines/${enc(machineID)}/botmux`, { method: "PUT", json: { on: next } });
      setOn(r.on);
    } catch (e) {
      setOn(!next);
      toast(t("machines-botmuxswitch.save_failed", { error: errorText(e) }), "error");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="mt-3" data-testid="botmux-switch">
      <div className="flex items-center gap-3 rounded-card bg-surface px-4 py-2.5 shadow-card">
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink">{t("machines-botmuxswitch.label")}</span>
        <Switch on={on} disabled={saving} onChange={(v) => void change(v)} label={t("machines-botmuxswitch.label")} />
      </div>
      <p className="mt-2 px-1 text-[11.5px] leading-relaxed text-ink-3">{t("machines-botmuxswitch.footer")}</p>
    </div>
  );
}
