# Updating the node over Persona Link

When a machine connects to the hub actively through Persona Link (`kind: link`), or the hub connects to the computer (`kind: service`), the resident personad is updated over the existing node RPC. Link still only forwards byte streams: no new listening port, no SSH required, and it is not responsible for installing anything.

This updates the executable used by the resident node; it does not update the connector jarvis-link or the Mac menu bar App itself. Implementation: `server/internal/devices/deploy_rpc.go`, `server/internal/node/update.go`, `server/internal/nodeupdate/`. The old SSH / tailcat deploy entry points stay.

## Usage

When the web Machines page or machine detail shows a version mismatch, click **Redeploy**. The page then shows **Waiting for idle**, **Transferring**, **Restarting**, **Confirming** in turn. Finishing the file transfer does not mean success: the machine must reconnect and actually run the target version before the hub confirms the update is complete.

- The hub picks the node matching this computer's OS / architecture from the existing platform artifact directory; the target is the version the hub itself runs.
- A normal update waits for the machine's Agents to go idle, including waits for the user's answer and background tasks still running; the node checks again before installing, in case a new task started during the transfer.
- Only one update transaction is accepted per machine; a dropped connection does not lose the transaction's mutual exclusion.
- After the hub updates itself it updates the online nodes one by one; Link machines that are offline at that moment keep a pending target and continue on their next connection. The connection method is never changed for the sake of an update.
- The hub's task target, progress and result are stored in Postgres settings (`node.deploy.<machine id>`). After a hub restart, accepted tasks can continue; if the hub version has changed, the old target is stopped and the user is told clearly to update again, so an outdated target is never installed.

## First upgrade of an old node

A new protocol cannot make an old node that does not know it update itself. When a machine does not report the update capability, the web page does not offer a button that is bound to fail; it explains that the resident node must first be upgraded once on the computer.

1. Get a personad that includes this capability and fits the machine's OS and architecture, and make sure it includes the features and custom changes this machine uses.
2. On the computer, find where the resident node is actually installed: `bin` in `jarvis-link node status --json`. Respect the existing service's runtime root, launch arguments and environment; do not guess from the default directory alone.
3. Replace that binary through the original install or deploy channel, then restart the corresponding resident node service. On Linux the standard user service is `jarvisd-node`; on Mac the standard LaunchAgent label is `com.jarvis.node`.
4. Go back to the Machines page and confirm the node is connected and supports remote updates; later upgrades can then be done from the client.

Reinstalling the Persona Link App on Mac currently does not replace an installed resident node automatically; upgrading the menu bar App does not count as doing this step. The first migration should use a node artifact that includes this capability.

Updates are offered only for resident nodes with a supported way to recover the process. A stdio node run directly, or a resident process started by hand without a supported service manager, should show why it cannot be updated remotely. An old hub lacks the new orchestration logic and must also first be upgraded through its original channel.

## Protocol and install boundaries

`node.info` gains the optional `node_update` and `node_update_reason`. Old nodes do not report them and are treated as unsupported. The existing `POST /api/v1/machines/{mid}/deploy` is unchanged; the machine model gains `can_deploy`, `deploy_reason` and `deploy_phase`, and old clients that ignore the new fields still work.

node RPC:

| Method | Parameters | Result / meaning |
| --- | --- | --- |
| `node.update.begin` | `size`, `sha256`, `version` | `id`, the received `offset`; the same target can be retried |
| `node.update.chunk` | `id`, `offset`, `data` (JSON base64) | the acknowledged `offset`; checks order and size |
| `node.update.abort` | `id` | abandons an uncommitted staged transaction |
| `node.update.commit` | `id` | accepts the install after the integrity, self-test and idle checks pass; restarts only after the success response is written |
| `node.update.confirm` | `version` | confirmation after the new node reconnects to the hub; must match the running version and the pending target; can be called repeatedly |

The node determines the install target from the real executable and its symlinks; the hub cannot name an arbitrary path to overwrite through parameters. The new file is staged in the same directory and checked for size, SHA-256, OS / architecture and the output of running `version`; then the old version is kept and the file is replaced atomically. A corrupt, truncated or wrong-version file never replaces the existing program. Integrity checking follows the existing release trust model; no new release signing requirement is added.

The confirm call follows the trust boundary of the paired hub; HTTP deploy permission is still governed by the existing authentication and role filtering, and the new node RPCs do not widen client permissions.

## Restart and rollback

The update manager belongs to the resident process and is shared by all attach connections. Ending one RPC connection only closes that stream; it cannot stand in for restarting the whole resident node. A normal switch first replies to the commit request, then closes connections and listeners, drains the resident service, and starts the new executable with the original arguments and environment.

An independent recovery process from the old version stays next to the executable; it restores the backup if the new version crashes, fails to start or times out before confirmation. On Linux it is a separate systemd transient user service, so it does not exit when the node's cgroup is cleaned up; on Mac it is a recovery process detached from the process group. Replacement is allowed only after the recovery process is ready. Recovery does not depend on the new node starting successfully, nor on the hub being online at the time. The necessary local recovery transaction marker is stored next to the binary, for the same reason as the hub's self-update: recovery must work when the database and remote connections are unavailable; the hub's business state and update tasks are not written to local JSON.

After reconnecting, the hub first confirms the node can read sessions normally, then confirms the target version; confirmation can complete even if the original update task vanished because the hub restarted. Until it is confirmed online, the client keeps the failed or confirming state and does not treat a successful file transfer as a finished update. A failed update of one node does not trigger a rollback of the whole hub.

## Verification

- hub: `TestLinkedDeploy*`, `TestUpdateNodesQueuesOfflineLinkedMachine`, covering chunking, resume position, bad confirm positions, no update while busy, becoming busy before commit, target version confirmation, recovery after a hub restart, and offline targets.
- node / local updater: isolated executable and temporary HOME, covering a complete package, a corrupt package, the wrong version, concurrent requests, resident process restart, confirmation and rollback on failure. Never install or stop the dev machine's real `com.jarvis.node` service.
- Web: `machine-deploy.test.ts` covers old and new hubs, the update capability and each phase; anonymized data is checked in real screenshots in light / dark, list / detail and narrow screens.
- The full server suite uses an isolated test database on real Postgres and follows the test environment isolation rules in `docs/testing.md`.
