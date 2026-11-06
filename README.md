<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.svg">
  <img src="docs/assets/readme/banner-light.svg" alt="Persona: every computer you own, in your pocket" width="100%">
</picture>

English · [简体中文](README.zh-CN.md)

**Watch and steer Claude Code and Codex on all your computers from your phone,<br>with an AI assistant that knows your calendar, messages and mail.**

[![iOS 26+](docs/assets/readme/badge-ios.svg)](docs/ios.md)
[![watchOS 10+](docs/assets/readme/badge-watchos.svg)](docs/ios.md)
[![macOS 26+](docs/assets/readme/badge-macos.svg)](docs/mac.md)
[![visionOS 26+](docs/assets/readme/badge-visionos.svg)](docs/vision.md)
[![Android 11+](docs/assets/readme/badge-android.svg)](docs/android.md)
[![hub: Go 1.27](docs/assets/readme/badge-hub.svg)](docs/server.md)
[![agents: Claude Code · Codex](docs/assets/readme/badge-agents.svg)](docs/runtimes.md)

[Use it](#use-it) · [Build it](#build-it) · [What it does](#what-it-does) · [How it works](#how-it-works) · [Quick start](#quick-start) · [Platforms and connectors](#platforms-and-connectors) · [Docs](#docs)

</div>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/hero-dark.webp">
  <img src="docs/assets/readme/hero-light.webp" alt="Persona on iPhone: the phone in the middle switches from the assistant to Agents, opens a Claude that is asking a question, and answers it with one tap; beside it are the Inbox, a plan to approve, live progress and machines" width="100%">
</picture>

<p align="center"><sub>A demo-mode recording with made-up data: switch from the assistant to Agents, open the Claude that is asking you something, tap an answer, and it carries on.</sub></p>

Claude Code always runs on your own computer (an ordinary session in [herdr](https://herdr.dev)); the phone is just another screen.
Answer its questions and approve its plans from your phone while you are out; back at your desk, open herdr and keep going in the same session.

## Use it

You can set it up without writing code: paste one command on an always-on computer to install the hub, scan a QR code with your phone, then add your computers in the web app and pick a model provider and paste its key.
The step-by-step guide, what each status card means, and what still needs a terminal or is not possible yet are in **[Getting started](docs/getting-started.md)**.

Just want a look? Install the [public TestFlight beta](https://testflight.apple.com/join/vsjfy3hE) on an iPhone and tap **No hub? Try a demo** on the pairing screen to walk through every feature with demo data.

## Build it

To build the hub and the apps from source, see [Quick start](#quick-start) below; conventions and the contribution workflow are in [Contributing](#contributing) and [CLAUDE.md](CLAUDE.md).

## What it does

<table>
  <tr>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-agents.svg" width="44" alt=""><br>
      <b>Agents in your pocket</b><br>
      One list of the Claude Code and Codex sessions in herdr (or another engine) on every computer. Open one to watch it read files, run commands and edit code live.
    </td>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-approve.svg" width="44" alt=""><br>
      <b>Decide from your phone</b><br>
      Answer questions, approve plans and grant tool permissions, also from notifications, Apple Watch and Siri.
    </td>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-assistant.svg" width="44" alt=""><br>
      <b>The Persona assistant</b><br>
      A Claude with an always-on main chat that reads your calendar, Lark, mail, knowledge base and phone data. It runs scheduled tasks, and you can change its avatar.
    </td>
  </tr>
  <tr>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-inbox.svg" width="44" alt=""><br>
      <b>One Inbox</b><br>
      Lark, mail, GitHub, iMessage and Telegram, one row per thing. AI sorts it first, the AI twin drafts replies, and you send with a tap.
    </td>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-island.svg" width="44" alt=""><br>
      <b>Dynamic Island and Lock Screen</b><br>
      A Live Activity sums up the Agents on all your computers: who is working, who is done, and it lights up when something needs you.
    </td>
    <td width="33%" valign="top">
      <img src="docs/assets/readme/icon-hub.svg" width="44" alt=""><br>
      <b>Your own hub</b><br>
      Your data stays on your own machine. Public access uses TLS with key pinning, and credentials are encrypted before they are stored.
    </td>
  </tr>
</table>

Claude can also use the `phone_*` tools to read and write contacts, calendar, reminders, photos, health, location and the clipboard on your iPhone ([phone tools](docs/phone-tools.md)).
Each kind of data can be turned off in Settings, and writes such as creating an event or a reminder are confirmed on the phone first.

## How it works

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/architecture-dark.svg">
  <img src="docs/assets/readme/architecture-light.svg" alt="Architecture: iPhone, Apple Watch, Mac, the web app and Android connect to the hub over HTTPS / WSS; the hub reaches personad node on each computer over SSH or Persona Link, Lark, mail, GitHub and Telegram through connectors, and the phone through APNs" width="100%">
</picture>

- **The hub** (`personad hub`) is the only thing the phone talks to: it relays chats, tracks what needs your answer, runs the assistant, and fetches messages and mail. Its state lives in a local Postgres. When the app is closed, it reaches you through Apple push notifications.
- **Each computer** runs `personad node`: the hub starts it over SSH on demand, or the computer runs the node service (the [Persona Link](docs/link.md) connector) and connects to the hub itself.
  The node drives herdr, reads Claude Code transcripts, and forwards questions and permission requests (hooks) and phone tool calls (MCP) to the hub.
- **The phone** connects to the hub with a device token and syncs in real time. When Claude calls a phone tool, the request travels through the node and the hub to the iPhone, runs there, and the result comes back the same way.

Design and trade-offs: [docs/architecture.md](docs/architecture.md).

## Quick start

This section is for developers building from source and deploying with the repository's scripts. If you don't write code, see [Getting started](docs/getting-started.md).

What you need (the full list and every config field are in [docs/config.md](docs/config.md)):

- **hub**: an always-on Linux machine, or a NAS that can run Docker
- **computers**: Macs or Linux machines with Claude Code or Codex installed; Windows works too ([docs/windows.md](docs/windows.md))
- **build tools**: Go 1.27.1+; for iOS, a Mac with Xcode and [XcodeGen](https://github.com/yonaskolb/XcodeGen); for Android, JDK 21 and the Android SDK

**1. Write the config**: bundle id, developer team, which machine runs the hub, and which computers to connect.

```sh
cp config.example.json ~/.config/persona/config.json
```

**2. Deploy**: build `personad`, install it on every computer (installing herdr where it is missing), and start Postgres and the hub on the hub machine.

```sh
./scripts/deploy/deploy.sh
```

After the hub's first start, add the addresses the computers and phones use to reach it in its `~/.persona/hub.json` ([network setup](docs/network.md)). For a hub on a NAS, use [Docker](docs/docker.md) instead.
Computers can also join without SSH: get a pairing link from **Machines › Add computer** in the web app (a computer added this way must be confirmed with **This is my computer** in the web app), or run `personad hub pair --machine <id> --add` on the hub and install the node service on that computer with the one-line command it prints (Linux: `curl -fsSL <site>/install.sh | sh -s -- --node --pair '<link>'`). On a Mac, use the **Persona Link** menu bar app ([docs/link.md](docs/link.md)).

**3. Install the app and pair**

```sh
./scripts/app/sim.sh                 # iOS Simulator; for a device, open apple/Persona.xcodeproj in Xcode
./scripts/app/android.sh install     # Android: enable USB debugging and connect the phone to your dev machine
~/.persona/bin/personad hub pair     # run on the hub machine, then scan the QR code it prints with the app
```

Once set up, every computer shows as online in **Settings › Machines**, and **Agents** lists the sessions in their engines (herdr by default).

> [!TIP]
> No hub yet? Tap **No hub? Try a demo** on the app's pairing screen to walk through every feature with built-in demo data, without connecting to any server.

> [!NOTE]
> Persona used to be called Jarvis. `jarvisd`, `~/.jarvis`, `~/.config/jarvis/config.json` and `JARVIS_*` still work, and existing pairings, signing and database identities are unchanged.
> Before upgrading, read [naming and compatibility](docs/config.md#命名与兼容) and the [migration checklist](docs/rename.md).

## Platforms and connectors

| Client | Minimum | What it does | How to install |
| --- | --- | --- | --- |
| iPhone | iOS 26 | Everything, plus Dynamic Island, Siri and Shortcuts | [Public TestFlight beta](https://testflight.apple.com/join/vsjfy3hE) (listed as Persona Side), or build it yourself ([docs/release.md](docs/release.md)) |
| Apple Watch | watchOS 10 | See who is waiting on you at a glance, approve or answer right there | Installed with the iPhone app |
| Mac | macOS 26 | A native app sharing the iOS logic layer; the **Persona Link** menu bar app connects the Mac to the hub | Build `PersonaMac` in Xcode; `./scripts/app/mac-link.sh install` ([guide](docs/persona-link-install.md)) |
| Apple Vision Pro | visionOS 26 | A native app sharing the iOS logic layer: tabs in an ornament beside the window, chats in their own windows, and a **Needs you** ornament under the main window for what is waiting on you; no Dynamic Island, camera or picture-in-picture | Build `PersonaVision` in Xcode ([docs/vision.md](docs/vision.md)) |
| Android | Android 11 | Assistant, Agents, Inbox, knowledge base, files, voice and some phone tools; background notifications work without a push service | `./scripts/app/android.sh install` |
| Web | Modern browsers | Served by the hub: Agents (with terminal, files and Git), assistant, Inbox, knowledge base, to-dos, health and machine monitoring; installable as an app (PWA) | Open the hub's address |

Every client's interface is available in English, 简体中文 and 日本語; it follows the system language and can be changed in Settings ([docs/i18n.md](docs/i18n.md)).

| Integration | Support |
| --- | --- |
| Agents | Claude Code and Codex; pi, opencode, DSH and other runtimes through the [runtime SDK](docs/runtimes.md) |
| Engines | herdr (default), Herdr App, Orca and Paseo, one per computer; on computers with botmux, its sessions show up too ([docs/engines.md](docs/engines.md)) |
| Lark | Chatting with the bot is talking to the Persona main chat; the assistant can use the official lark-cli. Messages in the Inbox and knowledge base, `/agent …` in a group or a doc comment to start an Agent, and Lark chats in the app come from a Lark extension ([docs/extensions.md](docs/extensions.md)) |
| Mail | Any IMAP / SMTP account; Gmail and Outlook sign in with your account ([docs/mail.md](docs/mail.md)) |
| GitHub | Inbox, search and reading; comments and reviews go through a confirmation gate |
| iMessage | Reads Messages on a Mac; personad on that Mac needs Full Disk Access |
| Telegram | Your own account, read-only; install it from the extension market |
| WeChat | Personal WeChat through a connector extension (installed from an extension market, published by its own repository); direct messages and group messages that @ you land in the Inbox; sending requires approval on the phone |

How connectors work and how to add one: [docs/connectors.md](docs/connectors.md).

## Data and security

- The phone talks only to your own hub, with no other server in between; only notifications go through Apple's push service.
- The hub token and the database password live only in `~/.persona/hub.json` on the hub machine (mode 0600); mail credentials and model keys are encrypted with AES-GCM before they go into the database.
- Public access uses an explicitly configured TLS port (self-signed certificate plus key pinning), prefers IPv6, and rejects plain-text API requests from the internet by default. The LAN and VPNs keep HTTP ([docs/network.md](docs/network.md)).
- Anything done on your behalf (sending a message or an email, writing phone data) is confirmed on the phone first by default. Persona injects hooks only into the Claude sessions it starts itself and leaves your global Claude Code settings alone.

## Current limitations

- The Apple apps are not on the App Store: install the iPhone app from the [public TestFlight beta](https://testflight.apple.com/join/vsjfy3hE) (no tester limit) or build it yourself; the Vision Pro app can only be built yourself for now.
- The native hub install supports Linux only (systemd plus Postgres in Docker); on other systems run the hub in Docker, and that machine cannot run Agents at the same time.
- Android is not fully on par with iOS yet: health data is read-only, and background notifications need background activity allowed in system settings ([docs/android.md](docs/android.md)).

## Docs

| To… | Read |
| --- | --- |
| Install for the first time, without a terminal | [docs/getting-started.md](docs/getting-started.md) |
| Configure and connect several computers | [docs/config.md](docs/config.md) |
| Run the hub in Docker | [docs/docker.md](docs/docker.md) |
| Connect over the internet: TLS and pairing | [docs/network.md](docs/network.md) |
| Server modes and operations | [docs/server.md](docs/server.md) |
| Connect computers without SSH (Persona Link), Windows computers | [docs/link.md](docs/link.md) · [docs/windows.md](docs/windows.md) |
| Engines, other Agent runtimes | [docs/engines.md](docs/engines.md) · [docs/runtimes.md](docs/runtimes.md) |
| The hub's API and protocol | [docs/protocol.md](docs/protocol.md) |
| Assistant, Inbox, knowledge base | [docs/assistant.md](docs/assistant.md) · [docs/inbox.md](docs/inbox.md) · [docs/knowledge.md](docs/knowledge.md) |
| The apps | [iOS](docs/ios.md) · [Mac](docs/mac.md) · [Vision Pro](docs/vision.md) · [Android](docs/android.md) · [Web](docs/web.md) |
| Dynamic Island, Siri | [docs/live-activity.md](docs/live-activity.md) · [docs/siri.md](docs/siri.md) |

The index of all docs is in the "Docs index" section of [CLAUDE.md](CLAUDE.md).

## Contributing

- Conventions are in [CLAUDE.md](CLAUDE.md); the workflow is issue → claim → PR ([docs/collaboration.md](docs/collaboration.md)).
- Test commands and the screenshot checklist are in [docs/testing.md](docs/testing.md).
  A running DEBUG app can be inspected, navigated and screenshotted with `scripts/app/personactl.py` ([Runtime CLI](docs/runtime-cli.md)).
- The pictures on this page come from `scripts/assets/make_readme_assets.py`, in English and Chinese: screenshots and recordings are taken in demo mode, and the badges read their versions from the build config.
- Keep README.md and [README.zh-CN.md](README.zh-CN.md) in sync: change both in the same PR.

<sub>Third-party licenses are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).</sub>
