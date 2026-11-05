import { useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { machineDeployment } from "../../lib/machine-deploy";
import { refresh } from "../../lib/store";
import type { Machine } from "../../lib/types";
import { Button, cx, toast } from "../../ui/atoms";
import { t } from "../../i18n";

/** The update action both the deployment strip and the node self-check use: the same request, the same capability checks. */
export function useDeploy(machine: Machine) {
  const [pending, setPending] = useState(false);
  const deployment = machineDeployment(machine);

  const deploy = async () => {
    if (!deployment.canDeploy || pending) return;
    setPending(true);
    try {
      await api(`/machines/${enc(machine.id)}/deploy`, { method: "POST" });
      await refresh().catch(() => {});
    } catch (e) {
      toast(t("machines-machinedeploy.failed", { error: errorText(e) }), "error");
    } finally {
      setPending(false);
    }
  };
  return { deployment, pending, deploy };
}

/** The list and detail keep the same capability checks, stages and confirmation wording. */
export function MachineDeploy({ machine, className }: { machine: Machine; className?: string }) {
  const { deployment, pending, deploy } = useDeploy(machine);
  if (!deployment.visible) return null;

  return (
    <div className={cx("mx-4 flex items-center gap-2 rounded-[8px] bg-orange-tint py-1.5 pl-2.5 pr-1.5 text-[12px] text-orange", className)}>
      <div className="min-w-0 flex-1 break-words" role="status">
        {deployment.message && <p>{deployment.message}</p>}
        {deployment.error && <p>{deployment.error}</p>}
        {deployment.reason && !deployment.busy && <p>{deployment.reason}</p>}
      </div>
      {(deployment.supported || deployment.busy) && (
        <Button tone="secondary" icon="retry" busy={deployment.busy || pending} disabled={!deployment.canDeploy || pending} title={deployment.reason || undefined} onClick={() => void deploy()}>
          {pending && !deployment.busy ? t("machines-machinedeploy.requesting") : deployment.label}
        </Button>
      )}
    </div>
  );
}
