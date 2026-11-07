# Model profiles

A model profile is an editable, named configuration. It holds default bindings for the five model categories, selected model fields of managed launchers, and named execution tiers. It does not hold credentials, endpoints, permissions, or the main chat's runtime.

## Switch in conversation

Ask Persona to “switch to economy” or “switch to performance”. Persona uses:

- `model_profiles_list`: current profile, configurations, drift, rollback preview, and recent history.
- `model_profile_get` with `id`: inspect one profile.
- `model_profile_switch` with `id`: apply it atomically.
- `model_profile_rollback`: undo the last switch; another rollback redoes it.

The live `persona-context` snapshot identifies the current profile and its tiers on every turn, without waiting for the ordinary snapshot cache. Model calls use the changed configuration on their next invocation. Existing Agent sessions and running turns are not interrupted.

## What the built-in profiles use

On first access, `performance` and `economy` are created as independent copies of the **current configuration**. This never switches models automatically. They are templates, not a hard-coded routing policy: configure them for your providers before relying on different behavior. Model availability, billing, and CLI compatibility depend on the configured provider.

A suggested policy:

| Purpose or tier | Performance | Economy |
| --- | --- | --- |
| Main chat and question delegation | Strong available model | Available supervisor model; no unavailable-model dependency |
| New Agents / standard | Strong available coding model | Standard coding model, using its native runtime where available |
| Knowledge processing and screening / bulk | Strong model where useful | Fast economical model |
| strong | Design and review model | Available supervisor model |
| fast / bulk | Fast or strong model as appropriate | Fast model for mechanical work |
| longread | Long-context model | Economical long-context model |

Every purpose binding has `provider`, `model`, and optional `effort`. Empty provider inherits the existing default source; empty model uses its default. Profiles cover `assistant`, `agent.new`, `kb`, `inbox.triage`, and `agent.delegate`; independent extension overrides such as `lark.agent` are not changed.

## Edit and add profiles

The web app adds **Settings › Models › Model profiles**: inspect a profile, apply it, or roll back. Use the hub API (authenticated as usual) to edit or add profiles. Native settings controls require an app release and are deferred in v1.

| Method | Path | Operation |
| --- | --- | --- |
| GET | `/api/v1/model-profiles` | Catalog, revision, active profile, rollback preview, history |
| PUT | `/api/v1/model-profiles/{id}` | Create or replace a profile; include catalog `revision` |
| DELETE | `/api/v1/model-profiles/{id}?revision=N` | Delete an inactive profile |
| POST | `/api/v1/model-profiles/{id}/apply` | Apply the profile |
| POST | `/api/v1/model-profiles/rollback` | Restore the saved before-image |

A PUT body looks like this (use your actual provider and launcher names):

```json
{
  "revision": 1,
  "name": "Economy",
  "description": "Available models for everyday work",
  "purposes": {
    "assistant": {"provider": "launcher.supervisor", "model": "available-supervisor"},
    "agent.new": {"provider": "launcher.coding", "model": "standard-coding"}
  },
  "launchers": {
    "supervisor": {"model": "available-supervisor", "small_model": "fast-model", "subagent_model": "standard-coding"}
  },
  "tiers": {
    "strong": {"launcher": "supervisor", "model": "available-supervisor"},
    "standard": {"launcher": "coding", "model": "standard-coding"},
    "bulk": {"launcher": "supervisor", "model": "fast-model"}
  }
}
```

PUT replaces the profile; omitted purposes and launcher fields are not touched on application. A launcher field containing `""` clears that field (except the main model); omitted or null fields leave it unchanged. Saving a profile does **not** apply purpose or launcher settings. Tiers are looked up from the saved active profile when a new Agent actually starts, so editing its tiers affects subsequent dispatches directly. Use `capture: true` with `name` and `revision` to capture today's bindings/launcher settings; existing tiers are retained when recapturing a profile.

`agent_start` and individual `task_plan` tasks accept `tier`, `model`, and `effort`. Explicit choices win over the tier. A different explicit launcher does not inherit the tier's model or effort; a different explicit model does not inherit the tier's effort. A delayed plan resolves its tier when dispatched, using the profile active then. Unknown tiers fail rather than falling back silently.

## Verify and finish a switch

1. Read `GET /api/v1/model-config` and `GET /api/v1/model-profiles`; keep the purpose revisions and the rollback preview.
2. Edit both templates with the current catalog revision, then apply the desired profile. Read the catalog again and check the active profile has no unexpected drift.
3. Inspect each affected managed launcher with `GET /api/v1/launchers/{name}`. Check `model`, `small_model`, `subagent_model`, and every strong/fast alias (`opus_model`, `sonnet_model`, `fable_model`, `haiku_model`). Alias names are runtime roles, not a requirement to use the model family they name.
4. Run `POST /api/v1/launchers/{name}/check?machine=<machine id>` on a chosen reachable computer. This is a real headless request **without a purpose model override**, so it tests the launcher's actual default. Check the returned model, success, and latency. Test the selected small/subagent models separately; a successful supervisor call does not prove those routes work.
5. Use `model_profile_rollback`, read back the restored fields, then reapply the profile you want to leave active. Confirm credentials and unrelated settings remained unchanged, and keep the before-image until acceptance is complete.

A launcher's custom environment or arguments can override its model fields. Review them privately rather than clearing credentials or unrelated settings. Connected nodes receive managed-launcher updates; offline nodes get them on reconnection. Do not claim fleet-wide verification from one computer's health result. Existing running sessions retain their own execution choices.

## Atomicity, rollback, and audit

Switches commit the purpose records, managed launcher model fields, active profile, before-image, and audit event in one Postgres transaction. A validation or storage failure leaves the switch unapplied. Rollback restores only the purposes and launcher model fields changed by the switch, preserving the current runtime, credentials, endpoints, and unrelated launcher settings. If a required launcher has been deleted, restore it before rollback. Manual edits to those touched model fields after a switch are replaced by rollback; inspect the preview first.

The history retains the last 50 switch/edit/delete/rollback events and exposes the most recent 20, including actor kind, device name where available, time, source/target profile, and changed fields. Reapplying the already-active unchanged profile retains the previous rollback. In-memory serialization protects model-profile operations within the manager; this is not a distributed multi-manager configuration coordinator.
