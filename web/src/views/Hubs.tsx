// Going between hubs (#1014, lib/hubs.ts): the list in 设置 › 通用, the other hubs in the account menu and on the sign-in
// page. Switching opens the other hub's own page; with no other hub in the list, the menu and the sign-in page look as
// before (only 设置 shows the list, to add one from).
import { useEffect, useState } from "react";
import { addHub, hostOf, hubTitle, loadHubs, normalizeHubURL, onHubsChange, removeHub, renameHub, switchURL, type SavedHub } from "../lib/hubs";
import { Button, Dialog, IconButton, toast, type MenuItem } from "../ui/atoms";
import { Icon } from "../ui/icons";
import { Group, Pill, Row } from "./settings/SettingsParts";

/** This page's hub. */
export const here = () => (typeof location === "undefined" ? "" : location.origin);

export function useHubs(): SavedHub[] {
  const [list, setList] = useState(loadHubs);
  useEffect(() => {
    const sync = () => setList(loadHubs());
    // another tab of this hub's page changed it
    const storage = (e: StorageEvent) => e.key === "persona.hubs" && sync();
    addEventListener("storage", storage);
    const off = onHubsChange(sync);
    return () => {
      off();
      removeEventListener("storage", storage);
    };
  }, []);
  return list;
}

/** Open another hub's page, handing it the list. */
export function goToHub(h: SavedHub) {
  location.href = switchURL(h.url, loadHubs());
}

/** The account menu's rows for the other hubs (none when there is no other one). */
export function hubMenuItems(list: SavedHub[]): MenuItem[] {
  return list
    .filter((h) => h.url !== here())
    .map((h, i) => ({ key: `hub:${h.url}`, label: hubTitle(h), hint: "切换", icon: "hub" as const, divider: i === 0, onSelect: () => goToHub(h) }));
}

// ── 设置 › 通用 ─────────────────────────────────────────

export function HubList() {
  const list = useHubs();
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState<SavedHub | null>(null);
  const current = here();
  // the page's own hub first, the rest as the user added them
  const rows = [...list.filter((h) => h.url === current), ...list.filter((h) => h.url !== current)];
  return (
    <>
      <Group
        title="hub"
        right={
          <Button tone="quiet" icon="plus" onClick={() => setAdding(true)} className="-mb-1 h-7 px-2">
            添加 hub
          </Button>
        }
        footer="在这几台 hub 的网页之间切换。每台 hub 用这个浏览器里各自的登录，第一次打开某台要贴它的 token。列表只存在这个浏览器里，不含 token。"
      >
        {rows.length === 0 && <Row label={hostOf(current)} hint="这台 hub" />}
        {rows.map((h) => (
          <Row key={h.url} label={hubTitle(h)} hint={<span className="font-mono">{hostOf(h.url)}</span>}>
            {h.url === current ? (
              <Pill tone="green">当前</Pill>
            ) : (
              <Button tone="secondary" onClick={() => goToHub(h)} className="h-7 px-2.5">
                切换
              </Button>
            )}
            <IconButton icon="pencil" label={`改名：${hubTitle(h)}`} onClick={() => setRenaming(h)} />
            {h.url !== current && (
              <IconButton
                icon="trash"
                label={`从列表删除：${hubTitle(h)}`}
                onClick={() => {
                  removeHub(h.url);
                  toast(`已从列表删除 ${hubTitle(h)}`);
                }}
              />
            )}
          </Row>
        ))}
      </Group>
      <AddHub open={adding} onClose={() => setAdding(false)} />
      <RenameHub hub={renaming} onClose={() => setRenaming(null)} />
    </>
  );
}

function Field({ label, value, onChange, placeholder, hint, error, mono, autoFocus }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string; hint?: React.ReactNode; error?: string; mono?: boolean; autoFocus?: boolean }) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] font-medium text-ink-2">{label}</span>
      <input
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete="off"
        spellCheck={false}
        aria-invalid={error ? true : undefined}
        className={`h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] ${mono ? "font-mono" : ""}`}
      />
      {error ? <span className="mt-1 block text-[11px] text-red" role="alert">{error}</span> : hint && <span className="mt-1 block text-[11px] text-ink-3">{hint}</span>}
    </label>
  );
}

function AddHub({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    if (!open) return;
    setUrl("");
    setLabel("");
    setError("");
  }, [open]);
  const parsed = url.trim() ? normalizeHubURL(url) : null;
  const save = () => {
    const r = addHub(url, label);
    if ("error" in r) return setError(r.error);
    toast(`已添加 ${hubTitle(r.list[r.list.length - 1])}`);
    onClose();
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      width={440}
      title="添加 hub"
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" disabled={!parsed || "error" in parsed} onClick={save}>
            添加
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Field
          label="地址"
          autoFocus
          mono
          value={url}
          onChange={(v) => {
            setUrl(v);
            setError("");
          }}
          placeholder="https://<hub 地址>"
          error={error || (parsed && "error" in parsed ? parsed.error : "")}
          hint={parsed && "url" in parsed ? `会打开 ${parsed.url}` : "就是在浏览器里打开那台 hub 网页的地址，不带路径；没写 http:// 按 https://。"}
        />
        <Field label="名字（可选）" value={label} onChange={setLabel} placeholder="不填就用 hub 自己的名字" />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function RenameHub({ hub, onClose }: { hub: SavedHub | null; onClose: () => void }) {
  const [label, setLabel] = useState("");
  useEffect(() => setLabel(hub?.label ?? ""), [hub]);
  const save = () => {
    if (hub) renameHub(hub.url, label);
    onClose();
  };
  return (
    <Dialog
      open={hub !== null}
      onClose={onClose}
      width={400}
      title="改名"
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" onClick={save}>
            保存
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Field label="名字" autoFocus value={label} onChange={setLabel} placeholder={hub?.name || (hub ? hostOf(hub.url) : "")} hint="只改这个浏览器里的叫法；清空就用 hub 自己的名字。" />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

// ── 登录页 ──────────────────────────────────────────────

/** On the sign-in page: the other hubs, so a hub not signed in on yet is not a dead end. */
export function OtherHubs() {
  const others = useHubs().filter((h) => h.url !== here());
  if (others.length === 0) return null;
  return (
    <div className="mt-6">
      <div className="mb-1.5 px-1 text-[11.5px] font-medium text-ink-3">其他 hub</div>
      <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
        {others.map((h) => (
          <button key={h.url} type="button" onClick={() => goToHub(h)} className="flex w-full items-center gap-2.5 whitespace-nowrap px-3 py-2.5 text-left transition-colors hover:bg-hover">
            <Icon name="hub" size={14} className="shrink-0 text-ink-3" />
            <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{hubTitle(h)}</span>
            <span className="shrink-0 truncate font-mono text-[11px] text-ink-3">{hostOf(h.url)}</span>
            <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
          </button>
        ))}
      </div>
    </div>
  );
}
