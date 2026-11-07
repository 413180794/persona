# Auth relay

Agents often stop at "open this link to authorize" or "scan this code to sign in", waiting for the user, and the link expires within minutes. With the auth relay, when the triage finds that a turn stopped at an authorization link, the manager first hands the link to an extension that completes that kind of link with the account it is signed in with. If it is done, the Agent is told to go on and the user is not bothered; if not, the "Needs you" card goes up as before, with one line saying why Persona did not do it (#5644).

This repository has only the generic part: the extension point, where a link may come from, and what the triage does with the answer. Which links an extension completes and how is the extension's. For example, an extension that keeps the user's signed-in Lark web session can complete a Lark OAuth device-code page (what `lark-cli auth login --no-wait` prints) that asks only for permissions the app was granted before.

Off unless the user installs such an extension, grants it the `auth_relay` permission, and turns its contribution on (contributions start off, see "The extension point").

## Which links can be completed

| Kind | Can it be completed? | Why |
| --- | --- | --- |
| Device-code authorization (OAuth 2.0 device authorization grant, RFC 8628: a verification page with a user code) asking for permissions granted before | Yes | Whoever confirms the page, the token goes only to the program that polls with the device code: the one on the Agent's machine that printed the link. The page can be confirmed anywhere the user is signed in |
| The same, asking for new permissions | No (on purpose) | Widening what an app may do is the user's decision, and organizations often restrict it; extensions answer `new_permissions` |
| Web OAuth with a redirect (authorization code sent to `redirect_uri`) | No | The code goes to the browser that confirmed the page, on the extension's side, not to the Agent's machine; a `localhost` callback there is never reached |
| QR-code logins confirmed in a phone app | No | Only the phone app can confirm them |
| Logins of other systems (SSO) | Only with that system's session | An extension can complete only what its own signed-in session covers |
| Headless login with credentials already on the Agent's machine | Not the relay's | The Agent does it on its own machine; the relay never sends credentials anywhere |

## Flow

1. An Agent's turn stops; the triage ([agent-work.md](../assistant/agent-work.md)) decides it is `blocked · user` and gives an authorization link (`link`).
2. Where a card would go up (cards switched on, the session not muted), the manager first asks the extensions whether one names the link (`assistant/agents_auth_relay.go`, `extmgr/auth_relay.go`): `https`, no user info or port, one of an enabled `auth_relay` contribution's hosts and paths. None does: the card exactly as before.
3. Where the link came from; if any check fails, the card goes up (with its reason):
   - the machine is one of the user's own computers that this manager connects, not one a member hub brought (`not_own_machine`, no extra line on the card);
   - the session is known there (`unknown_session`, no extra line);
   - the link appears **as it is** in the output of a command of that session's last turn (`not_in_session`): the result of a tool that runs a command on the Agent's machine (Claude Code's `Bash` / `BashOutput`; Codex's commands are `Bash` in its transcript too), not an error result. A link the Agent only wrote, or a page it fetched, does not count. Long outputs are read in full, a JSON printer's `&` and `\/` are undone, and nothing of a longer link may follow it (a space, quote, bracket, the end, or a sentence's punctuation before one of those).
4. The extension's program is called: `auth_relay.complete {contribution, url}`, at most 60 seconds. It answers `{status, app?, reason?}`. An error is `failed` / `unavailable`; `unknown_method` (an older version of it) and `manual` / `not_handled` go on to the next extension that names the link; a status the schema does not have is `failed`; `app` is held to one line of 80 characters.
5. `done`: if the session is still stopped in the same turn, the manager sends it `prompts/agents/auth_relayed.md` (the program that printed the link can get its token now, then go on; don't keep making new links), and the work line says "Authorized “X” with your account; it goes on". No card. If the message can't be sent, the card goes up saying it was authorized and to tap "Authorized, go on". Anything else: the card, with the line for its reason.

All of it runs inside the triage. Authorization links are handed to the user at the end of a turn anyway (`lark-cli`'s own instructions tell Agents to use `--no-wait` and end the turn with the link). An Agent that blocks inside a turn waiting for authorization is not handled yet (see "Later").

## The extension point

```json
{
  "contributes": {
    "auth_relay": [
      {"id": "device", "name": "Device-code sign-in", "hosts": ["accounts.example.com"], "paths": ["/oauth/device/verify"]}
    ]
  },
  "permissions": {"auth_relay": true}
}
```

- `hosts`: the pages' exact lowercase host names (no `*.`), 1–16. `paths`: exact paths, 1–16; a trailing `/` is ignored. What is in the query is the extension's to judge.
- Needs the permission `auth_relay` (shown before install; an upgrade that adds it asks again) and a program that speaks the protocol.
- **Starts off**: an `auth_relay` contribution starts switched off at install, and when an upgrade adds it, whatever `off_by_default` says (`Manifest.StartsOff`). The user turns it on in the extension's details or with `personad extension contribution <id> auth_relay/<cid> on`.
- `auth_relay.complete`: params `AuthRelayCompleteParams` (`contribution`, `url`), result `AuthRelayResult` (`schema/extension/protocol.schema.json`):

| `status` | Meaning |
| --- | --- |
| `done` | Authorized: the Agent can go on |
| `manual` | Not one it completes: the user's (`not_handled` means "not mine at all", and the next extension is asked) |
| `failed` | One it completes, but it did not go through |

| `reason` | Line on the card |
| --- | --- |
| `new_permissions` | It asks for new permissions: Persona doesn't grant those for you |
| `app_not_allowed` | This app isn't one the extension authorizes by itself (its own settings, e.g. a list of apps) |
| `not_signed_in` | The extension's account wasn't signed in for this page |
| `expired` | The link has expired: ask it for a new one |
| `unknown_page`, `unavailable`, `not_confirmed` | (with `failed`) Persona tried to authorize it for you and couldn't |

What an extension must do:

- Complete only pages whose grant goes to the program that printed the link (device codes), and never grant more than was granted before without the user; offer the user a way to limit it to their own apps.
- Close whatever it opened, whatever happens; answer within 60 seconds.
- Never log or keep the link, a token, or a page's text; the manager logs only the extension, the contribution, the status and the reason.

## Security

Baseline (implemented):

- Off by default: the user installs an extension, agrees to `auth_relay`, and turns its contribution on.
- Only links on the hosts and paths an extension declared, only from the user's own computers, sessions known there, and their own commands' output in the last turn.
- The link is never logged; events (`auth_relay` in the work events) carry only the status and reason. The extension gets only the link and the contribution id.

Recorded, not blocking the feature:

- **A prompt-injected Agent**: an Agent tricked into running an attacker's device-code client prints the attacker's link with its own command. If the user granted that app before, the relay renews a token in the user's name for it; "its own command printed it" does not stop this. Limiting the extension to the user's own apps narrows it.
- **App names can be copied**: a list of apps by the name the page shows can be matched by anyone who names an app the same in the same organization; consent pages often show no app id.
- **The extension's session is broad**: it acts as the user in that account; what it opens is limited by the extension, not by this repository.
- **The link comes from the triage model**: only a link that appears verbatim in a command's output counts, so a model that changed one character leaves it to the user.

## Later

- Agents that block inside a turn waiting for authorization: recognize the link from `session.items` while the session is still working.
- Web OAuth: only if the callback can be handed back to the Agent's machine (the node requests the callback URL), with an allowlist of apps and redirects.
- Showing the QR code on the card: these links expire within minutes; the card shows only the link and how long it is valid.
