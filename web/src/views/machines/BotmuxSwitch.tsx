// "Also show botmux sessions" in a machine page's header, beside its engine (#2575, #2842, iOS BotmuxSwitchModel): whether
// this computer's botmux sessions are listed beside its engine's, one click to flip. GET / PUT
// /api/v1/machines/{mid}/botmux {"on"}; the hub keeps it per machine (hub.json's machines[].botmux is the default, on
// when unset) and tells the node at once.
import { useEffect, useId, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { cx, toast } from "../../ui/atoms";
import { t } from "../../i18n";

export function BotmuxSwitch({ machineID }: { machineID: string }) {
  const [on, setOn] = useState<boolean | null>(null); // null until the hub answered
  const hintID = useId();
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

  // A pill like the engine's beside it: a dot and "botmux · on / off"; the footer says what it does on hover and to a
  // screen reader. The hover fill is the off state's only, so hovering the on state does not make it look off.
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={t("machines-botmuxswitch.label")}
      aria-describedby={hintID}
      title={t("machines-botmuxswitch.footer")}
      data-testid="botmux-switch"
      disabled={saving}
      onClick={() => void change(!on)}
      className={cx(
        "touch-hit inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full px-1.5 text-[11px] font-medium shadow-hairline transition-colors disabled:opacity-60",
        on ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-2 enabled:hover:bg-hover",
      )}
    >
      <span className={cx("size-1.5 shrink-0 rounded-full", on ? "bg-accent" : "bg-ink-3")} />
      botmux
      <span>· {on ? t("machines-botmuxswitch.on") : t("machines-botmuxswitch.off")}</span>
      <span id={hintID} className="sr-only">{t("machines-botmuxswitch.footer")}</span>
    </button>
  );
}
