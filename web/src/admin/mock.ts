// Dev-only canned admin API (`npm run dev`, /admin?mock=<scenario>): build and screenshot the pages without a hub.
// Placeholder data only (documentation addresses, example names). main.tsx loads this behind import.meta.env.DEV, so
// production builds leave it out.
//
// Scenarios: login (any other name) · setup / setup-local / setup-remote · app (a lone hub, older computers, a restart
// pending) · nopeers (a hub without the mesh API) · center (the center of a group: members, computers with several
// paths, a service computer, one to pair again, one only a member has, an old ssh one) · member / member-offline (a
// member hub, its center reachable or not; member-stale: the page still thinks the center is up, the hub knows better
// and answers set-center with confirm_required) · empty (nothing but the hub's own machine). Adding computers works in every
// signed-in scenario: a connector link containing "fail" fails to pair.
import { setTransport } from "./api";
import type { AdminMachine, AdminState, Device, DiscoveredHub, MeshView, NetworkView, Overview, Peer } from "./types";

const SELF = "0d9e8f7a-6b5c-4d3e-8f2a-1b0c9d8e7f6a";
const OFFICE = "6f1c2a9e-0b7d-4c55-9e3a-2b8f41d07c11";
const OLD = "93b0d4e2-5a1f-4c7e-8d2b-6e9f0a1c3b57";
const LAB = "5e2b8c1d-7f4a-4b6e-9c3d-0a1b2c3d4e5f";
const NAS = "a24d77c0-91e2-4f1b-8f2d-5c0b3e6a9d42";
const VERSION = "v1.4.0-12-gabc1234";
const SIGNED_IN = new Set(["app", "nopeers", "center", "member", "member-offline", "member-stale", "empty"]);
const REPAIR = "连接器已经配对给了别的 hub，原来的凭据失效：在管理界面重新配对这台电脑";

const iso = (minutesAgo: number) => new Date(Date.now() - minutesAgo * 60_000).toISOString();
const later = (minutes: number) => new Date(Date.now() + minutes * 60_000).toISOString();

function fakeQr(n = 29): string[] {
  let seed = 7;
  const rnd = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff);
  const grid = Array.from({ length: n }, () => Array.from({ length: n }, () => (rnd() > 0.52 ? "1" : "0")));
  const finder = (r0: number, c0: number) => {
    for (let r = -1; r < 8; r++)
      for (let c = -1; c < 8; c++) {
        const y = r0 + r,
          x = c0 + c;
        if (y < 0 || x < 0 || y >= n || x >= n) continue;
        const ring = Math.max(Math.abs(r - 3), Math.abs(c - 3));
        grid[y][x] = r < 0 || c < 0 || r > 6 || c > 6 ? "0" : ring === 2 ? "0" : "1";
      }
  };
  finder(0, 0);
  finder(0, n - 7);
  finder(n - 7, 0);
  return grid.map((row) => row.join(""));
}

export function installMock(scenario: string) {
  const member = scenario.startsWith("member");
  const state: AdminState = {
    password_set: !scenario.startsWith("setup"),
    logged_in: SIGNED_IN.has(scenario),
    can_setup: scenario !== "setup-remote",
    setup_code_required: scenario === "setup-local",
    csrf: SIGNED_IN.has(scenario) ? "mock-csrf" : "",
    hub_name: member ? "公司的 hub" : "家里的 hub",
    version: VERSION,
  };
  let restart = { required: scenario === "app", reasons: scenario === "app" ? ["添加电脑「studio」", "TLS 监听改成 :18662"] : [] };
  const startedAt = iso(60 * 26 + 17);
  const local: AdminMachine = { id: "server", name: "服务器", kind: "local", icon: "server", online: true, transport: "local", node_version: VERSION, os: "linux", state: "active", hub_id: SELF, paths: [{ via: "local", hub_id: SELF, id: "server", ok: true, active: true }] };
  const machines: AdminMachine[] =
    scenario === "center"
      ? [
          local,
          {
            id: "example-machine", name: "开发机", kind: "service", icon: "server", online: true, transport: "service", node_version: VERSION, os: "linux", state: "active",
            address: "192.0.2.30:18663", proxy: "socks5://192.0.2.5:1080", hub_id: SELF,
            paths: [
              { via: "service", hub_id: SELF, id: "example-machine", ok: true, rtt_ms: 9, active: true },
              { via: "hub", hub_id: OFFICE, id: "example-machine", ok: true, rtt_ms: 41 },
            ],
          },
          {
            id: "macbook", name: "MacBook", kind: "link", icon: "laptop", online: true, transport: "hub", node_version: VERSION, os: "darwin", state: "active", hub_id: OFFICE,
            paths: [
              { via: "hub", hub_id: OFFICE, id: "mbp", ok: true, rtt_ms: 57, active: true },
              { via: "link", hub_id: SELF, id: "macbook", ok: false, error: "Jarvis Link 没有连上来（最后一次：连接被对方重置）" },
            ],
          },
          {
            id: "nas2", name: "客厅 NAS", kind: "service", icon: "server", online: false, error: REPAIR, os: "linux", last_seen: iso(60 * 5), state: "active",
            address: "192.0.2.41:18663", hub_id: SELF, paths: [{ via: "service", hub_id: SELF, id: "nas2", ok: false, error: REPAIR }],
          },
          {
            id: "office-pc", name: "公司台式机", kind: "hub", icon: "desktop", online: true, transport: "hub", node_version: VERSION, os: "linux", state: "active", hub_id: OFFICE,
            paths: [{ via: "hub", hub_id: OFFICE, id: "office-pc", ok: true, rtt_ms: 38, active: true }],
          },
          {
            id: "laptop", name: "旧笔记本", kind: "ssh", ssh_host: "laptop", online: true, transport: "ssh", node_version: "v1.3.9-4-gdef5678", node_mismatch: true, os: "darwin", state: "active", hub_id: SELF,
            paths: [{ via: "ssh", hub_id: SELF, id: "laptop", ok: true, rtt_ms: 15, active: true }],
          },
        ]
      : member
        ? [local, { id: "macbook", name: "MacBook", kind: "link", icon: "laptop", online: true, transport: "link", node_version: VERSION, os: "darwin", state: "active", hub_id: SELF, paths: [{ via: "link", hub_id: SELF, id: "macbook", ok: true, rtt_ms: 21, active: true }] }]
        : scenario === "empty"
          ? [local]
          : [
              { id: "server", name: "服务器", kind: "local", icon: "server", online: true, transport: "local", node_version: VERSION, os: "linux", state: "active" },
              { id: "laptop", name: "笔记本", kind: "ssh", ssh_host: "laptop", online: true, transport: "ssh", node_version: "v1.3.9-4-gdef5678", node_mismatch: true, os: "darwin", state: "active" },
              { id: "desk", name: "办公室电脑", kind: "link", online: false, error: "jarvis-link 没有连上来（最后一次：连接被对方重置）", os: "darwin", last_seen: iso(180), state: "active" },
              { id: "studio", name: "工作室", kind: "ssh", ssh_host: "studio", online: false, state: "pending_add" },
            ];
  let mesh: MeshView =
    scenario === "center"
      ? {
          role: "center",
          center: { hub_id: SELF, name: state.hub_name, online: true },
          term: 3,
          hubs: [
            { hub_id: SELF, name: state.hub_name, role: "center", online: true, self: true },
            { hub_id: OFFICE, name: "公司", role: "member", online: true, paired: true },
            { hub_id: OLD, name: "老家", role: "member", online: false, paired: true },
            { hub_id: LAB, name: "实验室", role: "member", online: true },
          ],
        }
      : member
        ? {
            role: "member",
            center: { hub_id: NAS, name: "书房 NAS", online: scenario !== "member-offline" },
            term: 5,
            hubs: [
              { hub_id: NAS, name: "书房 NAS", role: "center", online: scenario !== "member-offline", paired: true },
              { hub_id: SELF, name: state.hub_name, role: "member", online: true, self: true },
              { hub_id: LAB, name: "实验室", role: "member", online: true },
            ],
          }
        : { role: "center", center: { hub_id: SELF, name: state.hub_name, online: true }, term: 1, hubs: [{ hub_id: SELF, name: state.hub_name, role: "center", online: true, self: true }] };
  let devices: Device[] = [
    { id: "dev_a1", name: "iPhone", kind: "phone", role: "full", created_at: iso(60 * 24 * 40), last_seen: iso(3) },
    { id: "dev_b2", name: "Apple Watch", kind: "watch", role: "answer", created_at: iso(60 * 24 * 38), last_seen: iso(50) },
    { id: "dev_c3", name: "Chrome · 网页", kind: "other", role: "full", created_at: iso(60 * 24 * 2), last_seen: iso(60 * 5) },
    { id: "dev_d4", name: "办公室电脑 的 jarvis-link", kind: "link", role: "link", machine_id: "desk", created_at: iso(60 * 24 * 9), last_seen: iso(180) },
  ];
  let peers: Peer[] =
    scenario === "empty"
      ? []
      : member
        ? [{ hub_id: NAS, name: "书房 NAS", state: scenario === "member-offline" ? "offline" : "connected", address: "https://192.0.2.20:18662", rtt_ms: 4, since: iso(60 * 3), machines: 2, role: "center", error: scenario === "member-offline" ? "dial tcp 192.0.2.20:18662: connect: no route to host" : undefined }]
        : [
            { hub_id: OFFICE, name: "公司", state: "connected", address: "https://hub.example.com:18662", rtt_ms: 38, since: iso(60 * 7), machines: 3, role: scenario === "center" ? "member" : undefined },
            { hub_id: OLD, name: "老家", state: "refused", address: "https://198.51.100.40:18662", since: iso(60 * 30), machines: 0, role: scenario === "center" ? "member" : undefined },
          ];
  const discovered: DiscoveredHub[] = [
    { hub_id: "a24d77c0-91e2-4f1b-8f2d-5c0b3e6a9d42", name: "书房 NAS", addresses: ["https://192.0.2.20:18662", "https://[2001:db8::20]:18662"], fp: "q3J8pX0m-Vb7kQ2sT9yH4eW1rL6uN5cD8fG0aZ3iK7o", version: "v1.4.0" },
    { hub_id: "c8e1f5b3-2d6a-4e9f-b7c0-1a3d5f7e9b24", name: "客厅迷你主机", addresses: ["https://192.0.2.31:18662"], fp: "M2nB5vC8xZ1aS4dF7gH0jK3lQ6wE9rT2yU5iO8pA1sD", version: "v1.3.8" },
  ];
  const network: NetworkView = {
    listen: ":18660",
    public_url: "http://hub.example.com:18660",
    tls: { listen: ":18662", port: 18662, fp: "Xk3v9QeR2mT8bN1cY6wZ4pL7sD0fG5hJ2kA9uE3iO6q" },
    advertise: ["https://hub.example.com:18662"],
    advertise_ipv6: true,
    advertise_lan: true,
    endpoints: [
      { url: "https://[2001:db8:1::10]:18662", kind: "ipv6", port: 18662, scheme: "https", seen_at: iso(2) },
      { url: "https://hub.example.com:18662", kind: "name", port: 18662, scheme: "https", seen_at: iso(2) },
      { url: "https://192.0.2.10:18662", kind: "lan", port: 18662, scheme: "https", seen_at: iso(2) },
      { url: "http://hub.example.com:18660", kind: "name", port: 18660, scheme: "http", seen_at: iso(2) },
    ],
    security: { trusted_cidrs: ["198.51.100.0/24"], allow_public_plaintext: false },
    admin: { access: "any", cidrs: [], session_hours: 12, from_env: false, container: false },
    you: { address: "192.0.2.55", tls: false, allowed: { loopback: false, lan: true, cidrs: false, any: true } },
    restart,
  };

  const overview = (): Overview => ({
    hub: { hub_id: SELF, name: state.hub_name, name_set: true, default_name: "server", version: state.version, started_at: startedAt, os: "linux", arch: "amd64", container: false, can_restart: true },
    listen: network.listen,
    public_url: network.public_url,
    tls: network.tls,
    endpoints: network.endpoints,
    machines: { total: machines.filter((m) => m.state === "active").length, online: machines.filter((m) => m.online).length },
    devices: devices.length,
    restart,
  });

  const memberHub = () => fail(409, "member_hub", `这台 hub 是组里的成员，设备由中心 hub「${mesh.center.name}」管理：到它的管理界面去`);
  const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  const fail = (status: number, code: string, error = code, extra: Record<string, unknown> = {}) => json({ error, code, ...extra }, status);
  const wait = (ms = 220) => new Promise((r) => setTimeout(r, ms));
  let failures = 0;

  setTransport(async (url, init) => {
    await wait();
    const path = url.replace(/^\/api\/v1\/admin/, "");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(String(init.body)) : {};
    const key = `${method} ${path.split("?")[0]}`;
    if (key === "GET /state") return json(state);
    if (key === "POST /setup") {
      if (state.setup_code_required) {
        if (!body.setup_code) return fail(403, "setup_code_required");
        if (String(body.setup_code).replace(/-/g, "").toUpperCase() !== "ABCDEFGH") return fail(401, "bad_setup_code");
      }
      if (String(body.password ?? "").length < 8) return fail(400, "weak_password");
      Object.assign(state, { password_set: true, logged_in: true, csrf: "mock-csrf" });
      return json(state);
    }
    if (key === "POST /login") {
      if (body.password !== "password123") {
        failures++;
        return failures >= 3 ? fail(429, "rate_limited", "too many failures", { retry_after: 840 }) : fail(401, "bad_password");
      }
      Object.assign(state, { logged_in: true, csrf: "mock-csrf" });
      return json(state);
    }
    if (!state.logged_in) return fail(401, "login_required");
    if (method !== "GET" && (init.headers as Record<string, string>)["X-Jarvis-CSRF"] !== state.csrf) return fail(403, "csrf");
    switch (key) {
      case "POST /logout":
        state.logged_in = false;
        return json({ ok: true });
      case "POST /password":
        return body.current === "password123" ? json({ ok: true }) : fail(401, "bad_password");
      case "GET /overview":
        return json(overview());
      case "PUT /hub":
        state.hub_name = String(body.name || "server");
        return json({ name: state.hub_name, name_set: Boolean(body.name), restart });
      case "POST /doctor":
        await wait(900);
        return json({
          ran_at: new Date().toISOString(),
          items: [
            { level: "ok", item: "明文 listen", detail: ":18660；仅供内网或 VPN 使用" },
            { level: "ok", item: "public.tls_listen", detail: ":18662（配置值，尚未验证 TLS 监听）" },
            { level: "ok", item: "TLS 证书", detail: "SPKI SHA-256 钉扎值 Xk3v9QeR2mT8…；到期时间 2027-10-01T00:00:00Z" },
            { level: "problem", item: "本机 IPv6", detail: "没有可确认稳定的全局 IPv6；请检查运营商 IPv6 和网卡地址" },
            { level: "info", item: "外部可达性", detail: "未验证。本机连自己的地址走环回，不能证明公网可达。" },
          ],
          next: ["在主机、路由器和光猫上，对公网只放行 hub 的 TCP TLS 端口 18662；明文端口仅限内网或 VPN", "用手机关掉 Wi-Fi，打开 https://hub.example.com:18662/api/v1/health 测一次（不要携带 token）"],
        });
      case "POST /restart":
        restart = { required: false, reasons: [] };
        return json({ ok: true });
      case "GET /machines":
        return json({ machines, tls: Boolean(network.tls), restart });
      case "POST /machines": {
        if (body.kind === "ssh" || body.kind === "tailcat")
          return fail(400, "ssh_unsupported", "不再添加 ssh 电脑：在那台电脑上装 node 服务，选「电脑来连」或「hub 来连」；hub.json 里已有的 ssh 电脑照常能用");
        if (body.kind !== "link" && body.kind !== "service") return fail(400, "invalid_kind", "kind: link or service");
        if (!/^[a-z0-9][a-z0-9._-]{0,62}$/.test(String(body.id ?? ""))) return fail(400, "invalid_id");
        if (machines.some((m) => m.id === body.id)) return fail(409, "exists", "已经有这个 id 的电脑");
        const base = { id: body.id, name: body.name || body.id, kind: body.kind, icon: body.icon || "laptop", state: "active" as const, hub_id: SELF };
        if (body.kind === "link") {
          const m: AdminMachine = { ...base, online: false, paths: [{ via: "link", hub_id: SELF, id: body.id, ok: false }] };
          machines.push(m);
          return json({ machine: m, restart });
        }
        const link = String(body.pair_link ?? "");
        if (!link.startsWith("jarvis://connector")) return fail(502, "pair_failed", "这不是连接器的配对链接（jarvis://connector?…）");
        if (body.proxy && !/^socks5h?:\/\//.test(body.proxy)) return fail(400, "invalid_proxy", "proxy 要写成 socks5://主机:端口");
        await wait(1400);
        const addr = body.address || new URLSearchParams(link.split("?")[1] ?? "").get("addr") || "";
        if (link.includes("fail")) return fail(502, "pair_failed", `连不上那台电脑的连接器：${addr}：dial tcp ${addr}: connect: connection refused`);
        const m: AdminMachine = { ...base, online: false, address: addr, proxy: body.proxy || undefined, paths: [{ via: "service", hub_id: SELF, id: body.id, ok: false }] };
        machines.push(m);
        setTimeout(() => Object.assign(m, { online: true, transport: "service", node_version: VERSION, os: "linux", paths: [{ via: "service", hub_id: SELF, id: body.id, ok: true, rtt_ms: 11, active: true }] }), 3000);
        return json({ machine: m, restart });
      }
      case "GET /mesh":
        return scenario === "nopeers" ? fail(501, "not_implemented") : json(mesh);
      case "GET /devices":
        if (mesh.role === "member") return memberHub();
        return json({ devices });
      case "POST /devices/pair":
        if (mesh.role === "member") return memberHub();
        return json({ pair_url: "jarvis://pair?url=http%3A%2F%2Fhub.example.com%3A18660&token=jp_mock", url: network.public_url, expires_at: later(10), fp: network.tls?.fp, qr: { size: 29, rows: fakeQr() } });
      case "GET /network":
        return json({ ...network, restart });
      case "PUT /network/access":
        if (!body.force && !network.you.allowed[body.access as keyof typeof network.you.allowed]) return fail(409, "would_lock_you_out");
        network.admin = { ...network.admin, access: body.access, cidrs: body.cidrs ?? [] };
        return json({ ...network, restart });
      case "PUT /network/public":
        network.public_url = body.public_url;
        network.advertise = body.advertise;
        restart = { required: true, reasons: [...restart.reasons, "公网地址"] };
        return json({ ...network, restart });
      case "GET /peers":
        return scenario === "nopeers" ? fail(501, "not_implemented") : json({ peers, discovered });
      case "POST /peers/code":
        return json({ code: "jarvis://peer?hub=0d9e8f7a&fp=Xk3v9QeR2mT8&url=https%3A%2F%2Fhub.example.com%3A18662&code=pc_mock", expires_at: later(10) });
      case "POST /peers/probe": {
        const address = String(body.address ?? "").trim();
        if (!address) return fail(400, "invalid_address", "address is required");
        await wait(500);
        if (address.includes("192.0.2.31")) return json({ hub_id: "c8e1f5b3-2d6a-4e9f-b7c0-1a3d5f7e9b24", name: "客厅迷你主机", tls_port: 0, fp: "", version: "v1.3.8" });
        if (address.includes("unreachable")) return fail(502, "unreachable", "could not reach that address");
        const d = discovered.find((x) => x.addresses.some((a) => a.includes(address) || address.includes(a))) ?? discovered[0];
        return json({ hub_id: d.hub_id, name: d.name, tls_port: 18662, fp: d.fp, version: d.version });
      }
      case "POST /peers": {
        if (!String(body.code ?? "").startsWith("jarvis://peer?")) return fail(400, "invalid_code", "not a peer code");
        const p: Peer = { hub_id: "a24d77c0-91e2-4f1b-8f2d-5c0b3e6a9d42", name: "书房 NAS", state: "connecting", address: "https://192.0.2.20:18662", machines: 0 };
        peers = [...peers, p];
        return json(p);
      }
    }
    const m = /^\/machines\/([^/]+)(\/.*)?$/.exec(path);
    if (m) {
      const id = decodeURIComponent(m[1]);
      const machine = machines.find((x) => x.id === id);
      if (!machine) return fail(404, "not_found");
      if (m[2] === "/connector") {
        if (machine.kind !== "service") return fail(400, "not_service");
        const link = String(body.pair_link ?? "");
        if (!link.startsWith("jarvis://connector")) return fail(502, "pair_failed", "这不是连接器的配对链接（jarvis://connector?…）");
        await wait(1200);
        if (link.includes("fail")) return fail(502, "pair_failed", "配对码不对、用过了或过期了：在那台电脑上重新生成");
        const address = body.address || machine.address || "";
        const moved = address !== machine.address;
        Object.assign(machine, { error: undefined, online: true, transport: "service", address, paths: [{ via: "service", hub_id: SELF, id, ok: true, rtt_ms: 7, active: true }] });
        return json({ ok: true, address, version: VERSION, restart_required: moved });
      }
      if (method === "DELETE") {
        if (machine.state === "pending_add") machines.splice(machines.indexOf(machine), 1);
        else machine.state = "pending_remove";
        restart = { required: true, reasons: [...restart.reasons, `删除电脑「${id}」`] };
        return json({ ok: true, restart });
      }
      if (m[2] === "/reconnect") return json({ ok: true });
      if (m[2] === "/link-pair") return json({ pair_url: `jarvis://link?url=https%3A%2F%2Fhub.example.com%3A18662&machine=${id}&token=jp_mock`, expires_at: later(10), command: `jarvis-link pair 'jarvis://link?…&machine=${id}'` });
    }
    const d = /^\/devices\/([^/]+)$/.exec(path);
    if (d && method === "DELETE") {
      devices = devices.filter((x) => x.id !== decodeURIComponent(d[1]));
      return json({ ok: true });
    }
    const p = /^\/peers\/([^/]+)$/.exec(path);
    if (p && method === "DELETE") {
      peers = peers.filter((x) => x.hub_id !== decodeURIComponent(p[1]));
      return json({ ok: true });
    }
    const c = /^\/peers\/([^/]+)\/center$/.exec(path);
    if (c && method === "POST") {
      const target = mesh.hubs.find((h) => h.hub_id === decodeURIComponent(c[1]));
      if (!target) return fail(404, "not_in_group", "这台 hub 不在组里");
      await wait(600);
      if (!target.self && !target.online) return fail(409, "unreachable", "那台 hub 现在连不上");
      const centerDown = !mesh.center.online || (scenario === "member-stale" && mesh.term === 5);
      if (centerDown && !body.confirm) return fail(409, "confirm_required", `现在的中心「${mesh.center.name}」连不上`);
      const hubs = mesh.hubs.map((h) => ({ ...h, role: h.hub_id === target.hub_id ? ("center" as const) : ("member" as const) }));
      mesh = { role: target.self ? "center" : "member", center: { hub_id: target.hub_id, name: target.name, online: true }, term: mesh.term + 1, hubs };
      peers = peers.map((x) => ({ ...x, role: x.hub_id === target.hub_id ? "center" : "member" }));
      return json(mesh);
    }
    return fail(404, "not_found", "404 page not found");
  });
}
