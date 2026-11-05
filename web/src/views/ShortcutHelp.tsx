// ⌘/: every shortcut with the chord it has now (the user's own, if rebound), grouped by where it works.
import { closeHelp, openShortcutSettings, useHelpOpen } from "../lib/shortcut-help";
import { chordParts, currentPlatform, LOCAL_SHORTCUTS, SCOPE_LABEL, SHORTCUTS, useBindings, useShortcutPrefs, type ShortcutScope } from "../lib/shortcuts";
import { navigate } from "../lib/router";
import { Button, Dialog, Kbd } from "../ui/atoms";
import { t } from "../i18n";

const SCOPES: ShortcutScope[] = ["global", "session", "sidebar"];

export function ShortcutHelp() {
  const open = useHelpOpen();
  const bindings = useBindings();
  const prefs = useShortcutPrefs();
  const platform = currentPlatform();
  return (
    <Dialog
      open={open}
      onClose={closeHelp}
      title={t("shortcuthelp.title")}
      width={460}
      footer={
        <Button
          tone="quiet"
          onClick={() => {
            closeHelp();
            openShortcutSettings(navigate);
          }}
        >
          {t("shortcuthelp.edit_in_settings")}
        </Button>
      }
    >
      <div className="flex flex-col gap-4 pt-1">
        {SCOPES.map((scope) => (
          <section key={scope} aria-label={SCOPE_LABEL[scope]}>
            <h3 className="mb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">{SCOPE_LABEL[scope]}</h3>
            <ul className="divide-y divide-line">
              {SHORTCUTS.filter((s) => s.scope === scope).map((s) => (
                <li key={s.id} className="flex items-center gap-3 py-1.5">
                  <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{s.label}</span>
                  <span className="flex shrink-0 items-center gap-1">
                    {bindings[s.id] ? (
                      chordParts(bindings[s.id]!).map((k, i) => <Kbd key={i}>{k}</Kbd>)
                    ) : (
                      // cleared, turned off, or every shortcut off
                      <span className="text-[12px] text-ink-3">{t(prefs.off || prefs.disabled.includes(s.id) ? "shortcuthelp.off" : "shortcuthelp.unset")}</span>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <section aria-label={t("shortcuthelp.in_terminal")}>
          <h3 className="mb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">{t("shortcuthelp.in_terminal_fixed")}</h3>
          <ul className="divide-y divide-line">
            {LOCAL_SHORTCUTS.map((s) => (
              <li key={s.id} className="py-1.5">
                <div className="flex items-center gap-3">
                  <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{s.label}</span>
                  <span className="flex shrink-0 items-center gap-1">
                    {chordParts(s.chord[platform], platform).map((k, i) => (
                      <Kbd key={i}>{k}</Kbd>
                    ))}
                  </span>
                </div>
                {s.note && platform === "other" && <p className="mt-0.5 text-[11.5px] text-ink-3">{s.note}</p>}
              </li>
            ))}
          </ul>
        </section>
      </div>
    </Dialog>
  );
}
