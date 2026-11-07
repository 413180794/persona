import { isAppLink } from "../../lib/compatibility";
// "Linked hubs" (under "Advanced"): other hubs this one links with (docs/archive/operations/plan-hub-mesh.md 2.7) and which of the group is the center
// (docs/archive/operations/plan-hub-center.md 2). Most people never need it: one hub, the node service on every other computer. A hub
// without the API answers 501 not_implemented and this page says so.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { ago } from "../../lib/format";
import { Button, cx, Dialog, toast, useNow } from "../../ui/atoms";
import { adminApi, AdminError, errorText, setCenter } from "../api";
import { AdminIcon } from "../icons";
import { centerName, countdown, peerStatus, probeLinkable, sortMeshHubs } from "../logic";
import { devDialog, navigate } from "../router";
import { refreshMesh, share, useShared } from "../store";
import type { DiscoveredHub, MeshHub, MeshView, Peer, PeerCode, PeerProbe, PeersView } from "../types";
import { Card, ConfirmButton, CopyValue, Dot, EmptyState, ErrorBox, Field, Loader, Notice, PageHeader, Tag, useLoad, useVisiblePoll } from "../parts";

export function Peers() {
  const { tls, mesh } = useShared();
  const { data, error, loading, reload } = useLoad(() => adminApi<PeersView>("/peers"));
  const [making, setMaking] = useState(false);
  // adding: true for an address typed in, a discovered hub to go to its first address
  const [adding, setAdding] = useState<DiscoveredHub | true | null>(null);
  const missing = error instanceof AdminError && (error.status === 501 || error.code === "not_implemented");

  // Dev only (screenshots): ?dialog=addpeer / addpeer-notls opens the add flow on the first / second discovered hub.
  const [devOpened, setDevOpened] = useState(false);
  useEffect(() => {
    const d = devDialog();
    if (!data || devOpened || !d.startsWith("addpeer")) return;
    setDevOpened(true);
    setAdding(data.discovered[d === "addpeer-notls" ? 1 : 0] ?? true);
  }, [data, devOpened]);

  // link state, RTT and who is online move on their own
  useVisiblePoll(() => (reload(), void refreshMesh()), 10_000, Boolean(data) && !missing);

  return (
    <>
      <PageHeader
        title={t("admin-pages-peers.title")}
        subtitle={t("admin-pages-peers.subtitle")}
        right={
          !missing && (
            <>
              <Button onClick={() => setMaking(true)} disabled={tls === null}>
                <AdminIcon name="key" size={13} />
                {t("admin-pages-peers.make_code")}
              </Button>
              <Button tone="primary" onClick={() => setAdding(true)} disabled={tls === null}>
                <AdminIcon name="plus" size={13} stroke={2.2} />
                {t("admin-pages-peers.add_hub")}
              </Button>
            </>
          )
        }
      />
      <Notice
        tone="gray"
        icon="info"
        title={t("admin-pages-peers.rarely_needed")}
        action={
          <Button icon="plus" onClick={() => navigate("machines")}>
            {t("admin-pages-peers.add_computer")}
          </Button>
        }
      >
        {t("admin-pages-peers.rarely_needed_body")}
      </Notice>
      {tls === null && !missing && (
        <Notice tone="orange" icon="lock" title={t("admin-pages-peers.tls_only")} action={<Button onClick={() => navigate("network")}>{t("admin-pages-peers.enable")}</Button>}>
          {t("admin-pages-peers.tls_only_body")}
        </Notice>
      )}
      {missing ? (
        <Card>
          <EmptyState icon="mesh" title={t("admin-pages-peers.missing_title")}>
            {t("admin-pages-peers.missing_body")}
          </EmptyState>
        </Card>
      ) : !data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <>
          {/* A failed poll keeps the last list on screen; say so, or its dots and round trips look live. */}
          {error != null && <ErrorBox error={error} onRetry={reload} />}
          {mesh && mesh.hubs.length > 1 && <Group mesh={mesh} onChanged={reload} />}
          <Card title={t("admin-pages-peers.linked")} footer={t("admin-pages-peers.linked_footer")}>
            {data.peers.length === 0 ? (
              <EmptyState icon="mesh" title={t("admin-pages-peers.linked_empty")}>
                {t("admin-pages-peers.linked_empty_body")}
              </EmptyState>
            ) : (
              data.peers.map((p) => <PeerRow key={p.hub_id} p={p} onGone={reload} />)
            )}
          </Card>
          <Card title={t("admin-pages-peers.discovered")} footer={t("admin-pages-peers.discovered_footer")}>
            {data.discovered.length === 0 ? (
              <div className="flex items-center gap-2.5 px-4 py-3 text-[12.5px] text-ink-3">
                <AdminIcon name="radar" size={14} />
                {t("admin-pages-peers.discovered_empty")}
              </div>
            ) : (
              data.discovered.map((d) => (
                <div key={d.hub_id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
                    <AdminIcon name="radar" size={16} />
                  </span>
                  <div className="min-w-[180px] flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[13.5px] font-medium text-ink">{d.name}</span>
                      {d.version && <span className="font-mono text-[11px] text-ink-3">{d.version}</span>}
                    </div>
                    <div className="mt-1 truncate font-mono text-[11.5px] text-ink-3" title={d.addresses.join("\n")}>
                      {d.addresses.join(" · ")}
                    </div>
                    <div className="mt-0.5 truncate font-mono text-[11px] text-ink-3" title={d.fp}>
                      {t("admin-pages-peers.fingerprint_value", { fp: d.fp })}
                    </div>
                  </div>
                  <Button className="h-7 px-2.5" onClick={() => setAdding(d)}>
                    {t("admin-pages-peers.link_ellipsis")}
                  </Button>
                </div>
              ))
            )}
          </Card>
        </>
      )}
      <MakeCode open={making} onClose={() => setMaking(false)} />
      <AddPeer target={adding} onClose={() => setAdding(null)} onJoined={reload} />
    </>
  );
}

function PeerRow({ p, onGone }: { p: Peer; onGone: () => void }) {
  const st = peerStatus(p);
  const [busy, setBusy] = useState(false);
  const drop = async () => {
    setBusy(true);
    try {
      await adminApi(`/peers/${encodeURIComponent(p.hub_id)}`, "DELETE");
      toast(t("admin-pages-peers.disconnected"), "ok");
      onGone();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px] shadow-hairline", p.state === "connected" ? "bg-green-tint text-green" : "bg-inset text-ink-2")}>
        <AdminIcon name="mesh" size={16} />
      </span>
      <div className="min-w-[180px] flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{p.name}</span>
          <span className="truncate font-mono text-[11px] text-ink-3" title={p.hub_id}>
            {p.hub_id.slice(0, 8)}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
          <span className="flex items-center gap-1.5 whitespace-nowrap">
            <Dot tone={st.tone} pulse={p.state === "connecting"} />
            <span className={st.tone === "green" ? "text-green" : st.tone === "orange" ? "text-orange" : "text-ink-2"}>{st.label}</span>
          </span>
          {p.role && <Tag tone={p.role === "center" ? "accent" : "gray"}>{p.role === "center" ? t("admin-pages-peers.role_center") : t("admin-pages-peers.role_member")}</Tag>}
          {p.rtt_ms !== undefined && p.state === "connected" && <Tag>{p.rtt_ms} ms</Tag>}
          {p.address && <span className="truncate font-mono">{p.address}</span>}
          <span className="whitespace-nowrap">{t("admin-pages-peers.computers", { count: p.machines })}</span>
          {p.since && <span className="whitespace-nowrap">{p.state === "connected" ? t("admin-pages-peers.connected_since", { time: ago(p.since) }) : t("admin-pages-peers.disconnected_at", { time: ago(p.since) })}</span>}
        </div>
        {st.error && <div className="mt-1 text-[11.5px] break-words text-red">{st.error}</div>}
      </div>
      <ConfirmButton label={t("admin-pages-peers.disconnect")} confirm={t("admin-pages-peers.disconnect")} icon="unlink" busy={busy} onConfirm={drop} />
    </div>
  );
}

// Group: every hub of the group, which one is the center, and "Make primary" for the others (this hub included).
function Group({ mesh, onChanged }: { mesh: MeshView; onChanged: () => void }) {
  const [target, setTarget] = useState<MeshHub | null>(null);
  return (
    <>
      <Card
        title={t("admin-pages-peers.group_title", { count: mesh.hubs.length })}
        footer={t("admin-pages-peers.group_footer", { term: mesh.term })}
      >
        {sortMeshHubs(mesh.hubs).map((h) => (
          <div key={h.hub_id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
            <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px] shadow-hairline", h.role === "center" ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-2")}>
              <AdminIcon name="mesh" size={16} />
            </span>
            <div className="min-w-[180px] flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-[13.5px] font-medium text-ink">{h.name || h.hub_id.slice(0, 8)}</span>
                <Tag tone={h.role === "center" ? "accent" : "gray"}>{h.role === "center" ? t("admin-pages-peers.role_center") : t("admin-pages-peers.role_member")}</Tag>
                {h.self && <Tag>{t("admin-pages-peers.this_hub_tag")}</Tag>}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
                <span className="flex items-center gap-1.5 whitespace-nowrap">
                  <Dot tone={h.online || h.self ? "green" : "gray"} />
                  <span className={h.online || h.self ? "text-green" : "text-ink-2"}>{h.online || h.self ? t("admin-pages-peers.online") : t("admin-pages-peers.unreachable")}</span>
                </span>
                <span className="truncate font-mono" title={h.hub_id}>
                  {h.hub_id.slice(0, 8)}
                </span>
                {!h.self && !h.paired && <span className="whitespace-nowrap">{t("admin-pages-peers.via_other_hub")}</span>}
              </div>
            </div>
            {h.role !== "center" && (
              <Button className="h-7 px-2.5" disabled={!h.online && !h.self} title={!h.online && !h.self ? t("admin-pages-peers.set_center_offline_hint") : undefined} onClick={() => setTarget(h)}>
                {t("admin-pages-peers.set_center")}
              </Button>
            )}
          </div>
        ))}
      </Card>
      <SetCenter mesh={mesh} target={target} onClose={() => setTarget(null)} onDone={onChanged} />
    </>
  );
}

// SetCenter asks first, then hands the group over. When the center now in place cannot be reached the hub answers 409
// confirm_required: the dialog says what that means and goes ahead with confirm on the second press.
function SetCenter({ mesh, target, onClose, onDone }: { mesh: MeshView; target: MeshHub | null; onClose: () => void; onDone: () => void }) {
  const [offline, setOffline] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!target) return;
    setOffline(!mesh.center.online);
    setError("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);
  const old = centerName(mesh);
  // Each sentence has a form for this hub and one for a named hub: languages place and capitalize the two differently.
  const say = (key: string, vars: Record<string, string> = {}) => (target ? t(`${key}_${target.self ? "self" : "named"}`, { name: target.name, ...vars }) : "");
  const go = async () => {
    if (!target) return;
    setBusy(true);
    setError("");
    try {
      const m = await setCenter(target.hub_id, offline);
      share({ mesh: m });
      toast(say("admin-pages-peers.now_center"), "ok");
      onDone();
      onClose();
    } catch (e) {
      if (e instanceof AdminError && e.code === "confirm_required") setOffline(true);
      else if (e instanceof AdminError && e.code === "unreachable") setError(say("admin-pages-peers.set_unreachable"));
      else setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(target)}
      onClose={() => !busy && onClose()}
      title={say("admin-pages-peers.set_center_title")}
      width={480}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button tone={offline ? "danger" : "primary"} busy={busy} onClick={go}>
            {offline ? t("admin-pages-peers.set_center_anyway") : t("admin-pages-peers.set_center")}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-[12.5px] leading-relaxed text-ink-2">
          {say(offline ? "admin-pages-peers.set_center_body" : "admin-pages-peers.set_center_body_demote", { old })}
        </p>
        {offline && (
          <div className="flex items-start gap-2 rounded-control bg-orange-tint px-3 py-2.5 text-[12.5px] leading-relaxed text-orange" style={{ animation: "fade-in 160ms ease-out both" }}>
            <AdminIcon name="alert" size={14} className="mt-0.5 shrink-0" />
            <span>{t("admin-pages-peers.old_center_offline", { old })}</span>
          </div>
        )}
        {error && (
          <p className="text-[12.5px] text-red" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function MakeCode({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [code, setCode] = useState<PeerCode | null>(null);
  const [error, setError] = useState("");
  const now = useNow(1000, Boolean(code));
  useEffect(() => {
    if (!open) return;
    let live = true;
    setCode(null);
    setError("");
    adminApi<PeerCode>("/peers/code", "POST").then(
      (c) => live && setCode(c),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [open]);
  const left = code ? countdown(code.expires_at, now) : "";
  return (
    <Dialog open={open} onClose={onClose} title={t("admin-pages-peers.code_title")} width={520}>
      {!code && !error && <div className="py-6 text-center text-[12.5px] text-ink-3">{t("admin-pages-peers.code_making")}</div>}
      {error && <p className="py-3 text-[12.5px] text-red">{error}</p>}
      {code && (
        <div className="space-y-3">
          <p className="text-[12.5px] leading-relaxed text-ink-2">
            {left
              ? tRich("admin-pages-peers.code_hint_left", { left: <span className="font-mono text-ink">{left}</span> })
              : tRich("admin-pages-peers.code_hint_expired", { expired: <span className="text-red">{t("admin-pages-peers.code_expired")}</span> })}
          </p>
          <div className="rounded-control bg-inset p-2.5 shadow-hairline">
            <CopyValue value={code.code} />
          </div>
          <p className="text-[11.5px] leading-relaxed text-ink-3">{t("admin-pages-peers.code_warning")}</p>
        </div>
      )}
    </Dialog>
  );
}

// AddPeer links with another hub in two steps: probe its address and check who answered (name, hub id, key
// fingerprint against that hub's own admin page), then paste the peer code made there. No TLS there: no link.
function AddPeer({ target, onClose, onJoined }: { target: DiscoveredHub | true | null; onClose: () => void; onJoined: () => void }) {
  const [address, setAddress] = useState("");
  const [probe, setProbe] = useState<PeerProbe | null>(null);
  const [probing, setProbing] = useState(false);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const check = async (addr = address) => {
    const a = addr.trim();
    if (!a) return;
    setProbing(true);
    setProbe(null);
    setError("");
    try {
      setProbe(await adminApi<PeerProbe>("/peers/probe", "POST", { address: a }));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setProbing(false);
    }
  };

  // A discovered hub starts at its first address, checked right away.
  useEffect(() => {
    if (!target) return;
    const first = target === true ? "" : (target.addresses[0] ?? "");
    setAddress(first);
    setProbe(null);
    setCode("");
    setError("");
    if (first) void check(first);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  const join = async () => {
    setBusy(true);
    setError("");
    try {
      const p = await adminApi<Peer>("/peers", "POST", { code: code.trim() });
      toast(t("admin-pages-peers.linked_with", { name: p.name }), "ok");
      onJoined();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const ok = probe && probeLinkable(probe);
  return (
    <Dialog
      open={Boolean(target)}
      onClose={onClose}
      title={target && target !== true ? t("admin-pages-peers.link_with", { name: target.name }) : t("admin-pages-peers.add_hub")}
      width={540}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button tone="primary" busy={busy} disabled={!ok || !isAppLink(code.trim())} onClick={join}>
            {t("admin-pages-peers.link")}
          </Button>
        </>
      }
    >
      <div className="space-y-3.5">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void check();
          }}
          className="flex items-end gap-2"
        >
          <div className="min-w-0 flex-1">
            <Field
              label={t("admin-pages-peers.step_address")}
              value={address}
              onChange={(v) => (setAddress(v), setProbe(null))}
              placeholder={t("admin-pages-peers.address_placeholder")}
              mono
              autoFocus={target === true}
            />
          </div>
          <Button type="submit" className="mb-px h-9" busy={probing} disabled={!address.trim()}>
            {t("admin-pages-peers.check")}
          </Button>
        </form>

        {probe && (
          <div className="overflow-hidden rounded-control bg-inset shadow-hairline" style={{ animation: "fade-in 160ms ease-out both" }}>
            <div className="divide-y divide-line">
              <ProbeLine label={t("admin-pages-peers.probe_name")}>
                <span className="text-[13px] font-medium text-ink">{probe.name}</span>
                {probe.version && <span className="font-mono text-[11px] text-ink-3">{probe.version}</span>}
              </ProbeLine>
              <ProbeLine label={t("admin-pages-peers.probe_hub_id")}>
                <span className="truncate font-mono text-[12px] text-ink-2" title={probe.hub_id}>
                  {probe.hub_id}
                </span>
              </ProbeLine>
              <ProbeLine label={t("admin-pages-peers.probe_fp")}>
                {probe.fp ? (
                  <span className="font-mono text-[12px] break-all text-ink">{probe.fp}</span>
                ) : (
                  <span className="text-[12px] text-ink-3">{t("admin-pages-peers.none")}</span>
                )}
              </ProbeLine>
            </div>
            {ok ? (
              <p className="border-t border-line px-3 py-2 text-[11.5px] leading-relaxed text-ink-3">
                {t("admin-pages-peers.probe_ok")}
              </p>
            ) : (
              <div className="flex items-start gap-2 border-t border-line bg-orange-tint px-3 py-2 text-[12px] leading-relaxed text-orange">
                <AdminIcon name="lock" size={13} className="mt-0.5 shrink-0" />
                {t("admin-pages-peers.probe_no_tls")}
              </div>
            )}
          </div>
        )}

        <Field
          label={t("admin-pages-peers.step_code")}
          hint={ok ? t("admin-pages-peers.code_field_hint") : probe ? t("admin-pages-peers.code_field_no_tls") : t("admin-pages-peers.code_field_check_first")}
          value={code}
          onChange={setCode}
          placeholder="persona://peer?…"
          mono
          multiline
          disabled={!ok}
        />
        {error && (
          <p className="text-[12.5px] text-red" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function ProbeLine({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <span className="min-w-12 shrink-0 whitespace-nowrap text-[11.5px] text-ink-3">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-2">{children}</div>
    </div>
  );
}
