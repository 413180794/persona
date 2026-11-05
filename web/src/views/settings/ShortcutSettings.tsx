// 设置 › 快捷键: every shortcut with its chord; click one and press the new chord (Esc cancels), 恢复默认 per row and for
// all. Stored in this browser (lib/shortcuts.ts). docs/web.md「快捷键」.
import { useEffect, useState } from "react";
import { checkBinding, chordParts, currentPlatform, eventChord, getOverrides, holdShortcuts, LOCAL_SHORTCUTS, modName, resetAllBindings, resetBinding, SCOPE_LABEL, SHORTCUTS, setBinding, useBindings, type ShortcutDef, type ShortcutScope } from "../../lib/shortcuts";
import { t } from "../../i18n";
import { Button, cx, IconButton, Kbd } from "../../ui/atoms";
import { Group, Row } from "./SettingsParts";

const SCOPES: ShortcutScope[] = ["global", "session", "sidebar"];

export function ShortcutSettings() {
  const bindings = useBindings();
  // the command whose new chord is awaited, and what was wrong with the last one pressed for it
  const [recording, setRecording] = useState<string | null>(null);
  const [problem, setProblem] = useState<{ id: string; text: string } | null>(null);
  const platform = currentPlatform();
  const overridden = Object.keys(getOverrides());

  // the app's own shortcuts stand aside while a chord is awaited
  useEffect(() => (recording ? holdShortcuts() : undefined), [recording]);

  const stop = () => {
    setRecording(null);
    setProblem(null);
  };

  const onKey = (e: React.KeyboardEvent, def: ShortcutDef) => {
    if (e.nativeEvent.isComposing) return;
    // Tab on its own moves on (and the blur ends the recording): a keyboard user is not trapped here
    if (e.key === "Tab" && !e.ctrlKey && !e.metaKey && !e.altKey) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape") return stop();
    const pressed = eventChord(e.nativeEvent, platform);
    // a modifier on its own: still waiting for the key
    if (!pressed && ["Control", "Meta", "Alt", "Shift", "AltGraph"].includes(e.key)) return;
    const check = checkBinding(def.id, pressed, bindings, platform);
    if (!check.ok) return setProblem({ id: def.id, text: check.message });
    setBinding(def.id, check.chord);
    stop();
  };

  return (
    <>
      {SCOPES.map((scope, n) => (
        <Group
          key={scope}
          title={n === 0 ? t("settings-shortcutsettings.title_first", { scope: SCOPE_LABEL[scope] }) : SCOPE_LABEL[scope]}
          right={n === 0 ? <Button tone="quiet" disabled={!overridden.length} onClick={() => (stop(), resetAllBindings())}>{t("settings-shortcutsettings.reset_all")}</Button> : undefined}
          footer={
            n === SCOPES.length - 1
              ? t(platform === "mac" ? "settings-shortcutsettings.footer_mac" : "settings-shortcutsettings.footer_other", {
                  mod: modName(platform),
                  help: chordParts(bindings.help ?? "", platform).join(platform === "mac" ? "" : "+"),
                })
              : undefined
          }
        >
          {SHORTCUTS.filter((s) => s.scope === scope).map((s) => {
            const waiting = recording === s.id;
            const changed = overridden.includes(s.id);
            const error = problem?.id === s.id ? problem.text : "";
            return (
              <Row key={s.id} label={s.label} hint={error ? <span className="text-red">{error}</span> : undefined}>
                {changed && !waiting && <IconButton icon="undo" label={t("settings-shortcutsettings.reset_shortcut", { name: s.label })} size={26} iconSize={13} onClick={() => resetBinding(s.id)} />}
                <button
                  type="button"
                  aria-label={waiting ? t("settings-shortcutsettings.recording_label", { name: s.label }) : t("settings-shortcutsettings.chord_label", { name: s.label, chord: chordParts(bindings[s.id] ?? s.chord, platform).join(" ") })}
                  onClick={() => {
                    setProblem(null);
                    setRecording(s.id);
                  }}
                  onKeyDown={waiting ? (e) => onKey(e, s) : undefined}
                  onBlur={() => waiting && stop()}
                  autoFocus={waiting}
                  ref={(el) => {
                    if (waiting) el?.focus();
                  }}
                  className={cx(
                    "touch-hit flex min-h-7 items-center gap-1 whitespace-nowrap rounded-control px-1.5 transition-[background-color,box-shadow] duration-150",
                    waiting ? "bg-accent-tint text-[12px] text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "hover:bg-hover",
                  )}
                >
                  {waiting ? t("settings-shortcutsettings.recording") : chordParts(bindings[s.id] ?? s.chord, platform).map((k, i) => <Kbd key={i}>{k}</Kbd>)}
                </button>
              </Row>
            );
          })}
        </Group>
      ))}
      <Group title={t("settings-shortcutsettings.terminal_title")} footer={t("settings-shortcutsettings.terminal_footer")}>
        {LOCAL_SHORTCUTS.map((s) => (
          <Row key={s.id} label={s.label} hint={platform === "other" ? s.note : undefined}>
            <span className="flex items-center gap-1 px-1.5">
              {chordParts(s.chord[platform], platform).map((k, i) => (
                <Kbd key={i}>{k}</Kbd>
              ))}
            </span>
          </Row>
        ))}
      </Group>
    </>
  );
}
