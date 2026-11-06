// Settings › Model providers › Import from ~/.zshrc (#2455): pick a computer, preview the providers its claude aliases add up to
// (GET /api/v1/launchers/import), tick some, import them (POST). The keys never come to the browser: the hub reads them
// again from that computer and keeps them sealed.
import { useEffect, useState } from "react";
import { api } from "../../lib/api";
import { importErrorText, importRow, importSummary, type ImportedProvider } from "../../lib/providers";
import { useStore } from "../../lib/store";
import { Button, Dialog, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Pill } from "./SettingsParts";
import { CheckMark, INPUT, Labelled } from "./ProviderFields";
import { t } from "../../i18n";

export function ProviderImport({ open, onClose, onImported }: { open: boolean; onClose: () => void; onImported: () => void }) {
  const machines = useStore((s) => s.machines);
  const [machine, setMachine] = useState("");
  const [list, setList] = useState<ImportedProvider[] | null>(null);
  const [error, setError] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setMachine((m) => m || machines.find((x) => x.online)?.id || machines[0]?.id || "");
  }, [open, machines]);

  useEffect(() => {
    if (!open || !machine) return;
    let live = true;
    setList(null);
    setError("");
    api<{ providers?: ImportedProvider[] }>(`/launchers/import?machine=${encodeURIComponent(machine)}`)
      .then((r) => {
        if (!live) return;
        const ps = r.providers ?? [];
        setList(ps);
        setPicked(new Set(ps.map((p) => p.name)));
      })
      .catch((e) => live && (setError(importErrorText(e)), setList([])));
    return () => void (live = false);
  }, [open, machine]);

  const toggle = (name: string) =>
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(name)) n.delete(name);
      else n.add(name);
      return n;
    });

  const confirm = async () => {
    if (busy || picked.size === 0) return;
    setBusy(true);
    setError("");
    try {
      const r = await api<{ created?: string[]; updated?: string[] }>("/launchers/import", { method: "POST", json: { machine, providers: [...picked] } });
      toast(importSummary(r), "ok");
      onImported();
      onClose();
    } catch (e) {
      setError(importErrorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      width={560}
      title={t("settings-providerimport.title")}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <span className="flex-1" />
          <Button tone="primary" busy={busy} disabled={!list?.length || picked.size === 0} onClick={() => void confirm()}>
            {picked.size ? t("settings-providerimport.import_count", { count: picked.size }) : t("settings-providerimport.import")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Labelled label={t("settings-providerimport.computer")} hint={t("settings-providerimport.computer_hint")}>
          <select value={machine} onChange={(e) => setMachine(e.target.value)} aria-label={t("settings-providerimport.computer")} className={INPUT}>
            {machines.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
                {m.online ? "" : t("settings-providerimport.offline_suffix")}
              </option>
            ))}
          </select>
        </Labelled>
        {!list && !error && <Loading />}
        {error && (
          <p role="alert" className="flex items-start gap-1.5 text-[12.5px] text-red">
            <Icon name="warning" size={13} stroke={2.2} className="mt-0.5 shrink-0" />
            {error}
          </p>
        )}
        {list && !error && list.length === 0 && <p className="py-6 text-center text-[12.5px] text-ink-3">{t("settings-providerimport.empty")}</p>}
        {list && list.length > 0 && (
          <ul aria-label={t("settings-providerimport.list_label")} className="overflow-hidden rounded-card bg-surface shadow-btn">
            {list.map((p) => {
              const r = importRow(p);
              const on = picked.has(p.name);
              return (
                <li key={p.name} className="border-b border-line last:border-b-0">
                  <button type="button" role="checkbox" aria-checked={on} onClick={() => toggle(p.name)} data-import={p.name}
                    className="touch-hit flex w-full items-start gap-3 px-3 py-2.5 text-left transition-colors hover:bg-hover">
                    <CheckMark on={on} className="mt-0.5" />
                    <span className="min-w-0 flex-1">
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-mono text-[13px] font-medium text-ink">{r.name}</span>
                        <Pill tone={r.merge ? undefined : "accent"}>{r.action}</Pill>
                      </span>
                      <span className="mt-0.5 flex min-w-0 flex-wrap gap-x-2 text-[11.5px] text-ink-3">
                        <span className="truncate">{r.host}</span>
                        <span className="tabular-nums">{r.models}</span>
                        {r.aliases && <span className="truncate">{t("settings-providerimport.from_aliases", { aliases: r.aliases })}</span>}
                      </span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
        {list && list.some((p) => p.existing) && <p className="text-[11.5px] text-ink-3">{t("settings-providerimport.merge_note")}</p>}
      </div>
    </Dialog>
  );
}
