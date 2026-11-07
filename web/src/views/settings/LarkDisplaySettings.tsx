// Settings › Connectors › Lark › Message display (iOS LarkMessageDisplaySettingsView): the Lark gateway's display switches of the hub's Feishu
// account, shared with the Lark gateway's own web page. A switch writes only itself, with the account and the old value;
// a conflict or an unknown result is not retried, "Reload" reads them again (lark-display.ts).
import { larkDisplayModel, useLarkDisplayState, type LarkDisplayKey } from "../../lib/lark-display";
import { t } from "../../i18n";
import { Button, Spinner } from "../../ui/atoms";
import { Switch } from "./SettingsParts";

export function LarkDisplaySettings() {
  const s = useLarkDisplayState();
  const model = larkDisplayModel();
  const busy = s.loading || s.saving;
  const row = (key: LarkDisplayKey, value: boolean, label: string, hint: string) => (
    <div key={key} className="flex items-center gap-3 px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="text-[13px] text-ink">{label}</div>
        <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{hint}</div>
      </div>
      <Switch on={value} disabled={!s.snapshot || busy} onChange={(on) => void model.change(key, on)} label={label} />
    </div>
  );
  return (
    <>
      <div className="flex items-center gap-2 px-4 pt-2.5 pb-1 text-[11.5px] font-medium text-ink-3">
        <span>{t("settings-larkdisplay.title")}</span>
        {busy && <Spinner size={10} />}
      </div>
      {row("anti_recall", s.snapshot?.anti_recall ?? false, t("settings-larkdisplay.anti_recall"), t("settings-larkdisplay.anti_recall_hint"))}
      {row("show_edit_history", s.snapshot?.show_edit_history ?? false, t("settings-larkdisplay.edit_history"), t("settings-larkdisplay.edit_history_hint"))}
      {s.problem && (
        <div className="flex items-center gap-3 px-4 py-2">
          <p className="min-w-0 flex-1 text-[12px] leading-snug text-ink-2">{s.problem}</p>
          <Button tone="secondary" icon="retry" disabled={busy} onClick={() => void model.refresh()}>{t("settings-larkdisplay.reload")}</Button>
        </div>
      )}
      <p className="px-4 pt-1 pb-2.5 text-[11.5px] leading-snug text-ink-3">{t("settings-larkdisplay.shared")}</p>
    </>
  );
}
