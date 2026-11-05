// ⌘/: every shortcut with the chord it has now (the user's own, if rebound), grouped by where it works.
import { closeHelp, openShortcutSettings, useHelpOpen } from "../lib/shortcut-help";
import { chordParts, currentPlatform, LOCAL_SHORTCUTS, SCOPE_LABEL, SHORTCUTS, useBindings, type ShortcutScope } from "../lib/shortcuts";
import { navigate } from "../lib/router";
import { Button, Dialog, Kbd } from "../ui/atoms";

const SCOPES: ShortcutScope[] = ["global", "session", "sidebar"];

export function ShortcutHelp() {
  const open = useHelpOpen();
  const bindings = useBindings();
  const platform = currentPlatform();
  return (
    <Dialog
      open={open}
      onClose={closeHelp}
      title="快捷键"
      width={460}
      footer={
        <Button
          tone="quiet"
          onClick={() => {
            closeHelp();
            openShortcutSettings(navigate);
          }}
        >
          在设置里改键
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
                    {chordParts(bindings[s.id] ?? s.chord).map((k, i) => (
                      <Kbd key={i}>{k}</Kbd>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <section aria-label="终端里">
          <h3 className="mb-1 text-[10.5px] font-medium tracking-[0.06em] text-ink-3 uppercase">终端里（固定）</h3>
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
