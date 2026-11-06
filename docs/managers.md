# Managers

A manager is the assistant's process, `personad assistant` (`docs/assistant.md`("Process")). One hub can have several managers, and **they are all equal**: each one is a whole assistant with its own main conversation, conversations, scheduled tasks, inbox, knowledge base, model settings, memory, computers and sources. There is no chief manager: nothing is escalated, handed down, or seen "all at once".

- The assistant the hub starts itself (id `assistant`) is the first manager and, to begin with, the **default manager**: entry points that belong to no manager (Siri, the **Ask Persona** shortcut, the assistant page as the app opens) go to the default manager, which can be changed in settings.
- Everything belongs to one manager (computers, sources, inbox events, sessions, model settings, the knowledge base), and the hub forwards by ownership; list endpoints are merged by the hub, each item carrying `manager` (the manager's id).
- Each manager keeps its own data: a database of its own (one more database in the same Postgres instance, or one it brings); the default manager keeps using the hub's database, with nothing migrated.
- Where a manager runs is the user's choice: another service on the hub machine, a service on another computer, or a container. Bots, channels, plugins and sources are set up in each manager's own settings; none of it is global.

## Steps

1. **Roster and identity** (below): the roster, each manager's own token, install codes, `/api/v1/managers`. Behavior unchanged.
2. **Ownership and forwarding by owner** (see "Ownership" below): computers, sessions, cards and list items carry `manager`, and the hub forwards by it and merges lists; for now everything belongs to the default manager, so behavior is unchanged.
3. A second manager runs: its own database and directory, connected to the hub, taking over the computers and sources given to it.
4. Clients: **Settings › Managers**, switching managers on the assistant page, grouping, per-manager push switches (all four platforms).

## Roster

The roster is the bus's identity table `bus_identities` (`docs/protocol.md`, "Bus"): every row with role `manager` is a manager. Migration 0419 adds where its last handshake said it runs (`host`, `os`, `arch`, `deploy`, `version`) and `last_seen` (recorded on handshake and on disconnect), so the app can show an offline manager's place and "Last synced…".

- The hub gives the assistant it starts a new token on every start, but **the name stays**: a name the user gave it is kept.
- Manager ids: `m-` plus 6 hex digits for one made in the app; `assistant` for the hub's own.

## Install codes

**New manager** in the app (`POST /api/v1/managers {name}`) returns a one-time install code `mi_…`, **valid for 10 minutes and usable once**; the hub keeps only its sha256 (table `manager_install_codes`). Until the code is traded, the manager shows in the roster as installing (`installing` is when the code expires).

The new manager trades the code for its bus token: `POST /api/v1/bus/join {"code"}` → `{id, name, token, hub_id}`. This endpoint needs no credential (like the bus, it sits outside the apps' auth); failed trades count against the source and go `429 rate_limited` past the limit, as a bad token does. A manager that already has a token and trades another code (`POST /api/v1/managers/{id}/code`, to install it somewhere else) gets a new token: the old one stops working at once and a connection using it is closed with 4002.

The install commands handed out (`curl … /i/<code> | sh`, `docker run …`) and `personad manager join` are described under "A second manager" below.

## Revoking

`DELETE /api/v1/managers/{id}` deletes its identity and codes: the token stops working at once and a connection using it is closed with 4002; for a manager still being installed it calls off the code. The default manager can't be deleted (make another one the default first: `409 default_manager`), nor can the assistant the hub starts (`409 builtin_manager`). Revoking the sources and computer credentials attached to it comes in step 3.

## Ownership

Who owns a thing is decided in one place: `server/internal/hub/ownership.go`. When step 3 moves a computer or a source to another manager, this ownership is what changes, and forwarding and lists follow.

| Thing | Owner | How the hub knows |
| --- | --- | --- |
| Computer | The manager that reports it (`devices.state`) | Until one does: the one that connects hub.json's computers (`assistant` today) |
| Session | A manager's own conversations: the manager that reports them (snapshot); everything else: the computer's | |
| Pending card | The manager that reports it (`fleet.interaction(s)`) | |
| Inbox, todos, knowledge base, model settings, extensions and sources, the assistant page… | The manager the request names, else the default one | Header `X-Persona-Manager` or `?manager=` (`docs/protocol.md`, "Per-manager routing") |

- What a manager reports replaces only its own share: a manager reporting `devices.state` touches only its computers, a full list of pending cards replaces only its cards, and other managers' stay. The app gets computers ordered by manager: the one connecting hub.json's computers first, the rest by id.
- When a hub feature needs the assistant: things tied to a computer or a session (message parameters, renaming, starting an Agent, question cards, read state) go to its manager; things that aren't (Siri, shortcuts, the knowledge base, an extension's hub_call) go to the default manager; notices that concern everyone (the app can fill in forms now, mutes changed, a source has something new, events for extensions) go to every connected manager. An extension's token is checked with each manager in turn (the default first); whichever recognizes it owns it.
- The hub caches the default manager for 2 seconds (every forwarded request needs it) and reads it again when the roster changes (`managersChanged`).
- How lists are merged, the `manager` field and what offline looks like are in `docs/protocol.md`, "Per-manager routing". An offline manager's things get no extra field: the app uses its `online` and `last_seen` in the roster to show "Last synced…".

Not done yet, left for step 3:

- The hub keeps one assistant snapshot (`manager.snapshot`), one extensions report (`extensions.report`) and one tool set (`assistant.tools`); if a second manager reported them it would overwrite the first, so step 3 keeps one per manager.
- hub.json's computers, computers whose Persona Link dials in, monitoring, the usage sources' admin page and jarvis_settings still go to `assistant` (`devicesOwner`); a manager that connects doesn't get `fleet.clients` right after its handshake (only once the app's capabilities change).

## A second manager (first half of step 3)

A manager made in the app installs and runs on its own: its own id, directory and database; it connects to the hub, shows in the roster and is online. Its assistant page, conversations and the other assistant endpoints reach it through `X-Persona-Manager: <id>` (or `?manager=<id>`, see "Ownership" above). It may report everything the hub already keeps per manager: computers (`devices.*`), pending cards (`fleet.interaction(s)`, `fleet.interaction_resolved`) and messages for the apps (`ws.broadcast`); it may also call `device.call` and `app.call`. What the hub still keeps only one of (the assistant snapshot `manager.snapshot`, the extensions report `extensions.report`, `fleet.live`, the tools `assistant.tools`) is taken only from the assistant the hub starts; other managers' calls get `not_routed` and their events are dropped (`busRouted` / `busOpen` in `hub/bus.go`), so they can't overwrite the default manager's. That goes once those are kept per manager. Moving a computer or a source to it is the second half of step 3.

### Directory and manager.json

- The default directory is `managers/<id>` in this computer's data directory (`~/.persona/managers/<id>`); `--dir` picks another. The directory is 0700 and its `manager.json` 0600:
  `{id, name, hub, hub_id, token, fingerprint?, database_url?}`. `hub` is the address it reaches the hub at; `fingerprint` is the pin of the hub's own TLS certificate (only when it was installed through the hub's TLS port).
- Its knowledge base (`data/`), `secret.key` and extensions are all under this directory; it never writes to the hub's. While it runs, `PERSONA_HOME` points at it, so the programs it starts use it too.
- Moving to another computer: copy the whole directory, and the same token still connects; or get a new code for it in the app and install it again there (the old token stops working at once).

### Database

- By default: one more database `persona_mgr_<id>` (the id's `-` as `_`) in the hub's Postgres, from the `database_url` in the hub machine's `hub.json` (`PERSONA_DATABASE_URL` wins) with the database name swapped. On its first run it does `CREATE DATABASE` through the same server's `postgres` database, then runs every migration. The database role must be allowed to create databases (the ones `hub setup` and the Docker setup make are); when it isn't, the manager says so, and you create the database yourself or give `--database-url`.
- **The image brings no Postgres**: in a container (and on any other computer that doesn't run the hub) you must give it a Postgres with `--database-url`.
- Off the hub machine (another computer, a container) there is no hub `hub.json`, and **the hub never hands out its database password** (the join answer has nothing about databases), so `--database-url postgres://USER:PASSWORD@HOST:5432/DBNAME` gives it a database: a Postgres of its own, or a separate database and role on the same Postgres. A database that isn't there yet is created the same way. It is written to `manager.json` and used from then on. With none given and no hub database to find, `manager join` and `manager run --join` print one line saying what to add (`--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'`) and exit 2 (a service's or a container's restart won't loop on it), and the install code is not used.
- It can't be the hub's own database (a `database_url` pointing at it is refused); the default manager (the assistant the hub starts) keeps using the hub's database, with nothing migrated.

### Install commands

The answers of `POST /api/v1/managers` and `POST /api/v1/managers/{id}/code` carry `commands`, built from the hub address the app used for that request (with the pin when it came through the hub's TLS port):

| Key | Command | For |
| --- | --- | --- |
| `service` | `curl -fsSL <hub>/i/<code> \| sh` (through the hub's TLS port it adds `-k --pinnedpubkey 'sha256//<pin>'`) | Any Linux or Mac, the hub machine included |
| `join` | `personad manager join <hub> <code> --install` | A computer that has personad already |
| `docker` | `docker run -d --name persona-manager-<id> --restart unless-stopped -v persona-manager-<id>:/home/jarvis/.jarvis <image> manager run --dir /home/jarvis/.jarvis/managers/<id> --join <hub> <code> --database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'` | A container; only when the hub knows its image (`JARVIS_IMAGE` or the one it was built as); replace `USER:PASSWORD@HOST:5432/DBNAME` with the Postgres you give it |

`GET /i/<code>` answers a POSIX sh script. It needs no credential (the code is the credential), and **fetching the script doesn't use up the code**: only the join does. Unknown codes are 404 and count against the source, as at the join; public plaintext requests get 403 `tls_required`. The script looks for this computer's personad (`~/.persona/bin/jarvisd`, personad or jarvisd on the PATH); if there is none, it downloads it from the hub's release address the way `install.sh` does and checks it against `SHA256SUMS`, then runs `personad manager join <hub> <code> --install`. Arguments after `sh -s --` go to the join, e.g. `curl … | sh -s -- --database-url '…'`.

### Commands

- `personad manager join <hub> <code> [--dir D] [--database-url U] [--fingerprint F] [--install]`: first checks that there will be a database (no hub database and no `--database-url`: it stops without using the code), then trades the code and writes `manager.json`. A directory that is already another manager's is not overwritten: the new manager goes to its own default directory.
- `personad manager run [--dir D] [--join <hub> <code>]`: runs it. `--dir` can be left out when this computer has only one manager. `--join`: trades the code first when the directory has no `manager.json` yet (a container runs the same command on every start, and from the second start on the join is skipped). Configuration mistakes (no `manager.json`, no database to use) exit 2, which the service doesn't restart.
- `personad manager install [--dir D]`: on Linux writes the systemd user unit `jarvis-manager-<id>.service` (`Restart=always`, `RestartPreventExitStatus=2`, with a reminder about linger); on a Mac the launchd agent `com.jarvis.manager.<id>` (its log is `manager.log` in its directory). The binary it runs is the hub machine's `~/.persona/bin/jarvisd` when there is one (so it upgrades with the hub), else the current one. On other systems, or without a systemd user instance, it says how to keep it running yourself.
- `personad manager uninstall [--dir D]`: stops and removes the service; the directory and the database stay (deleting it in the app is what makes the hub stop accepting its token).
- `personad manager status [--dir D]`: this computer's managers, with directory, hub, database and service state.

### Containers

Same image as the hub. When the entrypoint sees `manager` it writes no `hub.json` and shows none of the hub's reminders (`deploy/docker/entrypoint.sh`). The data volume is `/home/jarvis/.jarvis`, with the manager in its `managers/<id>`; don't share the hub's data volume (it holds the hub's keys). In the hub's compose file, a manager can use the same `JARVIS_DATABASE_URL` as the hub, and without `--database-url` it creates its own database in that Postgres. The database password must then be in `.env` as `JARVIS_DB_PASSWORD` (the password Postgres generates on its first start is in the hub's data volume, which the manager's container can't read):

```yaml
  manager-work:
    image: ${JARVIS_IMAGE}
    restart: unless-stopped
    depends_on: { postgres: { condition: service_healthy } }
    environment:
      JARVIS_DATABASE_URL: postgres://jarvis:${JARVIS_DB_PASSWORD:?}@postgres:5432/jarvis?sslmode=disable
    volumes: [ "./manager-work:/home/jarvis/.jarvis" ]
    command: ["manager", "run", "--dir", "/home/jarvis/.jarvis/managers/<id>", "--join", "http://hub:18660", "<install code>"]
```

### After it is revoked

When the hub deletes it (or it was installed again somewhere else), the connection is closed with 4002 and the next dial gets 401. It says once that the hub no longer accepts its token and **stops dialing**; the process stays up and checks `manager.json` every 10 seconds. Once you get a new code in the app and `personad manager join … --dir <the same directory>` writes a new token, it restarts itself in place (exec), with nothing to change in the service. The assistant the hub starts is not affected (it doesn't take this path and keeps retrying every minute).
