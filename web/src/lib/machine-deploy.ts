import type { Machine } from "./types";

const PHASES = {
  waiting_idle: { label: "等待空闲", message: "正在等待这台电脑上的 Agent 空闲后更新 node…" },
  transferring: { label: "传输中", message: "正在把 hub 这一版的 node 传到这台电脑…" },
  restarting: { label: "重启中", message: "正在重启这台电脑上的 node…" },
  confirming: { label: "确认中", message: "正在等待 node 重新连接，并确认与 hub 的版本一致…" },
} satisfies Record<NonNullable<Machine["deploy_phase"]>, { label: string; message: string }>;

/** Older hubs can deploy SSH nodes, but never advertised an update path for linked nodes. */
export function machineDeployment(m: Machine) {
  const linked = m.kind === "link" || m.kind === "service";
  const supported = m.kind !== "local" && (m.can_deploy ?? !linked);
  const phase = m.deploy_phase && PHASES[m.deploy_phase];
  const busy = !!(m.deploying || phase);
  const canDeploy = supported && (!linked || m.online);
  const reason = !supported
    ? m.deploy_reason || (m.kind === "local"
      ? "这是 hub 所在的机器，请在本机升级 hub 和 node。"
      : linked
        ? "请先在这台电脑上本地升级 node，并将 hub 升级到支持链路更新的版本。"
        : "这台电脑暂不支持远程更新，请先在本机升级 node。")
    : !canDeploy ? "这台电脑离线，重新连接后才能更新 node。" : "";
  return {
    visible: !!(m.node_mismatch || busy || m.deploy_error),
    busy,
    expectedDisconnect: !m.online && !m.deploy_error && (m.deploy_phase === "restarting" || m.deploy_phase === "confirming"),
    supported,
    canDeploy: canDeploy && !busy,
    label: busy ? phase?.label || "部署中" : "重新部署",
    message: busy ? phase?.message || "正在更新 node，完成后还需确认与 hub 的版本一致…" : m.node_mismatch ? "这台电脑上的 node 和 hub 不是同一版。" : "",
    error: m.deploy_error ? `上次部署没成：${m.deploy_error}` : "",
    reason,
  };
}
