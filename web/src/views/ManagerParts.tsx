// What the pages show of the managers once there is more than one (docs/assistant/managers.md "Clients"): the assistant page's
// switcher, the headings the machines, extensions and inbox lists group under, and what an offline manager's assistant
// page says. With one manager none of it renders, so those pages stay as they were.
import { useRef, useState } from "react";
import { t } from "../i18n";
import { lastSyncedText, managerOrder, multiManager, useRoster, type Manager } from "../lib/managers";
import { chooseManager, useShownManager } from "../lib/manager-choice";
import { ago } from "../lib/format";
import { refreshReadiness } from "../lib/readiness";
import { navigate } from "../lib/router";
import { api, enc, errorText } from "../lib/api";
import { Button, cx, Menu, toast, useMenu } from "../ui/atoms";
import { SegmentedTrack } from "../ui/Segmented";

/** The status dot of a manager: green online, grey offline. */
function Dot({ online }: { online: boolean }) {
  return <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", online ? "bg-green" : "bg-line-strong")} />;
}

/** The assistant page's switcher: one segment per installed manager, the default first; offline ones greyed with when
 *  they last synced. Nothing with one manager. */
export function ManagerSwitcher({ className }: { className?: string }) {
  const roster = useRoster();
  const shown = useShownManager();
  if (!multiManager(roster)) return null;
  const pick = (m: Manager) => {
    if (m.id === shown) return;
    chooseManager(m.id);
    // the conversation open now is the other manager's: back to the main one of this, and its readiness
    navigate({ name: "persona" });
    void refreshReadiness({ force: true });
  };
  return (
    <div className={cx("flex shrink-0 overflow-x-auto px-3 py-2 sm:px-4", className)}>
      <SegmentedTrack role="radiogroup" label={t("settings-managers.switcher")} className="flex gap-0.5 rounded-control p-0.5" thumbClassName="rounded-[7px]">
        {managerOrder(roster).map((m) => {
          const on = m.id === shown;
          const synced = lastSyncedText(m);
          return (
            <button
              key={m.id}
              type="button"
              role="radio"
              aria-checked={on}
              data-selected={on}
              title={synced || undefined}
              onClick={() => pick(m)}
              className={cx(
                "relative z-[1] flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[7px] px-3 text-[12.5px] transition-colors duration-150",
                on && "font-medium",
                m.online ? (on ? "text-ink" : "text-ink-2 hover:text-ink") : "text-ink-3 hover:text-ink-2",
              )}
            >
              <Dot online={m.online} />
              <span className="max-w-[10em] truncate">{m.name}</span>
            </button>
          );
        })}
      </SegmentedTrack>
    </div>
  );
}

/** The heading of one manager's group in a list: its name, and when an offline one last synced. */
export function ManagerHeading({ manager, count, className }: { manager: Manager; count?: number; className?: string }) {
  const synced = lastSyncedText(manager);
  return (
    <div className={cx("flex min-w-0 items-center gap-2 whitespace-nowrap", className)}>
      <Dot online={manager.online} />
      <span className={cx("truncate text-[12.5px] font-semibold", manager.online ? "text-ink-2" : "text-ink-3")}>{manager.name}</span>
      {count !== undefined && <span className="text-[11.5px] text-ink-3 tabular-nums">{count}</span>}
      {synced && <span className="ml-auto truncate text-[11.5px] text-ink-3">{synced}</span>}
    </div>
  );
}

/** What a composer says for a conversation of manager id while it is offline (several managers only): "<name> · last
 *  synced …"; undefined when it is online, unknown, or the hub has one manager. Such a conversation can't be sent to,
 *  wherever it was opened from (the inbox, a notification, the Agents list). */
export function useManagerOffline(id: string | undefined): string | undefined {
  const roster = useRoster();
  if (!id || !multiManager(roster)) return undefined;
  const m = roster.managers.find((x) => x.id === id);
  if (!m || m.online || m.installing) return undefined;
  const when = ago(m.last_seen);
  return when ? t("settings-managers.offline_composer", { name: m.name, time: when }) : t("settings-managers.offline_composer_never", { name: m.name });
}

/** The shown manager when it is offline (only with more than one): the assistant page then can't send. */
export function useShownOffline(): { manager: Manager; composer: string; body: string } | null {
  const roster = useRoster();
  const shown = useShownManager();
  if (!multiManager(roster)) return null;
  const m = roster.managers.find((x) => x.id === shown);
  if (!m || m.online) return null;
  const when = ago(m.last_seen);
  return {
    manager: m,
    composer: when ? t("settings-managers.offline_composer", { name: m.name, time: when }) : t("settings-managers.offline_composer_never", { name: m.name }),
    body: t("settings-managers.offline_body", { name: m.name }),
  };
}

/** Move to… on a computer's page: gives it to another manager (`POST /managers/{id}/machines`, docs/assistant/managers.md "Moving a
 *  machine or a source"); its sessions and cards follow, the machines push shows where it went. Only with more than
 *  one manager, and not for a linked hub's computer (not movable). */
export function MoveToManager({ machineId, manager, movable = true }: { machineId: string; manager?: string; movable?: boolean }) {
  const roster = useRoster();
  const menu = useMenu();
  const [busy, setBusy] = useState(false);
  const at = useRef<HTMLSpanElement>(null);
  if (!multiManager(roster) || !movable) return null;
  const owner = manager || roster.default;
  const move = async (to: Manager) => {
    setBusy(true);
    try {
      await api(`/managers/${enc(to.id)}/machines`, { method: "POST", json: { machine_id: machineId } });
      toast(t("settings-managers.moved", { name: to.name }), "ok");
    } catch (e) {
      toast(t("settings-managers.move_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <span ref={at} className="contents">
        <Button tone="secondary" icon="arrowRight" busy={busy} onClick={() => (menu.anchor ? menu.close() : menu.openAt(at.current?.firstElementChild as HTMLElement | null))}>
          {t("settings-managers.move_to")}
        </Button>
      </span>
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          align="right"
          header={t("settings-managers.move_title")}
          items={managerOrder(roster).map((m) => ({
            key: m.id,
            label: m.name,
            checked: m.id === owner,
            hint: m.online ? undefined : t("settings-managers.move_offline"),
            disabled: m.id === owner,
            onSelect: () => void move(m),
          }))}
        />
      )}
    </>
  );
}
