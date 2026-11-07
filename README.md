<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.svg">
  <img src="docs/assets/readme/banner-light.svg" alt="Persona — your superagent. One place. Every task." width="100%">
</picture>

English · [简体中文](README.zh-CN.md)

**Your superagent. One place for every task.**

Persona brings your agents, devices and personal context into one conversation—so you can ask, delegate and follow through without switching apps.

[![Claude Code](docs/assets/readme/badge-claude-code.svg)](docs/claude-integration.md) [![Codex](docs/assets/readme/badge-codex.svg)](docs/codex.md) [![herdr](docs/assets/readme/badge-herdr.svg)](https://herdr.dev) [![Orca](docs/assets/readme/badge-orca.svg)](https://github.com/stablyai/orca)

[![iPhone · Watch · Mac · Vision Pro · Android · Web](docs/assets/readme/badge-clients.svg)](#docs)

[Try it](#try-it) · [What it does](#what-it-does) · [How it works](#how-it-works) · [Docs](#docs)

</div>

## What it does

<table>
  <tr>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-assistant.svg" width="32" alt=""><br><b>One superagent</b><br>
      Bring any task to one chat. Your assistant draws on your context and delegates work to agents.<br><a href="docs/assistant.md">Assistant →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-handoff.svg" width="32" alt=""><br><b>Phone ↔ computer</b><br>
      Watch, answer and approve on your phone. At your desk, pick up the very same session—not a copy.<br><a href="docs/engines.md">Engines →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-extensions.svg" width="32" alt=""><br><b>Built to extend</b><br>
      Add tools, runtimes, connectors, channels and native UI through a versioned extension system with SDKs and a marketplace.<br><a href="docs/extensions.md">Extensions →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-island.svg" width="32" alt=""><br><b>Beyond the app</b><br>
      Track work on Dynamic Island, start tasks with Siri or Shortcuts, and answer from Apple Watch.<br><a href="docs/live-activity.md">Live Activities</a> · <a href="docs/siri.md">Siri</a> · <a href="docs/intents.md">Shortcuts</a></td>
  </tr>
  <tr>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-inbox.svg" width="32" alt=""><br><b>Knowledge &amp; Inbox</b><br>
      A shared knowledge base gives agents context. One AI-triaged Inbox turns incoming messages and mail into things to act on.<br><a href="docs/knowledge.md">Knowledge</a> · <a href="docs/inbox.md">Inbox</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-phone-data.svg" width="32" alt=""><br><b>Your phone, useful to AI</b><br>
      Connect calendar, reminders, health, location, photos and more. You choose access; calendar and reminder writes ask first.<br><a href="docs/phone-tools.md">Phone tools →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-connectors.svg" width="32" alt=""><br><b>Connect your world</b><br>
      Connect Lark, mail, GitHub, iMessage and Telegram; reach Persona through messaging channels such as Lark and WeChat.<br><a href="docs/connectors.md">Connectors</a> · <a href="docs/channels.md">Channels</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-clients.svg" width="32" alt=""><br><b>Every screen</b><br>
      iPhone, Apple Watch, Mac, Vision Pro, Android and web. One hub keeps your conversations connected across devices.<br><a href="docs/ios.md">Apple</a> · <a href="docs/mac.md">Mac</a> · <a href="docs/vision.md">Vision</a> · <a href="docs/android.md">Android</a> · <a href="docs/web.md">Web</a></td>
  </tr>
</table>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/hero-dark.webp">
  <img src="docs/assets/readme/hero-light.webp" alt="Persona on iPhone: the assistant, live agent progress, questions, plan approvals, Inbox and computers" width="100%">
</picture>
<p align="center"><sub>Demo data. An agent asks, you answer, it keeps going.</sub></p>

## Try it

**[Join the TestFlight beta](https://testflight.apple.com/join/vsjfy3hE)** · On the pairing screen, tap **No hub? Try a demo** to explore without a server.

Ready to use your own data? **[Getting started](docs/getting-started.md)** walks you through installing a hub, pairing your phone and connecting your computers.

## How it works

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/architecture-dark.svg">
  <img src="docs/assets/readme/architecture-light.svg" alt="Your devices connect to your hub; the hub connects agents on your computers, data connectors and phone tools" width="100%">
</picture>

Your hub connects the pieces; agents run on your computers, phone tools run on your phone. **[Architecture](docs/architecture.md)** · **[Network and security](docs/network.md)**

## Docs

| Start here | Go deeper |
| --- | --- |
| **Use Persona** | [Getting started](docs/getting-started.md) · [Configuration](docs/config.md) · [Docker](docs/docker.md) |
| **Connect computers** | [Persona Link](docs/link.md) · [Windows](docs/windows.md) · [Engines](docs/engines.md) · [Workspace board](docs/workspace-board.md) |
| **Build on Persona** | [Extensions](docs/extensions.md) · [Connectors](docs/connectors.md) · [Runtime SDK](docs/runtimes.md) · [API](docs/protocol.md) |
| **Develop with an Agent** | [Conventions and full docs index](CLAUDE.md) · [Issue → claim → PR](docs/collaboration.md) · [Testing](docs/testing.md) |
| **Operate or upgrade** | [Server](docs/server.md) · [Network](docs/network.md) · [Migration](docs/rename.md) |

<sub>Client support and limitations are documented in the platform guides above. [Third-party notices](THIRD_PARTY_NOTICES.md).</sub>
